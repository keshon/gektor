# GEKTOR

A terminal coding agent, written in Go. Bring your model server; GEKTOR supplies
the tool loop, workspace access, session history and terminal UI.

I'm building GEKTOR to keep the agent understandable: one execution loop,
explicit tools and a codebase I can reason about. The focus is coding work
in a repository, with local inference as the starting point.

![GEKTOR terminal UI showing a file patch and a passing Go test](docs/images/gektor-demo.png)

*Scripted demo rendered by the real TUI. The file patch and test command were
executed on a small fixture project; model replies were scripted. This is an
interface preview, not a model benchmark.*

## What it does

- **Plan and Act.** Explore with read-only tools, then switch to execution.
- **Work with code.** Search, read, patch and move files; run commands and
  manage background processes. Delegate a bounded subtask when useful.
- **Show the work.** Stream answers and reasoning when the backend exposes
  them. Inspect tool arguments, results, diffs and permission requests.
- **Keep sessions.** Reopen and search conversations, continue interrupted
  work, or fork a task. Context compaction keeps the full conversation archive.
- **Extend through files and tools.** Read project `AGENTS.md` instructions
  and task-specific skills. Connect external tools over MCP.
- **Use the same loop outside the TUI.** One-shot CLI, JSONL events and a
  stdio integration protocol are included.

GEKTOR connects to **llama.cpp**, **KoboldCPP**, and **OpenAI-compatible APIs**.
Models, weights and inference servers are supplied separately. The Go binary
builds without cgo; Windows is the primary development platform, with Windows
and Linux checks configured in CI.

## Start with a local model

Requirements: **Go 1.26.6+** and a running model server capable of following
tool instructions. These commands assume llama.cpp at `127.0.0.1:8080`.

```powershell
git clone https://github.com/keshon/gektor.git
cd gektor
go build -o gektor.exe ./cmd/gektor
.\gektor.exe -tui -backend-kind llama -backend http://127.0.0.1:8080 -workspace "D:\path\to\project"
```

On Linux, build with `go build -o gektor ./cmd/gektor` and launch `./gektor`
with the same flags and your workspace path.

The binary connects to an existing server. On Windows, the optional
`run-server-llama.cmd` helper starts one using `LLAMA_SERVER_EXE` and
`LLAMA_MODEL` from a gitignored `.env` file; copy `.env.example` to get started.
`run-tui.cmd` builds and launches the TUI with saved settings.

In the UI:

- **Enter** sends; **Shift+Enter** adds a line; **Esc** stops a running turn.
- **F5** switches Plan/Act while idle. **Ctrl+G** expands thinking and tool details.
- **Ctrl+P** searches sessions. **Ctrl+B** toggles the session list.
- Type **`/`** for commands or **`@`** to attach a workspace file.
- **Settings → Model** changes the connection. **F1** opens help.

Text attachments work without vision. Image attachments require a supported
vision-capable backend. See the [terminal guide](docs/tui.md) for details.

## Run a task from the CLI

Specify the backend explicitly: the standalone CLI's legacy default is
KoboldCPP on port 5001, while the TUI's first-run connection is llama.cpp on 8080.

```powershell
.\gektor.exe -backend-kind llama -backend http://127.0.0.1:8080 -workspace "D:\path\to\project" -verify-cmd "go test ./..." "Fix the failing title test"
```

`-plan` selects read-only planning. `-mode json` emits JSONL events.
`-verify-cmd` runs a final command once and records its result; failure fails
the run without silently starting another model turn. The default step budget
is 25; explicit `-max-steps 0` removes the limit for the main agent.

For a hosted OpenAI-compatible API, use `-backend-kind openai`, its API-root
URL, and `-model`. Supply credentials through an environment variable such as
`GEKTOR_API_KEY` rather than putting a key on the command line. Local-server
context detection takes precedence over a manual limit; hosted endpoints may
need `-context-limit` set explicitly.

[CLI reference](docs/cli.md) · [stdio protocol](docs/rpc.md) ·
[setup and service ownership](docs/bootstrap.md)

## History and instructions

Submitted input and completed steps are saved under the user's `.gektor/`
directory. Existing installations reuse `~/.tars` when `~/.gektor` is absent;
no session data is moved. `GEKTOR_API_KEY` replaces `TARS_API_KEY`, which remains
accepted for existing setups. `-resume` accepts a session directory or its `state.json` file;
`-fork` continues a copy under a new task ID. Compaction reduces the model's
working context to an extractive summary and recent messages, preserving
the full archive for display and search. It does not ask an LLM to summarize.

Ancestor and workspace `AGENTS.md` files are loaded into the agent's
instructions. Guides in [`skills/`](skills/README.md) are read by the model
when relevant. They belong to the task workspace; running GEKTOR from this
checkout does not install them into another project.

## Current limits

Model quality and tool-call reliability depend on the model and its server.
Streaming and prompt-processing telemetry also depend on what the backend
exposes; KoboldCPP uses non-streaming replies.

GEKTOR is **not a sandbox**. File tools guard against paths outside the workspace,
but shell commands run with your account's permissions and symlinks are
followed. Use a container or VM when isolation matters. There is no automatic
workspace backup or rollback. See [SECURITY.md](SECURITY.md).

## Develop and evaluate

```text
go test -race ./...
go vet ./...
go build ./...
go run ./cmd/eval -dry
```

The dry run validates probe definitions without model calls. Live evaluations
run tasks in throwaway fixture workspaces and check files, tool usage and the
answer. See the [eval guide](eval/README.md) for setup, scoring and model-backed
runs. On Windows, `build.cmd check` runs the local checks together.

[Architecture](ARCHITECTURE.md) · [conventions](docs/conventions.md) ·
[documentation index](docs/README.md)

## License

[MIT](LICENSE).
