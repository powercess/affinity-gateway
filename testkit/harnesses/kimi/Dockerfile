# syntax=docker/dockerfile:1
#
# Harness image: Kimi CLI (PyPI, installed and locked with uv).
#
# "uv sync --frozen" installs exactly uv.lock into the shared /opt/venv and
# fails if the lock would need to change. No separate Python image is pulled:
# the interpreter already comes from the base.
ARG BASE_IMAGE=testkit/base:local
FROM ${BASE_IMAGE}

ARG HARNESS_VERSION
RUN test -n "${HARNESS_VERSION}" || { echo "HARNESS_VERSION build arg is required" >&2; exit 1; }

WORKDIR /harness
COPY pyproject.toml uv.lock ./
RUN grep -q "specifier = \"==${HARNESS_VERSION}\"" uv.lock \
    || { echo "manifest version ${HARNESS_VERSION} != uv.lock" >&2; exit 1; }

ENV UV_PROJECT_ENVIRONMENT=/opt/venv
RUN --mount=type=cache,target=/root/.cache/uv \
    uv sync --frozen --no-dev \
    && test -x /opt/venv/bin/kimi \
    || { echo "uv sync did not install the kimi entrypoint" >&2; exit 1; }

COPY run.sh extract.py /harness/
RUN chmod +x /harness/run.sh

ENTRYPOINT ["/usr/bin/tini", "--", "/harness/run.sh"]
