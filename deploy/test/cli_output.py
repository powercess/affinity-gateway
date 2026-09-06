"""Extract assistant text from actual CLI output without import side effects."""
import json


def reply_text(binary, stdout):
    if binary in ("omp", "pi", "qwen", "kimi"):
        return stdout.strip()
    replies = []
    for line in stdout.splitlines():
        try:
            event = json.loads(line)
        except ValueError:
            continue
        if not isinstance(event, dict):
            continue
        if binary == "opencode" and event.get("type") == "text":
            replies.append(event.get("part", {}).get("text", ""))
        if binary == "claude" and event.get("type") == "result" and not event.get("is_error"):
            replies.append(event.get("result", ""))
    return "".join(replies)
