# Workflow HTTP API

Configure authenticated loopback submission, webhook and callback routes alongside the workflow worker. This API is separate from the Agent [stdio RPC protocol](rpc.md).

## HTTP API and webhooks

`workflow serve -listen 127.0.0.1:9841 -api-config api.json` starts the queue
worker and a loopback HTTP listener together. The normal `-workspace`, `-allow`
and `-deny` flags apply. Omitting both HTTP flags keeps the queue-only service.
The listener accepts literal loopback addresses only. Remote access requires a
separately configured TLS reverse proxy; GEKTOR does not configure one.

Configuration names recipes and environment variables containing bearer tokens:

```json
{
  "token_env": "GEKTOR_WORKFLOW_API_TOKEN",
  "routes": {
    "note": {
      "recipe_path": "recipe.json",
      "token_env": "GEKTOR_NOTE_WEBHOOK_TOKEN"
    }
  }
}
```

Tokens must be at least 32 bytes with no surrounding whitespace. Use randomly
generated secrets. Each webhook token must differ from the operator token and
all other webhook tokens. Omit a route's `token_env` to expose it only to the
operator API. Tokens are read once at startup; configuration stores only their
environment variable names. Restart to rotate tokens or publish recipe changes.
Recipes and their subworkflow dependencies are frozen at startup.

All requests use `Authorization: Bearer TOKEN`. Endpoints:

- `POST /v1/submit/{route}`: operator submits a registered recipe.
- `GET /v1/jobs`: operator lists retained job statuses.
- `GET /v1/jobs/{id}`: operator reads one job status and its run ID.
- `GET /v1/status`: operator reads the same metadata snapshot as
  [`workflow status`](cli.md#workflow-queue-and-service), including worker lease
  ownership, last successful scheduler tick and queue/run/artifact counts.
- `POST /v1/jobs/{id}/cancel`: operator requests cancellation. Review-waiting
  jobs must instead be rejected through `workflow review`.
- `POST /hooks/{route}`: the route's webhook token submits only that recipe.

Submission bodies must be `application/json`, at most 64 KiB, with the shape
`{"inputs":{"text":"hello"}}`. Use `{"inputs":{}}` for recipes without inputs.
Recipe paths, policies and review approvals cannot be supplied by callers.
Webhook credentials cannot read jobs or cancel them. No browser CORS access is
enabled. The server bounds active authenticated requests to 32.

Status is also restricted to the operator token and returns no recipe paths,
inputs, outputs or artifact contents. HTTP 200 means the observation succeeded;
it is not a readiness or task-success guarantee. A stopped worker or an old tick
is reported in the response. Storage/lease observation failures return HTTP 503
with `status_unavailable`; unsupported status methods return HTTP 405.

An accepted submission returns HTTP 202 with `id`, `status`, `run_id`, `created`
and `cancel_requested`. Acceptance is not execution success: the worker checks
its policy and records failures. Inspect details and artifacts using the CLI.
Review gates still require an operator's CLI approval.

Every HTTP response carries a generated `X-Request-ID`. Error JSON also includes
`request_id` and a stable `code` such as `validation_rejected`, `submission_conflict`
or `queue_unavailable`. Request logs go to stderr with that ID, the endpoint,
configured route name, status, outcome code, elapsed time and accepted job ID.
Job start/finish logs link the job to its run ID and status. These logs omit raw
errors, request bodies, headers and idempotency keys. No incoming request ID is trusted.

To diagnose a failed accepted job, use `workflow job` and `workflow inspect`;
those local records contain the detailed error.

For rejected inputs, reproduce
with `workflow validate -inputs inputs.json recipe.json` under the same workspace
and policy. HTTP validation errors deliberately do not echo submitted values.

Send an `Idempotency-Key` header (up to 128 bytes) when retrying deliveries.
The same endpoint, credential, key and submission return the original retained
job; changed inputs or recipe content return HTTP 409. This survives restarts.
Keys stop deduplicating after terminal jobs are evicted from the 1,000-job queue
history. A new key, rotated token or different endpoint creates a new submission;
this is not an exactly-once guarantee for external effects.
Explicit `workflow prune-runs` also removes linked terminal jobs and ends their
submission deduplication window.

See [the webhook example](../../examples/workflows/webhook-note/README.md) for a
local PowerShell walkthrough without AI or external services.

## Callback waits

Configure callback channels separately from submission routes:

```json
{
  "token_env": "GEKTOR_WORKFLOW_API_TOKEN",
  "callbacks": {
    "approval": {"token_env": "GEKTOR_APPROVAL_CALLBACK_TOKEN"}
  }
}
```

At least one submission route or callback channel is required, with at most 64
of each. A channel name contains 1–64 ASCII letters, digits, underscores or
hyphens. Each callback token must differ from the operator, webhook and other
callback tokens. The operator token does not authorize callback delivery.

A pending [`workflow_wait_callback`](recipes.md#callback-waits) exposes its
`wait_id`, `step_id`, `channel` and `expires_at` through run inspection and the
initial run summary. Deliver to `POST /callbacks/{channel}/{run_id}/{wait_id}`
using the channel's bearer token and `Content-Type: application/json`:

```json
{"event_id":"approval-123","payload":{"approved":true}}
```

`event_id` is a nonempty opaque string of at most 128 bytes, without surrounding
whitespace or control characters. `payload` is required; any JSON value,
including null, is allowed. Its submitted and normalized JSON must fit 32 KiB;
the entire request must fit 64 KiB. Accepted payloads are untrusted data, never
recipe definitions or instructions. A run retains at most 1,000 callback receipts
and 1 MiB of normalized callback payloads.

HTTP 202 acknowledges durable storage, not successful continuation. The worker
rechecks its current permissions and resumes the existing run. Repeating the
same event ID and normalized payload returns 202 with `duplicate:true`, including
after consumption, without scheduling another continuation. Object key order
and insignificant whitespace are normalized; number spellings are preserved.
Another event ID or payload for that wait returns 409. Deduplication lasts while
the run is retained, independently of credential rotation.
Pruning run history removes those receipts and ends callback deduplication.

An unknown wait, wrong run/channel or delivery before the wait checkpoint returns
404. A first delivery after expiry, cancellation or closure returns 410. An event
already accepted before cancellation can still receive a duplicate acknowledgement;
that acknowledgement does not undo cancellation. Invalid or oversized events
return 400; storage failures return 503 without acknowledging acceptance.

Events are not buffered before a wait exists. This slice does not register
provider callbacks or supply provider-specific signatures. Use a trusted caller
that can obtain the pending receipt. Keep the service running; queued jobs and
due continuations alternate within each worker slot. A crash during resumed external work
still requires operator inspection and never triggers automatic replay.
