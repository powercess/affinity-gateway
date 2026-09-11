# syntax=docker/dockerfile:1
#
# Harness image: OpenCode (npm registry, installed and locked with bun).
#
# The version is pinned twice on purpose: matrix.json is the matrix source of
# truth and bun.lock is the transitive-dependency truth. "bun install
# --frozen-lockfile" makes the build fail rather than resolve anything new.
ARG BASE_IMAGE=testkit/base:local
FROM ${BASE_IMAGE}

ARG HARNESS_VERSION
RUN test -n "${HARNESS_VERSION}" || { echo "HARNESS_VERSION build arg is required" >&2; exit 1; }

WORKDIR /harness
COPY package.json bun.lock ./
RUN bun -e 'const p = require("/harness/package.json"); const v = process.env.HARNESS_VERSION; \
if (p.dependencies["opencode-ai"] !== v) { console.error(`manifest ${v} != package.json ${p.dependencies["opencode-ai"]}`); process.exit(1) }'

ENV PATH="/harness/node_modules/.bin:${PATH}" \
    OPENCODE_DISABLE_AUTOUPDATE=true \
    OPENCODE_DISABLE_MODELS_FETCH=true

# Install, verify and prune in ONE layer. A separate later "rm" would leave the
# ~550MB of unusable platform binaries inside the install layer.
#
# * --production skips devDependencies; lifecycle scripts run only because the
#   package is listed in trustedDependencies (bun blocks them by default).
# * bun materialises every optional platform package (darwin/windows/musl/
#   baseline). The postinstall has already placed the real binary inside
#   opencode-ai, so the rest can go; "opencode --version" proves it still runs.
RUN --mount=type=cache,target=/root/.bun/install/cache \
    bun install --frozen-lockfile --production \
    && { test -x /harness/node_modules/.bin/opencode \
         || { echo "bun install did not produce the opencode binary (postinstall blocked?)" >&2; exit 1; }; } \
    && rm -rf /harness/node_modules/opencode-linux-* \
              /harness/node_modules/opencode-darwin-* \
              /harness/node_modules/opencode-windows-* \
    && opencode --version

COPY run.sh extract.py /harness/
RUN chmod +x /harness/run.sh

ENTRYPOINT ["/usr/bin/tini", "--", "/harness/run.sh"]
