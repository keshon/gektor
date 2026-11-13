#!/usr/bin/env bash
set -e
script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
exec powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$script_dir/demo.ps1" -Demo workflow-review
