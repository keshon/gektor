# Reusable report workflow

`recipe.json` invokes `report.json` twice with different typed inputs. Each call
returns only its declared `artifact` output. Both reports belong to the same run;
their producing steps are `daily/save` and `handoff/save`.

Run in **PowerShell** from the repository root:

```powershell
$workspace = 'examples/workflows/subworkflow'
$rules = 'workflow_run=*,workflow_call=report.json,script_transform=*,artifact_write=*.md'
.\gektor.exe workflow validate -workspace $workspace recipe.json
$run = .\gektor.exe workflow run -workspace $workspace -yes -allow $rules recipe.json | ConvertFrom-Json
.\gektor.exe workflow inspect -workspace $workspace $run.run_id
.\gektor.exe workflow review -workspace $workspace -decision approve -allow $rules $run.run_id daily/save
$completed = .\gektor.exe workflow review -workspace $workspace -decision approve -allow $rules $run.run_id handoff/save | ConvertFrom-Json
.\gektor.exe workflow export-artifact -workspace $workspace $run.run_id $completed.outputs.daily.id daily.md
.\gektor.exe workflow export-artifact -workspace $workspace $run.run_id $completed.outputs.handoff.id handoff.md
```

Export refuses existing destinations. The first approval saves only the first report;
the second resumes at the next pending review without repeating the first write.
Both recipes are frozen when the run starts or is submitted to the queue. Editing
`report.json` later affects future submissions, not this run.
