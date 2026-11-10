# Agent stdio RPC

Use `gektor -serve` to run Agent from another program. Keep stdin open, send a
`run` request, and read events until that request receives its result or error.
If a gate asks for input, answer it with `respond`.

This page covers request methods, event handling and client lifecycle.
Workflow HTTP ingress has a [separate reference](http-api.md).

## RPC over stdio (`-serve`)

Send one JSON request per line on stdin. GEKTOR writes events and responses on
stdout; human-readable messages go to stderr, regardless of `-mode`.

Only one run and one pending gate are allowed at a time. Overlapping run or gate
requests return an error instead of entering a queue.

Launch settings apply to every RPC run: profile identity, generation settings,
permission rules, deterministic checks and `-plan`. Plan mode enforces read-only
tools just as it does in CLI and TUI. `-tui`, inline tasks, `-resume`, `-fork`,
`-answer` and `-image` are rejected with `-serve`: tasks and images arrive in run
requests, and answers arrive through `respond`.

The envelope is JSON-RPC-style, with these differences from full JSON-RPC 2.0:

- Requests use `id`, `method` and `params`; `jsonrpc` is not required.
- Errors contain `message` rather than a standard numeric code.
- Malformed lines receive `{"id": null, "error": ...}`.

## Methods

These lines show the three request shapes. Send `respond` only after a gate asks
for input; send `cancel` when you intend to stop the active run.

```json
{"id": 1, "method": "run", "params": {"task": "add Bye() to sample.go"}}
{"id": 2, "method": "respond", "params": {"answer": "y"}}
{"id": 3, "method": "cancel"}
```

### Start a run

`run` accepts `task` and optional `images`. For example, `"images": ["shot.png"]`
attaches workspace-relative pictures, like CLI `-image`.

Each run creates a fresh task directory. The request does not resume a session.
Its response is either `{"id": 1, "result": {"answer": "..."}}` or
`{"id": 1, "error": {"message": "..."}}`.

### Answer a gate

`respond` sends raw answer text to the currently suspended gate. The server
interprets permission and approve/reject answers. A reply with no pending gate
is refused.

For permission requests:

| Answer | Effect |
|---|---|
| `y` or `yes` | Allow the pending call |
| `a` or `always` | Allow the pending call and the same tool/resource pair for the rest of this run |
| `n: <note>` | Deny and attach the operator's redirect note to the refusal the model sees |
| Other text | Deny |

### Cancel a run

`cancel` aborts the active run. Its acknowledgement does not mean cleanup has
finished. Wait for the original `run` response before starting another run.

## Events (no id)

Events share the vocabulary used by CLI `-mode json`, so a client can use the
same parser for both. These are representative event lines:

```json
{"seq": 1, "event": "run_start", "task": "..."}
{"seq": 2, "event": "step", "label": "", "step": 0, "tool_calls": [{"name": "read_file", "args": "{...}"}]}
{"seq": 3, "event": "tool_result", "call_id": "call_1", "text": "...", "failed": false}
{"seq": 4, "event": "usage", "step": 0, "prompt": 2100, "completion": 120, "cached": 0}
{"seq": 5, "event": "awaiting_input", "kind": "permission", "id": "", "tool": "read_file", "resource": ".env", "prompt": "[permission] read_file on \".env\""}
{"seq": 6, "event": "input_answered", "kind": "permission", "id": ""}
{"seq": 8, "event": "result", "answer": "..."}
{"seq": 9, "event": "finding", "scope": "per-edit", "rule": "secret-aws", "path": "settings.txt", "line": 3, "summary": "possible secret (secret-aws)"}
{"seq": 10, "event": "nudge", "kind": "truncated", "text": "[harness] ..."}
{"seq": 11, "event": "delta", "text": "partial answer..."}
{"seq": 12, "event": "reasoning", "text": "partial deliberation..."}
```

Use the sections below to decide which events update the display, require an
answer or carry the final result. Events are separate from the response to the
original request ID.

## Permissions and questions

`awaiting_input` pauses for an answer. Its `kind` is `ask_user`, `permission` or
`budget`. `input_answered` reports that an answer was supplied.

Permission gates carry `tool`, `resource` and the operator-facing `prompt`, so
the client can show the requested action without reconstructing gate state.
Ask gates carry `prompt` only. Send the answer through `respond`.

## Post-write findings

`finding` reports a deterministic check after a write. Findings are reports,
never approval gates.

| `scope` | Content checked |
|---|---|
| `per-edit` | Content a tool addressed by `path` |
| `session-end` | Every path changed by the run, including changes from shell redirections, MCP writes and background jobs |

Both scopes are produced by every frontend that reports findings: TUI, print
and json. They share a deduplication cache within a run. A file that stays dirty
is reported once rather than once per pass.

## Harness notices and saved history

`nudge` reports a queued harness notice. Its `kind` is `overflow`, `truncated`,
`leak`, `compact`, `repeating` or `verification`. The text includes the same
`[harness]` marker used in the model-facing notice.

A queued notice is not proof that a request included it. The `context` event
confirms inclusion and carries `text`, `role`, `request` and `kind`.

Local persistence warnings use `local_warning`. They are not model messages.

Rejected malformed calls retain `raw_arguments` in the archive. They are omitted
from replayed tool_calls and replaced by an explicit model-facing diagnostic.

## Streaming and tool lifecycle

Treat streamed chunks as previews. Replace them with authoritative content when
it arrives:

| Event | Client action |
|---|---|
| `reasoning` | Display streamed deliberation; thinking models emit it before `delta` |
| `delta` | Display streamed answer text |
| `step` | Replace live reasoning/answer previews with the authoritative text |
| `tool_output` | Append a foreground shell output preview to the pending call identified by `call_id` |
| `tool_finished` | Mark execution as returned; this is not a success verdict |
| `tool_result` | Replace that call's preview with its authoritative result |

`tool_output` carries a UTF-8 chunk in `text`. Previews are capped at 48 KiB per
call, followed by a limit notice if needed. They are not added to model history.
Output buffered by a child process appears only when that process flushes it.

`tool_finished` carries `call_id` before ordered result processing. The later
`tool_result` carries `status` and a boolean `failed` independent of its text;
older producers may omit `failed`. A status of `skipped_for_input` means new
user input prevented the tool from starting (`failed: false`).

Direct runs also emit `outcome`: `completed`, `failed` or `cancelled`, with
observed `files`, current `verification`, persistence `warnings`, open todos and
the `resume` target.

## Context budgets

`prompt_budget` carries a `report` before each request. It contains
character-based section/tool estimates, followed by backend prompt/cached counts
when available. Reports contain sizes and names, not instruction or conversation
contents.

Backend tokenization can differ from these estimates. Cached tokens still
occupy context.

## Launch configuration

Serve mode shares the CLI's backend, authentication, context configuration,
workspace, permissions, MCP discovery, response/step budgets, streaming, audit
logging and verification settings. `-yes` auto-denies permission Ask requests.

RPC requests do not provide `-plan`, `-resume`, `-fork`, initial CLI task/images,
session browsing, slash commands or inline `@` expansion. Images come from each
`run` request. Stdout remains protocol output regardless of `-mode`.

## Client behavior

For interactive runs:

1. Keep stdin open while GEKTOR may ask for permission, clarification or budget approval.
2. Send `run` and read the event stream.
3. Answer only after `awaiting_input`. A `respond` request receives its own
   `{"ok": true}` result.
4. To stop, send `cancel`. It also receives a separate `{"ok": true}` result.
5. Continue reading until the original run ID receives its result or error.
   Only then start another run.

### EOF and process shutdown

When stdin closes, GEKTOR drains the active run before exiting. If the run is
already suspended on a gate, it is cancelled because its answer can no longer
arrive. Future gates fail closed; an already-submitted answer is preserved.

Ctrl+C cancels the run and drains process cleanup before exit.

### In-process steering

In-process sessions accept steering through `Session.Submit(text, images...)`.
A `user_input` event acknowledges insertion into saved history and carries
`text` and `images`. Inputs never split tool calls from their results.

The stdio RPC API does not expose a submit operation.

## Example

These one-shot examples require a source checkout and a running local model.
They close stdin after one request, so future gates are denied. Use a persistent
client for interactive runs.

```bash
printf '%s\n' \
  '{"id":1,"method":"run","params":{"task":"what files are here?"}}' |
  go run ./cmd/gektor -serve -backend-kind llama -backend http://127.0.0.1:9931 -workspace ./site |
  while IFS= read -r line; do echo "$line" | jq -c '{event, id, result}'; done
```

PowerShell one-shot (read-only, no interactive replies):

```powershell
'{"id":1,"method":"run","params":{"task":"what files are here?"}}' |
  go run ./cmd/gektor -serve -backend-kind llama -backend http://127.0.0.1:9931 -workspace .
```
