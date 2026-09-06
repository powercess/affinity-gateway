"""Run genuine installed CLIs against the isolated stack; preserve failures."""
import json
import os
from pathlib import Path
import subprocess
import time
import uuid

TOKEN = Path("/artifacts/token").read_text().strip()
RUN = str(uuid.uuid4())
OUT = Path("/artifacts") / RUN
OUT.mkdir()
RESULTS = []
RESUME_SCENARIO = os.environ.get("HARNESS_RESUME_SCENARIO") == "1"


def reply_text(binary, stdout):
    if binary == "omp":
        return stdout.strip()
    replies = []
    for line in stdout.splitlines():
        try:
            event = json.loads(line)
        except ValueError:
            continue
        if binary == "opencode" and event.get("type") == "text":
            replies.append(event.get("part", {}).get("text", ""))
        if binary == "claude" and event.get("type") == "result" and not event.get("is_error"):
            replies.append(event.get("result", ""))
    return "".join(replies)


def execute(name, command, env=None):
    expected = None
    if RESUME_SCENARIO and not name.endswith("-version"):
        turn = 3 if name.endswith("-third") else 2 if name.endswith("-resume") else 1
        command = command[:-1] + [f"[mock:resume:{turn}] Synthetic history restoration test. Do not use tools."]
        expected = json.loads(Path("/test/replies.json").read_text())["scenarios"]["resume"][turn - 1]["text"]
    start = time.time_ns()
    stdout = ""
    try:
        proc = subprocess.run(command, env=dict(os.environ, **(env or {})),
                              capture_output=True, text=True, timeout=75)
        code, output = proc.returncode, proc.stdout + proc.stderr
        stdout = proc.stdout
    except subprocess.TimeoutExpired as error:
        code = 124
        output = (error.stdout or b"").decode() + (error.stderr or b"").decode()
    output = output.replace(TOKEN, "[REDACTED]")
    (OUT / (name + ".log")).write_text(output)
    row = dict(name=name, command=command, exit_code=code, start_ns=start,
               end_ns=time.time_ns(), output=output[-2000:])
    if expected is not None:
        row.update(expected_reply=expected, reply_match=code == 0 and reply_text(command[0], stdout) == expected)
    RESULTS.append(row)
    (OUT / "runs.json").write_text(json.dumps(RESULTS, indent=2))
    print(json.dumps(dict(name=name, exit_code=code, output=output[-500:])), flush=True)
    if RESUME_SCENARIO and name.endswith("-resume") and code == 0:
        execute(name.removesuffix("-resume") + "-third", command, env)
    return output


def configure_opencode():
    config = {"$schema": "https://opencode.ai/config.json", "share": "disabled",
              "permission": {"*": "deny"}, "model": "affinity/affinity-test",
              "small_model": "affinity/affinity-test", "enabled_providers": ["affinity"],
              "provider": {"affinity": {"npm": "@ai-sdk/openai-compatible", "name": "Isolated test",
                  "options": {"baseURL": "http://capture:8001/v1", "apiKey": "{env:TEST_API_KEY}"},
                  "models": {"affinity-test": {"name": "Affinity Test", "limit": {"context": 32000, "output": 1024}}}}}}
    Path("/work/opencode.json").write_text(json.dumps(config))


def main():
    print("RUN=" + RUN, flush=True)
    for binary in ("opencode", "claude", "omp"):
        execute(binary + "-version", [binary, "--version"])
    configure_opencode()
    env = {"TEST_API_KEY": TOKEN, "OPENCODE_CONFIG": "/work/opencode.json"}
    base = ["opencode", "run", "--format", "json", "--model", "affinity/affinity-test"]
    execute("opencode-first", base + ["Synthetic affinity test: reply OK. Do not use tools."], env)
    execute("opencode-resume", base + ["--continue", "Synthetic second turn: reply OK."], env)
    execute("opencode-new", base + ["Synthetic independent conversation: reply OK."], env)
    claude_env = {"ANTHROPIC_API_KEY": TOKEN, "ANTHROPIC_BASE_URL": "http://capture:8001",
                  "ANTHROPIC_MODEL": "claude-sonnet-4-6", "ANTHROPIC_SMALL_FAST_MODEL": "claude-sonnet-4-6"}
    base = ["claude", "-p", "--output-format", "json", "--model", "claude-sonnet-4-6",
            "--tools", "", "--strict-mcp-config", "--max-turns", "1"]
    sid = str(uuid.uuid4())
    execute("claude-first", base + ["--session-id", sid, "Synthetic affinity test: reply OK."], claude_env)
    execute("claude-resume", base + ["--resume", sid, "Synthetic second turn: reply OK."], claude_env)
    execute("claude-new", base + ["--session-id", str(uuid.uuid4()), "Synthetic independent conversation: reply OK."], claude_env)
    # JSON is valid YAML. No host configuration or credentials are mounted.
    directory = Path("/root/.omp/agent")
    directory.mkdir(parents=True, exist_ok=True)
    (directory / "models.yml").write_text(json.dumps({"providers": {"affinity": {
        "baseUrl": "http://capture:8001/v1", "api": "openai-completions", "apiKey": "TEST_API_KEY",
        "models": [{"id": "affinity-test", "name": "Affinity Test", "contextWindow": 32000, "maxTokens": 1024}]}}}))
    base = ["omp", "-p", "--provider", "affinity", "--model", "affinity-test", "--no-tools",
            "--no-lsp", "--no-extensions", "--no-skills", "--no-rules", "--no-title", "--thinking", "off"]
    execute("omp-first", base + ["Synthetic affinity test: reply OK. Do not use tools."], env)
    execute("omp-resume", base + ["--continue", "Synthetic second turn: reply OK."], env)
    execute("omp-new", base + ["Synthetic independent conversation: reply OK."], env)
    models = json.loads((directory / "models.yml").read_text())
    provider = models["providers"]["affinity"]
    # Explicit per-conversation configuration, not an assertion of native support.
    provider["headers"] = {"X-Session-Id": str(uuid.uuid4())}
    (directory / "models.yml").write_text(json.dumps(models))
    execute("omp-configured-first", base + ["Synthetic configured session: reply OK."], env)
    execute("omp-configured-resume", base + ["--continue", "Synthetic second turn: reply OK."], env)
    provider["headers"]["X-Session-Id"] = str(uuid.uuid4())
    (directory / "models.yml").write_text(json.dumps(models))
    execute("omp-configured-new", base + ["Synthetic separate configured session: reply OK."], env)
    provider.pop("headers")
    provider.update(baseUrl="http://capture:8001", api="anthropic-messages")
    provider["models"][0]["id"] = "claude-sonnet-4-6"
    (directory / "models.yml").write_text(json.dumps(models))
    base[base.index("affinity-test")] = "claude-sonnet-4-6"
    execute("omp-anthropic-first", base + ["Synthetic Anthropic session: reply OK."], env)
    execute("omp-anthropic-resume", base + ["--continue", "Synthetic second turn: reply OK."], env)
    execute("omp-anthropic-new", base + ["Synthetic independent Anthropic session: reply OK."], env)
    Path("/artifacts/latest-run").write_text(RUN)
    if RESUME_SCENARIO:
        checks = [{"name": r["name"], "exit_code": r["exit_code"], "reply_match": r["reply_match"]}
                  for r in RESULTS if "reply_match" in r]
        (OUT / "recovery.json").write_text(json.dumps(checks, indent=2))
        print(json.dumps({"recovery_cases": len(checks), "reply_matched": sum(r["reply_match"] for r in checks)}, indent=2))


if __name__ == "__main__":
    main()
