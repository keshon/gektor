param(
    [Parameter(Mandatory = $true)][string]$Binary,
    [Parameter(Mandatory = $true)][string]$OutDir,
    [string[]]$Cases = @('05-patch-unique', '06-patch-ambiguous', '24-nested-skills', '25-ledger-repair'),
    [ValidateRange(3, 100)][int]$Repeats = 3,
    [string]$Backend = 'http://127.0.0.1:9931',
    [string]$RequireModel = 'Ornith'
)
$ErrorActionPreference = 'Stop'
$binaryPath = (Resolve-Path -LiteralPath $Binary).Path
$outputRoot = [IO.Path]::GetFullPath($OutDir)
if (Test-Path -LiteralPath $outputRoot) { throw 'OutDir must be fresh.' }
New-Item -ItemType Directory -Path $outputRoot | Out-Null
$plan = @()
foreach ($case in $Cases) {
    if (-not (Test-Path -LiteralPath "eval/probes/$case.json")) { throw "Unknown case: $case" }
    for ($repeat = 1; $repeat -le $Repeats; $repeat++) {
        $profiles = if ($repeat % 2 -eq 1) { @('baseline', 'lazy-web') } else { @('lazy-web', 'baseline') }
        foreach ($profile in $profiles) {
            $plan += [ordered]@{ case = $case; repeat = $repeat; profile = $profile; status = 'not_run'; exit_code = $null }
        }
    }
}
$ledgerPath = Join-Path $outputRoot 'run-order.json'
$plan | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath $ledgerPath -Encoding utf8
foreach ($entry in $plan) {
    $runRoot = Join-Path $outputRoot "$($entry.case)-r$($entry.repeat)-$($entry.profile)"
    $entry.status = 'running'
    $plan | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath $ledgerPath -Encoding utf8
    Write-Output "Starting $($entry.case) repeat $($entry.repeat) $($entry.profile)"
    & $binaryPath -only $entry.case -runs 1 -jobs 1 -backend-kind llama -backend $Backend -require-model $RequireModel -context-limit 131072 -max-tokens 8192 -tool-profile $entry.profile -max-failures 0 -out $runRoot
    $entry.exit_code = $LASTEXITCODE
    $entry.status = 'finished'
    $plan | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath $ledgerPath -Encoding utf8
    if ($entry.exit_code -notin @(0, 1, 2)) { throw "Runner failed to launch or crashed: $($entry.exit_code)" }
}
Write-Output "Finished $($plan.Count) invocations; inspect every results.jsonl and summary.json, including errors."
