"""Real TUI side-session regression; isolated test stack and synthetic token only.

SIDE_CLIENT=omp|claude; SIDE_POLICY=strict|metadata. The ingress must already
have that policy. Run serially on a dedicated stack (captures use a time window).
Supplier is the repository's text/SSE mock, not a real model service.
"""
import collections
import fcntl
import json
import os
from pathlib import Path
import pty
import select
import struct
import subprocess
import termios
import time
import uuid

from validate import derive, validate_outbound, SESSION_HEADERS


def main():
    client = os.environ.get("SIDE_CLIENT", "omp")
    policy = os.environ.get("SIDE_POLICY", "metadata")
    assert client in ("omp", "claude") and policy in ("strict", "metadata")
    run = "side-" + str(uuid.uuid4())
    out = Path("/artifacts") / run
    out.mkdir()
    home = Path("/tmp") / run
    home.mkdir()
    token = Path("/artifacts/token").read_text().strip()
    env = dict(os.environ, HOME=str(home), TEST_API_KEY=token,
               ANTHROPIC_API_KEY=token, ANTHROPIC_BASE_URL="http://capture:8001",
               TERM="xterm-256color", XDG_CONFIG_HOME=str(home / ".config"))
    if client == "omp":
        directory = home / ".omp/agent"
        directory.mkdir(parents=True)
        (directory / "models.yml").write_text(json.dumps({"providers": {
            "anthropic": {"baseUrl": "http://capture:8001", "apiKey": "TEST_API_KEY"}}}))
        # Native settings; shorten only the idle delay, do not replace side requests.
        (directory / "config.yml").write_text(
            "startup:\n  setupWizard: false\n  checkUpdate: false\n"
            "recap:\n  enabled: true\n  idleSeconds: 5\n")
        command = [os.environ.get("SIDE_OMP_BINARY", "omp"), "--model",
                   "anthropic/claude-sonnet-4-6", "--no-tools", "--no-extensions",
                   "--no-skills", "--no-lsp"]
    else:
        (home / ".claude.json").write_text(json.dumps({
            "hasCompletedOnboarding": True, "theme": "dark",
            "customApiKeyResponses": {"approved": [token[-20:]], "rejected": []},
            "projects": {"/work": {"hasTrustDialogAccepted": True}}}))
        command = ["claude", "--model", "claude-sonnet-4-6", "--tools", "",
                   "--permission-mode", "dontAsk"]
    version = subprocess.check_output([command[0], "--version"], env=env, text=True).strip()
    master, slave = pty.openpty()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 120, 0, 0))
    start = time.time_ns()
    process = subprocess.Popen(command, stdin=slave, stdout=slave, stderr=slave,
                               env=env, cwd="/work", start_new_session=True)
    os.close(slave)
    output = bytearray()

    def pump(seconds):
        end = time.monotonic() + seconds
        while time.monotonic() < end:
            if select.select([master], [], [], .1)[0]:
                try:
                    data = os.read(master, 65536)
                except OSError:
                    break
                output.extend(data)
                if b"\x1b[6n" in data:
                    os.write(master, b"\x1b[1;1R")

    try:
        pump(7)
        os.write(master, b"[mock:resume:1] Reply with text only.\r")
        pump(10)  # Includes omp's real idle recap.
        os.write(master, b"/btw What is the test phrase?\r")
        pump(12)
        os.write(master, b"\x1b")
        pump(1)
        os.write(master, b"Continue main conversation. Reply with text only.\r")
        pump(4)
    finally:
        process.terminate()
        try:
            process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait()
        os.close(master)
        (out / "terminal.log").write_bytes(output.replace(token.encode(), b"[REDACTED]"))
    end = time.time_ns()
    rows = [json.loads(line) for line in Path("/artifacts/capture.jsonl").read_text().splitlines()]
    rows = [row for row in rows if start <= row["time_ns"] <= end]
    (out / "capture.json").write_text(json.dumps(rows, indent=2))
    report = {"run": run, "client": client, "version": version, "policy": policy,
              "passed": False, "results": []}
    try:
        validate(rows, token, client, policy, report)
        report["passed"] = True
    except Exception as error:
        report["error"] = str(error)
        raise
    finally:
        (out / "validation.json").write_text(json.dumps(report, indent=2))
        print(json.dumps(report, indent=2), flush=True)


def validate(rows, token, client, policy, report):
    index = collections.defaultdict(dict)
    for row in rows:
        index[row["headers"].get("x-test-request-id")][row["stage"]] = row
    groups = {}
    kinds = set()
    for stages in index.values():
        native = stages.get("native")
        if not native or not native["path"].startswith("/v1/messages") or not native["body"]:
            continue
        body = json.loads(native["body"])
        sid = json.loads(body.get("metadata", {}).get("user_id", "{}" )).get("session_id")
        assert sid, "missing metadata identity"
        header = native["headers"].get("x-claude-code-session-id")
        last_user = next((m for m in reversed(body.get("messages", [])) if m["role"] == "user"), {})
        text = json.dumps(last_user)
        kind = ("title" if "Write the title in the predominant language" in text else
                "btw" if "What is the test phrase?" in text else
                "recap" if "<recap>" in text else
                "resume" if "Continue main conversation." in text else "main-or-title")
        kinds.add(kind)
        if kind == "resume":
            assert "What is the test phrase?" not in native["body"], "side question polluted main history"
            assert "[mock:resume:1]" in native["body"], "main user history was not retained"
            assert any(m["role"] == "assistant" and m.get("content") for m in body["messages"]), "main assistant history was not retained"
        status = stages["native_response"]["status"]
        expected_status = 400 if header and header != sid and policy == "strict" else 200
        assert status == expected_status, (kind, status, expected_status)
        result = dict(session=sid, header=header, kind=kind, status=status)
        if status == 400:
            assert stages["native_response"]["response_headers"].get("x-affinity-error") == "affinity_identity_conflict"
            assert not any(k in stages for k in ("canonical", "egress", "supplier"))
        else:
            canonical = stages["canonical"]
            credential = "Bearer " + token if "authorization" in native["headers"] else token
            internal = "sa:v1:" + derive("inbound:v1", credential, "conversation", sid)
            assert canonical["headers"]["x-session-affinity"] == internal
            assert native["body_sha256"] == canonical["body_sha256"]
            assert not (SESSION_HEADERS - {"x-session-affinity"}) & canonical["headers"].keys()
            route, supplier_id = validate_outbound(canonical, stages["egress"], stages["supplier"], isolate_body=True)
            if sid in groups:
                assert groups[sid] == (route, supplier_id), "conversation drifted"
            elif kind in ("btw", "recap", "resume"):
                raise AssertionError("side/resume did not reuse an observed main conversation")
            groups[sid] = (route, supplier_id)
            result.update(route=route, canonical=internal, supplier_id=supplier_id)
        report["results"].append(result)
    assert {"btw", "resume"} <= kinds, "real /btw and subsequent main turn must send requests"
    if client == "omp":
        assert "recap" in kinds, "real idle recap was not exercised"


if __name__ == "__main__":
    main()
