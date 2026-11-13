#!/usr/bin/env bash
set -e
exec bash "$(dirname -- "${BASH_SOURCE[0]}")/../run-cli-agent.sh" -plan "$@"
