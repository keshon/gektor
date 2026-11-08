# Repository report

This pipeline fetches public repository metadata, transforms it into a small
record, pauses for review, then saves it. No credential or model is required.
From the repository root:

```powershell
go build -o gektor.exe ./cmd/gektor
.\gektor.exe workflow register-schema -workspace examples/workflows/repository-report repository_reports report.schema.json
.\gektor.exe workflow validate -workspace examples/workflows/repository-report recipe.json
.\gektor.exe workflow run -workspace examples/workflows/repository-report -yes -allow "workflow_run=*,http_request=GET https://api.github.com/repos/keshon/gektor,script_transform=*,record_write=repository_reports/*" recipe.json
.\gektor.exe workflow inspect -workspace examples/workflows/repository-report <run-id>
.\gektor.exe workflow review -workspace examples/workflows/repository-report -decision approve -allow "workflow_run=*,record_write=repository_reports/*" <run-id> save
.\gektor.exe workflow record -workspace examples/workflows/repository-report repository_reports gektor
```

Replace `<run-id>` with the run command's output. A second creation of `gektor`
fails with a revision conflict; supply a new `record_id` through `-inputs` for
another report. GitHub rate limits and network failures surface as failed steps.
Requests are not retried automatically.

For a model-assisted version, put a GEKTOR provider profile at `profile.json`
in this example directory, use `with-model.json`, and add
`model_generate=profile.json` to the run permission list. The model receives only
the normalized record; its summary is added without changing numeric facts.
Review and record inspection use the same commands.
