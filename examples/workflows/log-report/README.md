# Log error report

No model or network required. The recipe captures a workspace log, reads the
snapshot, extracts ERROR lines with JavaScript and pauses before saving a Markdown
artifact. The original log can change without altering the captured evidence.

Run this in **PowerShell**, from the repository root. Variables capture the actual
IDs; nothing needs to be substituted manually. Build first with
`go build -o gektor.exe ./cmd/gektor` if needed.

```powershell
$workspace = 'examples/workflows/log-report'
$rules = 'workflow_run=*,artifact_import=application.log,artifact_read_text=*,script_transform=*,artifact_write=errors.md'
.\gektor.exe workflow validate -workspace $workspace recipe.json
$run = .\gektor.exe workflow run -workspace $workspace -yes -allow $rules recipe.json | ConvertFrom-Json
.\gektor.exe workflow inspect -workspace $workspace $run.run_id
.\gektor.exe workflow review -workspace $workspace -decision approve -allow $rules $run.run_id save
$artifacts = .\gektor.exe workflow artifacts -workspace $workspace $run.run_id | ConvertFrom-Json
$report = $artifacts | Where-Object step_id -eq 'save'
.\gektor.exe workflow export-artifact -workspace $workspace $run.run_id $report.id errors.md
Get-Content "$workspace/errors.md"
```

The report contains two errors. Export refuses to replace an existing file; choose
another destination on later runs. Both the captured log and report remain in the
run store. Artifacts are retained until an operator runs `workflow prune-artifacts`.
