#!/usr/bin/env python3
"""Assert that a testkit run is trustworthy, not merely green.

Each case declares what "pass" means through expect_kind:
  reply  - stdout equals the deterministic fixture reply, byte for byte
  probe  - the identity the harness actually put on the wire matches the
           session_identity declared for it in matrix.json
  reject - the harness failed and the capture shows a 4xx with expect_error;
           implemented but unused until a rejecting component is in the chain

Independently of the cases, three things must also hold:
  * the L7 tap recorded structured, redacted, trace-correlated NDJSON
  * the tap and the supplier-side capture saw the same trace
  * the L4 sidecar recorded real bytes that tcpdump can parse
"""
import hashlib
import json
import pathlib
import sys

TAP_REQUIRED = {"hop", "phase", "time_ns", "trace_id", "headers", "body_sha256"}
SECRET_SENTINELS = {"authorization", "x-api-key", "api-key", "cookie", "proxy-authorization"}
# A header counts as an identity carrier when its name says so. This is a
# deliberate, narrow heuristic: it must not silently accept "some header that
# happens to change". matrix.json declares what is expected and the probe
# verifies it on every run.
SESSION_HEADER_HINTS = ("session", "affinity")
BODY_IDENTITY_KEYS = ("prompt_cache_key", "session_id", "user", "metadata", "conversation")


class Report:
    def __init__(self):
        self.failures = []
        self.checks = 0
        self.groups = {}
        self.current = "general"

    def group(self, name):
        self.current = name
        self.groups.setdefault(name, [0, 0])
        print(f"\n== {name}")

    def check(self, condition, message, detail=""):
        self.checks += 1
        checks, failures = self.groups.setdefault(self.current, [0, 0])
        self.groups[self.current] = [checks + 1, failures + (0 if condition else 1)]
        if condition:
            print(f"ok   {message}")
        else:
            suffix = f" ({detail})" if detail else ""
            print(f"FAIL {message}{suffix}", file=sys.stderr)
            self.failures.append(f"[{self.current}] {message}")

    def summary(self):
        print("\n== summary")
        for name, (checks, failures) in self.groups.items():
            mark = "ok  " if failures == 0 else "FAIL"
            print(f"{mark} {name:<12} {checks - failures}/{checks}")
        print(f"     {'total':<12} {self.checks - len(self.failures)}/{self.checks}")


def read_jsonl(path):
    entries = []
    if not path.exists():
        return entries
    for line in path.read_text(encoding="utf-8").splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            entries.append(json.loads(line))
        except ValueError:
            entries.append({"__parse_error__": line[:200]})
    return entries


def header(entry, name):
    for key, value in (entry.get("headers") or {}).items():
        if key.lower() == name:
            return value
    return None


def case_runs(run):
    """case -> {exit, from, to}; written by smoke.sh around each harness run."""
    return {entry["case"]: entry for entry in read_jsonl(run / "case-runs.jsonl") if "case" in entry}


def window(entries, record):
    if not record:
        return []
    return entries[record["from"] - 1:record["to"]]


def extract_identity(entries):
    headers = {}
    body_keys = {}
    values = set()
    requests = 0
    for entry in entries:
        if entry.get("phase") != "request":
            continue
        requests += 1
        for key, value in (entry.get("headers") or {}).items():
            if any(hint in key.lower() for hint in SESSION_HEADER_HINTS):
                headers.setdefault(key.lower(), set()).add(value)
                values.add(value)
        try:
            body = json.loads(entry.get("body") or "{}")
        except ValueError:
            body = {}
        if isinstance(body, dict):
            for key in BODY_IDENTITY_KEYS:
                if body.get(key) is not None:
                    body_keys[key] = body[key]

    if headers:
        supply, fields = "header", sorted(headers)
        distinct = len(values)
    elif body_keys:
        supply, fields = "declared_body", sorted(body_keys)
        distinct = len({json.dumps(value, sort_keys=True) for value in body_keys.values()})
    else:
        supply, fields, distinct = "none", [], 0
    return {
        "requests": requests,
        "supply": supply,
        "fields": fields,
        "distinct_values": distinct,
        # Raw session ids are not worth persisting; the fingerprint proves
        # stability, which is the property that matters here.
        "value_sha256_16": sorted(hashlib.sha256(v.encode()).hexdigest()[:16] for v in values),
    }


def check_reply(report, case, replies):
    path = replies / f"{case['name']}.txt"
    actual = path.read_text(encoding="utf-8").strip() if path.exists() else "<missing>"
    report.check(
        actual == case["expect"],
        f"reply {case['name']}",
        f"expected {case['expect']!r}, got {actual!r}",
    )


def check_probe(report, case, entries, declared, identity_dir):
    harness = case["harness"]
    found = extract_identity(entries)
    identity_dir.mkdir(parents=True, exist_ok=True)
    (identity_dir / f"{harness}.json").write_text(
        json.dumps(found, indent=2, ensure_ascii=False) + "\n", encoding="utf-8"
    )
    report.check(found["requests"] > 0, f"probe {case['name']} produced requests", "window was empty")
    report.check(
        found["supply"] == declared.get("supply"),
        f"{harness} identity supply matches matrix.json",
        f"declared {declared.get('supply')!r}, observed {found['supply']!r}",
    )
    report.check(
        found["fields"] == sorted(declared.get("fields") or []),
        f"{harness} identity fields match matrix.json",
        f"declared {sorted(declared.get('fields') or [])}, observed {found['fields']}",
    )
    if found["supply"] == "header":
        report.check(
            found["distinct_values"] == 1,
            f"{harness} session value is stable across the probe's requests",
            f"{found['distinct_values']} distinct values",
        )


def check_reject(report, case, record, entries):
    report.check(
        record is not None and record.get("exit", 0) != 0,
        f"reject {case['name']} failed as required",
        "harness exited 0",
    )
    refused = [e for e in entries if e.get("phase") == "response" and (e.get("status") or 0) >= 400]
    report.check(bool(refused), f"reject {case['name']} produced a 4xx", "no 4xx in the case window")
    token = case.get("expect_error", "")
    report.check(
        bool(token) and any(token in (e.get("body") or "") for e in refused),
        f"reject {case['name']} returned {token or '<expect_error>'}",
        "error token not found in the refusal body",
    )


def check_tap(report, run, api_key):
    entries = read_jsonl(run / "capture.jsonl")
    report.check(len(entries) > 0, "tap wrote capture.jsonl", "file empty or missing")
    malformed = [e for e in entries if "__parse_error__" in e]
    report.check(not malformed, "tap entries are valid JSON", f"{len(malformed)} bad lines")

    requests = [e for e in entries if e.get("phase") == "request"]
    responses = [e for e in entries if e.get("phase") == "response"]
    report.check(len(requests) > 0, "tap recorded requests", "no request entries")
    report.check(len(responses) > 0, "tap recorded responses", "no response entries")

    incomplete = [e for e in requests + responses if not TAP_REQUIRED <= set(e)]
    report.check(not incomplete, "tap entries carry the standard schema", f"{len(incomplete)} incomplete")

    redacted = [e for e in requests if header(e, "authorization") == "[REDACTED]"]
    report.check(len(redacted) > 0, "authorization header is redacted in the tap")

    leaked = [e for e in entries if api_key and api_key in json.dumps(e, ensure_ascii=False)]
    report.check(not leaked, "api key sentinel never persists in the tap", f"{len(leaked)} leaking entries")

    leaking = [
        e for e in entries
        if any(header(e, name) not in (None, "[REDACTED]") for name in SECRET_SENTINELS)
    ]
    report.check(not leaking, "no cleartext credential headers persist", f"{len(leaking)} entries")

    streaming = [e for e in responses if e.get("streaming")]
    report.check(len(streaming) > 0, "SSE responses were detected as streaming")
    return requests


def check_correlation(report, run, requests):
    supplier = read_jsonl(run / "supplier" / "capture.jsonl")
    report.check(len(supplier) > 0, "supplier-side mock wrote its own capture")
    tap_traces = {e.get("trace_id") for e in requests if e.get("trace_id")}
    supplier_traces = {header(e, "x-test-trace-id") for e in supplier}
    shared = tap_traces & supplier_traces
    report.check(
        len(shared) > 0,
        "same trace id observed at the tap and at the supplier",
        f"tap={len(tap_traces)} supplier={len(supplier_traces)} shared=0",
    )


def check_pcap(report, run):
    pcap = run / "pcap" / "entry.pcap"
    size = pcap.stat().st_size if pcap.exists() else 0
    report.check(size > 24, "pcap sidecar captured packets", f"size={size}")
    flows = run / "pcap" / "flows.txt"
    text = flows.read_text(encoding="utf-8") if flows.exists() else ""
    report.check(bool(text.strip()), "pcap is readable by tcpdump", "flows.txt empty")


def main():
    run = pathlib.Path(sys.argv[1])
    spec = json.loads(pathlib.Path(sys.argv[2]).read_text(encoding="utf-8"))
    matrix = json.loads(pathlib.Path(sys.argv[3]).read_text(encoding="utf-8"))
    declared = {h["name"]: h.get("session_identity", {}) for h in matrix["harnesses"]}

    entries = read_jsonl(run / "capture.jsonl")
    runs = case_runs(run)
    report = Report()

    print(f"== testkit assertions for {run.name}")

    for kind, checker in (("reply", None), ("probe", None), ("reject", None)):
        group = [c for c in spec["cases"] if c.get("expect_kind", "reply") == kind]
        report.group(f"{kind} ({len(group)})" if group else f"{kind} (0)")
        for case in group:
            record = runs.get(case["name"])
            if kind == "reply":
                check_reply(report, case, run / "replies")
            elif kind == "probe":
                check_probe(report, case, window(entries, record),
                            declared.get(case["harness"], {}), run / "identity")
            else:
                check_reject(report, case, record, window(entries, record))

    report.group("capture")
    requests = check_tap(report, run, spec.get("api_key_sentinel", ""))
    check_correlation(report, run, requests)
    check_pcap(report, run)

    report.summary()
    if report.failures:
        print("\ntestkit: FAILED", file=sys.stderr)
        return 1
    print("\ntestkit: PASSED")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
