# RPC over stdio (`-serve`)

`agent -serve` speaks line-delimited JSON-RPC on stdin and writes event
lines plus id-responses on stdout. Human chatter always goes to stderr,
so stdout parses cleanly. Single-flight: one run and one pending gate at
a time; overlapping run/gate requests report an error instead of queueing.
This is TARS's compact JSON-RPC-style envelope, not a full JSON-RPC 2.0
implementation: requests use id/method/params without a required jsonrpc
field, and errors carry message rather than a standard numeric code.

## Methods

```json
{"id": 1, "method": "run", "params": {"task": "add Bye() to sample.go"}}
{"id": 2, "method": "respond", "params": {"answer": "y"}}
{"id": 3, "method": "cancel"}
```

- `run` starts an agent run. `"images": ["shot.png"]` attaches workspace-relative
  pictures like CLI `-image`. Each run gets a fresh task dir, like
  the CLI. Answers arrive as `{"id": 1, "result": {"answer": "..."}}`
  or `{"id": 1, "error": {"message": "..."}}`. The request
  supports task/images only; it does not resume an existing session.
- `respond` answers the currently suspended gate with raw text. The
  permission and approve/reject mappings live server-side. For permissions,
  `"y"`, `"yes"`, `"a"`, and `"always"` allow the pending call; other replies
  deny it. `"a"` and `"always"` also allow the same tool/resource pair for the
  rest of that run.
- `cancel` aborts the in-flight run. A second `run` while one is active
  is refused; `respond` with no gate pending is refused.

Malformed lines answer `{"id": null, "error": ...}`. Stdin EOF drains
the in-flight run before exiting (a piped one-shot closes stdin right
after its request), unless the run is suspended on a gate — its answer
was going to arrive on the stdin that just closed, so the run is
cancelled instead of hanging. Future gates fail closed after EOF; an
already-submitted answer is preserved. Ctrl+C cancels the run and drains
process cleanup before exit. Human chatter
always goes to stderr in serve mode, whatever `-mode` says.

## Events (no id)

The same vocabulary as `-mode json`, so one parser serves both:

```json
{"seq": 1, "event": "run_start", "task": "..."}
{"seq": 2, "event": "step", "label": "", "step": 0, "tool_calls": [{"name": "read_file", "args": "{...}"}]}
{"seq": 3, "event": "tool_result", "call_id": "call_1", "text": "...", "failed": false}
{"seq": 4, "event": "usage", "step": 0, "prompt": 2100, "completion": 120, "cached": 0}
{"seq": 5, "event": "awaiting_input", "kind": "permission", "id": "", "tool": "read_file", "resource": ".env", "prompt": "[permission] read_file on \".env\""}
{"seq": 6, "event": "input_answered", "kind": "permission", "id": ""}
{"seq": 8, "event": "result", "answer": "..."}
{"seq": 9, "event": "finding", "scope": "per-edit", "rule": "gofmt", "path": "a.go", "line": 3, "summary": "not gofmt-clean"}
{"seq": 10, "event": "nudge", "kind": "truncated", "text": "[harness] ..."}
{"seq": 11, "event": "delta", "text": "partial answer..."}
{"seq": 12, "event": "reasoning", "text": "partial deliberation..."}
```

Direct runs also emit `outcome` with `completed`, `failed` or `cancelled`,
observed `files`, current `verification`, persistence `warnings`, open todos
and the `resume` target. Tool results carry a boolean `failed` independently
of their text; older producers may omit it.

`kind` is `ask_user`, `permission`, or `budget`. Permission gates
also carry `tool`, `resource`, and the operator-facing `prompt` (ask
gates carry `prompt` only), so a client can name the gated call
without keeping gate state. A `respond` answer of `n: <note>` denies
with the note attached: the refusal the model sees carries the
operator's redirect. Deterministic post-write checks surface as
`finding` events (`scope` per-edit or session-end); they are
report-only, never gates. Both scopes are produced by every frontend
that reports findings — TUI, print and json — and one run's
scopes share a dedupe cache, so a file dirty throughout a run is
reported once rather than once per pass. `per-edit` scans content a
tool addressed by `path`; `session-end` re-reads every path the run
changed, which is the only pass that sees shell redirections, MCP
writes and background jobs. Loop-generated harness notices surface as
`nudge` events (`kind` overflow/truncated/leak/compact/repeating/verification): the same `[harness]`-marked text the model
saw, so observers can audit every intervention. Streamed content
chunks surface as `delta` events, and streamed deliberation as
`reasoning` events (display-only; the step event carries the
authoritative text and replaces whatever was live). A thinking model
emits `reasoning` before `delta`, so a client can render deliberation
finishing before the answer starts. Serve mode shares backend/authentication/context configuration, workspace,
permissions, MCP discovery, response and step budgets, streaming, audit logging,
and verification settings with the CLI. `-yes` auto-denies permission Ask
requests. `-plan`, `-resume`, `-fork`, initial CLI task/images, session browsing,
slash commands and inline `@` expansion are not RPC request features. Images
come from each request. Stdout remains protocol output regardless of `-mode`.

## Client behavior

Keep stdin open when a run can ask for permission, clarification, or budget
approval. Respond only after awaiting_input, and continue reading until the
original run ID receives its result/error. Respond and cancel requests get
separate `{"ok": true}` results; a cancel acknowledgement does not mean
process cleanup has finished. Wait for the run response before starting again.

## Example

```bash
printf '%s\n' \
  '{"id":1,"method":"run","params":{"task":"what files are here?"}}' |
  go run ./cmd/agent -serve -backend-kind llama -backend http://127.0.0.1:8080 -workspace ./site |
  while IFS= read -r line; do echo "$line" | jq -c '{event, id, result}'; done
```

PowerShell one-shot (read-only, no interactive replies):

```powershell
'{"id":1,"method":"run","params":{"task":"what files are here?"}}' |
  go run ./cmd/agent -serve -backend-kind llama -backend http://127.0.0.1:8080 -workspace .
```

One-shot EOF denies future gates; use a persistent client for interactive runs.
