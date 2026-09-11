"""Run one mitmdump reverse proxy per configured hop inside a single container.

CAPTURE_HOPS is a JSON array:
    [{"name": "entry", "port": 8001, "target": "http://mock:8000"}, ...]

Each hop is an explicit, in-path L7 tap: the harness points at the tap, and the
tap records and forwards. That is what makes capture deterministic - there is
no packet-sniffing heuristics deciding whether a request was seen.
"""
import json
import os
import signal
import subprocess
import time

ADDON = os.environ.get("CAPTURE_ADDON", "/tap/addon.py")
# Use the console script: "python -m mitmproxy.tools.main mitmdump" silently
# exits 0 without starting a listener on mitmproxy 12.
MITMDUMP = os.environ.get("MITMDUMP_BIN", "mitmdump")


def spawn(hop):
    cmd = [
        MITMDUMP,
        "--quiet",
        "--mode",
        "reverse:" + hop["target"],
        "--listen-host",
        "0.0.0.0",
        "--listen-port",
        str(hop["port"]),
        "--scripts",
        ADDON,
        "--set",
        "confdir=/opt/mitm/" + hop["name"],
        "--set",
        "flow_detail=0",
        "--set",
        "connection_strategy=lazy",
    ]
    env = dict(os.environ, CAPTURE_HOP=hop["name"])
    print("hop %s: :%s -> %s" % (hop["name"], hop["port"], hop["target"]), flush=True)
    return subprocess.Popen(cmd, env=env)


def main():
    hops = json.loads(os.environ["CAPTURE_HOPS"])
    if not hops:
        raise SystemExit("CAPTURE_HOPS is empty; nothing to tap")
    processes = [(hop["name"], spawn(hop)) for hop in hops]

    stopping = False

    def shutdown(signum, _frame):
        nonlocal stopping
        if stopping:
            return
        stopping = True
        print("tap shutting down (signal %s)" % signum, flush=True)
        for _, process in processes:
            process.terminate()
        deadline = time.time() + 10
        for _, process in processes:
            try:
                process.wait(timeout=max(0, deadline - time.time()))
            except subprocess.TimeoutExpired:
                process.kill()
        raise SystemExit(0)

    signal.signal(signal.SIGTERM, shutdown)
    signal.signal(signal.SIGINT, shutdown)

    # If any hop dies, take the whole tap down: a partially-observing tap is
    # worse than a stopped one, because the capture would look plausible.
    while True:
        for name, process in processes:
            code = process.poll()
            if code is not None:
                print("hop %s exited with %d" % (name, code), flush=True)
                shutdown(15, None)
        time.sleep(0.5)


if __name__ == "__main__":
    main()
