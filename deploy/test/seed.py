"""Initialize ONLY the pinned isolated Compose new-api, then run the probe.

Uses fixed disposable test credentials; never point this at a production server.
"""
import http.cookiejar
import json
import os
import runpy
import time
import urllib.error
import urllib.request
import uuid

BASE = "http://new-api:3000"
USER = "affinitytest"
PASSWORD = "Isolated-Affinity-Test-Only-2026"
client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
user_id = None
access_token = None


def api(path, data=None, method=None):
    headers = {"Content-Type": "application/json"}
    if user_id is not None:
        headers["New-Api-User"] = str(user_id)
    if access_token:
        headers["Authorization"] = "Bearer " + access_token
    req = urllib.request.Request(BASE + path, headers=headers,
        data=None if data is None else json.dumps(data).encode(), method=method)
    with client.open(req, timeout=30) as response:
        result = json.load(response)
    if not result.get("success"):
        raise RuntimeError(f"{path}: {result.get('message')}")
    return result.get("data")


if not api("/api/setup")["status"]:
    api("/api/setup", {"username": USER, "password": PASSWORD,
        "confirmPassword": PASSWORD, "SelfUseModeEnabled": True, "DemoSiteEnabled": False})
login = api("/api/user/login", {"username": USER, "password": PASSWORD})
user_id = login.get("user", login)["id"]
access_token = login.get("access_token")

rules = [{"name": "affinity-integration", "model_regex": ["^affinity-test$", "^claude-sonnet-4-6$", "^gpt-4\\.1(-mini)?$", "^gpt-5\\.2$", "^gpt-5\\.4-nano$", "^claude-haiku-4-5.*$"],
    "path_regex": ["^/v1/chat/completions$", "^/v1/messages$", "^/v1/responses$"], "ttl_seconds": 300,
    "key_sources": [{"type": "request_header", "key": "X-Session-Affinity", "path": ""}],
    "param_override_template": None, "user_agent_include": [], "value_regex": "",
    "skip_retry_on_failure": True,
    "include_using_group": True, "include_model_name": True, "include_rule_name": True}]
for name, value in {
    "channel_affinity_setting.enabled": "true",
    "channel_affinity_setting.rules": json.dumps(rules),
    "ModelRatio": json.dumps({m: 1 for m in ("affinity-test", "strip-test", "kimi-for-coding", "claude-sonnet-4-6", "gpt-4.1", "gpt-4.1-mini", "gpt-5.2", "gpt-5.4-nano", "claude-haiku-4-5", "claude-haiku-4-5-20251001")}),
    "CompletionRatio": json.dumps({m: 1 for m in ("affinity-test", "strip-test", "kimi-for-coding", "claude-sonnet-4-6", "gpt-4.1", "gpt-4.1-mini", "gpt-5.2", "gpt-5.4-nano", "claude-haiku-4-5", "claude-haiku-4-5-20251001")}),
}.items():
    api("/api/option/", {"key": name, "value": value}, "PUT")

existing = {c["name"]: c for c in api("/api/channel/?page_size=100")["items"]}
for route, model in [("opencode-a", "affinity-test"), ("opencode-b", "affinity-test"), ("no-session", "strip-test"),
                     ("opencode-a", "claude-sonnet-4-6"), ("opencode-b", "claude-sonnet-4-6")]:
    name = "affinity-test-" + route + ("-anthropic" if model.startswith("claude-") else "")
    channel = {
            "name": name, "type": 14 if model.startswith("claude-") else 1, "key": "mock-only", "status": 1,
            "base_url": "http://capture:8003/r/" + route,
            "models": ("affinity-test,gpt-4.1,gpt-4.1-mini,gpt-5.2,gpt-5.4-nano,kimi-for-coding" if model == "affinity-test" else
                       "claude-sonnet-4-6,claude-haiku-4-5,claude-haiku-4-5-20251001" if model.startswith("claude-") else model),
            "group": "default", "priority": 0, "weight": 1,
            "header_override": json.dumps({"X-Test-Request-Id": "{client_header:X-Test-Request-Id}"})}
    if name not in existing:
        api("/api/channel/", {"mode": "single", "channel": channel})
    else:
        channel.pop("status")
        api("/api/channel/", dict(channel, id=existing[name]["id"]), "PUT")

tokens = api("/api/token/?page_size=100")["items"]
token = next((t for t in tokens if t["name"] == "affinity-integration"), None)
if token is None:
    api("/api/token/", {"name": "affinity-integration", "expired_time": -1,
        "unlimited_quota": True, "group": "default", "model_limits_enabled": False})
    token = next(t for t in api("/api/token/?page_size=100")["items"] if t["name"] == "affinity-integration")
value = api(f"/api/token/{token['id']}/key", {})
os.environ["TEST_API_KEY"] = value["key"] if isinstance(value, dict) else value
print("Seed ready: isolated admin, five mock channels, affinity rule and test token (credentials omitted)")
# This version asynchronously refreshes the channel cache after creation.
# Wait only during setup; the actual probe has no retry that could mask failures.
readiness_session = "seed-readiness-" + str(uuid.uuid4())
for attempt in range(36):
    req = urllib.request.Request("http://affinity-gateway:8236/v1/chat/completions",
        data=json.dumps({"model": "affinity-test", "messages": [{"role": "user", "content": "ready"}]}).encode(),
        headers={"Content-Type": "application/json", "Authorization": "Bearer " + os.environ["TEST_API_KEY"],
                 "X-Session-Id": readiness_session})
    try:
        with urllib.request.urlopen(req, timeout=10) as response:
            json.load(response)
        break
    except urllib.error.HTTPError as error:
        if error.code != 503 or attempt == 35:
            raise RuntimeError(f"readiness HTTP {error.code}: {error.read().decode()}") from None
        time.sleep(2)
if os.environ.get("SEED_ONLY") == "1":
    # Disposable token stays inside the test volume, never in reports or stdout.
    fd = os.open("/artifacts/token", os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, "w") as file:
        file.write(os.environ["TEST_API_KEY"])
else:
    runpy.run_path("/test/probe.py", run_name="__main__")
