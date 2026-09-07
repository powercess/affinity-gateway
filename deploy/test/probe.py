"""Checks the real new-api path after manual isolated instance initialization."""
import json
import os
import urllib.request
import uuid
import time
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

key = os.environ.get("TEST_API_KEY") or (Path("/artifacts/token").read_text().strip() if Path("/artifacts/token").exists() else "")
if not key:
    raise SystemExit("Set TEST_API_KEY after initializing the isolated new-api; see deploy/README.md")


def call(model, session, stream=False):
    request = urllib.request.Request(
        "http://affinity-gateway:8236/v1/chat/completions",
        data=json.dumps({"model": model, "stream": stream,
                         "messages": [{"role": "user", "content": "affinity probe"}]}).encode(),
        headers={"Authorization": "Bearer " + key, "X-Session-Id": session,
                 "Content-Type": "application/json"})
    with urllib.request.urlopen(request, timeout=30) as response:
        if not stream:
            return json.loads(json.load(response)["choices"][0]["message"]["content"])
        text = ""
        done = False
        for line in response:
            if line.strip() == b"data: [DONE]":
                done = True
                break
            if line.startswith(b"data: "):
                chunk = json.loads(line[6:])
                for choice in chunk.get("choices", []):
                    text += choice.get("delta", {}).get("content", "")
        assert done, "SSE missing DONE"
        return json.loads(text)


session = str(uuid.uuid4())
results = [call("affinity-test", session) for _ in range(3)]
assert len({x["route"] for x in results}) == 1, "channel changed"
assert len({x["session"] for x in results}) == 1, "session changed"
assert results[0]["session"], "missing supplier session"
assert all(x["internal"] is None and x["generic"] is None for x in results), "internal header leaked"
assert call("affinity-test", session, True) == results[0], "stream path differs"
stripped = call("strip-test", str(uuid.uuid4()))
assert stripped["route"] == "no-session"
assert all(stripped[k] is None for k in ("session", "internal", "generic")), "strip policy failed"
print("PASS: repeated route/session, header isolation, SSE integrity and strip policy")

# Independent chats need distinct identities, not necessarily distinct channels.
sessions = [str(uuid.uuid4()) for _ in range(8)]
def check_chat(sid):
    items = []
    for _ in range(3):
        items.append(call("affinity-test", sid))
        time.sleep(float(os.environ.get("TEST_TURN_DELAY", "0")))
    assert all(item == items[0] for item in items), f"concurrent chat drifted: routes={[x['route'] for x in items]}"
    return items[0]
with ThreadPoolExecutor(max_workers=4) as pool:
    chats = list(pool.map(check_chat, sessions))
assert len({chat["session"] for chat in chats}) == len(sessions), "independent chats merged"
print(f"PASS: {len(sessions)} chats x 3 turns, 4 concurrent workers, distinct identities; routes={sorted({x['route'] for x in chats})}")
