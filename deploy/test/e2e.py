"""Executable protocol boundary + stress regression tests (stdlib unittest)."""
from concurrent.futures import ThreadPoolExecutor
import http.client
import json
import os
import io
from pathlib import Path
import time
import unittest
import uuid

from validate import derive

TOKEN = Path("/artifacts/token").read_text().strip()
BODY = {"model": "affinity-test", "messages": [{"role": "user", "content": "Synthetic protocol probe"}]}


def post(path="/v1/chat/completions", headers=None, body=None, host="affinity-gateway:8236"):
    connection = http.client.HTTPConnection(host, timeout=15)
    data = json.dumps(BODY if body is None else body).encode()
    connection.request("POST", path, data, {"Content-Type": "application/json", **(headers or {})})
    response = connection.getresponse()
    result = response.status, response.read()
    connection.close()
    return result


def echo(result):
    status, body = result
    assert status == 200, (status, body[:300])
    return json.loads(json.loads(body)["choices"][0]["message"]["content"])


class Boundaries(unittest.TestCase):
    def test_missing_credential(self):
        self.assertEqual(post(headers={"X-Session-Id": "s"})[0], 401)

    def test_missing_session(self):
        self.assertEqual(post(headers={"Authorization": "Bearer " + TOKEN})[0], 400)

    def test_conflict(self):
        self.assertEqual(post(headers={"Authorization": "Bearer " + TOKEN, "X-Session-Id": "a",
                                       "X-Session-Affinity": "b"})[0], 400)

    def test_duplicate_header(self):
        conn = http.client.HTTPConnection("affinity-gateway:8236", timeout=15)
        data = json.dumps(BODY).encode()
        conn.putrequest("POST", "/v1/chat/completions")
        for key, value in [("Authorization", "Bearer " + TOKEN), ("Content-Length", str(len(data))),
                           ("X-Session-Id", "a"), ("X-Session-Id", "a")]:
            conn.putheader(key, value)
        conn.endheaders(data)
        self.assertEqual(conn.getresponse().status, 400)
        conn.close()

    def test_too_long_id(self):
        self.assertEqual(post(headers={"Authorization": "Bearer " + TOKEN, "X-Session-Id": "x" * 513})[0], 400)

    def test_body_limit(self):
        body = dict(BODY, padding="x" * (2 * 1024 * 1024), conversation="session-overflow")
        self.assertEqual(post(headers={"Authorization": "Bearer " + TOKEN}, body=body)[0], 400)

    def test_body_signals(self):
        for signal in ({"conversation": "日本語-session"}, {"conversation": {"id": "日本語-session"}},
                       {"metadata": {"user_id": json.dumps({"session_id": "日本語-session"})}}):
            with self.subTest(signal=signal):
                response = echo(post(headers={"Authorization": "Bearer " + TOKEN}, body=dict(BODY, **signal)))
                canonical = "sa:v1:" + derive("inbound:v1", "Bearer " + TOKEN, "conversation", "日本語-session")
                self.assertEqual(response["session"], derive("outbound:v1", response["route"], canonical))

    def test_header_aliases(self):
        for header in ("Session-Id", "Session_id", "Conversation-Id", "Conversation_id",
                       "X-Session-Affinity", "X-Session-Id", "X-Opencode-Session", "Thread-Id"):
            with self.subTest(header=header):
                response = echo(post(headers={"Authorization": "Bearer " + TOKEN, header: "alias-test"}))
                canonical = "sa:v1:" + derive("inbound:v1", "Bearer " + TOKEN, "conversation", "alias-test")
                self.assertEqual(response["session"], derive("outbound:v1", response["route"], canonical))

    def test_output_isolation(self):
        canonical = "sa:v1:" + "a" * 64
        results = [echo(post("/r/" + route + "/v1/chat/completions", host="affinity-gateway:8237",
                            headers={"X-Session-Affinity": canonical, "X-Session-Id": "raw"}))
                   for route in ("opencode-a", "opencode-a", "opencode-b", "no-session")]
        self.assertEqual(results[0], results[1])
        self.assertNotEqual(results[0]["session"], results[2]["session"])
        self.assertIsNone(results[3]["session"])
        for result in results:
            self.assertIsNone(result["internal"])
            self.assertIsNone(result["generic"])

    def test_outbound_rejections(self):
        for path, headers, status in [
            ("/r/unknown/v1/chat/completions", {}, 404),
            ("/r/opencode-a/v1/chat/completions", {}, 400),
            ("/r/opencode-a/v1/chat/completions", {"X-Session-Affinity": "sa:v1:invalid"}, 400),
        ]:
            with self.subTest(path=path, headers=headers):
                self.assertEqual(post(path, host="affinity-gateway:8237", headers=headers)[0], status)

    def test_stream_flush(self):
        conn = http.client.HTTPConnection("affinity-gateway:8236", timeout=15)
        conn.request("POST", "/v1/chat/completions", json.dumps(dict(BODY, stream=True)),
                     {"Content-Type": "application/json", "Authorization": "Bearer " + TOKEN,
                      "X-Session-Id": str(uuid.uuid4())})
        response = conn.getresponse()
        self.assertEqual(response.status, 200)
        first = response.readline()
        first_at = time.monotonic()
        rest = response.read()
        elapsed = time.monotonic() - first_at
        self.assertIn(b"data:", first)
        self.assertIn(b"[DONE]", rest)
        self.assertGreater(elapsed, .15, "stream apparently buffered to completion")
        conn.close()

    def test_credential_namespace(self):
        # The alternate credential must fail new-api auth, but its canonical
        # identity is still observable at the pre-new-api tap.
        for credential in (TOKEN, "invalid-disposable-token"):
            post(headers={"Authorization": "Bearer " + credential, "X-Session-Id": "namespace-regression"})
        rows = [json.loads(line) for line in Path("/artifacts/capture.jsonl").read_text().splitlines()]
        ids = {r["headers"].get("x-session-affinity") for r in rows if r["stage"] == "canonical"}
        expected = {"sa:v1:" + derive("inbound:v1", "Bearer " + credential, "conversation", "namespace-regression")
                    for credential in (TOKEN, "invalid-disposable-token")}
        self.assertEqual(len(expected), 2)
        self.assertTrue(expected <= ids)


def stress():
    summary = []
    for delay in (0, .2):
        def conversation(_):
            sid = str(uuid.uuid4())
            outputs = []
            errors = []
            for turn in range(3):
                try:
                    outputs.append(echo(post(headers={"Authorization": "Bearer " + TOKEN, "X-Session-Id": sid})))
                except (AssertionError, OSError) as error:
                    errors.append(str(error))
                if turn < 2 and delay:
                    time.sleep(delay)
            return {"session": sid, "routes": [r["route"] for r in outputs],
                    "identities": [r["session"] for r in outputs],
                    "errors": errors,
                    "stable": not errors and all(r == outputs[0] for r in outputs)}
        started = time.monotonic()
        with ThreadPoolExecutor(max_workers=8) as pool:
            results = list(pool.map(conversation, range(100)))
        summary.append({"delay": delay, "conversations": 100, "requests": 300, "workers": 8,
                        "seconds": round(time.monotonic() - started, 3),
                        "drifted": sum(not r["stable"] and not r["errors"] for r in results),
                        "errored": sum(bool(r["errors"]) for r in results), "results": results})
    Path("/artifacts/stress.json").write_text(json.dumps(summary, indent=2))
    print(json.dumps([{k: v for k, v in r.items() if k != "results"} for r in summary], indent=2))
    return any(row["drifted"] or row["errored"] for row in summary)


if __name__ == "__main__":
    stream = io.StringIO()
    result = unittest.TextTestRunner(verbosity=2, stream=stream).run(unittest.defaultTestLoader.loadTestsFromTestCase(Boundaries))
    Path("/artifacts/boundaries.log").write_text(stream.getvalue())
    print(stream.getvalue(), flush=True)
    failed = stress()
    raise SystemExit(int(not result.wasSuccessful() or failed))
