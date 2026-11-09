# Local program report

## Before running

The commands use PowerShell from the extracted application folder or a
[built source checkout](../../../docs/development/README.md). On Linux, use
`./gektor` in place of `.\gektor.exe` and equivalent shell syntax.
Copy this folder before editing and select that copy with `-workspace`.
Each run creates a separate saved record.

Requires Python on PATH as `python`. No model or network needed. A captured JSON
file goes to Python through stdin; stdout becomes a report artifact. Execution
pauses for review before starting Python. The script itself is a live workspace
file, not part of the captured input.

Run in **PowerShell** from the application folder:

```powershell
$workspace = 'examples/workflows/process-report'
$rules = 'workflow_run=*,artifact_import=items.json,run_program=python summarize.py,artifact_read_text=*,artifact_write=report.md'
.\gektor.exe workflow validate -workspace $workspace recipe.json
$run = .\gektor.exe workflow run -workspace $workspace -yes -allow $rules recipe.json | ConvertFrom-Json
.\gektor.exe workflow inspect -workspace $workspace $run.run_id
.\gektor.exe workflow review -workspace $workspace -decision approve -allow $rules $run.run_id generate
$artifacts = .\gektor.exe workflow artifacts -workspace $workspace $run.run_id | ConvertFrom-Json
$report = $artifacts | Where-Object step_id -eq 'save'
.\gektor.exe workflow export-artifact -workspace $workspace $run.run_id $report.id report.md
Get-Content "$workspace/report.md"
```

Choose another export destination if `report.md` already exists. The run also
retains separate stdout and stderr artifacts. If Python fails, use `workflow
inspect` and `workflow artifacts` to retrieve diagnostics; later steps do not run.
