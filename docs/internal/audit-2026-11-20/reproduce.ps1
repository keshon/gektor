$ErrorActionPreference = 'Stop'
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..\..')).Path
$evidenceRoot = Join-Path ([System.IO.Path]::GetTempPath()) ('gektor-audit-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $evidenceRoot | Out-Null

$replacements = @{}
$probes = @(
    @{ Source = 'budget_test.go.txt'; Package = 'internal/agent' },
    @{ Source = 'background_test.go.txt'; Package = 'internal/tools/background' },
    @{ Source = 'close_test.go.txt'; Package = 'internal/tui/agent' },
    @{ Source = 'markdown_test.go.txt'; Package = 'internal/tui/agent' }
)
foreach ($probe in $probes) {
    $sourcePath = Join-Path $PSScriptRoot $probe.Source
    $testPath = Join-Path $evidenceRoot ($probe.Source -replace '\.txt$', '')
    Copy-Item -LiteralPath $sourcePath -Destination $testPath
    $targetPath = Join-Path $repoRoot ($probe.Package + '/audit_' + ($probe.Source -replace '\.txt$', ''))
    if (Test-Path -LiteralPath $targetPath) {
        throw "Refusing to overlay an existing file: $targetPath"
    }
    $replacements[$targetPath] = $testPath
}

$overlayPath = Join-Path $evidenceRoot 'overlay.json'
@{ Replace = $replacements } | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath $overlayPath -Encoding utf8
Write-Output "Audit probes intentionally fail on the audited revision. Evidence: $evidenceRoot"
Push-Location $repoRoot
try {
    & go test -p 1 -overlay $overlayPath ./internal/agent ./internal/tools/background ./internal/tui/agent -run '^TestAudit' -count=1 -v
    $probeExit = $LASTEXITCODE
} finally {
    Pop-Location
}
exit $probeExit
