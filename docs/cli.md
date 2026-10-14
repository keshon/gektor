# CLI reference

Every flag of `cmd/agent`, grouped by purpose. A conventions test
(`internal/conventions`, tag `cli-flags`) fails if a flag exists in code
but is missing here. It checks coverage, not defaults or semantics; those
are verified against the implementation.

## Invocation

Build the terminal application with `go build -o agent.exe ./cmd/agent`.
Run the built executable so bundled profiles resolve from the project directory.

```bash
go run ./cmd/agent [flags] "task description"
./agent.exe -tui
go run ./cmd/agent -resume .tars/tasks/<id>
```

Put all flags before the task: Go's flag parser stops at the first positional
argument. Run `go run ./cmd/agent -h` for the binary's registered flags.
Direct execution is the default. An empty task is allowed for `-tui`,
`-serve`, resume/fork, and checkpoint operations; ordinary print mode needs one.

Local llama-server example:

```bash
./agent.exe -tui -backend-kind llama -backend http://127.0.0.1:8080 -stream
```

For LM Studio or a hosted OpenAI-compatible service, use the `openai` dialect
and the provider's API root, including `/v1` when required:

```bash
./agent.exe -tui -backend-kind openai -backend http://127.0.0.1:1234/v1 -model your-model -context-limit 16384
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
| `-context-limit N` | `0` | Context window override. Remote backends expose no probe endpoint and fall back to a per-model estimate unless set |
| `-grammar` | `true` | Apply the backend's own content grammar (blocks a model writing its native tool-call tags as plain text on koboldcpp) |
| `-max-tokens N` | `8192` | Generation budget per response. Too low truncates large outputs mid-JSON, which looks like a model failure |
| `-max-steps N` | `0` (=25) | Base direct-run step budget. CLI/TUI checklist funding can add up to another base budget plus two closing steps; workers and subagents keep fixed budgets. The RPC server currently uses the default budget |
| `-reasoning-budget N` | `0` (=6000) | Characters of `<think>` deliberation allowed per response before a wrap-up round demands commitment. Negative disables wrapping |

## Authentication (remote providers)

The key resolves as `-api-key`, then `-api-key-env`, then the first token
found under `TARS_API_KEY`, `OPENAI_API_KEY`, `OPENROUTER_API_KEY`,
`DEEPSEEK_API_KEY`, `GROQ_API_KEY`, `TOGETHER_API_KEY`, `ANTHROPIC_API_KEY`.

| Flag | Default | Meaning |
|---|---|---|
| `-api-key KEY` | — | Bearer token. Prefer env vars: a flag lands in shell history |
| `-api-key-env NAME` | — | Env var holding the bearer token, e.g. `OPENROUTER_API_KEY` |

## Task and modes

| Flag | Default | Meaning |
|---|---|---|
| `-workspace DIR` | `.` | Workspace root the agent may read and write |
| `-mission` | `false` | Plan first (human-approved), then one fresh-context worker per subtask, each verified mechanically. Enable explicitly; multi-file tasks otherwise receive a recommendation |
| `-direct` | `false` | Suppress the multi-file mission recommendation; direct mode is already the default |
| `-plan` | `false` | Plan mode: read-only tools, proposes a plan and changes nothing |
| `-yes` | `false` | Skip mission plan approval. Print/RPC permission Ask calls are denied instead of prompting; this is not allow-all. TUI permissions still use the interactive gate |
| `-audit PATH` | - | Append gate decisions as JSONL to PATH (off when empty) |
| `-verify-cmd CMD` | — | Command run at the self-check checkpoint, e.g. `"go test ./..."`. Real output is fed back as fact instead of trusting the model's claim |
| `-resume PATH` | — | Continue an interrupted run from its saved state |
| `-fork PATH` | — | Branch from a prior transcript file but write to a fresh task id |
| `-answer TEXT` | — | Answer to supply with `-resume` when the run paused on `ask_user` |
| `-revert` | `false` | Restore the latest pre-run checkpoint, including dirty files, staged changes and pre-existing untracked files, then exit |
| `-revert-preview` | `false` | List checkpoint restore actions without changing files |
| `-revert-from PATH` | latest checkpoint | Select an archive for `-revert` or `-revert-preview` |

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
| `-pure` | `false` | Reserved compatibility flag: project permission files are not loaded yet, so it currently has no effect. Policy uses built-in defaults plus `-allow`/`-deny` |
| `-no-verify` | `false` | Skip the self-check verify round (finish accepted without the extra verification turn). Also disables `-verify-cmd` in direct mode. Loop guards stay on |
| `-mcp SERVERS` | - | MCP servers as `"name=cmd args...;name2=https://host/mcp"` over stdio JSON-RPC or Streamable HTTP. Tools appear as `mcp__name__tool`. A server that fails to start is skipped with a warning |

## Output and debugging

| Flag | Default | Meaning |
|---|---|---|
| `-mode MODE` | `print` | `print` (human-readable) or `json` (one JSON object per line on stdout; human chatter goes to stderr so the stream pipes cleanly) |
| `-serve` | `false` | Serve JSON-RPC over stdio instead of running one task: methods `run`/`respond`/`cancel`, events on stdout. See `docs/rpc.md` |
| `-tui` | `false` | Fullscreen terminal UI instead of print mode: live transcript, responsive session sidebar, centered title/history search (F2 / Ctrl+P), and inline gate prompts. Supports direct, `-plan`, `-resume`, `-fork` and `-mission` runs |
| `-image PATHS` | — | Attach pictures to the task (comma-separated, e.g. `-image shot.png,plan.webp`). In the TUI use `@path` inline instead (`@"my shot.png"` when the name has spaces). Text references are also supported in the TUI; `-image` remains image-only. Requires a vision-capable OpenAI-compatible provider or llama-server with `--mmproj` and VL weights. Koboldcpp refuses loudly; a text-only model on a vision server fails at the backend, not silently |
| `-stream` | `false` | Enable streaming in print/RPC mode. The TUI enables it automatically; direct `-mode json` disables it. OpenAI/llama only; KoboldCPP falls back to unary |
| `-debug` | `false` | Log raw request/response JSON plus per-call token usage to `agent-debug.log` |
| `-log-max N` | `300` | Max characters per line in step console output. `0` means no limit. The final answer always prints whole |

## State and checkpoints

Task state belongs to the selected workspace, under `.tars/tasks/<id>/`.
Direct conversations use `state.json` and the rotating `state.jsonl` log;
missions also use `mission.json` and worker transcripts. Session display title
and Plan/Act metadata are saved alongside the history. Resume accepts a task
directory or state file; a directory containing `mission.json` resumes a mission.
Fork loads a transcript into a fresh task; it does not create a Git branch or
an isolated checkout. Ancestor/workspace AGENTS.md instructions are refreshed.

Snapshots require a usable Git workspace. Preview before reverting if needed.
Restore retains pre-existing dirty/staged/untracked state, removes newly created
nonignored files, and refuses a changed HEAD. Ignored files, `.tars`, and Git
history are excluded. Missing checkpoints produce a warning rather than a
promise that a run is reversible.

## Mode-specific limits

The TUI honors direct/plan/resume/fork/mission modes. Slash commands and inline
text-file references are UI features, not special syntax in print mode.
Streaming is automatic in the TUI, opt-in with `-stream` in print/RPC mode,
and disabled in direct JSON output. Server support is still required.

The RPC server takes tasks, mission selection, and images from each request.
It does not expose session browsing, resume/fork, Plan/Act switching, or local
slash commands. `-max-steps` is not wired into serve mode, which uses the default
budget without CLI/TUI checklist funding. See [rpc.md](rpc.md) for the supported
run configuration and event contract.

## Migration profiles

`-profile <path>` loads a versioned JSON provider and generation profile. Explicit
CLI flags override profile values. Existing commands without a profile retain
their defaults. `-check-profile` validates and prints the effective configuration
without creating a workspace, contacting a backend, or starting any process.
Secrets remain environment references; the printed profile contains no API key.
This checks configuration, not backend availability or model capacity.

```cmd
agent.exe -profile profiles\llama-external.json -check-profile
agent.exe -profile profiles\llama-external.json -tui
```

The sample endpoint and context size must match your running server. This first
stage supports external thinking services only; it does not install or start
speech services. See [bootstrap.md](bootstrap.md) for the migration contract.

## Tool-free chat migration

`-chat` runs plain text conversation through the existing backend transport,
without workspace initialization, workspace AGENTS instructions, tools, gates
or coding verification. `-system-prompt` supplies its system instruction.
Use a profile or explicit `-context-limit` and `-max-tokens` with a positive
response budget smaller than the context limit.

```cmd
agent.exe -profile profiles\llama-external.json -chat
agent.exe -profile profiles\llama-external.json -chat "Hello"
```

Without a positional message it reads one message per line; `/reset` clears
history and `/quit` exits. A failed, canceled, empty, tool-calling or truncated
response is not saved to the conversation. The prompt window includes recent
whole turns using a conservative UTF-8 byte estimate and reports omissions;
this is not summary compaction. In line-based CLI mode, completed history stays in memory only.
Chat currently rejects coding options and `-serve`, `-stream`, and
`-mode`; RPC and streaming still belong to the coding runner. Voice and character
cards are subsequent migration steps.

`-chat -tui` uses the Commander interface with a separate conversation store.
`-chat-dir <directory>` selects that store; its default is `var/chats` beside
the executable. Successful complete turns are saved atomically. The sidebar
opens, searches, renames and deletes chat sessions independently of `.tars`
coding tasks. Reopening requires matching prompt and generation settings.
Act/Plan and manual coding compaction are unavailable for tool-free chat.

```cmd
agent.exe -profile profiles\llama-external.json -chat -tui
```

## Character cards and conversation prompts

`-character <PNG-or-JSON>` imports a card for `-chat`, including its intro.
`-character-png` is a compatibility alias for the same path.
`-user-name` fills `{{user}}` (default User). `-system-prompt` and `-post-history`
set base instructions; `-character-main-prompt` and
`-character-post-history` accept false to retain those bases instead of card overrides.
The final post-history directive is a system message after the latest user
message, not an invented user turn. `-temperature` controls chat sampling;
0 retains transport default 0.4, positive values up to 2 are explicit. Literal
greedy zero is not supported by the existing model transport.

Profiles can contain `conversation` prompt/card settings and
`generation.temperature`. Card paths in profiles resolve relative to the
profile file; CLI paths resolve from the working directory. A conversation
profile requires `-chat`. The compiled prompt, intro and settings are retained
in chat archives; reopening with different settings requires a new conversation.

Flat JSON, character-card V2 2.0 and basic V3 3.0 are supported. PNG metadata
uses `ccv3`/`chara` with bounded, CRC-checked parsing. Duplicate valid metadata
is handled with warnings; source images are never rewritten. Nonempty lorebooks
are rejected explicitly until activation is implemented. Advanced character
behavior and the full SillyTavern setting surface are not included.

Example character profile (adjust its endpoint/context to your server):

```cmd
agent.exe -profile profiles\chat-veyra.json -chat -tui
agent.exe -profile profiles\chat-veyra.json -check-profile
agent.exe -profile profiles\llama-external.json -chat -tui -character "D:\Cards\my-character.png" -user-name Alex
```

`-voice` enables the speech configuration in a chat TUI profile. `-voice=false`
keeps a voice-capable profile in text mode. Speech assets are checked only when
enabling speech. Voice profiles use `speech` with an owned Chatterbox runtime
and either managed or external Whisper recognition. No speech process is
started for an ordinary text-only profile.

## Common terminal entry

    .\\agent.exe -tui

First use selects Chat and voice off, with the installed local profile (or the
external text profile when its starter weights are absent). Models load only
when a request needs them.

Mode > Chat/Agent selects the conversation mode. Settings > Application chooses
its workspace and voice input style. Settings > Profiles loads or saves presets;
Settings > Model selects the model in either mode. Enter opens shared pickers.
Switching preserves the session and draft. Agent opens in Plan; Agent > Act enables
execution under the existing permission gates. A model-only change in an existing
Agent session retains its Plan/Act choice. Temperature applies to both modes.

Choices and effective profile settings are saved under var/application.json.
Explicit command-line combinations remain supported as temporary launches;
mode changes through the GUI become the saved defaults. The conversation flag
selects chat or agent. Direct API-key flags remain in memory across a mode change
and are never written to preferences.

Voice input works with both runners. Chat speaks streamed sentences; Agent
speaks its final answer, leaving intermediate tool output in the transcript.
