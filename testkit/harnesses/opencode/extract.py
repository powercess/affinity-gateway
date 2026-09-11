"""Extract the assistant reply from `opencode run --format json` stdout."""
import json
import re
import sys

ANSI = re.compile(r"\x1b\[[0-9;?]*[A-Za-z]")


def main() -> int:
    parts = []
    with open(sys.argv[1], encoding="utf-8", errors="replace") as handle:
        for line in handle:
            line = ANSI.sub("", line).strip()
            if not line.startswith("{"):
                continue
            try:
                event = json.loads(line)
            except ValueError:
                continue
            if event.get("type") == "text":
                parts.append(event.get("part", {}).get("text", ""))
    sys.stdout.write("".join(parts))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
