"""mitmproxy addon: L7 tap that records every hop as redacted NDJSON.

Why mitmproxy instead of a hand-rolled forwarding HTTP server: keep-alive,
chunked transfer, HTTP/2 and SSE pass-through are the tap's problem to get
right, and a tap that mangles them invalidates the test it is observing. The
addon only observes, redacts and labels.

Environment:
    CAPTURE_FILE  NDJSON output path (default /artifacts/capture.jsonl)
    CAPTURE_HOP   logical hop name, e.g. "entry", "canonical", "egress"
"""
import hashlib
import json
import os
import threading
import time
import uuid

CAPTURE_FILE = os.environ.get("CAPTURE_FILE", "/artifacts/capture.jsonl")
HOP = os.environ.get("CAPTURE_HOP", "unknown")
TRACE_HEADER = "x-test-trace-id"
STREAM_TYPE = "text/event-stream"
SECRET_HEADERS = {
    "authorization",
    "x-api-key",
    "api-key",
    "cookie",
    "proxy-authorization",
    "set-cookie",
}
LOCK = threading.Lock()


def _redact(headers):
    return {
        key: ("[REDACTED]" if key.lower() in SECRET_HEADERS else value)
        for key, value in headers.items()
    }


def _write(entry):
    entry["hop"] = HOP
    entry["time_ns"] = time.time_ns()
    line = json.dumps(entry, ensure_ascii=False)
    with LOCK:
        with open(CAPTURE_FILE, "a", encoding="utf-8") as handle:
            handle.write(line + "\n")


def _is_stream(response):
    return STREAM_TYPE in response.headers.get("content-type", "")


def request(flow):
    trace_id = flow.request.headers.get(TRACE_HEADER) or uuid.uuid4().hex
    flow.request.headers[TRACE_HEADER] = trace_id
    body = flow.request.raw_content or b""
    _write(
        {
            "phase": "request",
            "trace_id": trace_id,
            "method": flow.request.method,
            "scheme": flow.request.scheme,
            "host": flow.request.pretty_host,
            "path": flow.request.path,
            "headers": _redact(dict(flow.request.headers)),
            "body": body.decode("utf-8", "replace"),
            "body_sha256": hashlib.sha256(body).hexdigest(),
        }
    )


def responseheaders(flow):
    # Never buffer SSE: a proxy that buffers would itself change the behaviour
    # the harness is being tested against.
    if _is_stream(flow.response):
        flow.response.stream = True


def response(flow):
    streaming = _is_stream(flow.response)
    body = b"" if streaming else (flow.response.raw_content or b"")
    _write(
        {
            "phase": "response",
            "trace_id": flow.request.headers.get(TRACE_HEADER),
            "status": flow.response.status_code,
            "streaming": streaming,
            "headers": _redact(dict(flow.response.headers)),
            "body": body.decode("utf-8", "replace"),
            "body_sha256": hashlib.sha256(body).hexdigest(),
        }
    )
