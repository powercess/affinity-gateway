"""Wait for the isolated new-api HTTP service before the existing initializer."""
import runpy
import time
import urllib.error
import urllib.request

for attempt in range(60):
    try:
        with urllib.request.urlopen("http://new-api:3000/api/status", timeout=2) as response:
            if response.status == 200:
                break
    except (OSError, urllib.error.URLError):
        pass
    time.sleep(1)
else:
    raise SystemExit("new-api did not become ready")
runpy.run_path("/test/seed.py", run_name="__main__")
