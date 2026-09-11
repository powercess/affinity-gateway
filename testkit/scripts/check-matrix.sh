#!/usr/bin/env bash
# Fast, docker-free consistency gate: matrix.json must agree with the lockfiles
# and compose build args that actually drive the builds.
#
# This is what stops "the manifest says 1.18.29 but the image installed
# something else", which is the usual way a harness matrix silently rots.
set -euo pipefail

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

failures=0

fail() {
    printf 'FAIL %s\n' "$*" >&2
    failures=$((failures + 1))
}

ok() {
    printf 'ok   %s\n' "$*"
}

expect_eq() {
    local label="$1" expected="$2" actual="$3"
    if [ "$expected" = "$actual" ]; then
        ok "$label"
    else
        fail "$label: expected '$expected', got '$actual'"
    fi
}

while IFS=$'\t' read -r name distribution package version lockfile context dockerfile; do
    case "$distribution" in
        bun)
            expect_eq "$name package.json" "$version" \
                "$(jq -r --arg p "$package" '.dependencies[$p] // "missing"' "$TESTKIT_DIR/$context/package.json")"
            if grep -qF "\"${package}@${version}\"" "$TESTKIT_DIR/$lockfile"; then
                ok "$name bun.lock"
            else
                fail "$name bun.lock does not pin ${package}@${version}"
            fi
            if jq -e --arg p "$package" '.trustedDependencies // [] | index($p)' "$TESTKIT_DIR/$context/package.json" >/dev/null; then
                ok "$name lifecycle scripts trusted"
            else
                fail "$name must list '$package' in trustedDependencies or bun blocks its postinstall"
            fi
            ;;
        uv)
            expect_eq "$name pyproject.toml" "${package}==${version}" \
                "$(grep -oE "${package}==[0-9][^\"]*" "$TESTKIT_DIR/$context/pyproject.toml" | head -1)"
            if grep -qF "specifier = \"==${version}\"" "$TESTKIT_DIR/$lockfile"; then
                ok "$name uv.lock"
            else
                fail "$name uv.lock does not pin ${package}==${version}"
            fi
            ;;
        *)
            fail "$name has unknown distribution '$distribution'"
            ;;
    esac

    if [ -f "$TESTKIT_DIR/$lockfile" ]; then
        ok "$name lockfile present"
    else
        fail "$name lockfile $lockfile is missing"
    fi

    if grep -q 'ARG HARNESS_VERSION' "$TESTKIT_DIR/$dockerfile"; then
        ok "$name Dockerfile takes HARNESS_VERSION"
    else
        fail "$name Dockerfile is missing 'ARG HARNESS_VERSION'"
    fi

    var="$(printf '%s' "$name" | tr '[:lower:]-' '[:upper:]_')_VERSION"
    if grep -q "HARNESS_VERSION: \${${var}" "$TESTKIT_DIR/compose.poc.yaml"; then
        ok "$name compose wiring"
    else
        fail "$name is not wired to \${${var}} in compose.poc.yaml"
    fi

    # session_identity is what the *-probe case checks against real traffic, so
    # it has to exist and use the closed vocabulary the probe understands.
    supply="$(jq -r --arg n "$name" '.harnesses[] | select(.name == $n) | .session_identity.supply // "missing"' "$TESTKIT_DIR/harnesses/matrix.json")"
    case "$supply" in
        header | declared_body | none) ok "$name session_identity.supply = $supply" ;;
        *) fail "$name session_identity.supply must be header|declared_body|none (got '$supply')" ;;
    esac
    if jq -e --arg n "$name" '.harnesses[] | select(.name == $n) | (.session_identity.fields | type == "array")' "$TESTKIT_DIR/harnesses/matrix.json" >/dev/null; then
        ok "$name session_identity.fields is a list"
    else
        fail "$name session_identity.fields must be an array"
    fi
done < <(jq -r '.harnesses[] | [.name, .distribution, .package, .version, .lockfile, .context, .dockerfile] | @tsv' "$TESTKIT_DIR/harnesses/matrix.json")

# Capture image: the Dockerfile ARG default must match the hash-locked set.
mitmproxy_arg="$(grep -E '^ARG MITMPROXY_VERSION=' "$TESTKIT_DIR/capture/Dockerfile" | head -1 | cut -d= -f2)"
mitmproxy_pin="$(grep -E '^mitmproxy==' "$TESTKIT_DIR/capture/requirements.in" | head -1)"
expect_eq "capture mitmproxy pin" "mitmproxy==${mitmproxy_arg}" "$mitmproxy_pin"
if grep -qE "^mitmproxy==${mitmproxy_arg}([[:space:]]|\\\\)" "$TESTKIT_DIR/capture/requirements.lock"; then
    ok "capture requirements.lock"
else
    fail "capture requirements.lock does not pin mitmproxy==${mitmproxy_arg}"
fi

if [ "$failures" -eq 0 ]; then
    echo "testkit: matrix is consistent"
else
    echo "testkit: $failures problem(s) found" >&2
    exit 1
fi
