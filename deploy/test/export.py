"""Export only sanitized test evidence; never export the shared token file."""
import json
import os
from pathlib import Path
import shutil
import sys

run = sys.argv[1] if len(sys.argv) > 1 else Path("/artifacts/latest-run").read_text().strip()
directory = Path("/artifacts") / run
cases = json.loads((directory / "runs.json").read_text())
rows = [json.loads(line) for line in Path("/artifacts/capture.jsonl").read_text().splitlines()]
traces = {r["headers"].get("x-test-request-id") for r in rows if r["stage"] == "native"
          and any(case["start_ns"] <= r["time_ns"] <= case["end_ns"] for case in cases)}
selected = [r for r in rows if r["headers"].get("x-test-request-id") in traces]
output = "".join(json.dumps(r) + "\n" for r in selected)
token = Path("/artifacts/token").read_text().strip()
assert token not in output, "Refusing credential-bearing evidence export"
(directory / "capture.jsonl").write_text(output)
for name in (("boundaries.log", "stress.json") if os.environ.get("EXPORT_PROTOCOL_RESULTS") == "1" else ()):
    if (Path("/artifacts") / name).exists():
        shutil.copyfile(Path("/artifacts") / name, directory / name)
print(json.dumps({"run": run, "captured_events": len(selected), "requests": len(traces), "token_scan": "PASS"}))
