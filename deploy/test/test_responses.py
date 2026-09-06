"""Validate the documented Responses text event lifecycle and input forms."""
import json
from pathlib import Path
import unittest
from mock_protocol import InvalidRequest, ReplyPool
from mock_responses import messages_for_fixture, render_responses


class Responses(unittest.TestCase):
    def test_input_and_restoration(self):
        pool = ReplyPool.load(Path(__file__).with_name("replies.json"))
        request = {"model": "gpt-5.2", "store": False, "input": "[mock:resume:1]"}
        first = pool.select({"messages": messages_for_fixture(request)}, {})
        response, _ = render_responses(request, first)
        request["input"] = [{"role": "user", "content": [{"type": "input_text", "text": "[mock:resume:1]"}]},
                            response["output"][0], {"role": "user", "content": "[mock:resume:2]"}]
        self.assertEqual(pool.select({"messages": messages_for_fixture(request)}, {})["turn"], 2)

    def test_no_fake_stored_conversation(self):
        for request in ({"model": "m", "input": "hi", "previous_response_id": "resp_missing"},
                        {"model": "m", "input": "hi", "conversation": "conv_missing"},
                        {"model": "m", "input": "hi", "store": True},
                        {"model": "m", "input": "hi"}):
            with self.assertRaises(InvalidRequest):
                messages_for_fixture(request)

    def test_event_lifecycle_and_fields(self):
        pool = ReplyPool.load(Path(__file__).with_name("replies.json"))
        request = {"model": "gpt-5.2", "store": False, "input": "[mock:resume:1]"}
        reply = pool.select({"messages": messages_for_fixture(request)}, {})
        response, frames = render_responses(request, reply)
        events = []
        for _, frame in frames:
            event_name, payload = frame.decode().strip().split("\n")
            event = json.loads(payload[6:])
            self.assertEqual(event_name, "event: " + event["type"])
            events.append(event)
        self.assertEqual([e["sequence_number"] for e in events], list(range(len(events))))
        self.assertEqual([e["type"] for e in events[:4]], ["response.created", "response.in_progress", "response.output_item.added", "response.content_part.added"])
        self.assertEqual([e["type"] for e in events[-4:]], ["response.output_text.done", "response.content_part.done", "response.output_item.done", "response.completed"])
        self.assertEqual(events[0]["response"]["status"], "in_progress")
        self.assertEqual(events[0]["response"]["output"], [])
        self.assertEqual(events[-1]["response"], response)
        self.assertEqual(response["object"], "response")
        self.assertEqual(response["status"], "completed")
        self.assertEqual(response["output"][0]["content"][0]["type"], "output_text")
        self.assertEqual("".join(e["delta"] for e in events if e["type"] == "response.output_text.delta"), reply["text"])
        for event in events:
            if "item_id" in event:
                self.assertEqual(event["item_id"], response["output"][0]["id"])
                self.assertEqual((event["output_index"], event["content_index"]), (0, 0))
        usage = response["usage"]
        self.assertEqual(usage["total_tokens"], usage["input_tokens"] + usage["output_tokens"])
        self.assertNotIn(b"[DONE]", b"".join(f for _, f in frames))


if __name__ == "__main__":
    unittest.main(verbosity=2)
