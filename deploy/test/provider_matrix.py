"""Real CLI/provider matrix. Only native configuration, no injected session IDs.

Two lanes: direct protocol control and the actual strict affinity stack.
Separate disposable HOME/workspace per registration and lane; each resume is
a new CLI process reading the client's own saved conversation.
"""
import json
import os
from pathlib import Path
import subprocess
import time
import uuid

from cli_output import reply_text

TOKEN = Path("/artifacts/token").read_text().strip()
RUN = "matrix-" + str(uuid.uuid4())
OUT = Path("/artifacts") / RUN
OUT.mkdir()
EXPECTED = [r["text"] for r in json.loads(Path("/test/replies.json").read_text())["scenarios"]["resume"]]

# Explicitly supported registrations: actual packages/API enums, not invented
# cross-product switches. Built-ins keep their original model metadata/API.
CASES = [
    ("oc-native-openai", "opencode", "openai", "gpt-5.2", None, True),
    ("oc-native-anthropic", "opencode", "anthropic", "claude-sonnet-4-6", None, True),
    ("oc-custom-compatible", "opencode", "fixture", "gpt-4.1", "@ai-sdk/openai-compatible", False),
    ("oc-custom-openai", "opencode", "fixture", "gpt-5.2", "@ai-sdk/openai", False),
    ("oc-custom-anthropic", "opencode", "fixture", "claude-sonnet-4-6", "@ai-sdk/anthropic", False),
    ("omp-native-openai-4", "omp", "openai", "gpt-4.1", None, True),
    ("omp-native-openai-5", "omp", "openai", "gpt-5.2", None, True),
    ("omp-native-anthropic", "omp", "anthropic", "claude-sonnet-4-6", None, True),
    ("omp-custom-chat", "omp", "fixture", "gpt-4.1", "openai-completions", False),
    ("omp-custom-responses", "omp", "fixture", "gpt-5.2", "openai-responses", False),
    ("omp-custom-anthropic", "omp", "fixture", "claude-sonnet-4-6", "anthropic-messages", False),
    ("cc-native-anthropic", "claude", "anthropic", "claude-sonnet-4-6", None, True),
    ("pi-native-openai", "pi", "openai", "gpt-5.2", None, True),
    ("pi-native-anthropic", "pi", "anthropic", "claude-sonnet-4-6", None, True),
    ("pi-custom-chat", "pi", "fixture", "gpt-4.1", "openai-completions", False),
    ("pi-custom-responses", "pi", "fixture", "gpt-5.2", "openai-responses", False),
    ("pi-custom-anthropic", "pi", "fixture", "claude-sonnet-4-6", "anthropic-messages", False),
    ("qwen-openai", "qwen", "openai", "gpt-4.1", None, True),
    ("qwen-anthropic", "qwen", "anthropic", "claude-sonnet-4-6", None, True),
    ("kimi-chat", "kimi", "openai_legacy", "gpt-4.1", None, True),
    ("kimi-responses", "kimi", "openai_responses", "gpt-5.2", None, True),
    ("kimi-anthropic", "kimi", "anthropic", "claude-sonnet-4-6", None, True),
    ("kimi-native", "kimi", "kimi", "kimi-for-coding", None, True),
]


def setup(case, lane):
    name, binary, provider, model, adapter, builtin = case
    root = Path("/work") / RUN / lane / name
    root.mkdir(parents=True)
    home = root / "home"
    home.mkdir()
    workspace = root / "workspace"
    workspace.mkdir()
    env = dict(os.environ, HOME=str(home), XDG_CONFIG_HOME=str(home / ".config"),
               XDG_DATA_HOME=str(home / ".local/share"), XDG_CACHE_HOME=str(home / ".cache"))
    env.update(TEST_API_KEY=TOKEN)
    endpoint = "http://capture:" + ("8004" if lane == "direct" else "8005" if lane == "cache-contract" else "8001")
    anthropic = "anthropic" in (adapter or provider) or model.startswith("claude-")
    base = endpoint + ("" if binary != "opencode" and anthropic else "/v1")
    config = {}
    if binary == "opencode":
        registration = {"options": {"baseURL": base, "apiKey": "{env:TEST_API_KEY}"}}
        if not builtin:
            registration.update(npm=adapter, models={model: {"name": model}})
        config = {"provider": {provider: registration}, "model": provider + "/" + model,
                  "permission": {"*": "deny"}, "share": "disabled"}
        path = workspace / "opencode.json"
        path.write_text(json.dumps(config))
        env["OPENCODE_CONFIG"] = str(path)
        env["ANTHROPIC_API_KEY" if anthropic else "OPENAI_API_KEY"] = TOKEN
        command = ["opencode", "run", "--format", "json", "--model", provider + "/" + model]
    elif binary in ("omp", "pi"):
        registration = {"baseUrl": base, "apiKey": "TEST_API_KEY"}
        if not builtin:
            registration.update(api=adapter, models=[{"id": model, "name": model}])
        config = {"providers": {provider: registration}}
        directory = home / (".omp/agent" if binary=="omp" else ".pi/agent")
        directory.mkdir(parents=True)
        (directory / ("models.yml" if binary=="omp" else "models.json")).write_text(json.dumps(config))
        # No compat flags, thinking overrides, artificial IDs or title disable.
        command = [binary, "-p", "--model", provider + "/" + model]
    elif binary == "qwen":
        config={"modelProviders":{provider:[{"id":model,"envKey":"TEST_API_KEY","baseUrl":base}]},
                "security":{"auth":{"selectedType":provider}},"model":{"name":model},"telemetry":{"enabled":False}}
        directory=home/".qwen"; directory.mkdir()
        (directory/"settings.json").write_text(json.dumps(config))
        command=["qwen","--auth-type",provider,"--model",model,"--output-format","text"]
    elif binary == "kimi":
        config={"default_model":"fixture","providers":{"fixture":{"type":provider,"base_url":base,"api_key":TOKEN}},
                "models":{"fixture":{"provider":"fixture","model":model,"max_context_size":262144}},"telemetry":False}
        directory=home/".kimi"; directory.mkdir()
        (directory/"config.json").write_text(json.dumps(config))
        command=["kimi","--config-file",str(directory/"config.json"),"--quiet"]
    else:
        env.update(ANTHROPIC_API_KEY=TOKEN, ANTHROPIC_BASE_URL=endpoint)
        config = {"ANTHROPIC_BASE_URL": endpoint, "ANTHROPIC_API_KEY": "[TEST ENV]"}
        command = ["claude", "-p", "--output-format", "json", "--model", model, "--max-turns", "1"]
    (OUT / f"{lane}-{name}-config.json").write_text(json.dumps(config, indent=2).replace(TOKEN,"[TEST ENV]"))
    return env, workspace, command


def run_case(case, lane, results):
    name, binary, *_ = case
    env, workspace, command = setup(case, lane)
    for phase, turn in (("first", 1), ("resume", 2), ("third", 3), ("new", 1)):
        cmd = command + (["--continue"] if phase in ("resume", "third") else [])
        if binary=="kimi": cmd += ["--prompt"]
        cmd += [f"[mock:resume:{turn}] Synthetic protocol test. Reply with text only; do not use tools."]
        start = time.time_ns()
        try:
            proc = subprocess.run(cmd, cwd=workspace, env=env, text=True, capture_output=True, timeout=60)
            code, stdout, stderr = proc.returncode, proc.stdout, proc.stderr
        except subprocess.TimeoutExpired as error:
            code = 124
            stdout = (error.stdout or b"").decode(errors="replace")
            stderr = (error.stderr or b"").decode(errors="replace")
        text = reply_text(binary, stdout)
        log = (stdout + stderr).replace(TOKEN, "[REDACTED]")
        label = f"{lane}-{name}-{phase}"
        (OUT / (label + ".log")).write_text(log)
        result = dict(case=name, lane=lane, phase=phase, binary=binary, command=cmd,
                      start_ns=start, end_ns=time.time_ns(), exit_code=code,
                      reply_match=code == 0 and text == EXPECTED[turn - 1],
                      actual_reply=text.replace(TOKEN, "[REDACTED]"), output_tail=log[-700:])
        results.append(result)
        (OUT / "runs.json").write_text(json.dumps(results, indent=2))
        print(json.dumps({k: result[k] for k in ("case", "lane", "phase", "exit_code", "reply_match")}), flush=True)


if __name__ == "__main__":
    print("RUN=" + RUN, flush=True)
    results = []
    selected = os.environ.get("MATRIX_CASES", "").split(",")
    for lane in os.environ.get("MATRIX_LANES", "direct,affinity").split(","):
        for case in CASES:
            if selected != [""] and case[0] not in selected:
                continue
            run_case(case, lane, results)
    Path("/artifacts/latest-matrix").write_text(RUN)
    print(json.dumps({"run": RUN, "cases": len(results), "reply_matched": sum(r["reply_match"] for r in results)}), flush=True)
