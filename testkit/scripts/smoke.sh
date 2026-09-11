#!/usr/bin/env bash
# End-to-end proof-of-concept run: bring up mock + L7 tap + L4 pcap, run the
# harness matrix, then assert per-case expectations and capture integrity.
# Everything lands in artifacts/testkit/runs/<run-id>/.
#
# Prerequisite: testkit/scripts/build.sh
set -euo pipefail

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

RUN_ID="${TESTKIT_RUN_ID:-$(date -u +%Y%m%dT%H%M%SZ)}"
export TESTKIT_ARTIFACTS="$TESTKIT_ARTIFACTS_ROOT/runs/$RUN_ID"
rm -rf "$TESTKIT_ARTIFACTS"
mkdir -p "$TESTKIT_ARTIFACTS"/{logs,replies,pcap,supplier,home,identity}
printf '%s\n' "$RUN_ID" > "$TESTKIT_ARTIFACTS/run-id"

capture="$TESTKIT_ARTIFACTS/capture.jsonl"
case_runs="$TESTKIT_ARTIFACTS/case-runs.jsonl"
: > "$case_runs"

# Resolved toolchain versions for this run, read from the image that actually
# ran. The base Dockerfile is the source of truth for what those versions are.
docker run --rm "$TESTKIT_BASE_IMAGE" cat /opt/testkit/versions.txt \
    > "$TESTKIT_ARTIFACTS/toolchains.txt" 2>/dev/null || true

compose=(docker compose -f "$TESTKIT_DIR/compose.poc.yaml")
cleanup() {
    "${compose[@]}" --profile harness down --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "==> testkit run ${RUN_ID}"
echo "    artifacts: ${TESTKIT_ARTIFACTS}"

"${compose[@]}" up -d --wait mock tap pcap

api_key="$(jq -r '.api_key_sentinel' "$TESTKIT_DIR/cases.json")"

capture_lines() {
    if [ -f "$capture" ]; then wc -l < "$capture"; else echo 0; fi
}

while IFS=$'\t' read -r name conversation service model protocol resume prompt; do
    # The home directory is keyed by conversation, not by case: a resume case
    # must see the session state written by the first turn of that conversation.
    home="$TESTKIT_ARTIFACTS/home/$conversation"
    mkdir -p "$home"
    from=$(( $(capture_lines) + 1 ))
    echo "--> ${name} (${service}, ${protocol}, resume=${resume})"

    code=0
    "${compose[@]}" --profile harness run --rm -T --no-deps \
        -v "$home:/harness-home" \
        -e "CASE_NAME=$name" \
        -e "PROMPT=$prompt" \
        -e "RESUME=$resume" \
        -e "MODEL=$model" \
        -e "PROTOCOL=$protocol" \
        -e "API_KEY=$api_key" \
        "$service" < /dev/null \
        > "$TESTKIT_ARTIFACTS/replies/$name.txt" \
        2> "$TESTKIT_ARTIFACTS/logs/$name.log" || code=$?

    # Let the tap flush the last response entry before snapshotting the window.
    sleep 0.5
    to=$(capture_lines)

    # Case attribution: the tap writes one flat NDJSON file, so each case's
    # slice of it is recorded here. Probes and future feature scenarios rely on
    # this to know which traffic belongs to them.
    jq -cn --arg case "$name" --argjson exit "$code" --argjson from "$from" --argjson to "$to" \
        '{case: $case, exit: $exit, from: $from, to: $to}' >> "$case_runs"

    if [ "$code" -eq 0 ]; then
        echo "    harness exit 0"
    else
        echo "    harness exit non-zero (see logs/${name}.log)" >&2
    fi
done < <(jq -r '.cases[] | [.name, .conversation, .service, .model, .protocol, (if .resume then "1" else "0" end), .prompt] | @tsv' "$TESTKIT_DIR/cases.json")

# Stop the writers before reading the capture files back.
"${compose[@]}" stop pcap tap >/dev/null

if [ -s "$TESTKIT_ARTIFACTS/pcap/entry.pcap" ]; then
    docker run --rm -v "$TESTKIT_ARTIFACTS:/artifacts" "$TESTKIT_PCAP_IMAGE" \
        -r /artifacts/pcap/entry.pcap -nn -q > "$TESTKIT_ARTIFACTS/pcap/flows.txt" 2>/dev/null || true
fi

python3 "$TESTKIT_DIR/scripts/assert.py" \
    "$TESTKIT_ARTIFACTS" \
    "$TESTKIT_DIR/cases.json" \
    "$TESTKIT_DIR/harnesses/matrix.json"
