# Setup and saved data

## Model server

Start an inference server independently and wait for the model to load.
GEKTOR defaults to llama.cpp at `http://127.0.0.1:8080` in both CLI and the
first-run TUI profile. Configure other addresses under Model connection in Settings, or with
`-backend`; use `-backend-kind openai` for an OpenAI-compatible API.
KoboldCPP remains available through `-backend-kind kobold`.

For a ChatGPT account connection, run `gektor auth login` and select ChatGPT
in Model connection settings. See [authentication](cli.md#authentication-remote-providers).

GEKTOR does not install models or start, stop or supervise inference servers.
Readiness, server logs and recovery belong to your server setup. A lost
connection is reported as a request error.

## Windows launchers

`start-gektor-agent.cmd` opens Agent; `start-gektor-workflows.cmd` opens Workflows.
Both restore saved settings. Browser access is optional with `-browser chrome`.
In a source checkout it rebuilds the binary first; release archives use the
included executable without requiring Go.

The root also has `run-gektor-agent.cmd`, `run-gektor-workflow.cmd` and
`serve-gektor-workflows.cmd` for CLI use. Relative paths use the caller's folder.
See [launchers and helpers](../scripts/README.md) and [demos](../demos/README.md).
No backend launchers are included.

## Configuration

The TUI saves connection and generation settings in the user's GEKTOR directory.
Explicit CLI flags override the selected configuration. API keys are read from
the process environment; GEKTOR does not automatically load `.env`.
See [authentication](cli.md#authentication-remote-providers).

`.env.example` documents the API key variable and optional external benchmark
settings. Only `scripts/bench-llama.cmd` reads its benchmark settings from `.env`.
The file is gitignored; do not commit credentials or machine-specific paths.

## Saved data

Preferences, sessions and retained tool output live under `~/.gektor`.
On Windows, `~` means your user folder, usually `C:\Users\<name>`.
`GEKTOR_API_KEY` is the generic API key variable.

| Data | Location | What it is for |
|---|---|---|
| TUI preferences | `~/.gektor/application.json` | Workspace and display preferences |
| Custom profiles | `~/.gektor/profiles/custom/` | Saved model and identity settings |
| Agent sessions | `~/.gektor/sessions/` | TUI conversation history and saved context |
| CLI tasks | `~/.gektor/tasks/` | Standalone CLI session history |
| Workflow recipes | Files you choose inside the workspace | Editable instructions; can be tracked in Git |
| Workflow run records | `~/.gektor/workflows/<workspace-hash>/runs/<run-id>/run.json` | Captured recipe, inputs, results and status |
| Workflow records | `~/.gektor/workflows/<workspace-hash>/records/` | Structured data used by record operations |
| Canvas layouts | `~/.gektor/workflow-layouts/` | Card positions and position locks, separate from recipes |

Workflow history belongs to a workspace path. Moving the workspace gives it a
different history location. Existing records remain under the old workspace key.
There is no shared recipe/template library in the user directory yet.
To reuse a recipe, copy it into the target workspace and check its paths.

Schedules and queued jobs also use the workspace's workflow store. They need
the [workflow service](cli.md#workflow-queue-and-service) to process them.
The TUI does not have a schedule-management screen.

A session stores the full conversation archive and its compacted model context.
The archive allows resume, fork and historical reads. It is not a backup of
workspace files. Output retention and session controls are described in the
[terminal guide](tui.md).

## Distribution

Release archives contain a Windows or Linux amd64 binary, profiles, skills and
documentation and workflow examples. Windows archives also include launchers,
CLI helpers and demos.
Models, server runtimes, Node.js and browsers are external dependencies.
Browser requirements and first-use preparation are documented in
[browser setup](browser.md).
