#!/usr/bin/env bash
# Kimi CLI harness adapter (PyPI distribution, installed by uv).
#
# Kimi registers provider *types* (openai_legacy / openai_responses / anthropic
# / kimi), not OpenAI-compatible base URLs alone, so PROTOCOL selects the type.
# OpenAI-style types expect the /v1 prefix inside base_url; the anthropic type
# does not.
set -euo pipefail

source /opt/testkit/contract.sh

harness_require BASE_URL API_KEY PROMPT
harness_prepare_home

MODEL="${MODEL:-gpt-4.1}"
PROTOCOL="${PROTOCOL:-chat}"
CASE="${CASE_NAME:-kimi}"

case "$PROTOCOL" in
    chat)      KIMI_TYPE="openai_legacy";    PROVIDER_BASE="${BASE_URL%/}/v1" ;;
    responses) KIMI_TYPE="openai_responses"; PROVIDER_BASE="${BASE_URL%/}/v1" ;;
    messages)  KIMI_TYPE="anthropic";        PROVIDER_BASE="${BASE_URL%/}" ;;
    kimi)      KIMI_TYPE="kimi";             PROVIDER_BASE="${BASE_URL%/}/v1" ;;
    *) harness_die "unsupported PROTOCOL '$PROTOCOL' (chat|responses|messages|kimi)" ;;
esac

CONFIG="$WORKSPACE/kimi-config.json"
python3 - "$CONFIG" "$PROVIDER_BASE" "$MODEL" "$KIMI_TYPE" "$API_KEY" <<'PY'
import json, sys

path, base_url, model, provider_type, api_key = sys.argv[1:6]
config = {
    "default_model": "fixture",
    "providers": {
        "fixture": {"type": provider_type, "base_url": base_url, "api_key": api_key}
    },
    "models": {
        "fixture": {"provider": "fixture", "model": model, "max_context_size": 262144}
    },
    "telemetry": False,
}
with open(path, "w", encoding="utf-8") as handle:
    json.dump(config, handle, indent=2)
PY

args=(--config-file "$CONFIG" --quiet)
if harness_resume; then
    args+=(--continue)
fi
args+=(--prompt "$PROMPT")

run_harness "$CASE" /harness/extract.py kimi "${args[@]}"
