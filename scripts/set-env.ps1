param([string]$Path=".env")

# Load KEY=VALUE entries from .env into the caller's environment.
# The parent .cmd evals the printed lines:
#   for /f "delims=" %%a in ('powershell -NoProfile -ExecutionPolicy Bypass -File scripts\set-env.ps1 .env > tmp 2>nul & type tmp & del tmp') do set "%%a"
#
# Comments (#) and blank lines are skipped. A missing file is silent.
# Only the first '=' splits key from value, so values may contain '='.
if (!(Test-Path -LiteralPath $Path)) { exit 0 }

Get-Content -LiteralPath $Path -ErrorAction SilentlyContinue |
    Where-Object { $_.Trim() -ne '' -and $_.TrimStart() -notmatch '^#' } |
    ForEach-Object {
        if ($_ -match '^([^=]+)=(.*)$') {
            '{0}={1}' -f $matches[1].Trim(), $matches[2].Trim()
        }
    }
