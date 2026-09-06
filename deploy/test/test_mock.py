"""Reply selection, protocol and live HTTP timing tests, no model access."""
import http.client
import json
from pathlib import Path
import threading
import time
import unittest
from unittest.mock import patch
from http.server import ThreadingHTTPServer

from mock import Mock
from mock_protocol import InvalidRequest, ReplyPool, render, validate_request


class Protocol(unittest.TestCase):
    def setUp(self):
        self.pool = ReplyPool.load(Path(__file__).with_name("replies.json"))
        self.request = {"model": "fixture-model", "max_tokens": 100, "messages": [{"role": "user", "content": "[mock:resume:1]"}]}

    def test_three_turn_history_and_restart(self):
        for turn in range(1, 4):
            reply = ReplyPool.load(Path(__file__).with_name("replies.json")).select(self.request, {})
            self.assertEqual(reply["turn"], turn)
            self.assertEqual(reply, self.pool.select(self.request, {}))  # retry
            self.request["messages"] += [{"role": "assistant", "content": [{"type": "text", "text": reply["text"]}]},
                                          {"role": "user", "content": f"[mock:resume:{turn + 1}]"}]

    def test_missing_and_corrupted_history(self):
        self.request["messages"][0]["content"] = "[mock:resume:2]"
        with self.assertRaises(InvalidRequest):
            self.pool.select(self.request, {})
        self.request["messages"][:0] = [{"role": "user", "content": "[mock:resume:1]"},
                                         {"role": "assistant", "content": "wrong answer"}]
        with self.assertRaises(InvalidRequest):
            self.pool.select(self.request, {})

    def test_unknown_exhausted_ambiguous_pool(self):
        for marker in ("[mock:unknown:1]", "[mock:resume:4]", "[mock:resume:0]", "[mock:resume:1] [mock:slow:1]"):
            self.request["messages"][0]["content"] = marker
            with self.subTest(marker=marker), self.assertRaises(InvalidRequest):
                self.pool.select(self.request, {})

    def test_bad_config_fails_fast(self):
        for options in ({"chunk_chars": 0}, {"initial_delay_ms": -1}, {"end_delay_ms": True}, {"unknown": 1}):
            with self.subTest(options=options), self.assertRaises(ValueError):
                ReplyPool({"defaults": options})
        with self.assertRaises(ValueError):
            ReplyPool({"scenarios": {"bad": [{"text": "x" * 100, "chunk_chars": 1, "chunk_interval_ms": 1000}]}})

    def test_default_diagnostic_compatibility(self):
        self.request["messages"][0]["content"] = "ordinary synthetic probe"
        reply = self.pool.select(self.request, {"route": "a", "session": "s"})
        self.assertEqual(json.loads(reply["text"]), {"route": "a", "session": "s"})

    def test_chat_wire_and_usage(self):
        reply = self.pool.select(self.request, {})
        for include in (True, False):
            request = dict(self.request, stream_options={"include_usage": include})
            response, frames = render(request, reply, False)
            self.assertTrue(response["id"].startswith("chatcmpl-"))
            self.assertEqual(response["object"], "chat.completion")
            self.assertEqual(response["choices"][0]["message"]["role"], "assistant")
            self.assertEqual(frames[-1][1], b"data: [DONE]\n\n")
            events = [json.loads(frame[6:]) for _, frame in frames[:-1]]
            self.assertEqual({e["id"] for e in events}, {response["id"]})
            self.assertEqual({e["created"] for e in events}, {response["created"]})
            self.assertTrue(all(e["object"] == "chat.completion.chunk" for e in events))
            self.assertEqual(events[0]["choices"][0]["delta"]["role"], "assistant")
            choices = [e["choices"][0] for e in events if e["choices"]]
            self.assertEqual("".join(c["delta"].get("content", "") for c in choices), reply["text"])
            self.assertEqual(choices[-1]["finish_reason"], "stop")
            self.assertEqual(choices[-1]["delta"], {})
            self.assertTrue(all(c["finish_reason"] is None for c in choices[:-1]))
            if include:
                self.assertEqual(events[-1]["choices"], [])
                self.assertEqual(events[-1]["usage"], response["usage"])
                self.assertTrue(all(e["usage"] is None for e in events[:-1]))
            else:
                self.assertTrue(all("usage" not in e for e in events))
        self.assertNotEqual(response["id"], render(request, reply, False)[0]["id"])

    def test_anthropic_wire(self):
        reply = self.pool.select(self.request, {})
        response, frames = render(self.request, reply, True)
        self.assertTrue(response["id"].startswith("msg_"))
        self.assertEqual(response["stop_reason"], "end_turn")
        events = []
        for _, frame in frames:
            name, data = frame.decode().strip().split("\n")
            event = json.loads(data[6:])
            self.assertEqual(name[7:], event["type"])
            events.append(event)
        self.assertEqual(events[0]["message"]["content"], [])
        self.assertEqual(events[0]["message"]["id"], response["id"])
        self.assertIsNone(events[0]["message"]["stop_reason"])
        self.assertEqual([e["type"] for e in events[:2]], ["message_start", "content_block_start"])
        self.assertEqual([e["type"] for e in events[-3:]], ["content_block_stop", "message_delta", "message_stop"])
        self.assertEqual("".join(e["delta"]["text"] for e in events[2:-3]), reply["text"])
        self.assertTrue(all(e["index"] == 0 for e in events[1:-2]))
        self.assertEqual(events[-2]["usage"]["output_tokens"], response["usage"]["output_tokens"])

    def test_request_validation(self):
        for request in (None, [], {}, dict(self.request, stream="yes"), dict(self.request, messages=[1]), dict(self.request, tool_choice="required")):
            with self.subTest(request=request), self.assertRaises(InvalidRequest):
                validate_request(request, False)
        with self.assertRaises(InvalidRequest):
            validate_request(dict(self.request, max_tokens=0), True)


class HTTP(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.record_patch = patch("mock.record")
        cls.record_patch.start()
        cls.server = ThreadingHTTPServer(("127.0.0.1", 0), Mock)
        cls.server.pool = ReplyPool({"scenarios": {"timing": [{"text": "甲乙丙丁", "initial_delay_ms": 100,
                                    "chunk_interval_ms": 40, "end_delay_ms": 80, "chunk_chars": 1}]}})
        cls.thread = threading.Thread(target=cls.server.serve_forever, daemon=True)
        cls.thread.start()

    @classmethod
    def tearDownClass(cls):
        cls.server.shutdown()
        cls.server.server_close()
        cls.thread.join()
        cls.record_patch.stop()

    def call(self, path, data):
        conn = http.client.HTTPConnection("127.0.0.1", self.server.server_port, timeout=5)
        start = time.monotonic()
        conn.request("POST", path, data, {"Content-Type": "application/json"})
        response = conn.getresponse()
        return conn, response, start

    def test_timing_and_sse_over_http(self):
        for path in ("/v1/chat/completions", "/v1/messages?beta=true"):
            request = dict(model="fixture-model", max_tokens=100, stream=True,
                           messages=[{"role": "user", "content": "[mock:timing:1]"}])
            conn, response, start = self.call(path, json.dumps(request))
            self.assertEqual(response.status, 200)
            self.assertGreaterEqual(time.monotonic() - start, .085)
            times = []
            for line in response:
                if line.startswith(b"data: {"):
                    event = json.loads(line[6:])
                    if event.get("type") == "content_block_delta" or event.get("choices", [{}])[0].get("delta", {}).get("content"):
                        times.append(time.monotonic())
            self.assertEqual(len(times), 4)
            self.assertTrue(all(b - a >= .025 for a, b in zip(times, times[1:])))
            self.assertGreaterEqual(time.monotonic() - times[-1], .06)
            conn.close()

    def test_json_success_and_protocol_errors(self):
        for path in ("/v1/chat/completions", "/v1/messages"):
            for data, status in [(b"not-json", 400), (b"[]", 400), (json.dumps({"model": "m", "max_tokens": 1,
                    "messages": [{"role": "user", "content": "hello"}]}).encode(), 200)]:
                conn, response, _ = self.call(path, data)
                self.assertEqual(response.status, status)
                self.assertEqual(response.getheader("Content-Type"), "application/json")
                value = json.loads(response.read())
                if status == 400:
                    self.assertEqual(value["error"]["type"], "invalid_request_error")
                    if path == "/v1/messages":
                        self.assertEqual(value["type"], "error")
                        self.assertEqual(value["request_id"], response.getheader("request-id"))
                conn.close()


if __name__ == "__main__":
    unittest.main(verbosity=2)
