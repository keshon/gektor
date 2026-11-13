#!/usr/bin/env bash
set -e
gektor_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
gektor_binary="$gektor_root/gektor"
case "$(uname -s)" in
    MINGW*|MSYS*|CYGWIN*) gektor_binary="$gektor_binary.exe" ;;
esac
if [[ -f "$gektor_root/go.mod" ]]; then
    (cd -- "$gektor_root" && go build -o "$gektor_binary" ./cmd/gektor)
elif [[ ! -f "$gektor_binary" ]]; then
    printf 'ERROR: %s is missing. Extract the complete release archive.\n' "$gektor_binary" >&2
    exit 1
fi
