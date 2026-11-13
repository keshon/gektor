#!/usr/bin/env bash
set -e
if [[ $# -eq 0 ]]; then
    printf '%s\n' 'Usage: run-cli-workflow.sh [flags] recipe.json' 'Relative paths use your current directory. Put flags before the task or recipe.' >&2
    exit 1
fi
source "$(dirname -- "${BASH_SOURCE[0]}")/scripts/ensure-build.sh"
exec "$gektor_binary" workflow run "$@"
