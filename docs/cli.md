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
`-serve` and resume/fork; ordinary print mode needs one.

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
| `-no-verify` | `false` | Skip the self-check verify round (finish accepted without the extra verification turn). Also disables `-verify-cmd` in direct mode. Loop guards stay on |
| `-nudges LEVEL` | `full` | How much harness advice the model hears: `full`, `recovery`, `off`. See [Nudge levels](#nudge-levels) |
| `-mcp SERVERS` | - | MCP servers as `"name=cmd args...;name2=https://host/mcp"` over stdio JSON-RPC or Streamable HTTP. Tools appear as `mcp__name__tool`. A server that fails to start is skipped with a warning |

## Nudge levels

Tars tells the model when a step went wrong. `-nudges` decides how much
of that it hears. The levels are not a quality ladder — they split by
what a notice costs.

A nudge delivered at the end of a step that already made tool calls
**rides a turn the loop was taking anyway**: another model call happens
regardless, and the notice adds roughly 40 tokens. These are the stuck
escalations and the context-budget notices.

A nudge that answers a finish — truncated, think-wrap, todo still open,
closing, context overflow, announced-but-not-written — returns no
answer, so the loop **calls the model again**. That is a whole extra
turn.

| Level | Notices |
|---|---|
| `full` | Everything (default) |
| `recovery` | Only the notices that turn a bad ending into another attempt; the free advice is dropped |
| `off` | None. A truncated response is accepted as the answer, and a finish with nothing written is accepted too |

Lower the level when the model is strong enough not to need the help —
a large model pays those extra turns and follows none of them. The
narrow case for `off` is a local model already tuned by a profile;
raise it back to `full` for a small one.

Every nudge still reaches `-mode json` observers and the session
transcript whatever the level: turning the harness down changes what the
model is told, not the record of what happened.

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

## State

Task state belongs to the user's `.tars` directory, under `tasks/<id>/`.
Every run writes one file, `state.json`, rewritten atomically on each step;
missions also use
`mission.json` and worker transcripts. Session display title and Plan/Act metadata
are saved alongside the history. Resume accepts a task
directory or state file; a directory containing `mission.json` resumes a mission.
Fork loads a transcript into a fresh task; it does not create a Git branch or
an isolated checkout. Ancestor/workspace AGENTS.md instructions are refreshed.

TARS does not archive the workspace, so it does not revert it. Commit or stash
before a run on a tree you care about.

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

## Profiles

`-profile <path>` loads a versioned JSON provider, generation and identity
profile. Explicit CLI flags override profile values. Existing commands without a
profile retain their defaults. `-check-profile` validates and prints the effective
configuration without creating a workspace, contacting a backend, or starting any
process.
Secrets remain environment references; the printed profile contains no API key.
This checks configuration, not backend availability or model capacity.

```cmd
agent.exe -profile profiles\llama-external.json -check-profile
agent.exe -profile profiles\llama-external.json -tui
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
agent.exe -profile profiles\llama-external.json -user-name Alex -system-prompt "be terse"
```

## Common terminal entry

    .\\agent.exe -tui

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

Choices and effective profile settings are saved under `~/.tars/application.json`.
Explicit command-line combinations remain supported as temporary launches; a
change applied through the UI becomes the saved default. Direct API-key flags stay
in memory across a restart and are never written to preferences.
