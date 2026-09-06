"""Offline configurable text mock. No real supplier credentials or inference."""
import json
import os
from pathlib import Path
import time
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from capture import record
from mock_protocol import InvalidRequest, ReplyPool, render, validate_request, text_content
from mock_responses import messages_for_fixture, render_responses


class Mock(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *args):
        pass

    def json_response(self, status, value):
        data = json.dumps(value, ensure_ascii=False).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.send_header("request-id" if self.anthropic else "x-request-id", self.request_id)
        self.send_header("Connection", "close")
        self.end_headers()
        self.close_connection = True
        self.wfile.write(data)

    def error(self, status, message):
        if self.anthropic:
            kind = {404: "not_found_error", 413: "request_too_large", 429:"rate_limit_error",500:"api_error",503:"api_error",529:"overloaded_error"}.get(status, "invalid_request_error")
            value = {"type": "error", "error": {"type": kind, "message": message}, "request_id": self.request_id}
        else:
            value = {"error": {"message": message, "type": "server_error" if status>=500 else "invalid_request_error", "param": None, "code": None}}
        self.json_response(status, value)

    def do_POST(self):
        self.request_id = "req_" + uuid.uuid4().hex
        path = self.path.split("?")[0]
        self.anthropic = path.startswith("/v1/messages")
        if path not in ("/v1/chat/completions", "/v1/messages", "/v1/responses"):
            self.error(404, "endpoint outside supported mock subset")
            return
        try:
            if self.headers.get("Transfer-Encoding") or self.headers.get("Content-Encoding"):
                raise InvalidRequest("text mock requires uncompressed Content-Length requests")
            try:
                size = int(self.headers.get("Content-Length", "0"))
            except ValueError:
                raise InvalidRequest("invalid Content-Length") from None
            if not 0 < size <= 2 * 1024 * 1024:
                self.error(413, "request body exceeds mock limit or is empty")
                return
            body = self.rfile.read(size)
            try:
                request = json.loads(body)
            except (ValueError, UnicodeError):
                raise InvalidRequest("invalid JSON body") from None
            responses = path == "/v1/responses"
            if responses:
                fixture_request = {"messages": messages_for_fixture(request)}
            else:
                validate_request(request, self.anthropic)
                fixture_request = request
            record("supplier", self.path, self.headers, body)
            if any(text_content(m.get('content')) == '[mock-fail:503]' for m in fixture_request['messages'] if m.get('role')=='user'):
                self.error(503,'isolated fixture upstream unavailable')
                return
            diagnostic = {"route": self.headers.get("X-Test-Route"), "session": self.headers.get("x-opencode-session"),
                          "internal": self.headers.get("X-Session-Affinity"), "generic": self.headers.get("X-Session-Id")}
            reply = self.server.pool.select(fixture_request, diagnostic)
            response, frames = render_responses(request, reply) if responses else render(request, reply, self.anthropic)
            record("mock_reply", self.path, self.headers, b"", scenario=reply["scenario"], turn=reply["turn"],
                   request_id=self.request_id, response_id=response["id"],
                   timing={key: reply[key] for key in ("initial_delay_ms", "chunk_interval_ms", "end_delay_ms", "chunk_chars")})
            time.sleep(reply["initial_delay_ms"] / 1000)
            if not request.get("stream"):
                self.json_response(200, response)
                return
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream; charset=utf-8")
            self.send_header("Cache-Control", "no-cache")
            self.send_header("request-id" if self.anthropic else "x-request-id", self.request_id)
            self.send_header("Connection", "close")
            self.end_headers()
            self.close_connection = True
            for delay, frame in frames:
                time.sleep(delay / 1000)
                self.wfile.write(frame)
                self.wfile.flush()
        except InvalidRequest as error:
            self.error(400, str(error))
        except (BrokenPipeError, ConnectionResetError):
            self.close_connection = True


if __name__ == "__main__":
    server = ThreadingHTTPServer(("0.0.0.0", 8000), Mock)
    server.pool = ReplyPool.load(os.environ.get("MOCK_REPLY_CONFIG", str(Path(__file__).with_name("replies.json"))))
    server.serve_forever()
