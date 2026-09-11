#!/usr/bin/env bash
# Shared environment for every testkit script. Source it, do not execute it.
#
# It exists so that matrix.json stays the only place a harness version is
# written down: the scripts export the versions that compose interpolates.
set -euo pipefail

TESTKIT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TESTKIT_DIR="$TESTKIT_ROOT/testkit"
export TESTKIT_ROOT TESTKIT_DIR

# All test output lives under the repo's already-ignored artifacts/ directory.
export TESTKIT_ARTIFACTS_ROOT="${TESTKIT_ARTIFACTS_ROOT:-$TESTKIT_ROOT/artifacts/testkit}"
export TESTKIT_ARTIFACTS="${TESTKIT_ARTIFACTS:-$TESTKIT_ARTIFACTS_ROOT/current}"

export TESTKIT_BASE_IMAGE="${TESTKIT_BASE_IMAGE:-testkit/base:local}"
export TESTKIT_OPENCODE_IMAGE="${TESTKIT_OPENCODE_IMAGE:-testkit/harness-opencode:local}"
export TESTKIT_KIMI_IMAGE="${TESTKIT_KIMI_IMAGE:-testkit/harness-kimi:local}"
export TESTKIT_CAPTURE_IMAGE="${TESTKIT_CAPTURE_IMAGE:-testkit/capture:local}"
export TESTKIT_PCAP_IMAGE="${TESTKIT_PCAP_IMAGE:-testkit/pcap:local}"

if ! command -v jq >/dev/null 2>&1; then
    echo "testkit: jq is required" >&2
    exit 2
fi

_matrix() {
    jq -r "$1" "$TESTKIT_DIR/harnesses/matrix.json"
}

export_matrix_versions() {
    local row name version
    while IFS=$'\t' read -r name version; do
        local var
        var="$(printf '%s' "$name" | tr '[:lower:]-' '[:upper:]_')_VERSION"
        export "${var}=${version}"
    done < <(_matrix '.harnesses[] | [.name, .version] | @tsv')
}

export_matrix_versions

testkit_compose() {
    docker compose -f "$TESTKIT_DIR/compose.poc.yaml" "$@"
}
