#!/usr/bin/env bash
# Build every testkit image. The base is built first and explicitly, because
# the harness images only consume it by tag; compose cannot order image builds
# by FROM dependency.
set -euo pipefail

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

BUILD_ARGS=()
if [ -n "${TESTKIT_APT_MIRROR:-}" ]; then
    BUILD_ARGS+=(--build-arg "APT_MIRROR=${TESTKIT_APT_MIRROR}")
    echo "testkit: apt mirror override -> ${TESTKIT_APT_MIRROR}"
fi

echo "==> base ${TESTKIT_BASE_IMAGE}"
docker build "${BUILD_ARGS[@]}" -f "$TESTKIT_DIR/base/Dockerfile" -t "$TESTKIT_BASE_IMAGE" "$TESTKIT_DIR/base"

echo "==> capture/pcap/harness images"
testkit_compose --profile harness build

echo "==> built:"
docker images --format '{{.Repository}}:{{.Tag}}' \
    | grep -E '^testkit/' | sort || true
