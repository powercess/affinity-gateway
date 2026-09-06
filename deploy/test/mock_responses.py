"""OpenAI Responses text fixture, based on official Responses API + openai-node
6.42.0 ResponseStreamEvent types (also vendored by omp 18.1.11).

Only stateless, full-input text generations are implemented. No invented
Chat-to-Responses translation in the clients and no fake stored response IDs.
"""
import json
import time
import uuid
from mock_protocol import InvalidRequest


def messages_for_fixture(request):
    if not isinstance(request, dict) or not isinstance(request.get("model"), str):
        raise InvalidRequest("model is required")
    if request.get("previous_response_id") or request.get("conversation"):
        raise InvalidRequest("mock supports full-input Responses only; stored conversations are not implemented")
    if request.get("store") is not False:
        raise InvalidRequest("mock requires store=false; response persistence is not implemented")
    if type(request.get("stream", False)) is not bool:
        raise InvalidRequest("stream must be boolean")
    items = request.get("input")
    if isinstance(items, str):
        return [{"role": "user", "content": items}]
    if not isinstance(items, list):
        raise InvalidRequest("input must be a string or array")
    messages = []
    for item in items:
        if not isinstance(item, dict):
            raise InvalidRequest("input items must be objects")
        if item.get("type", "message") != "message":
            # Reasoning items can accompany normal restored message history.
            if item.get("type") == "reasoning":
                continue
            raise InvalidRequest("mock supports message and reasoning input items only")
        content = item.get("content", "")
        if isinstance(content, list):
            if any(not isinstance(p, dict) or p.get("type") not in ("input_text", "output_text") for p in content):
                raise InvalidRequest("mock supports text input content only")
            content = [{"type": "text", "text": p.get("text", "")} for p in content]
        messages.append({"role": item.get("role"), "content": content})
    return messages


def render_responses(request, reply):
    text = reply["text"]
    response_id, item_id = "resp_" + uuid.uuid4().hex, "msg_" + uuid.uuid4().hex
    part = {"type": "output_text", "text": text, "annotations": [], "logprobs": []}
    item = {"id": item_id, "type": "message", "status": "completed", "role": "assistant", "content": [part]}
    response = dict(id=response_id, object="response", created_at=int(time.time()), status="completed",
                    error=None, incomplete_details=None, instructions=request.get("instructions"),
                    model=request["model"], output=[item], parallel_tool_calls=request.get("parallel_tool_calls", True),
                    temperature=request.get("temperature", 1.0), top_p=request.get("top_p", 1.0),
                    tool_choice=request.get("tool_choice", "auto"), tools=request.get("tools", []),
                    metadata=request.get("metadata", {}), previous_response_id=None,
                    max_output_tokens=request.get("max_output_tokens"), reasoning=request.get("reasoning"),
                    text=request.get("text", {"format": {"type": "text"}}), truncation=request.get("truncation", "disabled"),
                    store=False, usage={"input_tokens": reply["input_tokens"],
                        "input_tokens_details": {"cached_tokens": 0}, "output_tokens": reply["output_tokens"],
                        "output_tokens_details": {"reasoning_tokens": 0},
                        "total_tokens": reply["input_tokens"] + reply["output_tokens"]})
    # store=false reflects the fixture's deliberately non-persistent service.
    pending = dict(response, status="in_progress", output=[], usage=None)
    pending_item = dict(item, status="in_progress", content=[])
    empty_part = dict(part, text="")
    events = [(0, {"type": "response.created", "response": pending}),
              (0, {"type": "response.in_progress", "response": pending}),
              (0, {"type": "response.output_item.added", "output_index": 0, "item": pending_item}),
              (0, {"type": "response.content_part.added", "item_id": item_id, "output_index": 0, "content_index": 0, "part": empty_part})]
    for i in range(0, len(text), reply["chunk_chars"]):
        events.append((0 if i == 0 else reply["chunk_interval_ms"],
                       {"type": "response.output_text.delta", "item_id": item_id, "output_index": 0,
                        "content_index": 0, "delta": text[i:i + reply["chunk_chars"]], "logprobs": []}))
    events += [(reply["end_delay_ms"], {"type": "response.output_text.done", "item_id": item_id, "output_index": 0,
                 "content_index": 0, "text": text, "logprobs": []}),
               (0, {"type": "response.content_part.done", "item_id": item_id, "output_index": 0, "content_index": 0, "part": part}),
               (0, {"type": "response.output_item.done", "output_index": 0, "item": item}),
               (0, {"type": "response.completed", "response": response})]
    frames = []
    for sequence, (delay, event) in enumerate(events):
        event["sequence_number"] = sequence
        frames.append((delay, ("event: " + event["type"] + "\ndata: " + json.dumps(event, ensure_ascii=False) + "\n\n").encode()))
    return response, frames
