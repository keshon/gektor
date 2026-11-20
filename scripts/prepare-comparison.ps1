param(
    [Parameter(Mandatory = $true)][string]$OutDir,
    [string]$PiRoot = 'D:\Projects\dev\_incubator\pi',
    [string]$OpenCodeRoot = 'D:\Projects\dev\_incubator\opencode',
    [string]$Backend = 'http://127.0.0.1:9931'
)
$ErrorActionPreference = 'Stop'
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$outputRoot = [IO.Path]::GetFullPath($OutDir)
if (Test-Path -LiteralPath $outputRoot) { throw 'OutDir must not already exist; each preparation needs a fresh directory.' }
New-Item -ItemType Directory -Path $outputRoot | Out-Null
function Checkout([string]$root) {
    $revision = & git -C $root rev-parse HEAD
    if ($LASTEXITCODE -ne 0) { throw "Cannot identify checkout: $root" }
    $status = @(& git -C $root status --porcelain)
    if ($LASTEXITCODE -ne 0) { throw "Cannot inspect checkout: $root" }
    return [ordered]@{ root = $root; revision = "$revision"; dirty = $status.Count -gt 0 }
}
$checkouts = [ordered]@{ gektor = (Checkout $repoRoot); pi = (Checkout $PiRoot); opencode = (Checkout $OpenCodeRoot) }
$availability = [ordered]@{ url = "$($Backend.TrimEnd('/'))/health"; available = $false; error = $null }
try {
    $response = Invoke-WebRequest -Uri $availability.url -TimeoutSec 8
    $availability.available = $response.StatusCode -eq 200
} catch { $availability.error = $_.Exception.Message }
$cases = @()
foreach ($name in @('05-patch-unique', '06-patch-ambiguous', '24-nested-skills', '25-ledger-repair')) {
    $definition = Join-Path $repoRoot "eval/probes/$name.json"
    $probe = Get-Content -LiteralPath $definition -Raw | ConvertFrom-Json
    $hashes = @()
    foreach ($file in @(Get-ChildItem -LiteralPath (Join-Path $repoRoot $probe.seed) -File -Recurse | Sort-Object FullName)) {
        $hashes += [ordered]@{ path = [IO.Path]::GetRelativePath($repoRoot, $file.FullName).Replace('\', '/'); sha256 = (Get-FileHash -LiteralPath $file.FullName -Algorithm SHA256).Hash.ToLowerInvariant() }
    }
    $hashes += [ordered]@{ path = "eval/probes/$name.json"; sha256 = (Get-FileHash -LiteralPath $definition -Algorithm SHA256).Hash.ToLowerInvariant() }
    foreach ($check in $probe.verify) {
        if ($check.type -eq 'go_acceptance') {
            $hashes += [ordered]@{ path = $check.path; sha256 = (Get-FileHash -LiteralPath (Join-Path $repoRoot $check.path) -Algorithm SHA256).Hash.ToLowerInvariant() }
        }
    }
    $cases += [ordered]@{ name = $name; task = $probe.task; seed = $probe.seed; verify = @($probe.verify); input_hashes = $hashes }
}
# Rotate product order between repeats to avoid always giving one product a warmer server.
$order = @(@('gektor', 'pi', 'opencode'), @('pi', 'opencode', 'gektor'), @('opencode', 'gektor', 'pi'))
$plan = @()
foreach ($case in $cases) {
    for ($repeat = 0; $repeat -lt 3; $repeat++) {
        foreach ($product in $order[$repeat]) {
            $plan += [ordered]@{ case = $case.name; repeat = $repeat + 1; product = $product; status = 'not_run' }
        }
    }
}
$manifest = [ordered]@{
    schema_version = 1
    prepared_at_utc = [DateTime]::UtcNow.ToString('o')
    status = 'prepared_only'
    backend_health = $availability
    checkouts = $checkouts
    required_settings = [ordered]@{ model_weights_sha256 = $null; quantization = $null; server_arguments = $null; context_limit = $null; max_output_tokens = 8192; sampler_settings = $null; reasoning_settings = $null; cache_policy = $null; concurrency = 1 }
    cases = $cases
    run_order = $plan
    blockers = @('Pin and verify model/server/sampler/context/cache settings before any scored run.', 'Build and identify each checkout binary; validate request settings and acceptance adapters.', 'GEKTOR-specific tool-name trace criteria cannot score competitors without reviewed semantic mapping.', 'Smaller tool/context profiles, cancellation/resume, unavailable-tool and long-context cases remain separate uncompleted gates.')
}
$manifest | ConvertTo-Json -Depth 30 | Set-Content -LiteralPath (Join-Path $outputRoot 'manifest.json') -Encoding utf8
Write-Output "Prepared $($plan.Count) unexecuted runs in $outputRoot; backend available: $($availability.available)"
