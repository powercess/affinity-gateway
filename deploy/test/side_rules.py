"""Switch only the dedicated harness-main profile; restore with revision CAS."""
import base64
import json
from pathlib import Path
import sys
import urllib.request

URL = "http://affinity-gateway:8240/api/inbound-rules"
HEADERS = {
    "Authorization": "Basic " + base64.b64encode(
        b"admin:isolated-side-test-password-not-production").decode(),
    "Content-Type": "application/json",
}
BACKUP = Path("/artifacts/side-original-rules.json")


def request(value=None):
    req = urllib.request.Request(URL, headers=HEADERS,
        data=None if value is None else json.dumps(value).encode(),
        method="GET" if value is None else "PUT")
    with urllib.request.urlopen(req, timeout=10) as response:
        return next(row for row in json.load(response)["items"] if row["profile"] == "harness-main")


def main():
    if sys.argv[1:] == ["metadata"]:
        if BACKUP.exists():
            raise SystemExit("Existing backup: restore it before switching again")
        original = request()
        updated = dict(original, mode="strict", metadata=True, conversation=False, cache_key=False,
                       headers=[dict(h, enabled=False, strip=True) for h in original["headers"]])
        # Save before mutation so interrupted runs still have a restoration record.
        BACKUP.write_text(json.dumps({"original": original, "active_revision": original["revision"] + 1}))
        saved = request(updated)
    elif sys.argv[1:] == ["restore"]:
        backup = json.loads(BACKUP.read_text())
        # Never use the current revision to overwrite another actor's update.
        saved = request(dict(backup["original"], revision=backup["active_revision"]))
        BACKUP.unlink()
    else:
        raise SystemExit("Usage: side_rules.py metadata|restore")
    print(json.dumps(saved))


if __name__ == "__main__":
    main()
