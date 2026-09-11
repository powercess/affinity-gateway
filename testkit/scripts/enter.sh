#!/usr/bin/env bash
# Enter a fresh, configured harness container.
#
#   enter.sh <harness>                  interactive shell, whole session recorded
#   enter.sh <harness> -- "<command>"   run one command in the same environment
#
# The container itself is one-shot (compose run --rm): anything that has to
# survive lives in the run directory. The mock/tap/pcap stack is left running so
# the next session does not pay to start it again, and the run directory is
# stable per harness so HOME (and therefore the CLI's own session state)
# persists across sessions.
#
# Env: TESTKIT_RUN_ID=<name>  use an isolated run directory instead of "shell"
#      MODEL=... PROTOCOL=... passed through to the harness adapter
set -euo pipefail

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

harness="${1:-}"
if [ -z "$harness" ]; then
    echo "usage: enter.sh <harness> [-- <command>]" >&2
    exit 2
fi
shift

mode=shell
exec_command=""
if [ "${1:-}" = "--" ]; then
    shift
    mode=exec
    exec_command="${1:-}"
    if [ -z "$exec_command" ]; then
        echo "enter.sh: '--' needs a command string" >&2
        exit 2
    fi
fi

if ! jq -e --arg n "$harness" '.harnesses[] | select(.name == $n)' \
        "$TESTKIT_DIR/harnesses/matrix.json" >/dev/null; then
    known="$(jq -r '[.harnesses[].name] | join(", ")' "$TESTKIT_DIR/harnesses/matrix.json")"
    echo "enter.sh: unknown harness '$harness'; known: $known" >&2
    exit 2
fi
service="harness-$harness"
label="$mode-$harness"

RUN_ID="${TESTKIT_RUN_ID:-shell}"
export TESTKIT_ARTIFACTS="$TESTKIT_ARTIFACTS_ROOT/runs/$RUN_ID"
mkdir -p "$TESTKIT_ARTIFACTS"/{logs,pcap,supplier,home,identity}

capture="$TESTKIT_ARTIFACTS/capture.jsonl"
capture_lines() {
    if [ -f "$capture" ]; then wc -l < "$capture"; else echo 0; fi
}

echo "==> ${mode} session: ${harness}"
echo "    run dir : ${TESTKIT_ARTIFACTS}"

testkit_compose up -d --wait mock tap pcap

api_key="$(jq -r '.api_key_sentinel' "$TESTKIT_DIR/cases.json")"
env_args=(-e "MODE=$mode" -e "CASE_NAME=$label" -e "API_KEY=$api_key")
if [ -n "${MODEL:-}" ]; then env_args+=(-e "MODEL=$MODEL"); fi
if [ -n "${PROTOCOL:-}" ]; then env_args+=(-e "PROTOCOL=$PROTOCOL"); fi
if [ "$mode" = "exec" ]; then env_args+=(-e "EXEC_COMMAND=$exec_command"); fi

# -t only when we actually have a terminal, so an agent can pipe into this.
run_args=(--rm -i)
if [ -t 0 ] && [ -t 1 ]; then run_args+=(-t); fi

home="$TESTKIT_ARTIFACTS/home/$harness"
mkdir -p "$home"

from=$(( $(capture_lines) + 1 ))
code=0
testkit_compose --profile harness run "${run_args[@]}" --no-deps \
    -v "$home:/harness-home" \
    "${env_args[@]}" \
    "$service" || code=$?

sleep 0.5
to=$(capture_lines)
jq -cn --arg case "$label" --argjson exit "$code" --argjson from "$from" --argjson to "$to" \
    '{case: $case, exit: $exit, from: $from, to: $to}' >> "$TESTKIT_ARTIFACTS/case-runs.jsonl"

echo
echo "==> session finished (exit ${code})"
echo "    transcript : ${TESTKIT_ARTIFACTS}/logs/terminal.log"
echo "    requests   : ${TESTKIT_ARTIFACTS}/capture.jsonl  (lines ${from}-${to})"
echo "    raw bytes  : ${TESTKIT_ARTIFACTS}/pcap/entry.pcap"
echo "    stack is still up; stop it with:"
echo "      docker compose -f testkit/compose.poc.yaml down"
exit "$code"
