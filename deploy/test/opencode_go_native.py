"""Opt-in live native OpenCode Go probe. Reads API key from stdin; saves no secrets."""
import hashlib
import http.client
import json
import os
from pathlib import Path
import subprocess
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

key = sys.stdin.readline().strip()
if not key:
    raise SystemExit("API key required on stdin")
os.environ["LIVE_GO_KEY"] = key
records = []
phase = "setup"


class Tap(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *args):
        pass

    def do_POST(self):
        if not self.path.startswith("/zen/go/v1/"):
            self.send_error(404)
            return
        body = self.rfile.read(int(self.headers.get("Content-Length", "0")))
        payload = json.loads(body)
        headers = dict(self.headers)
        safe = {k: ("[REDACTED]" if k.lower() in ("authorization", "x-api-key", "cookie") else v)
                for k, v in headers.items()}
        rec = {"phase": phase, "path": self.path, "headers": safe,
               "body": payload, "body_sha256": hashlib.sha256(body).hexdigest()}
        records.append(rec)
        for name in list(headers):
            if name.lower() in ("host", "connection", "transfer-encoding"):
                del headers[name]
        conn = http.client.HTTPSConnection("opencode.ai", timeout=90)
        start = time.monotonic()
        received = bytearray()
        try:
            conn.request("POST", self.path, body=body, headers=headers)
            response = conn.getresponse()
            rec.update(status=response.status, response_content_type=response.getheader("Content-Type"))
            self.send_response(response.status)
            for name, value in response.getheaders():
                if name.lower() not in ("connection", "transfer-encoding", "content-length"):
                    self.send_header(name, value)
            self.send_header("Connection", "close")
            self.end_headers()
            while chunk := response.read1(16384):
                received.extend(chunk)
                self.wfile.write(chunk)
                self.wfile.flush()
            rec["response"] = received.decode(errors="replace").replace(key, "[REDACTED]")
        except Exception as exc:
            rec["error"] = type(exc).__name__
        finally:
            rec["response"] = received.decode(errors="replace").replace(key, "[REDACTED]")
            rec["seconds"] = round(time.monotonic() - start, 3)
            conn.close()
            self.close_connection = True


server = ThreadingHTTPServer(("127.0.0.1", 8765), Tap)
threading.Thread(target=server.serve_forever, daemon=True).start()
config = {
    "$schema": "https://opencode.ai/config.json",
    "share": "disabled", "permission": {"*": "deny"},
    "enabled_providers": ["opencode-go"],
    "provider": {"opencode-go": {"options": {
        "apiKey": "{env:LIVE_GO_KEY}", "baseURL": "http://127.0.0.1:8765/zen/go/v1"}}},
}
Path("/work/opencode.json").write_text(json.dumps(config))
runs = []
session = None
for label, prompt in [("first", "Remember the word ORCHID. Reply only OK. Do not use tools."),
                      ("resume", "What word did I ask you to remember? Reply only that word. Do not use tools."),
                      ("independent", "Reply only HELLO. Do not use tools.")]:
    phase = label
    command = ["opencode", "run", "--format", "json", "--model", "opencode-go/deepseek-v4-flash"]
    if label == "resume":
        if not session:
            break
        command += ["--session", session]
    try:
        result = subprocess.run(command + [prompt], text=True, capture_output=True, timeout=150)
        output = result.stdout.replace(key, "[REDACTED]")
        runs.append({"phase": label, "exit": result.returncode, "stdout": output,
                     "stderr": result.stderr.replace(key, "[REDACTED]")})
        for line in output.splitlines():
            try:
                event = json.loads(line)
                if label == "first" and event.get("sessionID"):
                    session = event["sessionID"]
            except ValueError:
                pass
        print(json.dumps({"phase": label, "exit": result.returncode,
                          "requests": len(records), "output": output[:1500],
                          "stderr": runs[-1]["stderr"][-1000:]}), flush=True)
        if result.returncode:
            break
    except subprocess.TimeoutExpired:
        runs.append({"phase": label, "error": "timeout"})
        break
server.shutdown()
report = {"version": subprocess.check_output(["opencode", "--version"], text=True).strip(),
          "config": config, "runs": runs, "requests": records}
Path("/results/native-go.json").write_text(json.dumps(report, indent=2).replace(key, "[REDACTED]"))
print(json.dumps({"requests": [{"phase": r["phase"], "status": r.get("status"),
                                "headers": r["headers"], "fields": list(r["body"])} for r in records]}))
