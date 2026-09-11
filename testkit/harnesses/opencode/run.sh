#!/usr/bin/env bash
# OpenCode harness adapter (npm distribution, Chat Completions via the
# @ai-sdk/openai-compatible provider).
#
# Extra env understood here:
#   PROVIDER  provider id written into the generated config (default: affinity)
#   MODE      run (default) | shell | exec
set -euo pipefail

source /opt/testkit/contract.sh

MODE="${MODE:-run}"
case "$MODE" in
    run)        harness_require BASE_URL API_KEY PROMPT ;;
    shell|exec) harness_require BASE_URL API_KEY ;;
    *)          harness_die "unsupported MODE '$MODE' (run|shell|exec)" ;;
esac
harness_prepare_home

MODEL="${MODEL:-gpt-5.2}"
PROVIDER="${PROVIDER:-affinity}"
CASE="${CASE_NAME:-opencode}"

CONFIG="$WORKSPACE/opencode.json"
python3 - "$CONFIG" "$BASE_URL" "$MODEL" "$PROVIDER" <<'PY'
import json, sys

path, base_url, model, provider = sys.argv[1:5]
config = {
    "$schema": "https://opencode.ai/config.json",
    "share": "disabled",
    "permission": {"*": "deny"},
    "model": f"{provider}/{model}",
    "small_model": f"{provider}/{model}",
    "enabled_providers": [provider],
    "provider": {
        provider: {
            "npm": "@ai-sdk/openai-compatible",
            "name": "testkit",
            "options": {
                "baseURL": base_url.rstrip("/") + "/v1",
                "apiKey": "{env:API_KEY}",
            },
            "models": {model: {"name": model}},
        }
    },
}
with open(path, "w", encoding="utf-8") as handle:
    json.dump(config, handle, indent=2)
PY

export OPENCODE_CONFIG="$CONFIG"
export API_KEY
export OPENAI_API_KEY="$API_KEY"

cli="opencode run --format json --model ${PROVIDER}/${MODEL} \"your prompt\""

# shell/exec: everything above is the preparation; hand the result over instead
# of firing one canned request.
if [ "$MODE" != "run" ]; then
    harness_enter "$MODE" "$cli" "config  : $CONFIG"
fi

args=(run --format json --model "$PROVIDER/$MODEL")
if harness_resume; then
    args+=(--continue)
fi
args+=("$PROMPT")

run_harness "$CASE" /harness/extract.py opencode "${args[@]}"
