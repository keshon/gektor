# bench-report.ps1 - builds a comparison table + recommendation from bench-llama.cmd results
#
# Usage (normally called by bench-llama.cmd, but can be re-run on any results folder):
#   powershell -ExecutionPolicy Bypass -File bench-report.ps1 -Dir bench-results\20261017-153000
#
# -StepPrompt / -StepGen : model of a typical agent step (new prompt tokens, generated tokens)
# -Margin                : extra n-cpu-moe layers kept in RAM for the "safe" pick

param(
    [Parameter(Mandatory = $true)][string]$Dir,
    [int]$StepPrompt = 3000,
    [int]$StepGen = 300,
    [int]$Margin = 2
)

$inv = [Globalization.CultureInfo]::InvariantCulture

function ToNum($v) {
    $d = 0.0
    if ([double]::TryParse([string]$v, [Globalization.NumberStyles]::Float, $inv, [ref]$d)) { return $d }
    return 0.0
}

function Pick($grp, $type, $depth) {
    $m = $grp | Where-Object { $_.Type -eq $type -and $_.Depth -eq $depth } | Select-Object -First 1
    if ($m) { return $m.Ts }
    return $null
}

function R1($v) {
    if ($null -eq $v) { return '-' }
    return [math]::Round([double]$v, 1)
}

# ---------------------------------------------------------------- parse
$rows = New-Object System.Collections.ArrayList
$gpu = ''
$model = ''

foreach ($f in (Get-ChildItem -LiteralPath $Dir -Filter 'ncmoe-*_kv-*.csv')) {
    if ($f.BaseName -notmatch '^ncmoe-(?<moe>[^_]+)_kv-(?<kv>.+)$') { continue }
    $moe = $Matches['moe']
    $kv = $Matches['kv']

    $lines = @(Get-Content -LiteralPath $f.FullName)
    $hi = -1
    for ($i = 0; $i -lt $lines.Count; $i++) {
        if ($lines[$i] -like '*avg_ts*') { $hi = $i; break }
    }
    if ($hi -lt 0) { continue }

    $data = @($lines[$hi..($lines.Count - 1)] | ConvertFrom-Csv)
    foreach ($r in $data) {
        $np = [int](ToNum $r.n_prompt)
        $ng = [int](ToNum $r.n_gen)
        $depth = 0
        if ($r.PSObject.Properties['n_depth']) { $depth = [int](ToNum $r.n_depth) }

        if ($np -gt 0 -and $ng -eq 0) { $type = 'pp' }
        elseif ($ng -gt 0 -and $np -eq 0) { $type = 'tg' }
        else { continue }

        [void]$rows.Add([pscustomobject]@{
            Moe   = $moe
            Kv    = $kv
            Ub    = [int](ToNum $r.n_ubatch)
            Depth = $depth
            Type  = $type
            Ts    = (ToNum $r.avg_ts)
        })

        if (-not $gpu -and $r.PSObject.Properties['gpu_info']) { $gpu = [string]$r.gpu_info }
        if (-not $model -and $r.PSObject.Properties['model_type']) { $model = [string]$r.model_type }
    }
}

# failed runs (exit code != 0), written by bench-llama.cmd
$fails = @()
$runsFile = Join-Path $Dir 'runs.csv'
if (Test-Path -LiteralPath $runsFile) {
    $fails = @(Import-Csv -LiteralPath $runsFile | Where-Object { $_.exit -ne '0' })
}

Write-Host ''
if ($rows.Count -eq 0) {
    Write-Host 'No benchmark results found in' $Dir
    foreach ($x in $fails) { Write-Host ('  FAILED: n-cpu-moe={0} kv={1} (exit {2}) - see the .log file' -f $x.moe, $x.kv, $x.exit) }
    exit 1
}

# ---------------------------------------------------------------- aggregate
$depths = @($rows | ForEach-Object { $_.Depth } | Sort-Object -Unique)
$d0 = $depths[0]
$dD = $depths[$depths.Count - 1]

$recs = New-Object System.Collections.ArrayList
foreach ($g in ($rows | Group-Object { '{0}|{1}|{2}' -f $_.Moe, $_.Kv, $_.Ub })) {
    $x = $g.Group[0]
    $moeNum = -1
    if ($x.Moe -ne '-') { $moeNum = [int]$x.Moe }

    $pp0 = Pick $g.Group 'pp' $d0
    $tg0 = Pick $g.Group 'tg' $d0
    $ppD = Pick $g.Group 'pp' $dD
    $tgD = Pick $g.Group 'tg' $dD

    $est = $null
    if ($ppD -and $tgD) { $est = ($StepPrompt / $ppD) + ($StepGen / $tgD) }

    [void]$recs.Add([pscustomobject]@{
        Moe = $x.Moe; MoeNum = $moeNum; Kv = $x.Kv; Ub = $x.Ub
        PP0 = $pp0; TG0 = $tg0; PPD = $ppD; TGD = $tgD; Est = $est
    })
}

$valid = @($recs | Where-Object { $null -ne $_.Est })
$best = $null
$safe = $null
if ($valid.Count -gt 0) {
    $best = $valid | Sort-Object Est | Select-Object -First 1
    $safe = $best
    if ($best.MoeNum -ge 0) {
        $cands = @($valid | Where-Object { $_.MoeNum -ge ($best.MoeNum + $Margin) } | Sort-Object Est)
        if ($cands.Count -gt 0) { $safe = $cands[0] }
    }
}

# ---------------------------------------------------------------- table
$sorted = @($recs | Sort-Object @{ Expression = { if ($null -eq $_.Est) { [double]::MaxValue } else { $_.Est } } })

$display = New-Object System.Collections.ArrayList
foreach ($r in $sorted) {
    $o = [ordered]@{}
    $o['n-cpu-moe'] = $r.Moe
    $o['kv'] = $r.Kv
    $o['ubatch'] = $r.Ub
    $o["pp@$d0"] = R1 $r.PP0
    $o["tg@$d0"] = R1 $r.TG0
    if ($dD -ne $d0) {
        $o["pp@$dD"] = R1 $r.PPD
        $o["tg@$dD"] = R1 $r.TGD
    }
    $o['step_s'] = R1 $r.Est
    $tag = ''
    if ($best -and $r.Moe -eq $best.Moe -and $r.Kv -eq $best.Kv -and $r.Ub -eq $best.Ub) { $tag = 'fastest' }
    if ($safe -and $r.Moe -eq $safe.Moe -and $r.Kv -eq $safe.Kv -and $r.Ub -eq $safe.Ub) {
        if ($tag) { $tag = 'fastest + safe pick' } else { $tag = 'safe pick' }
    }
    $o['note'] = $tag
    [void]$display.Add([pscustomobject]$o)
}
foreach ($x in $fails) {
    $o = [ordered]@{}
    $o['n-cpu-moe'] = $x.moe
    $o['kv'] = $x.kv
    $o['ubatch'] = '-'
    $o["pp@$d0"] = '-'
    $o["tg@$d0"] = '-'
    if ($dD -ne $d0) { $o["pp@$dD"] = '-'; $o["tg@$dD"] = '-' }
    $o['step_s'] = '-'
    $o['note'] = 'FAILED (likely OOM)'
    [void]$display.Add([pscustomobject]$o)
}

# ---------------------------------------------------------------- output
$out = New-Object System.Collections.ArrayList
[void]$out.Add('llama-bench comparison')
if ($model) { [void]$out.Add("Model : $model") }
if ($gpu) { [void]$out.Add("GPU   : $gpu") }
[void]$out.Add("Units : tokens/second (pp = prompt processing, tg = generation); @N = N tokens already in context")
[void]$out.Add("step_s: estimated seconds for one agent step = $StepPrompt new prompt tokens + $StepGen generated tokens, at depth $dD (lower is better)")
[void]$out.Add('')

$tableText = ($display | Format-Table -AutoSize | Out-String -Width 220).TrimEnd()
[void]$out.Add($tableText)
[void]$out.Add('')

if ($best) {
    [void]$out.Add('Recommended settings')
    [void]$out.Add('--------------------')
    $sp = '{0:N1}' -f $safe.Est
    [void]$out.Add(('Safe pick: n-cpu-moe={0} kv={1} ubatch={2}  (est. {3} s/step)' -f $safe.Moe, $safe.Kv, $safe.Ub, $sp))
    if ($safe.Moe -ne $best.Moe -or $safe.Kv -ne $best.Kv -or $safe.Ub -ne $best.Ub) {
        $bp = '{0:N1}' -f $best.Est
        [void]$out.Add(('Fastest   : n-cpu-moe={0} kv={1} ubatch={2}  (est. {3} s/step) - uses more VRAM' -f $best.Moe, $best.Kv, $best.Ub, $bp))
    }
    [void]$out.Add('')
    [void]$out.Add('Equivalent llama-server arguments (apply in your own server configuration):')
    $moeArg = ''
    if ($safe.Moe -ne '-') { $moeArg = "--n-cpu-moe $($safe.Moe) " }
    [void]$out.Add("$moeArg--ubatch-size $($safe.Ub) -fa on --cache-type-k $($safe.Kv) --cache-type-v $($safe.Kv)")
    [void]$out.Add('')
    [void]$out.Add('Notes')
    [void]$out.Add('-----')
    [void]$out.Add("* llama-bench only allocates context for the test (depth $dD + prompt), not your real --ctx-size,")
    [void]$out.Add('  so the real server needs more VRAM than the benchmark. Start the server with the real context and')
    [void]$out.Add('  watch VRAM (nvidia-smi); if it is full or spills into shared memory, raise n-cpu-moe by 2.')

    if ($best.MoeNum -ge 0) {
        $moes = @($recs | ForEach-Object { $_.MoeNum } | Sort-Object -Unique)
        if ($moes.Count -gt 1) {
            if ($best.MoeNum -eq $moes[0] -and $fails.Count -eq 0) {
                [void]$out.Add('* The fastest result is at the LOWEST n-cpu-moe you tested and nothing failed: lower values may be faster.')
                [void]$out.Add('  Re-run with BENCH_NCMOE set lower (e.g. in .env).')
            }
            if ($best.MoeNum -eq $moes[$moes.Count - 1]) {
                [void]$out.Add('* The fastest result is at the HIGHEST n-cpu-moe you tested: extend BENCH_NCMOE upward to confirm.')
            }
        }
    }
    if ($fails.Count -gt 0) {
        [void]$out.Add('* Failed runs usually mean the model did not fit in VRAM; the lowest passing n-cpu-moe is your VRAM edge.')
    }
    [void]$out.Add('* If f16 and q8_0 KV cache are within a few percent, prefer q8_0: it halves KV memory at long context.')
    [void]$out.Add('* Differences under ~3-5% are measurement noise; repeat the run before drawing conclusions.')
} else {
    [void]$out.Add('No complete results (every run failed or has no depth data), nothing to recommend.')
}

$out | ForEach-Object { Write-Host $_ }

# ---------------------------------------------------------------- files
$mdPath = Join-Path $Dir 'report.md'
$csvPath = Join-Path $Dir 'report.csv'

$md = New-Object System.Collections.ArrayList
[void]$md.Add('# llama-bench comparison')
[void]$md.Add('')
$cols = @($display[0].PSObject.Properties | ForEach-Object { $_.Name })
[void]$md.Add('| ' + ($cols -join ' | ') + ' |')
[void]$md.Add('|' + (($cols | ForEach-Object { '---' }) -join '|') + '|')
foreach ($o in $display) {
    $cells = foreach ($c in $cols) { [string]$o.PSObject.Properties[$c].Value }
    [void]$md.Add('| ' + ($cells -join ' | ') + ' |')
}
$ri = [array]::IndexOf(@($out), 'Recommended settings')
if ($ri -ge 0) {
    [void]$md.Add('')
    [void]$md.Add('```')
    for ($i = $ri; $i -lt $out.Count; $i++) { [void]$md.Add([string]$out[$i]) }
    [void]$md.Add('```')
}
Set-Content -LiteralPath $mdPath -Value $md -Encoding ASCII
$display | Export-Csv -LiteralPath $csvPath -NoTypeInformation -Encoding ASCII

Write-Host ''
Write-Host "Saved: $mdPath"
Write-Host "       $csvPath"
