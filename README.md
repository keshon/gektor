# TARS

A local coding agent, written in Go. It runs against a workspace with tools
and permission prompts, in Plan or Act mode.

The Go application builds without cgo.

## Quick start

Requirements: Go 1.26.6+ and a model backend you start yourself. TARS connects
to a server; it does not ship or launch one, and the repository contains no
model weights.

```powershell
go build -o agent.exe ./cmd/agent
.\agent.exe -tui
```

On a fresh clone the launcher expects a backend at `http://127.0.0.1:8080`.
Start one with `run-server-llama.cmd` in its own terminal, wait for the model to
load, then run `run-tui.cmd`. Use **Settings → Model** to point TARS somewhere
else if you want.

- **Run** selects Plan (read-only) or Act (executes), stops a run, or retries a failed one.
- **Settings** selects models, generation parameters, identity and service profiles.
- **View → Theme** selects Dark or Light.
- **Session → Compact history** compacts the current history.

Applying settings restarts the launch so the new profile is composed into
fresh services. The draft and the session carry across.

See [bootstrap and service ownership](docs/bootstrap.md) for setup and
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

Enter sends text, Shift+Enter adds a line, and Tab switches panel focus. The
menu, session panel, status labels and contextual footer share one theme. Type
`/` for commands or `@` for workspace files. F1 opens help, F2 searches
sessions.
## Model servers

Start a model server in its own terminal before using TARS. Machine-specific
paths and keys live in `.env` (copy from `.env.example`; gitignored).

**llama.cpp (default backend):**
1. Fill in `LLAMA_SERVER_EXE` and `LLAMA_MODEL` in `.env`.
2. Run `run-server-llama.cmd` and wait for the model to finish loading.
3. In another terminal, run `run-tui.cmd`. It checks `127.0.0.1:8080`,
   builds `agent.exe`, and opens the UI.

**KoboldCPP (secondary):**
1. Fill in `KOBOLD_EXE` and `KOBOLD_MODEL` in `.env`.
2. Run `run-server-kobold.cmd` and wait for the model to finish loading.
3. In another terminal, run `run-tui.cmd -backend-kind kobold -backend http://127.0.0.1:5001`.

**OpenAI-compatible / remote:**
1. Fill in `TARS_API_KEY` in `.env`.
2. Run `run-server-openai.cmd` for a stdio JSON-RPC server, or
   `run-remote.cmd "task"` for a one-shot task run.

The sibling launchers follow the same build-then-run shape:
`run-task.cmd "task"` for a one-shot print-mode run, `run-plan.cmd` and
`run-mission.cmd` for those modes, `run-resume.cmd` to list or resume saved
tasks.
Maintenance lives under `build.cmd` (`build test`, `build check`, `build fmt`,
`build vuln`, `build clean`; `build help` lists everything), and the eval
helpers are `eval-dry.cmd`, `eval-run.cmd` and `eval-reset.cmd`.

## Backend and saved runs

All flags and backend examples are in [docs/cli.md](docs/cli.md).
The [documentation index](docs/README.md) links the current guides and archived audits.
The stdio integration protocol is in [docs/rpc.md](docs/rpc.md). Remote-provider keys resolve
from `-api-key`, `-api-key-env`, or environment (`TARS_API_KEY`,
`OPENAI_API_KEY`, `OPENROUTER_API_KEY`, `DEEPSEEK_API_KEY`, `GROQ_API_KEY`,
`TOGETHER_API_KEY`, `ANTHROPIC_API_KEY`).

State lands in the user's `.tars/tasks/<id>/` after every step; `-resume`
accepts its directory or state file. Ancestor and workspace `AGENTS.md`
instructions are loaded in order and refreshed on resume.

TARS does not archive the workspace and cannot revert it. Commit or stash
before a run on a tree you care about.

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
- `internal/run`, `internal/tui` — shared session events/gates and terminal UI
- `internal/audit`, `internal/checks` — approval records and deterministic findings
- `internal/permission`, `internal/session`,
  `internal/events` — policy, conversation snapshots, JSONL output

How it fits together is in [ARCHITECTURE.md](ARCHITECTURE.md). House rules
are in [docs/conventions.md](docs/conventions.md), enforced by tests.
The trust boundary is in [SECURITY.md](SECURITY.md): file tools are
guardrails against mistyped paths, not a sandbox — use a container or a VM
if process isolation matters.

## License

MIT. See [LICENSE](LICENSE).
