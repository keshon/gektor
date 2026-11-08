# CLI reference

`-capabilities <file>` selects versioned JSON configuration for optional MCP
and executable packs and additional skill roots. Packs load on request through
`discover_capabilities` and `enable_capability`. See [Extensions](extensions.md)
for configuration, executable protocol, permissions and skill precedence.

Every flag of `cmd/gektor`, grouped by purpose. A conventions test
(`internal/conventions`, tag `cli-flags`) fails if a flag exists in code
but is missing here. It checks coverage, not defaults or semantics; those
are verified against the implementation.

## Invocation

Build the terminal application with `go build -o gektor.exe ./cmd/gektor`.
Run the built executable so bundled profiles resolve from the project directory.

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
reads a stored entry, including its revision and schema hash, without a model. `register-schema` takes a kind and a
workspace schema file after its `-workspace` flag; existing schemas cannot change.

For draft, put the destination immediately after `draft`, followed by ordinary
agent flags and the task. It uses the selected model/profile but a restricted
role: read files, inspect/preview images, fetch workflow schemas, validate and
save only the selected recipe. There is no shell, delegation, external adapter
activation, image writing or workflow execution. The model budget is at most
16 steps. It cannot resume/fork sessions or use TUI/serve/Plan mode, attached
images, browser/MCP/capability flags, verification commands or inspection-only
startup flags. Its history is saved, but resuming it as a generic coding session
does not preserve these restrictions. Use `workflow draft` again to revise it.

Saved definitions, input binding, supported operations and execution limits are
documented in [Workflows](workflows.md).

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
The terminal controls and local slash commands are in [tui.md](tui.md).

### ChatGPT account

The `chatgpt` backend uses browser sign-in and eligible ChatGPT plan usage;
it does not require an API key. Connect before starting a task:

```powershell
.\gektor.exe auth login
.\gektor.exe auth models
.\gektor.exe auth test gpt-6.1-sol
.\gektor.exe -tui -backend-kind chatgpt -model gpt-6.1-sol -context-limit 32768
```

Use an available model ID from `auth models`. The endpoint is fixed to
`https://api.openai.com/v1`; local-server URLs and API-key environment variables
do not apply. `auth status` shows the connection; `auth logout` revokes it.
`auth login --new` adds an account, `auth accounts` lists registrations and
`auth select <id>` selects one for subsequent requests. Switch accounts between
runs. Credentials are independent of workspace profiles and session history.

Credentials live under `~/.gektor/auth/chatgpt`. Windows encrypts the store with
user-bound DPAPI; Unix uses a private directory and owner-only file. Refreshes
are locked across processes. Login callbacks validate state, PKCE, the signed
ID token, nonce and the granted plan-usage scope. Tokens never enter profiles,
transcripts or debug output.

This backend uses Responses with streaming and client-maintained history.
GEKTOR still executes tools under its normal permissions. Opaque reasoning state
is retained with assistant messages for session resume and is replayed only to
this backend with the same model. Visible reasoning is a provider summary,
when supplied. Image input support comes from the account model catalog's
`input_modalities`. Models declaring `image` enable image-reading tools and
attachments. Missing metadata remains unknown; explicitly text-only models reject
images before inference.

Sampling and `max_output_tokens` are unsupported on this OAuth route.
Use `-reasoning-effort high` to select an effort level, or leave it unset for
the model default. Settings > Response settings lists levels from the selected
account's model catalog. Unsupported levels fail before inference.
Enable `-reasoning-summary` to request a visible thinking summary; Ctrl+G
expands the THINK block. This is a provider summary, not raw hidden reasoning.
Both controls are saved in profiles and currently apply only to ChatGPT.

Temperature and response-limit controls are hidden in Settings; `-max-tokens`
remains a local planning budget, not a server-enforced generation limit.
The configured context limit is also a local budget, not a measured model window;
the default without a profile is 32768. Plan-usage limits stop the run and refer
to ChatGPT Settings > Usage. Account eligibility and model access require a
successful inference request, not just a successful login or model listing.

See [OpenAI sign-in](https://developers.openai.com/siwc/token-sharing-open-source/sign-in)
and [route limitations](https://developers.openai.com/siwc/token-sharing-open-source/preview-limitations).

## Skill metadata

`GEKTOR_SKILL_DESCRIPTION_LIMIT` sets the maximum skill description length in
Unicode characters (default: 1024). Set it before launching GEKTOR; it must be
a positive integer. The separate 16 KiB frontmatter limit still applies.
Malformed skills are skipped without hiding valid skills. Discovery warnings
appear in the skill catalog and `system_info` with `group=skills`.

## Command execution

On Windows, shell tools and `-verify-cmd` use `pwsh.exe` when available,
otherwise `powershell.exe`. The executable is selected once per process;
the agent's platform instructions and `system_info` report that same shell.
Scripts travel through UTF-8 stdin with profiles and interactive prompts disabled.
Use PowerShell syntax; `&&` and `||` are not portable to Windows PowerShell 5.1.
On other platforms shell commands use `sh`.

Use `run_program` for native executables with literal arguments. PowerShell
has its own quoting, pipeline and error semantics (notably native stderr
redirection in 5.1); the harness does not rewrite scripts or enable global
fail-fast behavior. Shell success describes the final statement, not every
earlier command. Use explicit conditions when one command depends on another.

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

## Session history and context

The Agent can search its saved conversation, including records removed from its
working context by compaction. Retrieval runs locally without calling a model
or a separate memory service. The Agent reads the tool result on its next turn.

Two read-only tools answer different questions:

| Tool | Use it for |
|---|---|
| `read_history` | Earlier user requests, decisions, messages and tool results in the saved archive |
| `inspect_context` | Which archive records are included in the calling model request, its instructions, advertised tools and recorded system prompt versions |

For example, `read_history` with `{"query":"queue","count":3}` finds matching
records. Each result has a stable, one-based message ID. Open an original with
`{"start":42,"count":1}`. To continue a search, keep `query` and pass `next_start`
as `start`. For a long message, keep its ID and pass `offset` from `next_offset`
with `count:1` and no `query`.
Search is literal and case-insensitive, not semantic or a regular expression.
Queries allow up to 256 characters. `count` defaults to 8 and allows at most 10
messages. During an agent call, reading its own session stops before the calling
response, so the search call cannot become its own match. Child archive reads
use the child's saved history.

Start context inspection with `{}`. The overview lists available instruction
sections and included archive ID ranges. Use `{"view":"instructions","section":"identity"}`
to read the current persona section, when present. Follow `next_offset` to read
the rest. `{"view":"revisions"}` lists saved system versions;
`{"view":"instructions","revision":1}` reads one. Instructions return at most
2,000 characters per page; tool, skill and revision lists are paged too.
The overview also pages its archive ranges and rewritten IDs with `next_offset`.
Pass that value as `offset`, keeping the same view, section and revision.
Offsets start at zero and count characters for instructions, items for lists.
The `directive` and `summary` sections describe only the current request and
cannot be combined with a historical `revision`.

Archive presence does not prove current context inclusion. Included records may
also have shorter reused tool results or rejected calls removed; the manifest
marks those IDs as rewritten. Skill inspection lists retained successful load
records, not every available skill or proof that full skill text remains.

System prompt tracking starts with this feature. A prompt recovered from an old
archive is marked `saved_archive`, with original delivery unknown. New versions
are recorded before the provider call; a saved record alone does not confirm
that the backend received it. These records show text, not how a
settings change was applied. They do not expose provider templates, caches or
private reasoning. The latest request manifest is saved; full request bodies are
not duplicated on every step.

Search stays within the current saved session, or its own child archive selected
with `delegate_call_id`. A delegate gets its own archive. Cross-session search
is not provided. Without a saved archive, `read_history` reports that limitation;
`inspect_context` still works during an agent tool call.

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
| `-audit PATH` | - | Append gate decisions as JSONL to PATH (off when empty) |
| `-verify-cmd CMD` | — | Final command; record output and fail on a command error without another model call |
| `-resume PATH` | — | Continue an interrupted run from its saved state |
| `-fork PATH` | — | Branch from a prior transcript file but write to a fresh task id |
| `-answer TEXT` | — | Answer to supply with `-resume` when the run paused on `ask_user` |

## Safety and permissions

Policy allows ordinary calls, asks for sensitive reads (`*.env`, credentials,
secrets) and known destructive shell/background command shapes, and applies
CLI overrides afterward. Deny is terminal; Ask without an approval handler
fails closed. Rules are pattern tripwires, not process isolation. See
[SECURITY.md](../SECURITY.md) for the trust boundary.

| Flag | Default | Meaning |
|---|---|---|
| `-allow RULES` | — | Comma-separated `tool=pattern` rules to allow, e.g. `"run_shell=go *,read_file=*.go"`. Wins over defaults |
| `-deny RULES` | — | Comma-separated `tool=pattern` rules to deny, e.g. `"run_shell=rm *,read_file=.env"`. Wins over `-allow` |
| `-mcp SERVERS` | - | MCP servers as `"name=cmd args...;name2=https://host/mcp"` over stdio JSON-RPC or Streamable HTTP. Tools appear as `mcp__name__tool`. A server that fails to start is skipped with a warning |
| `-capabilities FILE` | — | Explicit JSON configuration for optional tool packs and extra skill roots. See [Extensions](extensions.md) |
| `-browser CHANNEL` | - | Make installed `chrome` or `msedge` available through `enable_browser`. Check prerequisites, prepare pinned Playwright MCP and load its tools only on activation. Setup failures are tool results. See [browser setup](browser.md) |
| `-browser-profile NAME` | `default` | Dedicated persistent browser profile. Saved conversations restore their profile unless this flag explicitly overrides it. Browser files stay under `~/.gektor/browser-profiles/` |
| `-browser-headless` | `false` | Hide the browser window for unattended checks. Interactive login uses the visible default mode |

## Output and debugging

| Flag | Default | Meaning |
|---|---|---|
| `-mode MODE` | `print` | `print` (human-readable) or `json` (one JSON object per line on stdout; human chatter goes to stderr so the stream pipes cleanly) |
| `-serve` | `false` | Serve JSON-RPC over stdio instead of running one task: methods `run`/`respond`/`cancel`, events on stdout. See `docs/rpc.md` |
| `-image PATHS` | — | Attach pictures to the task (comma-separated, e.g. `-image shot.png,plan.webp`). In the TUI use `@path` inline instead (`@"my shot.png"` when the name has spaces). Text references are also supported in the TUI; `-image` remains image-only. Supports ChatGPT models with image input, a vision-capable OpenAI-compatible provider, or llama-server with `--mmproj` and VL weights. Koboldcpp refuses loudly; a text-only model on a vision server fails at the backend, not silently |
| `-stream` | `false` | Enable streaming in print/RPC mode. The TUI enables it automatically; direct `-mode json` disables it. Supports OpenAI, llama and KoboldCPP |
| `-debug` | `false` | Log raw request/response JSON plus per-call token usage to `agent-debug.log` |
| `-log-max N` | `300` | Max characters per line in step console output. `0` means no limit. The final answer always prints whole |

## State

CLI task history is stored under `~/.gektor/tasks/<id>/`; TUI Agent sessions
use `~/.gektor/sessions/<id>/`. Each has a `state.json` snapshot, rewritten
atomically as the agent progresses. Display titles, mode metadata, child
archives and retained tool output can be stored alongside it. See
[saved data](bootstrap.md#saved-data) for the storage locations.
Resume accepts a task directory or state file.
Fork loads a transcript into a fresh task; it does not create a Git branch or
an isolated checkout. Ancestor/workspace AGENTS.md instructions are refreshed.

GEKTOR does not archive the workspace, so it does not revert it. Commit or stash
before a run on a tree you care about.

## Mode-specific limits

The TUI honors Act/Plan/resume/fork modes. Slash commands and inline
text-file references are UI features, not special syntax in print mode.
Streaming is automatic in the TUI, opt-in with `-stream` in print/RPC mode,
and disabled in direct JSON output. Server support is still required.

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

The repository includes two profiles under `profiles/`:

- `llama-external.json` connects to a server at `http://127.0.0.1:9931`.
  Adjust its endpoint and context window to match your server.
- `chatgpt.json` uses a ChatGPT account connection. Run `gektor auth login` first.

You can also set `-backend` and `-model` without loading a profile.

A profile carries three blocks. `provider` names the protocol, endpoint and
model. `generation` sets the response budget, context window and
temperature. `identity` sets the persona:
`user_name`, `system_prompt`, `post_history`, `name`, `user_persona` and
`prompt_preset`.

Decoding is strict. An unrecognized key is an error, so a misspelled setting fails at
load instead of being silently ignored, and a profile written for a removed schema is
rejected with the offending key named rather than being rewritten.

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

## Common terminal entry

    .\gektor.exe -tui

First use opens in Plan and connects to the default external server described
in [Setup](bootstrap.md#model-server). Later launches reuse saved settings.
GEKTOR does not install model weights or manage the server.

Run > Plan / Act switches execution mode; Act enables tool calls under the
existing permission gates. GEKTOR > Change workspace chooses the workspace.
Settings > Setup profiles loads or saves setups; Settings > Model connection
selects the connection and model. GEKTOR also switches between Agent and Workflows.
Enter opens shared pickers.

Applying settings restarts the launch rather than reconfiguring in place, so the
new profile is composed into fresh services. The composer draft and the session
directory carry across the restart.

Choices and effective profile settings are saved under `~/.gektor/application.json`.
Explicit workspace, profile, connection, generation and identity flags override
saved settings for that launch. Applying settings through the UI saves them.
Runtime limits do not make a launch temporary: main-agent and delegate step
limits carry across TUI relaunches and are saved with preferences.
Direct API-key flags stay in memory across a restart and are never written
to preferences.

## Editing and Git tools

Existing files are edited with `patch_file`: its `old_content` must match one
exact block copied from the current file. Include surrounding lines to make
the block unique. Missing, ambiguous or unchanged matches are refused before
writing. CRLF and UTF-8 BOM are
preserved; UTF-16 text must be converted before patching.

`read_file` returns a continuation when its content limit is reached. Use the
returned `offset` for a complete-line boundary or `byte_offset` to continue
inside a long line. Byte positions refer to decoded UTF-8 with CRLF normalized
to LF, not raw bytes in a UTF-16 or CRLF file. Do not combine the two offsets.

Reading a picture (PNG, JPG/JPEG, WebP, GIF, BMP) attaches its pixels to the
conversation on a vision-capable backend instead of returning binary content;
`metadata_only=true` still returns just the header. Vision capability is
detected at startup from llama-server's `/props` modalities, or from an
OpenAI-compatible `/models` list when the provider publishes input modalities,
with a known-model-family fallback. Missing metadata stays unknown and does not
block explicit attachments. ChatGPT reads `input_modalities` from the account
model catalog. A text-only backend reports the image type
and says the pixels are unavailable, so the agent does not re-read the file.

`git` inspects status, log, diff, show, blame and tracked files without shell
interpretation. `diff` includes actual changes; `staged: true` selects the
index. `show` requires a commit revision; `blame` requires a file path. Log
results are limited to 50 commits; all output is byte-bounded.

`git_add` stages a literal workspace-relative path. `git_commit` commits the
already-staged changes using the supplied message and appends
`Co-authored-by: GEKTOR <no-reply@keshon.ru>`. It keeps the configured author
and runs normal repository hooks. These two tools are unavailable for
execution in Plan and read-only workers. Inspect staged changes and obtain
user authorization before committing; a commit includes unrelated changes
that were already staged too.

## Search context and process output

`system_info {"group":"skills"}` discovers metadata from nested `skills/**/SKILL.md`
packages, including libraries copied into an extra category directory. Filter
matches path or description; offset/limit paginate all matches. Discovery is
live, while the initial prompt catalog stays stable for context caching.
Large libraries are searched on demand instead of loading all guides or
arbitrarily hiding entries after the first 32. Read a guide before using it.

`grep_files` accepts `before_context` and `after_context`, each from 0 to 10.
Matches use `path:line:text`, context uses `path-line-text`; overlapping windows
are merged. Disjoint groups are separated by `--`. Results stop at 200 matches
or 48 KiB of rows and report truncation. Long lines remain individually capped.

`run_shell` and `start_background` take a required shell `command`. Use them for
builtins, pipelines, redirection and Windows `.cmd`/`.bat` wrappers.
`run_program` and `start_program` take a required native `program` plus literal
`args`; the executable is started directly, so no shell quoting, expansion,
pipes or redirection applies. All four tools accept a workspace-relative
`workdir`.

`run_shell` and `run_program` also accept `timeout_seconds` from 1 to 600.
Defaults are the workspace root and 600 seconds. The working directory applies
to that call only. Unknown parameters and invalid bounds are refused before
starting a process. Servers still use `start_background` or `start_program`
rather than a longer blocking timeout.

Inspect startup context without running a task:

```powershell
gektor -workspace . -context-report "Explain this project"
```

Reports separate core instructions, project instructions, skill metadata,
identity, history/task and tool schemas. Component costs use character-based
estimates; they are not tokenizer measurements. Normal runs also publish
`prompt_budget` events with backend prompt/cached counts when available.
The TUI `/context` dialog shows the breakdown and largest tool schemas.
Cached tokens still occupy context. Offline previews exclude eager `-mcp`
catalogs and images, and use text-only schemas; runtime reports use the actual
request. Resumed sessions use runtime reporting instead of offline preview.

`start_background` and `start_program` return an id and `next_cursor`.
`check_background` requires
both `id` and `cursor`: pass the previous `next_cursor` to retrieve only new
output. Cursors are byte positions in that process's output; use the returned
value rather than calculating it from rendered text. `cursor: 0` explicitly
reads retained output from the beginning. Evicted bytes are reported, and
incomplete UTF-8 characters are held until complete or the process exits.
Cursors are owned by the caller, so two readers do not consume each other's
output. `stop_background` still takes only the process id.


### Connection ownership

Each launch binds its ChatGPT account registration. Selecting another registration
with `gektor auth select` affects new launches; an existing launch continues using
its bound account. Signing that account out makes further requests fail until it
is connected again. Local model facts are refreshed before each run, while one
run and its delegated agents share a consistent model/capability snapshot.

Saved ChatGPT native state is reused only for a matching integration, model,
account, and unchanged message. Otherwise GEKTOR sends the normal transcript.
Old saved sessions remain readable; legacy native state without identity metadata
is not replayed. Interrupted output is retained as incomplete history.

### Workflow queue and service

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
See [HTTP API and webhooks](workflows.md#http-api-and-webhooks).

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

### Workflow credential references

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

### Workflow artifacts

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
[subworkflows](workflows.md#reusable-subworkflows) for syntax and limits.
