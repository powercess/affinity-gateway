# syntax=docker/dockerfile:1
#
# L7 tap image: one mitmdump reverse proxy per hop, driven by CAPTURE_HOPS.
# mitmproxy itself is hash-locked via requirements.lock so a rebuild cannot
# silently pull a different dependency tree.
FROM python:3.13-slim-bookworm

ARG MITMPROXY_VERSION=12.2.3

WORKDIR /tap
COPY requirements.in requirements.lock ./
RUN grep -q "mitmproxy==${MITMPROXY_VERSION}" requirements.lock \
    || { echo "requested mitmproxy ${MITMPROXY_VERSION} != requirements.lock" >&2; exit 1; }
RUN --mount=type=cache,target=/root/.cache/pip \
    pip install --no-cache-dir --require-hashes -r requirements.lock

COPY addon.py run_hops.py /tap/

ENTRYPOINT ["python", "/tap/run_hops.py"]
