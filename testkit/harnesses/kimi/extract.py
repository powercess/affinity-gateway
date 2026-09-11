"""Extract the assistant reply from `kimi --quiet` stdout."""
import re
import sys

ANSI = re.compile(r"\x1b\[[0-9;?]*[A-Za-z]")


def main() -> int:
    with open(sys.argv[1], encoding="utf-8", errors="replace") as handle:
        text = ANSI.sub("", handle.read())
    sys.stdout.write(text.strip())
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
