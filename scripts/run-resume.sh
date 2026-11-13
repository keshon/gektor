#!/usr/bin/env bash
set -e
state_root="${USERPROFILE:-$HOME}/.gektor"
if command -v cygpath >/dev/null 2>&1; then
    state_root=$(cygpath -u "$state_root")
fi
if [[ $# -eq 0 ]]; then
    if [[ ! -d "$state_root/tasks" ]]; then
        printf 'No saved tasks: %s does not exist.\n' "$state_root/tasks"
        exit 0
    fi
    printf 'Saved tasks in %s (newest first):\n' "$state_root/tasks"
    ls -1t -- "$state_root/tasks"
    exit 0
fi
resume=$1
shift
if [[ ! -e "$resume" ]]; then
    resume="$state_root/tasks/$resume"
fi
if [[ ! -e "$resume" ]]; then
    printf 'ERROR: no task directory or state file at %s\n' "$resume" >&2
    exit 1
fi
source "$(dirname -- "${BASH_SOURCE[0]}")/ensure-build.sh"
exec "$gektor_binary" -resume "$resume" "$@"
