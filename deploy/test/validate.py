"""Independent capture oracle. Exit 1 for real compatibility/isolation failures."""
import collections
import hashlib
import hmac
import json
from pathlib import Path
import sys

SECRET = b"isolated-test-only-secret-do-not-use-in-production"
SESSION_HEADERS = {"session-id", "session_id", "conversation-id", "conversation_id",
                   "x-session-affinity", "x-session-id", "x-opencode-session", "thread-id", "x-claude-code-session-id"}


def derive(*parts):
    framed = b"".join(str(len(p.encode())).encode() + b":" + p.encode() for p in parts)
    return hmac.new(SECRET, framed, hashlib.sha256).hexdigest()


def identity(row):
    for name in SESSION_HEADERS:
        if row["headers"].get(name):
            return row["headers"][name]
    body = json.loads(row["body"])
    try:
        return json.loads(body.get("metadata", {}).get("user_id", "{}"))["session_id"]
    except (ValueError, KeyError, TypeError):
        conversation = body.get("conversation")
        return conversation.get("id") if isinstance(conversation, dict) else conversation


def validate_inbound(native, canonical, token, cache_contract=False):
    sid = identity(native)
    if not sid and cache_contract:
        sid = json.loads(native['body']).get('prompt_cache_key')
    assert sid, "no recognized native session identity"
    credential = "Bearer " + token if "authorization" in native["headers"] else token
    expected = "sa:v1:" + derive("inbound:v1", credential, "conversation", sid)
    assert canonical["headers"].get("x-session-affinity") == expected, "inbound HMAC mismatch"
    assert native["body_sha256"] == canonical["body_sha256"], "inbound altered body bytes"
    assert not (SESSION_HEADERS - {"x-session-affinity"}) & canonical["headers"].keys(), "raw header survived"
    return expected


def validate_outbound(canonical, egress, supplier, isolate_body=False):
    internal = canonical["headers"]["x-session-affinity"]
    assert egress["headers"].get("x-session-affinity") == internal, "new-api lost identity"
    route = supplier["headers"]["x-test-route"]
    expected = derive("outbound:v1", route, internal)
    assert supplier["headers"].get("x-opencode-session") == expected, "outbound HMAC mismatch"
    assert not (SESSION_HEADERS - {"x-opencode-session"}) & supplier["headers"].keys(), "internal/raw header leaked"
    if isolate_body:
        before, after = json.loads(egress['body']), json.loads(supplier['body'])
        if before.get('prompt_cache_key') is not None:
            before['prompt_cache_key'] = derive('cache:v1', route, internal, before['prompt_cache_key'])
        user = before.get('metadata', {}).get('user_id', '')
        if user.startswith('{'):
            fields = json.loads(user)
            if fields.get('session_id'):
                fields['session_id'] = expected
                actual_fields = json.loads(after['metadata']['user_id'])
                assert fields == actual_fields, 'outbound metadata identity or unrelated fields altered'
                before['metadata']['user_id'] = after['metadata']['user_id']
        assert before == after, 'outbound body differs outside declared identity rewrites'
    else:
        assert egress["body_sha256"] == supplier["body_sha256"], "outbound unexpectedly altered body"
    return route, expected


def main():
    run = sys.argv[1] if len(sys.argv) > 1 else Path("/artifacts/latest-run").read_text().strip()
    directory = Path("/artifacts") / run
    cases = json.loads((directory / "runs.json").read_text())
    rows = [json.loads(line) for line in Path("/artifacts/capture.jsonl").read_text().splitlines()]
    index = collections.defaultdict(lambda: collections.defaultdict(list))
    for row in rows:
        index[row["headers"].get("x-test-request-id")][row["stage"]].append(row)
    token = Path("/artifacts/token").read_text().strip()
    report = {"run": run, "cases": [], "failures": [], "body_identity_exposure": []}
    groups = {}
    for case in cases:
        if case["name"].endswith("-version"):
            continue
        natives = [r for r in rows if r["stage"] == "native" and case["start_ns"] <= r["time_ns"] <= case["end_ns"]]
        result = {"name": case["name"], "exit_code": case["exit_code"], "requests": len(natives),
                  "identities": [], "routes": [], "checks": [], "errors": []}
        if case["exit_code"] != 0 or not natives:
            result["errors"].append("CLI failed or emitted no captured request")
        if case.get("reply_match") is False:
            result["errors"].append("scenario reply did not match fixture")
        for native in natives:
            trace = native["headers"]["x-test-request-id"]
            chain = index[trace]
            try:
                assert len(chain["canonical"]) == len(chain["egress"]) == len(chain["supplier"]) == 1, "incomplete or retried chain"
                canonical, egress, supplier = (chain[s][0] for s in ("canonical", "egress", "supplier"))
                internal = validate_inbound(native, canonical, token)
                route, outbound = validate_outbound(canonical, egress, supplier)
                assert chain["native_response"][0]["status"] == 200, "HTTP not 200"
                result["identities"].append(internal)
                result["routes"].append(route)
                result["checks"].append({"trace": trace, "inbound": "PASS", "outbound": "PASS", "session": outbound})
                if identity(supplier):
                    # Header is now derived, so inspect body independently.
                    body_only = dict(supplier, headers={})
                    if identity(body_only) == identity(native):
                        report["body_identity_exposure"].append({"case": case["name"], "trace": trace,
                                                                  "field": "metadata.user_id.session_id"})
            except (AssertionError, IndexError, KeyError, ValueError) as error:
                result["errors"].append(str(error))
        result["identities"] = sorted(set(result["identities"]))
        result["routes"] = sorted(set(result["routes"]))
        if len(result["identities"]) != 1 or len(result["routes"]) != 1:
            result["errors"].append("not exactly one identity and route in this CLI turn")
        groups[case["name"]] = result
        report["cases"].append(result)
    for family in ("opencode", "claude", "omp", "omp-configured", "omp-anthropic"):
        first, resume, new = [groups[family + "-" + turn] for turn in ("first", "resume", "new")]
        third = groups.get(family + "-third")
        if third and not third["errors"] and not first["errors"]:
            if (first["identities"], first["routes"]) != (third["identities"], third["routes"]):
                report["failures"].append(family + ": third turn drifted")
        if not first["errors"] and not resume["errors"] and not new["errors"]:
            if (first["identities"], first["routes"]) != (resume["identities"], resume["routes"]):
                report["failures"].append(family + ": resumed conversation drifted")
            if first["identities"] == new["identities"]:
                report["failures"].append(family + ": independent conversations merged")
    for case in report["cases"]:
        if case["errors"]:
            report["failures"].append(case["name"] + ": " + "; ".join(case["errors"]))
    (directory / "validation.json").write_text(json.dumps(report, indent=2))
    print(json.dumps({"run": run, "cases": len(report["cases"]),
                      "validated_requests": sum(len(c["checks"]) for c in report["cases"]),
                      "failures": report["failures"],
                      "body_identity_exposure_count": len(report["body_identity_exposure"])}, indent=2))
    return int(bool(report["failures"] or report["body_identity_exposure"]))


if __name__ == "__main__":
    sys.exit(main())
