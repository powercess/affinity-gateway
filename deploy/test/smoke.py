"""No initialization required: verifies real proxy listeners and mock routing."""
import json
import urllib.error
import urllib.request


def request(path, headers=None):
    req = urllib.request.Request("http://affinity-gateway:8237" + path,
        data=b'{"model":"affinity-test","messages":[{"role":"user","content":"Synthetic smoke test"}]}',
        headers={"Content-Type": "application/json", **(headers or {})})
    return urllib.request.urlopen(req, timeout=10)


internal = "sa:v1:" + "a" * 64
values = []
for site in ("opencode-a", "opencode-a", "opencode-b", "no-session"):
    with request("/r/" + site + "/v1/chat/completions", {"X-Session-Affinity": internal}) as response:
        value = json.loads(json.load(response)["choices"][0]["message"]["content"])
    assert value["route"] == site
    assert value["internal"] is None and value["generic"] is None
    values.append(value["session"])
assert values[0] == values[1] and values[0] != values[2]
assert values[0] and values[2] and values[3] is None
for path, code in (("/r/unknown/v1/chat/completions", 404), ("/r/opencode-a/v1/chat/completions", 400)):
    try:
        request(path)
    except urllib.error.HTTPError as error:
        assert error.code == code
    else:
        raise AssertionError("request should have been rejected")
with urllib.request.urlopen("http://affinity-gateway:8236/api/status", timeout=10) as response:
    assert json.load(response)["success"], "new-api not ready"
print("PASS: live routes, prefix stripping, stable/site-isolated IDs, header stripping, rejection, new-api connectivity")
