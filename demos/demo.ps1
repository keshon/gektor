param(
    [Parameter(Mandatory = $true)]
    [ValidateSet('agent-task', 'workflow-draft', 'workflow-review')]
    [string]$Demo
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$binary = Join-Path $root 'gektor.exe'

function Invoke-Checked {
    param([string]$Command, [string[]]$Arguments)
    $output = & $Command @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "$Command exited with code $LASTEXITCODE."
    }
    return $output
}

function Invoke-JSON {
    param([string[]]$Arguments)
    $output = Invoke-Checked -Command $binary -Arguments $Arguments
    return ($output -join [Environment]::NewLine) | ConvertFrom-Json
}

try {
    Invoke-Checked -Command (Join-Path $root 'scripts\ensure-build.cmd') -Arguments @()
    $demoDirectory = Join-Path $root 'tmp\demos'
    $workspace = Join-Path $demoDirectory ($Demo + '-' + [Guid]::NewGuid().ToString('N').Substring(0, 8))
    New-Item -ItemType Directory -Path $workspace -Force | Out-Null
    Write-Host "Demo workspace: $workspace"

    switch ($Demo) {
        'agent-task' {
            $note = Join-Path $workspace 'note.txt'
            [IO.File]::WriteAllText($note, "Demo note\r\nCode: GEKTOR_AGENT_DEMO_OK\r\n")
            $before = Get-Content -LiteralPath $note -Raw
            $output = Invoke-Checked -Command (Join-Path $root 'run-gektor-agent.cmd') -Arguments @(
                '-workspace', $workspace,
                '-profile', (Join-Path $root 'profiles\chatgpt.json'),
                '-plan', '-yes', '-max-steps', '8', '-run-timeout', '120s',
                '-allow', 'read_file=note.txt',
                'Read note.txt using read_file. Reply only with the value on the Code line. Do not change any files.'
            )
            $text = $output -join [Environment]::NewLine
            Write-Output $text
            if ($text -notmatch 'GEKTOR_AGENT_DEMO_OK' -or (Get-Content -LiteralPath $note -Raw) -ne $before) {
                throw 'The Agent did not return the code or the source note changed.'
            }
        }
        'workflow-review' {
            Copy-Item -LiteralPath (Join-Path $root 'examples\workflows\transform-and-review\recipe.json') -Destination $workspace
            $rules = 'workflow_run=recipe.json,script_transform=*'
            $validation = Invoke-JSON -Arguments @('workflow', 'validate', '-workspace', $workspace, '-yes', 'recipe.json')
            if (-not $validation.valid) { throw 'Recipe validation failed.' }
            $output = Invoke-Checked -Command (Join-Path $root 'run-gektor-workflow.cmd') -Arguments @(
                '-workspace', $workspace, '-yes', '-allow', $rules, 'recipe.json'
            )
            $run = ($output -join [Environment]::NewLine) | ConvertFrom-Json
            if ($run.status -ne 'waiting_review') { throw 'The recipe did not pause for review.' }
            Write-Host "Run $($run.run_id) paused before double."
            Write-Host 'This demo now approves that step. No files or external services are changed.'
            $result = Invoke-JSON -Arguments @(
                'workflow', 'review', '-workspace', $workspace, '-decision', 'approve',
                '-allow', $rules, $run.run_id, 'double'
            )
            if ($result.status -ne 'completed') { throw 'The reviewed run did not complete.' }
            $record = Invoke-JSON -Arguments @('workflow', 'inspect', '-workspace', $workspace, $run.run_id)
            $step = @($record.record.steps | Where-Object { $_.id -eq 'double' })[0]
            $values = ($step.output | ConvertFrom-Json).items
            if (($values -join ',') -ne '4,6,10') { throw 'Unexpected workflow result.' }
            Write-Host 'Result: 4, 6, 10'
            Write-Host "Run ID: $($run.run_id)"
        }
        'workflow-draft' {
            $example = Join-Path $root 'examples\workflows\moltbook-draft'
            foreach ($name in @('recipe.json', 'state.schema.json', 'chatgpt.json', 'chatgpt.inputs.json')) {
                Copy-Item -LiteralPath (Join-Path $example $name) -Destination $workspace
            }
            Invoke-JSON -Arguments @('workflow', 'register-schema', '-workspace', $workspace, 'feed_state', 'state.schema.json') | Out-Null
            $rules = 'workflow_run=recipe.json,record_read=feed_state/*,record_write=feed_state/*,http_request=GET https://www.moltbook.com/*,script_transform=*,model_generate=chatgpt.json,artifact_write=draft.json'
            $output = Invoke-Checked -Command (Join-Path $root 'run-gektor-workflow.cmd') -Arguments @(
                '-workspace', $workspace, '-inputs', 'chatgpt.inputs.json', '-yes', '-allow', $rules, 'recipe.json'
            )
            $run = ($output -join [Environment]::NewLine) | ConvertFrom-Json
            if ($run.status -ne 'completed') { throw 'The draft run did not complete.' }
            $artifacts = Invoke-JSON -Arguments @('workflow', 'artifacts', '-workspace', $workspace, $run.run_id)
            $draft = @($artifacts | Where-Object { $_.step_id -eq 'save_draft' })
            if ($draft.Count -gt 0) {
                Invoke-JSON -Arguments @('workflow', 'export-artifact', '-workspace', $workspace, $run.run_id, $draft[0].id, 'draft.json') | Out-Null
                Write-Host "Draft: $(Join-Path $workspace 'draft.json')"
                Get-Content -LiteralPath (Join-Path $workspace 'draft.json') -Raw
            } else {
                $state = Invoke-JSON -Arguments @('workflow', 'record', '-workspace', $workspace, 'feed_state', 'moltbook')
                Write-Host "No draft: $($state.data.outcome), HTTP $($state.data.last_http_status)."
            }
            Write-Host "Run ID: $($run.run_id)"
            Write-Host 'Nothing was published.'
        }
    }
    Write-Host 'Demo finished.'
    exit 0
} catch {
    [Console]::Error.WriteLine($_.Exception.Message)
    exit 1
}
