"""Test-only streaming HTTP taps; no production credentials or workloads allowed.

8001: native harness -> inbound; 8002: canonical -> new-api;
8003: new-api -> outbound. Final supplier capture lives in mock.py.
Records synthetic request bodies verbatim, redacts credentials before persistence.
"""
import hashlib
import http.client
import json
import threading
import time
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

LOCK = threading.Lock()
HOPS = {"connection", "transfer-encoding", "keep-alive", "proxy-connection", "upgrade", "trailer"}
SECRET_HEADERS = {"authorization", "x-api-key", "cookie", "proxy-authorization"}


def record(stage, path, headers, body, **extra):
    sanitized = {k.lower(): ("[REDACTED]" if k.lower() in SECRET_HEADERS else v)
                 for k, v in headers.items()}
    entry = dict(stage=stage, time_ns=time.time_ns(), path=path, headers=sanitized,
                 body=body.decode("utf-8", errors="replace"),
                 body_sha256=hashlib.sha256(body).hexdigest(), **extra)
    with LOCK, open("/artifacts/capture.jsonl", "a") as file:
        file.write(json.dumps(entry) + "\n")


class Tap(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *args):
        pass

    def do_GET(self):
        self.proxy()

    def do_POST(self):
        self.proxy()

    def proxy(self):
        if self.headers.get("Transfer-Encoding"):
            self.send_error(501, "Test tap requires Content-Length")
            return
        size = int(self.headers.get("Content-Length", "0"))
        if size > 4 * 1024 * 1024:
            self.send_error(413)
            return
        body = self.rfile.read(size)
        headers = dict(self.headers)
        if self.server.stage in ("native", "direct"):
            headers["X-Test-Request-Id"] = str(uuid.uuid4())
        record(self.server.stage, self.path, headers, body)
        headers = {k: v for k, v in headers.items() if k.lower() not in HOPS | {"host"}}
        connection = http.client.HTTPConnection(self.server.target, timeout=60)
        status = 502
        response_headers = {}
        try:
            connection.request(self.command, self.path, body, headers)
            response = connection.getresponse()
            status = response.status
            response_headers = {k.lower(): v for k,v in response.getheaders() if k.lower() not in SECRET_HEADERS}
            self.send_response(status)
            for key, value in response.getheaders():
                if key.lower() not in HOPS | {"content-length"}:
                    self.send_header(key, value)
            self.send_header("Connection", "close")
            self.end_headers()
            while chunk := response.read1(65536):
                self.wfile.write(chunk)
                self.wfile.flush()
        except (OSError, http.client.HTTPException) as error:
            record(self.server.stage + "_error", self.path, headers, b"", error=str(error))
        finally:
            self.close_connection = True
            connection.close()
            record(self.server.stage + "_response", self.path, headers, b"", status=status, response_headers=response_headers)


if __name__ == "__main__":
    for port, stage, target in [(8001, "native", "affinity-gateway:8236"),
                                (8002, "canonical", "new-api:3000"),
                                (8003, "egress", "affinity-gateway:8237"),
                                (8004, "direct", "mock:8000")]:
        server = ThreadingHTTPServer(("0.0.0.0", port), Tap)
        server.stage, server.target = stage, target
        threading.Thread(target=server.serve_forever, daemon=True).start()
    threading.Event().wait()
