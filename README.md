# TARS

A local assistant for coding and voice conversations, written in Go.
Use **Chat** for conversation and character cards, or **Agent** for workspace
tools with Plan/Act modes and permission prompts. Both share model selection,
sessions, generation settings and optional voice input/output.

The Go application builds without cgo. Optional voice services run as owned
external processes: Whisper for recognition, Chatterbox Nano/Turbo for speech,
and Silero VAD for hands-free capture. The current voice path targets English
on Windows. Interruption is application-controlled, not native full duplex.

## Quick start

Requirements: Go 1.26.6+ and a local or remote model backend. Model weights and
optional speech runtimes are not included in Git.

```powershell
go build -o agent.exe ./cmd/agent
.\agent.exe -tui
```

On a fresh clone, the common launcher uses the external Chat profile at
`http://127.0.0.1:8080`. Start your backend, then use **Settings → Model** to
configure it. If the starter local model and runtime have been installed, the
launcher selects the managed local profile instead. Models load on demand.

- **Mode → Chat / Agent** switches the current session; Agent requires a workspace and starts in Plan.
- **Settings** selects models, generation parameters, character cards and service profiles.
- **Voice** controls recording, hands-free capture and playback; speech assets must be installed first.
- **View → Theme** selects Dark or Light.
- **Session → Compact conversation** compacts the current history. Chat keeps its opening exchange and eight recent turns, with an archive backup.

See [bootstrap and runtime ownership](docs/bootstrap.md) for service setup and
[the terminal guide](docs/tui.md) for interaction details. The CLI backend default
for launches without a profile remains `http://localhost:5001`.

## Usage

```bash
go run ./cmd/agent "add a Version constant to config.go"
```

Direct mode handles single-file and question-shaped tasks. Bigger work gets
`-mission`: an upfront plan you approve, then one fresh-context worker per
subtask, each verified mechanically. Mission mode is explicit; multi-file
tasks receive a recommendation. `-plan` proposes without touching
anything; `-mode json` emits machine-readable step events; `-tui` renders
the same run fullscreen with live transcript and inline gate prompts.

## Terminal UI

```bash
.\agent.exe -tui
```

The common entry opens Chat with voice off on first use, then remembers the
application configuration. Enter sends text, Shift+Enter adds a line, and Tab
switches panel focus. The menu, session panel, status labels and contextual footer
share one theme. Type `/` for commands or `@` for workspace files in Agent mode.
F1 opens help, F2 searches sessions, F8 records, and Ctrl+L toggles hands-free voice
when speech services are configured. Mode changes retain the session and draft.

External-server launchers remain available as an alternative to managed services.

On Windows, keep KoboldCPP in its own terminal:

1. Edit the executable/model paths in [run-kobold.cmd](run-kobold.cmd) if needed.
2. Run `run-kobold.cmd` and wait for the model to finish loading.
3. In another terminal, run `run-tui.cmd`. It checks the server and rebuilds
   `agent.exe` before opening the UI.

The other root launchers follow the same build-then-run shape:
`run-task.cmd "task"` for a one-shot print-mode run, `run-plan.cmd` and
`run-mission.cmd` for those modes, `run-resume.cmd` to list or resume saved
tasks, and `run-revert.cmd` to preview or restore a workspace checkpoint.
Maintenance lives under `build.cmd` (`build test`, `build check`, `build fmt`,
`build vuln`, `build clean`; `build help` lists everything), and the eval
helpers are `eval-dry.cmd`, `eval-run.cmd` and `eval-reset.cmd`.

The supplied launcher uses Qwen3.5-9B-Q4_K_M, a 16,384-token server context,
and a 4,096-token response budget. These are launcher settings; the CLI's
response default is 8,192. Server output stays in the server terminal.
See [the terminal guide](docs/tui.md) for keys, commands, file references,
permissions, sessions, and terminal troubleshooting.

## Backend and saved runs

All flags and backend examples are in [docs/cli.md](docs/cli.md).
The [documentation index](docs/README.md) links the current guides and archived audits.
The stdio integration protocol is in [docs/rpc.md](docs/rpc.md). Remote-provider keys resolve
from `-api-key`, `-api-key-env`, or environment (`TARS_API_KEY`,
`OPENAI_API_KEY`, `OPENROUTER_API_KEY`, `DEEPSEEK_API_KEY`, `GROQ_API_KEY`,
`TOGETHER_API_KEY`, `ANTHROPIC_API_KEY`).

State lands in `<workspace>/.tars/tasks/<id>/` after every step; `-resume`
accepts its directory or state file. Ancestor and workspace `AGENTS.md`
instructions are loaded in order and refreshed on resume.

Before a Git workspace run, TARS checkpoints tracked and nonignored files
and the Git index. `-revert-preview` lists changes; `-revert` restores the
checkpoint, including pre-existing dirty and staged work. New nonignored
files are removed. Ignored files and `.tars` state are excluded; restore
refuses a changed HEAD. Use `-revert-from PATH` to select an older archive.
A workspace without a usable checkpoint receives a warning.

## Evals

`eval/probes/` holds scenarios with frozen pass criteria, scored three
ways: workspace contents, tool usage, reported answer. Each runs in a
throwaway copy of its seed workspace. See [eval/README.md](eval/README.md).

```bash
go run ./cmd/eval -dry                      # validate probes, no model calls
go run ./cmd/eval -runs 3                   # run everything
go run ./cmd/eval -only 0 -require-model qwen
```

## Layout

- `cmd/agent`, `cmd/eval` — CLI, probe runner
- `internal/agent`, `internal/mission`, `internal/roles` — the loop, the
  plan-execute-verify pipeline, the agent kinds
- `internal/llm`, `internal/tools`, `internal/prompts` — backends, tools, prompts
- `internal/api`, `internal/tui` — shared session events/gates and terminal UI
- `internal/audit`, `internal/checks` — approval records and deterministic findings
- `internal/permission`, `internal/session`, `internal/snapshot`,
  `internal/events` — policy, session log, workspace checkpoints, JSONL output

How it fits together is in [ARCHITECTURE.md](ARCHITECTURE.md). House rules
are in [docs/conventions.md](docs/conventions.md), enforced by tests.
The trust boundary is in [SECURITY.md](SECURITY.md): file tools are
guardrails against mistyped paths, not a sandbox — use a container or a VM
if process isolation matters.

## License

MIT. See [LICENSE](LICENSE).
