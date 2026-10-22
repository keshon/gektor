# CLI reference

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

Local llama-server example:

```bash
./gektor.exe -tui -backend-kind llama -backend http://127.0.0.1:8080 -stream
```

For LM Studio or a hosted OpenAI-compatible service, use the `openai` dialect
and the provider's API root, including `/v1` when required:

```bash
./gektor.exe -tui -backend-kind openai -backend http://127.0.0.1:1234/v1 -model your-model -context-limit 16384
```

For a hosted service, set a key in the environment or select its variable
with `-api-key-env`; supply the provider's actual model ID and context limit.
The terminal controls and local slash commands are in [tui.md](tui.md).

## Backend

| Flag | Default | Meaning |
|---|---|---|
| `-backend URL` | `http://localhost:5001` | Backend base URL: bare origin for a local server, API root for a hosted OpenAI-compatible provider |
| `-backend-kind KIND` | `kobold` | `kobold`, `llama`, or `openai`. A mismatch is caught at startup: the dialects disagree about grammar and sampler fields, so a wrong value measures nothing |
| `-model NAME` | `local` | Model name. Ignored by most local servers (they serve whatever weights they started with); required for remote providers |
| `-context-limit N` | `0` | Remote context window or local fallback. A reported local server window takes precedence; remote backends fall back to a per-model estimate unless set |
| `-grammar` | `true` | Apply the backend's own content grammar (blocks a model writing its native tool-call tags as plain text on koboldcpp) |
| `-max-tokens N` | `8192` | Generation budget per response. Too low truncates large outputs mid-JSON, which looks like a model failure |
| `-max-steps N` | saved limit or 25 | Fixed per-run step limit; explicit `0` means unlimited. Todo cannot extend it. Subagents use 12 steps |

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
| `-browser CHANNEL` | - | Connect cached Playwright MCP to installed `chrome` or `msedge` using an isolated headless profile. Connection failure stops launch. See [browser setup](browser.md) |

## Output and debugging

| Flag | Default | Meaning |
|---|---|---|
| `-mode MODE` | `print` | `print` (human-readable) or `json` (one JSON object per line on stdout; human chatter goes to stderr so the stream pipes cleanly) |
| `-serve` | `false` | Serve JSON-RPC over stdio instead of running one task: methods `run`/`respond`/`cancel`, events on stdout. See `docs/rpc.md` |
| `-image PATHS` | — | Attach pictures to the task (comma-separated, e.g. `-image shot.png,plan.webp`). In the TUI use `@path` inline instead (`@"my shot.png"` when the name has spaces). Text references are also supported in the TUI; `-image` remains image-only. Requires a vision-capable OpenAI-compatible provider or llama-server with `--mmproj` and VL weights. Koboldcpp refuses loudly; a text-only model on a vision server fails at the backend, not silently |
| `-stream` | `false` | Enable streaming in print/RPC mode. The TUI enables it automatically; direct `-mode json` disables it. OpenAI/llama only; KoboldCPP falls back to unary |
| `-debug` | `false` | Log raw request/response JSON plus per-call token usage to `agent-debug.log` |
| `-log-max N` | `300` | Max characters per line in step console output. `0` means no limit. The final answer always prints whole |

## State

Task state belongs to the user's `.gektor` directory, under `tasks/<id>/`.
Every run writes one file, `state.json`, rewritten atomically on each step.
Session display title and Plan/Act metadata
are saved alongside the history. Resume accepts a task
directory or state file.
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

One profile ships with the repository: `llama-external.json`, which expects a
server already listening on `http://127.0.0.1:8080`. Adjust the endpoint and
context window to your own server, or point `-backend` and `-model` at a
different one without a profile at all.

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

    .\\gektor.exe -tui

First use selects the installed local profile (or the external one when its
starter weights are absent) and opens in Plan. Models load only when a request
needs them.

Run > Plan/Act switches execution mode; Run > Act enables tool calls under the
existing permission gates. Settings > Application chooses the workspace.
Settings > Profiles loads or saves presets; Settings > Model selects the model.
Enter opens shared pickers.

Applying settings restarts the launch rather than reconfiguring in place, so the
new profile is composed into fresh services. The composer draft and the session
directory carry across the restart.

Choices and effective profile settings are saved under `~/.gektor/application.json`.
Explicit command-line combinations remain supported as temporary launches; a
change applied through the UI becomes the saved default. Direct API-key flags stay
in memory across a restart and are never written to preferences.

## Editing and Git tools

Existing files are edited with `patch_file`: its `old_content` must match one
exact block copied from the current file. Include surrounding lines to make
the block unique. Missing, ambiguous or unchanged matches are refused before
writing. The line-number editor has been removed. CRLF and UTF-8 BOM are
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
with a known-model-family fallback. A text-only backend reports the image type
and says the pixels are unavailable, so the agent does not re-read the file.

`git` inspects status, log, diff, show, blame and tracked files without shell
interpretation. `diff` includes actual changes; `staged: true` selects the
index. `show` requires a commit revision; `blame` requires a file path. Log
results are limited to 50 commits; all output is byte-bounded.

`git_add` stages a literal workspace-relative path. `git_commit` commits the
already-staged changes using the supplied message and appends
`Co-authored-by: Gektor <no-reply@keshon.ru>`. It keeps the configured author
and runs normal repository hooks. These two tools are unavailable for
execution in Plan and read-only workers. Inspect staged changes and obtain
user authorization before committing; a commit includes unrelated changes
that were already staged too.

## Search context and process output

`grep_files` accepts `before_context` and `after_context`, each from 0 to 10.
Matches use `path:line:text`, context uses `path-line-text`; overlapping windows
are merged. Disjoint groups are separated by `--`. Results stop at 200 matches
or 48 KiB of rows and report truncation. Long lines remain individually capped.

`run_shell` and `start_background` accept either `program` with literal `args`
or a shell `command`. These modes are mutually exclusive. Prefer program/args
for executables: no shell quoting, expansion, pipes or redirection is applied.
Use command for shell builtins, pipelines and Windows `.cmd`/`.bat` wrappers.
Both tools accept a workspace-relative `workdir`.

`run_shell` also accepts
`timeout_seconds` from 1 to 600. Defaults are the workspace root and 600 seconds.
The working directory applies to that call only. Unknown parameters and invalid
bounds are refused before starting a command. Servers still use
`start_background` rather than a longer blocking timeout.

`start_background` returns an id and `next_cursor`. `check_background` requires
both `id` and `cursor`: pass the previous `next_cursor` to retrieve only new
output. Cursors are byte positions in that process's output; use the returned
value rather than calculating it from rendered text. `cursor: 0` explicitly
reads retained output from the beginning. Evicted bytes are reported, and
incomplete UTF-8 characters are held until complete or the process exits.
Cursors are owned by the caller, so two readers do not consume each other's
output. `stop_background` still takes only the process id.
