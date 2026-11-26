# Run from any directory. Production source files are not changed.
$ErrorActionPreference = 'Stop'
$repo = (Resolve-Path (Join-Path $PSScriptRoot '../../..')).Path
$source = (Resolve-Path (Join-Path $PSScriptRoot 'harness-resume-repro.go.txt')).Path
$overlayDir = Join-Path ([System.IO.Path]::GetTempPath()) ('gektor-resume-audit-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $overlayDir | Out-Null
$replacement = @{}
$replacement[(Join-Path $repo 'internal/harness/audit_20261126_test.go')] = $source
$overlay = Join-Path $overlayDir 'overlay.json'
@{ Replace = $replacement } | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath $overlay
Push-Location $repo
try {
    go test -overlay $overlay ./internal/harness -run '^TestAudit' -count=1
    $testExit = $LASTEXITCODE
} finally {
    Pop-Location
}
Write-Output "Overlay retained at $overlay"
exit $testExit
