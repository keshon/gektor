#!/usr/bin/env bash
set -e
source "$(dirname -- "${BASH_SOURCE[0]}")/scripts/ensure-build.sh"
exec "$gektor_binary" workflow serve "$@"
