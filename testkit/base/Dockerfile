# syntax=docker/dockerfile:1
#
# testkit/base — shared harness layer.
#
#   * bun: TS/JS package manager, runtime, and node replacement
#     (/usr/local/bin/node -> bun, so node shebangs and postinstalls work)
#   * uv: Python interpreter (prebuilt, no compile) + package manager
#   * toolchains come from the vendors' official images, pinned by version tag
#   * Debian bookworm; glibc is required by harness prebuilt binaries
FROM debian:bookworm-slim

SHELL ["/bin/bash", "-euo", "pipefail", "-c"]

ARG PYTHON_VERSION=3.13.15
# Networks that transparently rewrite the official mirror make apt's hash
# checks fail intermittently; the retry loop absorbs it. Accepts a full URI,
# e.g. --build-arg APT_MIRROR=http://mirror.rackspace.com.
ARG APT_MIRROR=http://deb.debian.org

RUN set -eux; \
    mirror="${APT_MIRROR}"; \
    case "$mirror" in *://*) ;; *) mirror="http://${mirror}" ;; esac; \
    if [ "$mirror" != "http://deb.debian.org" ]; then \
        for f in /etc/apt/sources.list /etc/apt/sources.list.d/debian.sources; do \
            [ -f "$f" ] && sed -i "s|http://deb.debian.org|${mirror}|g" "$f"; \
        done; \
    fi; \
    for attempt in 1 2 3 4 5; do \
        if apt-get -o Acquire::By-Hash=yes -o Acquire::Retries=3 update \
            && apt-get install -y --no-install-recommends \
                bash ca-certificates curl git jq procps tini; then \
            break; \
        fi; \
        echo "apt attempt ${attempt} failed (mirror ${mirror}); retrying" >&2; \
        apt-get clean; rm -rf /var/lib/apt/lists/*; sleep 3; \
    done; \
    for bin in bash curl git jq tini; do command -v "$bin" >/dev/null || exit 1; done; \
    rm -rf /var/lib/apt/lists/*

# ---- bun, from the official image -------------------------------------------
COPY --from=oven/bun:1.4.2-debian /usr/local/bin/bun /usr/local/bin/bun
RUN ln -sf /usr/local/bin/bun /usr/local/bin/bunx \
    && ln -sf /usr/local/bin/bun /usr/local/bin/node \
    && bun --version

# ---- uv, from the official distroless image ---------------------------------
COPY --from=ghcr.io/astral-sh/uv:0.12.12 /uv /uvx /usr/local/bin/
RUN uv --version

# ---- Python runtime: uv-managed standalone CPython, one shared venv ---------
ENV UV_PYTHON_INSTALL_DIR=/opt/python \
    UV_LINK_MODE=copy \
    UV_PYTHON_DOWNLOADS=automatic
RUN uv python install "${PYTHON_VERSION}" \
    && uv venv /opt/venv --python "${PYTHON_VERSION}" --seed

ENV PATH="/opt/venv/bin:${PATH}"

# Docker's ENV survives exec'd entrypoints; this keeps interactive login shells
# (docker run -it ... bash -l) working too, since Debian's /etc/profile resets PATH.
RUN printf 'export PATH="/opt/venv/bin:$PATH"\n' > /etc/profile.d/testkit.sh

# Shared harness entrypoint contract. Kept in the base image so every harness
# adapter sources the same file; changing it rebuilds harnesses, not runtimes.
COPY contract.sh /opt/testkit/contract.sh

# Record what actually got installed, for debugging a run after the fact.
RUN mkdir -p /opt/testkit \
    && { echo "bun $(bun --version)"; echo "node-compat $(node -e 'console.log(process.versions.node)')"; \
         echo "uv $(uv --version | awk '{print $2}')"; echo "python $(python3 --version | awk '{print $2}')"; } \
        > /opt/testkit/versions.txt

WORKDIR /work
ENTRYPOINT ["/usr/bin/tini", "--"]
CMD ["bash"]
