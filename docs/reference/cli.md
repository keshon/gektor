# CLI reference

From the extracted application folder, run a task in your project:

```powershell
.\gektor.exe -workspace "D:\path\to\project" -verify-cmd "go test ./..." "Fix the failing test"
```

Put flags before the task text. `-verify-cmd` runs after Agent finishes and fails the run if the command fails. Use `.\gektor.exe -tui` for the interactive terminal. On Linux, use `./gektor`.

## Invocation

Run from the extracted application folder. Use `gektor.exe` on Windows and
`./gektor` on Linux. The examples using `go run` require a source checkout;
see [building from source](../development/README.md).

```bash
go run ./cmd/gektor [flags] "task description"
./gektor.exe -tui
go run ./cmd/gektor -resume .gektor/tasks/<id>
```

Put all flags before the task: Go's flag parser stops at the first positional
argument. Run `go run ./cmd/gektor -h` for the binary's registered flags.
Direct execution is the default. An empty task is allowed for `-tui`,
`-serve` and resume/fork; ordinary print mode needs one.

`-resume` restores the workspace recorded in the saved snapshot, even when
launched from a different directory. An explicit `-workspace DIR` overrides that
assignment. For example:

```bash
gektor -workspace /path/to/project -resume /path/to/session/state.json
```

If the saved workspace is missing or an older snapshot has no workspace metadata,
resume stops rather than silently using the current directory. Restore the saved
directory or pass `-workspace` explicitly. The selected workspace is recorded by
the resumed agent when it saves; merely resolving a resume path does not rewrite
the archive.

### Saved workflows

For the TUI, `-tui-screen agent|workflows` chooses the initial screen and
requires `-tui`. It keeps saved settings. The workflows screen starts idle;
it cannot be combined with an opening task, resume, fork or RPC service.

`workflow validate` and `workflow run` operate without a model backend. They
print a JSON result to stdout, diagnostics and permission prompts to stderr,
and exit nonzero on failure. A failed run can still print its saved record path.

```powershell
.\gektor.exe workflow schema
.\gektor.exe workflow draft recipe.json -workspace . -backend-kind llama -backend http://127.0.0.1:9931 "Prepare a reusable image resize and comparison recipe"
.\gektor.exe workflow validate -workspace . -inputs inputs.json recipe.json
.\gektor.exe workflow run -workspace . -inputs inputs.json -yes recipe.json
.\gektor.exe workflow runs -workspace .
.\gektor.exe workflow inspect -workspace . <run-id>
.\gektor.exe workflow record -workspace . assets image
.\gektor.exe workflow register-schema -workspace . assets assets.schema.json
```

For validate/run, put flags before the recipe path. `-inputs` selects a JSON
object file inside the workspace, up to 64 KiB. Its read is permission-checked.

These commands support `-workspace`, `-allow`, `-deny`, `-yes` and
`-run-timeout` (default 5m, 0 disables). `-yes` denies Ask-gated operations;
it does not grant permission. Cancellation is cooperative during image encoding.
Model flags, profiles, browser and external adapters do not apply to execution.

Runs are stored outside the workspace under `~/.gektor/workflows`, scoped to
the workspace's canonical path. Inspect by run ID; `record_path` is absolute.

`runs`, `inspect` and `record` support `-workspace`. `record <kind> <id>`
reads a stored entry, including its revision and schema hash, without a model.

`register-schema` takes a kind and a
workspace schema file after its `-workspace` flag; existing schemas cannot change.

For draft, put the destination immediately after `draft`, followed by ordinary
agent flags and the task. It uses the selected model/profile but a restricted
role: read files, inspect/preview images, fetch workflow schemas, validate and
save only the selected recipe.

There is no shell, delegation, external adapter
activation, image writing or workflow execution. The model budget is at most
16 steps. It cannot resume/fork sessions or use TUI/serve/Plan mode, attached
images, browser/MCP/capability flags, verification commands or inspection-only
startup flags.

Its history is saved, but resuming it as a generic coding session
does not preserve these restrictions. Use `workflow draft` again to revise it.

Saved definitions, input binding, supported operations and execution limits are
documented in [Workflows](recipes.md).

Local llama-server example:

```bash
./gektor.exe -tui -backend-kind llama -backend http://127.0.0.1:9931 -stream
```

For LM Studio or a hosted OpenAI-compatible service, use the `openai` dialect
and the provider's API root, including `/v1` when required:

```bash
./gektor.exe -tui -backend-kind openai -backend http://127.0.0.1:1234/v1 -model your-model -context-limit 16384
```

For a hosted service, set a key in the environment or select its variable
with `-api-key-env`; supply the provider's actual model ID and context limit.
The terminal controls and local slash commands are in [tui.md](terminal.md).

### ChatGPT account

Use `auth login`, `auth models` and `auth test <model-id>` before running a task.
`auth status`, `auth accounts`, `auth select <id>` and `auth logout` manage the
connection. `auth login --new` adds another registration.
See [Connect a model](../guide/models.md#chatgpt-account) for setup and limitations.
`-reasoning-effort` selects a supported level; `-reasoning-summary` requests a
provider summary. These controls currently apply only to ChatGPT.

## Backend

| Flag | Default | Meaning |
|---|---|---|
| `-backend URL` | `http://127.0.0.1:9931` | Backend base URL: bare origin for a local server, API root for an OpenAI-compatible provider |
| `-backend-kind KIND` | `llama` | `llama`, `openai`, `kobold`, or `chatgpt`. Select the protocol; `chatgpt` uses browser OAuth and Responses |
| `-model NAME` | `local` | Model name. Ignored by most local servers (they serve whatever weights they started with); required for remote providers |
| `-context-limit N` | `0` | Remote context window or local fallback. A reported local server window takes precedence; remote backends fall back to a per-model estimate unless set |
| `-grammar` | `true` | Apply the backend's own content grammar (blocks a model writing its native tool-call tags as plain text on koboldcpp) |
| `-max-tokens N` | `8192` | Generation budget per response. Too low truncates large outputs mid-JSON, which looks like a model failure |
| `-context-report` | off | Print a fresh-run context breakdown as JSON and exit; no model, adapters or workspace writes. Lists estimated sections and tool costs |
| `-max-steps N` | saved limit or 25 | Fixed direct-run step limit; explicit `0` means unlimited. Todo cannot extend it. Delegates have separate finite limits |
| `-delegate-steps N` | saved TUI setting or 12 | Default model requests per delegate, including its final answer; must be positive |
| `-delegate-max-steps N` | saved TUI setting or delegate default | Maximum task-specific allowance accepted by `delegate_task`; must be at least the default |
| `-run-timeout D` | disabled | Total duration such as `5m`. Reserves up to 30 seconds (20%) for a tool-free incomplete-work report. A timed-out task still fails; cancellation does not request a closing answer |

The TUI saves delegate limits in `application.json`, under
`harness.delegate_steps` and `harness.delegate_max_steps`. Omitted or zero saved
values use the defaults; explicit CLI limits must be positive. For example,
`-delegate-steps 12 -delegate-max-steps 24` lets a task request up to 24 steps.
The model cannot raise this maximum. Increasing the direct-run limit does not
increase delegate limits.

When a delegate stops, its report includes the reason, used steps, elapsed time,
available token counts, observed changed paths and recent tool-result references.
Long reports are shortened; the archive retains full results. Model summaries
are labelled separately. An exhausted delegate remains a failed tool call even
if it changed files. The parent decides what to do next; the harness does not
automatically restart the child.

The parent can inspect full child messages with `read_history` and the report's
`delegate_call_id`. This selects a child of the current session, not an arbitrary
filesystem path. Normal history paging and search still apply.

## Authentication (remote providers)

The key resolves as `-api-key`, then `-api-key-env`, then the first token
found under `GEKTOR_API_KEY`, `OPENAI_API_KEY`, `OPENROUTER_API_KEY`,
`DEEPSEEK_API_KEY`, `GROQ_API_KEY`, `TOGETHER_API_KEY`, `ANTHROPIC_API_KEY`.

| Flag | Default | Meaning |
|---|---|---|
| `-api-key KEY` | — | Bearer token. Prefer env vars: a flag lands in shell history |
| `-api-key-env NAME` | — | Env var holding the bearer token, e.g. `OPENROUTER_API_KEY` |

## Task and modes

| Flag | Default | Meaning |
|---|---|---|
| `-workspace DIR` | `.` | Workspace root the agent may read and write |
| `-plan` | `false` | Plan mode: proposes a plan and changes nothing; mutating tool calls are refused |
| `-yes` | `false` | Print/RPC permission Ask calls are denied instead of prompting. TUI permissions remain interactive |
| `-audit PATH` | - | Append operator gate decisions as JSONL to PATH (off when empty) |
| `-verify-cmd CMD` | — | Final command; record output and fail on a command error without another model call |
| `-resume PATH` | — | Continue an interrupted run from its saved state |
| `-fork PATH` | — | Branch from a prior transcript file but write to a fresh task id |
| `-answer TEXT` | — | Answer to supply with `-resume` when the run paused on `ask_user` |

## Safety and permissions

Policy allows ordinary calls, asks for sensitive reads (`*.env`, credentials,
secrets) and known destructive shell/background command shapes, and applies
CLI overrides afterward. Deny is terminal; Ask without an approval handler
fails closed. Rules are pattern tripwires, not process isolation. See
[SECURITY.md](../../SECURITY.md) for the trust boundary.

| Flag | Default | Meaning |
|---|---|---|
| `-allow RULES` | — | Comma-separated `tool=pattern` rules to allow, e.g. `"run_shell=go *,read_file=*.go"`. Wins over defaults |
| `-deny RULES` | — | Comma-separated `tool=pattern` rules to deny, e.g. `"run_shell=rm *,read_file=.env"`. Wins over `-allow` |
| `-mcp SERVERS` | - | MCP servers as `"name=cmd args...;name2=https://host/mcp"` over stdio JSON-RPC or Streamable HTTP. Tools appear as `mcp__name__tool`. A server that fails to start is skipped with a warning |
| `-capabilities FILE` | — | Explicit JSON configuration for optional tool packs and extra skill roots. See [Extensions](../guide/extensions.md) |
| `-browser CHANNEL` | - | Make installed `chrome` or `msedge` available through `enable_browser`. Check prerequisites, prepare pinned Playwright MCP and load its tools only on activation. Setup failures are tool results. See [browser setup](../guide/browser.md) |
| `-browser-profile NAME` | `default` | Dedicated persistent browser profile. Saved conversations restore their profile unless this flag explicitly overrides it. Browser files stay under `~/.gektor/browser-profiles/` |
| `-browser-headless` | `false` | Hide the browser window for unattended checks. Interactive login uses the visible default mode |

## Output and debugging

| Flag | Default | Meaning |
|---|---|---|
| `-mode MODE` | `print` | `print` (human-readable) or `json` (one JSON object per line on stdout; human chatter goes to stderr so the stream pipes cleanly) |
| `-serve` | `false` | Serve JSON-RPC over stdio instead of running one task: methods `run`/`respond`/`cancel`, events on stdout. See [stdio RPC](rpc.md) |
| `-image PATHS` | — | Attach pictures to the task (comma-separated, e.g. `-image shot.png,plan.webp`). In the TUI use `@path` inline instead (`@"my shot.png"` when the name has spaces). Text references are also supported in the TUI; `-image` remains image-only. Supports ChatGPT models with image input, a vision-capable OpenAI-compatible provider, or llama-server with `--mmproj` and VL weights. Koboldcpp refuses loudly; a text-only model on a vision server fails at the backend, not silently |
| `-stream` | `false` | Enable streaming in print, JSON and RPC modes. JSON/RPC carry chunks as delta events; the TUI enables streaming automatically. Supports OpenAI, llama and KoboldCPP |
| `-debug` | `false` | Log raw request/response JSON plus per-call token usage to `agent-debug.log` |
| `-log-max N` | `300` | Max characters per line in step console output. `0` means no limit. The final answer always prints whole |

## Mode-specific limits

The TUI honors Act/Plan/resume/fork modes. Slash commands and inline
text-file references are UI features, not special syntax in print mode.
Streaming is automatic in the TUI and opt-in with `-stream` in print, JSON and RPC
modes. JSON/RPC carry streamed chunks as events. Server support is still required.

The RPC server takes tasks and images from each request.
It does not expose session browsing, resume/fork, Plan/Act switching, or local
slash commands. The per-run step limit also applies in serve mode. See [rpc.md](rpc.md) for the supported
run configuration and event contract.

## Profiles

`-profile <path>` loads a versioned JSON provider, generation and identity
profile. Explicit CLI flags override profile values. Existing commands without a
profile retain their defaults. `-check-profile` validates and prints the effective
configuration without creating a workspace, contacting a backend, or starting any
process.
Secrets remain environment references; the printed profile contains no API key.
This checks configuration, not backend availability or model capacity.

```cmd
gektor.exe -profile profiles\llama-external.json -check-profile
gektor.exe -profile profiles\llama-external.json -tui
```

See [profile files](configuration.md#profile-files) for the schema and bundled profiles.

## Identity flags

`-user-name` sets who the assistant addresses you by (default `User`).
`-system-prompt` sets the base instruction, and `-post-history` a directive
placed after the latest turn. `-temperature` controls sampling; 0 retains the
transport default of 0.4, and positive values up to 2 are explicit. Literal
greedy zero is not supported by the model transport.

The agent's own Plan/Act operational instructions are added on top of these and
are not configurable — they are the part that makes it an agent rather than a
chat completion.

Prompt presets (`prompt_preset`) add response style on top. `coding`,
`investigation`, `explanation` and `concise` suit coding work; `conversation`,
`roleplay` and `storytelling` are for a plain assistant and will read oddly in a
tool-using loop. Preset text may reference `{{char}}`, `{{user}}` and
`{{persona}}`, which are substituted at compile time. A macro that is left
unfilled is reported as a warning in the transcript rather than shipped to the
model as literal text.

```cmd
gektor.exe -profile profiles\llama-external.json -user-name Alex -system-prompt "be terse"
```

## SSH credentials

For SSH passwords and private keys, see
[supplying SSH credentials](operations.md#supplying-credentials-in-the-cli-and-tui).
SSH currently uses named user profiles and environment references; there is no
dedicated SSH credential CLI flag or TUI editor.

## Workflow queue and service

`workflow submit [-workspace dir] [-inputs inputs.json] recipe.json` validates
and queues a snapshot. `workflow jobs` lists jobs; `workflow job <id>` shows one;
`workflow cancel <id>` cancels pending work or requests cancellation of a running job.
These commands accept `-workspace`.

`workflow serve [-workspace dir] [-allow tool=pattern] [-deny tool=pattern]`
runs one local worker until Ctrl+C. Run it under your OS process supervisor for
unattended use. Permissions belong to this process, not to queued recipes.
Requests that need interactive approval are denied.

Add `-listen 127.0.0.1:9841 -api-config api.json` to enable authenticated HTTP
submission and webhook routes alongside the worker. Both flags are required
together and are accepted only by `serve`: `-listen` selects the loopback address,
and `-api-config` names a workspace-relative route configuration file.
See [HTTP API and webhooks](http-api.md#http-api-and-webhooks).

`workflow schedule -cron "0 9 * * 1-5" -timezone Europe/Moscow -missed once
-overlap skip daily recipe.json` saves a schedule and recipe snapshot.
All flags must precede the schedule ID and recipe path. `-inputs` binds a saved
input object. `-cron` uses five fields (minute, hour, day, month, weekday);
`-timezone` is an IANA name, default UTC. `-missed` is `skip` (default) or `once`;
`-overlap` is `skip` (default) or `queue`. Missed `skip` triggers older than one
minute are discarded. `once` queues one catch-up job, never a burst.

`workflow schedules`, `workflow pause <id>`, `workflow resume <id>`, and
`workflow unschedule <id>` inspect and manage schedules. Resume starts from the
next future occurrence. The service must be running to process triggers.
The queue retains 1000 jobs, evicting the oldest terminal job when needed;
active jobs are never evicted. Run records remain available through `workflow inspect`.
A service restart marks unfinished jobs interrupted; inspect their effects before resubmitting.

`workflow review -decision approve|reject [-workspace dir] [-allow tool=pattern]
<run-id> <step-id>` records an operator decision and, on approval, continues the
saved run. `-decision` is required. Completed steps are not repeated. Inspect
resolved arguments with `workflow inspect` before approving. Agent tools cannot
approve their own runs. Reject a waiting run through this command rather than `cancel`.



## Workflow credential references

`workflow register-credential -origin https://api.example.com -env EXAMPLE_TOKEN
-header Authorization -prefix "Bearer " example` registers a named reference.
`-origin` pins the exact HTTPS origin. `-env` names the environment variable read
at execution time. `-header` accepts `Authorization` or `X-API-Key`; `-prefix`
defaults to `Bearer ` (pass an empty string for an API key). The name is the final
argument. Registration refuses to overwrite an existing entry.

Only metadata is saved in `~/.gektor/credentials/<name>.json`; the secret stays in
the environment of the executing process. A running service must already have
that variable. Recipes use `"credential":"example"`. Permission checks cover
both `METHOD URL` and `credential:example@https://api.example.com` under the
`http_request` tool name. An origin mismatch fails before sending the request.



## Workflow artifacts

`workflow artifacts [-workspace dir] <run-id>` lists immutable artifact metadata.
`workflow export-artifact [-workspace dir] <run-id> <artifact-id> <destination>`
verifies the saved bytes and copies them to an explicit workspace-relative path.
The destination parent must exist. Export refuses existing files and paths outside
the workspace. These operator commands acquire the run lease; a currently executing
run reports that it is in use. Paused and completed runs are available.

`workflow prune-artifacts [-workspace dir] -older-than 168h` deletes artifacts of
completed, failed or cancelled runs whose finish time is older than seven days.
`-older-than` is required and must be positive. Active, interrupted and waiting-review
runs are retained. Run records and structured records are never deleted. Retention
is explicit; no background cleanup runs automatically.

Reusable recipes use the `workflow_call` node and declare named `outputs`.
Completed run/review summaries include those outputs. Nested reviews use qualified
step IDs such as `daily/save`; the CLI prints the exact pending ID. Submit and
schedule snapshot the root recipe and all called recipes together. See
[subworkflows](recipes.md#reusable-subworkflows) for syntax and limits.

## Create and run from the command line

Use `gektor workflow draft recipe.json [agent flags] "task"` for a constrained
authoring session. It can read documents, inspect or preview images, list archive
contents and save only that recipe; it cannot execute the recipe, write image variants or launch processes. Schema
details are loaded through `workflow_schema`, rather than added to every prompt.

The author must actually save and successfully validate the selected recipe
before its run can report success. Verification revalidates the current file
with the same input arguments, so a later edit cannot reuse stale validation.

A save alone checks only the definition's structure. Validation does not establish
image compatibility or execute any operation. A draft needing unknown input
values remains available on disk but cannot pass verification yet.

Run `gektor workflow validate [flags] recipe.json` and
`gektor workflow run [flags] recipe.json` to execute without an agent session. Only `model_generate` constructs a model client.
`-inputs inputs.json` supplies a workspace JSON object. See the
[CLI reference](cli.md#saved-workflows) for permissions, flags and drafting limits.

## CLI review guidance

When `workflow run` or `workflow review` pauses for review, stderr shows inspection,
approval and rejection commands with the actual run ID and pending step. Commands
retain the workspace and existing allow/deny rules; approval does not grant additional
permissions. Stdout remains JSON. Use the run ID, not a step name, for `inspect` and
`review`; `workflow runs -workspace <dir>` lists runs for that workspace.

## Windows launchers

`start-gektor-agent.cmd` opens Agent; `start-gektor-workflows.cmd` opens Workflows.
Both restore saved settings. Browser access is optional with `-browser chrome`.
In a source checkout it rebuilds the binary first; release archives use the
included executable without requiring Go.

The root also has `run-gektor-agent.cmd`, `run-gektor-workflow.cmd` and
`serve-gektor-workflows.cmd` for CLI use. Relative paths use the caller's folder.
See [launchers and helpers](../../scripts/README.md) and [demos](../../examples/demos/README.md).
No backend launchers are included.

| Launcher | Purpose |
|---|---|
| `start-gektor-agent.cmd` | Open Agent with saved settings |
| `start-gektor-workflows.cmd` | Open Workflows with saved settings |
| `run-gektor-agent.cmd [flags] "task"` | Run one Agent task |
| `run-gektor-workflow.cmd [flags] recipe.json` | Run one saved recipe |
| `serve-gektor-workflows.cmd [flags]` | Run the workflow worker until stopped |

Relative paths use the caller's current directory, even when the launcher is
installed elsewhere. Keep flags before task text or the recipe path. Launchers
do not choose a backend or grant permissions. Agent CLI uses normal flag/profile
defaults; workflows use the profiles named by their model nodes.
