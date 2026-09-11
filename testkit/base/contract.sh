#!/usr/bin/env bash
# Shared entrypoint contract for every testkit harness image.
#
# Contract (stable across harnesses, so the runner never learns CLI syntax):
#   in : BASE_URL, API_KEY, PROMPT, MODEL, PROTOCOL, RESUME, HARNESS_HOME
#   out: the assistant reply text on stdout, byte-exact, no CLI decoration
#        raw CLI stdout/stderr on stderr, plus $HARNESS_LOG_DIR/<name>.stdout
#
# A harness adapter is therefore "invoke the CLI, then extract the reply".
# Anything else - config file shape, provider names, flags - stays local to the
# adapter and never leaks into the matrix runner.

harness_die() {
    printf 'harness: %s\n' "$*" >&2
    exit 2
}

harness_require() {
    local name
    for name in "$@"; do
        [ -n "${!name:-}" ] || harness_die "required environment variable ${name} is empty"
    done
}

# harness_resume -> true when this invocation should continue an existing
# conversation. Accepts the shell-ish truthy forms so a runner passing
# "true"/"1"/"yes" all behave the same.
harness_resume() {
    case "${RESUME:-0}" in
        1 | true | TRUE | yes | on) return 0 ;;
        *) return 1 ;;
    esac
}

# harness_home -> prints the per-case HOME, creating it if needed.
harness_home() {
    local home="${HARNESS_HOME:-/harness-home}"
    mkdir -p "$home"
    printf '%s' "$home"
}

# harness_prepare_home -> exports a fully isolated HOME/XDG set and cds to the workspace.
harness_prepare_home() {
    local home
    home="$(harness_home)"
    export HOME="$home"
    export XDG_CONFIG_HOME="$home/.config"
    export XDG_DATA_HOME="$home/.local/share"
    export XDG_CACHE_HOME="$home/.cache"
    export XDG_STATE_HOME="$home/.local/state"
    mkdir -p "$XDG_CONFIG_HOME" "$XDG_DATA_HOME" "$XDG_CACHE_HOME" "$XDG_STATE_HOME"
    WORKSPACE="${WORKSPACE:-$home/workspace}"
    mkdir -p "$WORKSPACE"
    cd "$WORKSPACE"
}

harness_log_dir() {
    local dir="${HARNESS_LOG_DIR:-/artifacts/logs}"
    mkdir -p "$dir"
    printf '%s' "$dir"
}

# run_harness <case-name> <extractor.py> <command...>
run_harness() {
    local name="$1" extractor="$2"
    shift 2
    local dir out err code=0
    dir="$(harness_log_dir)"
    out="$dir/${name}.stdout"
    err="$dir/${name}.stderr"
    "$@" >"$out" 2>"$err" || code=$?
    # Raw client output belongs on stderr so stdout carries only the reply.
    cat "$out" >&2
    cat "$err" >&2
    if [ "$code" -ne 0 ]; then
        printf 'harness: %s exited with code %d (see %s)\n' "$name" "$code" "$err" >&2
        return "$code"
    fi
    python3 "$extractor" "$out"
}

# harness_enter <mode: shell|exec> <suggested-command> [note...]
#
# Hand a fully prepared environment to a human or an agent instead of running
# one canned request. Used after the adapter has already generated its config,
# so what the operator gets is a clean harness pointed at the tap - not an
# empty container. The whole session is recorded to terminal.log, flushed on
# every write so it can be read while the session is still running.
harness_enter() {
    local mode="$1" suggested="$2"
    shift 2
    local dir log note
    dir="$(harness_log_dir)"
    log="$dir/terminal.log"
    {
        echo "-----------------------------------------------------------"
        echo " testkit harness session (${mode})"
        echo "   workdir : $PWD"
        echo "   home    : $HOME"
        echo "   traffic : $BASE_URL  -- every request is recorded"
        echo "   logs    : $dir"
        for note in "$@"; do echo "   $note"; done
        [ -n "$suggested" ] && echo "   try     : $suggested"
        echo "   terminal transcript -> ${log} (live)"
        echo "-----------------------------------------------------------"
    } >&2

    if [ "$mode" = "exec" ]; then
        [ -n "${EXEC_COMMAND:-}" ] || harness_die "MODE=exec requires EXEC_COMMAND"
        exec script -q -e -f -c "$EXEC_COMMAND" "$log"
    fi
    # Deliberately NOT a login shell: /etc/profile resets PATH and would drop
    # the harness's own PATH entry (e.g. node_modules/.bin), so the CLI the
    # banner just advertised would be "command not found".
    exec script -q -e -f -c "bash" "$log"
}
