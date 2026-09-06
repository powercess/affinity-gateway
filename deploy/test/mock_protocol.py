"""Deterministic text-only fixture engine and supplier wire format encoders."""
import json
import re
import time
import uuid
from pathlib import Path

MARKER = re.compile(r"\[mock:([a-z0-9_-]+):(\d+)\]")
DEFAULTS = dict(initial_delay_ms=0, chunk_interval_ms=100, end_delay_ms=100,
                chunk_chars=64, input_tokens=10, output_tokens=5)


class InvalidRequest(ValueError):
    pass


def text_content(content):
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        return "".join(part.get("text", "") for part in content
                       if isinstance(part, dict) and part.get("type") == "text" and isinstance(part.get("text"), str))
    return ""


class ReplyPool:
    def __init__(self, config):
        if not isinstance(config, dict) or set(config) - {"defaults", "scenarios"}:
            raise ValueError("invalid reply pool config")
        self.defaults = self.options(config.get("defaults", {}), DEFAULTS)
        scenarios = config.get("scenarios", {})
        if not isinstance(scenarios, dict):
            raise ValueError("scenarios must be an object")
        self.scenarios = {}
        for name, entries in scenarios.items():
            if not re.fullmatch(r"[a-z0-9_-]+", name) or not isinstance(entries, list) or not entries:
                raise ValueError("invalid scenario name or empty pool")
            self.scenarios[name] = []
            for entry in entries:
                if not isinstance(entry, dict) or not isinstance(entry.get("text"), str) or not 0 < len(entry["text"]) <= 100000:
                    raise ValueError("reply text must be a nonempty bounded string")
                reply = dict(self.options({k: v for k, v in entry.items() if k != "text"}, self.defaults), text=entry["text"])
                chunks = (len(reply["text"]) + reply["chunk_chars"] - 1) // reply["chunk_chars"]
                if reply["initial_delay_ms"] + chunks * reply["chunk_interval_ms"] + reply["end_delay_ms"] > 30000:
                    raise ValueError("mock fixture exceeds 30 second timing budget")
                self.scenarios[name].append(reply)

    @staticmethod
    def options(options, base):
        if not isinstance(options, dict) or set(options) - DEFAULTS.keys():
            raise ValueError("unknown reply option")
        result = dict(base, **options)
        for key, value in result.items():
            minimum = 1 if key == "chunk_chars" else 0
            maximum = 10000 if key.endswith("_ms") else 100000
            if type(value) is not int or not minimum <= value <= maximum:
                raise ValueError("invalid reply option: " + key)
        return result

    @classmethod
    def load(cls, path):
        return cls(json.loads(Path(path).read_text()))

    def select(self, request, diagnostic):
        messages = request["messages"]
        users = [(i, text_content(m.get("content"))) for i, m in enumerate(messages) if m.get("role") == "user"]
        if not users:
            raise InvalidRequest("messages must contain a user message")
        index, latest = users[-1]
        matches = MARKER.findall(latest)
        if not matches:
            reply = dict(self.defaults, text=json.dumps(diagnostic), scenario=None, turn=None)
            chunks = (len(reply["text"]) + reply["chunk_chars"] - 1) // reply["chunk_chars"]
            if reply["initial_delay_ms"] + chunks * reply["chunk_interval_ms"] + reply["end_delay_ms"] > 30000:
                raise InvalidRequest("mock fixture exceeds 30 second timing budget")
            return reply
        if len(matches) != 1:
            raise InvalidRequest("one mock scenario marker required per user turn")
        name, number = matches[0]
        if len(number) > 6:
            raise InvalidRequest("mock turn is out of range")
        turn = int(number)
        pool = self.scenarios.get(name)
        if pool is None or not 1 <= turn <= len(pool):
            raise InvalidRequest("unknown mock scenario or turn outside reply pool")
        # Validate replayed history, not a mutable global turn counter. Retries,
        # title generation, concurrency and restarts never consume another turn.
        position = 0
        for previous in range(1, turn):
            marker = f"[mock:{name}:{previous}]"
            while position < index and not (messages[position].get("role") == "user" and marker in text_content(messages[position].get("content"))):
                position += 1
            if position >= index:
                raise InvalidRequest("restored history missing prior user turn")
            position += 1
            expected = pool[previous - 1]["text"]
            while position < index and messages[position].get("role") != "assistant":
                position += 1
            if position >= index or text_content(messages[position].get("content")) != expected:
                raise InvalidRequest("restored history missing or changed prior assistant reply")
            position += 1
        return dict(pool[turn - 1], scenario=name, turn=turn)


def validate_request(request, anthropic):
    if not isinstance(request, dict) or not isinstance(request.get("model"), str) or not request["model"]:
        raise InvalidRequest("model must be a nonempty string")
    messages = request.get("messages")
    if not isinstance(messages, list) or not messages or any(not isinstance(m, dict) for m in messages):
        raise InvalidRequest("messages must be a nonempty array of objects")
    if type(request.get("stream", False)) is not bool:
        raise InvalidRequest("stream must be boolean")
    if anthropic and (type(request.get("max_tokens")) is not int or request["max_tokens"] < 1):
        raise InvalidRequest("max_tokens must be a positive integer")
    if not anthropic:
        if request.get("n", 1) != 1:
            raise InvalidRequest("text mock supports n=1 only")
        options = request.get("stream_options")
        if options is not None and (not isinstance(options, dict) or type(options.get("include_usage", False)) is not bool):
            raise InvalidRequest("invalid stream_options")
    choice = request.get("tool_choice")
    if choice == "required" or isinstance(choice, dict) and choice.get("type") in ("any", "tool", "function"):
        raise InvalidRequest("forced tool calls are outside the text mock subset")


def render(request, reply, anthropic):
    """Return JSON response and delayed SSE frames without private wire fields."""
    text = reply["text"]
    fragments = [text[i:i + reply["chunk_chars"]] for i in range(0, len(text), reply["chunk_chars"])]
    identifier = ("msg_" if anthropic else "chatcmpl-") + uuid.uuid4().hex
    if anthropic:
        usage = {"input_tokens": reply["input_tokens"], "output_tokens": reply["output_tokens"]}
        response = dict(id=identifier, type="message", role="assistant", model=request["model"],
                        content=[{"type": "text", "text": text}], stop_reason="end_turn", stop_sequence=None, usage=usage)
        events = [dict(type="message_start", message=dict(response, content=[], stop_reason=None,
                      usage=dict(usage, output_tokens=0))),
                  dict(type="content_block_start", index=0, content_block={"type": "text", "text": ""})]
        events += [dict(type="content_block_delta", index=0, delta={"type": "text_delta", "text": part}) for part in fragments]
        events += [dict(type="content_block_stop", index=0),
                   dict(type="message_delta", delta={"stop_reason": "end_turn", "stop_sequence": None},
                        usage={"output_tokens": reply["output_tokens"]}), dict(type="message_stop")]
        frames = [("event: " + e["type"] + "\ndata: " + json.dumps(e, ensure_ascii=False) + "\n\n").encode() for e in events]
        delays = [0, 0] + [0 if i == 0 else reply["chunk_interval_ms"] for i in range(len(fragments))] + [reply["end_delay_ms"], 0, 0]
    else:
        base = dict(id=identifier, object="chat.completion", created=int(time.time()), model=request["model"])
        usage = dict(prompt_tokens=reply["input_tokens"], completion_tokens=reply["output_tokens"],
                     total_tokens=reply["input_tokens"] + reply["output_tokens"])
        response = dict(base, choices=[dict(index=0, message=dict(role="assistant", content=text, refusal=None),
                                           finish_reason="stop", logprobs=None)], usage=usage)
        include_usage = bool((request.get("stream_options") or {}).get("include_usage"))
        deltas = [{"role": "assistant", "content": ""}] + [{"content": part} for part in fragments] + [{}]
        events = [dict(base, object="chat.completion.chunk", choices=[dict(index=0, delta=delta,
                        finish_reason="stop" if i == len(deltas) - 1 else None, logprobs=None)]) for i, delta in enumerate(deltas)]
        if include_usage:
            for event in events:
                event["usage"] = None
            events.append(dict(base, object="chat.completion.chunk", choices=[], usage=usage))
        frames = [("data: " + json.dumps(e, ensure_ascii=False) + "\n\n").encode() for e in events] + [b"data: [DONE]\n\n"]
        delays = [0] + [0 if i == 0 else reply["chunk_interval_ms"] for i in range(len(fragments))] + [reply["end_delay_ms"]]
        delays += [0] * (len(frames) - len(delays))
    return response, list(zip(delays, frames))
