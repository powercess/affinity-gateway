#!/bin/sh
set -eu
# Compose secrets are file mounts; the plugin consumes an environment variable.
# Read it at runtime, never bake it into the image or print it.
if [ -n "${SESSION_AFFINITY_SECRET_FILE:-}" ]; then
    SESSION_AFFINITY_SECRET=$(cat "$SESSION_AFFINITY_SECRET_FILE")
    export SESSION_AFFINITY_SECRET
fi
exec "$@"
