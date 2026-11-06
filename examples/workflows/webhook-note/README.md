# Webhook note

Run from the repository root in PowerShell. No model or external service is needed.
The recipe writes the supplied text to a run artifact.

Generate two tokens in the first terminal:

```powershell
$env:GEKTOR_WORKFLOW_API_TOKEN = [guid]::NewGuid().ToString('N') + [guid]::NewGuid().ToString('N')
$env:GEKTOR_NOTE_WEBHOOK_TOKEN = [guid]::NewGuid().ToString('N') + [guid]::NewGuid().ToString('N')
$env:GEKTOR_NOTE_WEBHOOK_TOKEN
.\gektor.exe workflow serve -workspace examples/workflows/webhook-note -listen 127.0.0.1:9841 -api-config api.json
```

Copy the printed webhook token into a second PowerShell terminal:

```powershell
$token = Read-Host 'Webhook token'
$headers = @{ Authorization = "Bearer $token"; 'Idempotency-Key' = 'note-demo-1' }
$job = Invoke-RestMethod -Method Post -Uri http://127.0.0.1:9841/hooks/note -Headers $headers -ContentType application/json -Body '{"inputs":{"text":"Hello from a webhook"}}'
$job
.\gektor.exe workflow job -workspace examples/workflows/webhook-note $job.id
```

Submitting that same request again returns the same job. Use a new idempotency
key for another note. After the job completes, copy its `run_id` into:

```powershell
$run = Read-Host 'Run ID'
.\gektor.exe workflow artifacts -workspace examples/workflows/webhook-note $run
```

Use Ctrl+C in the first terminal to stop the server and worker. Queued jobs stay
on disk. A running job receives cancellation. Tokens are local to each terminal;
keep the same values to preserve deduplication across server restarts.
