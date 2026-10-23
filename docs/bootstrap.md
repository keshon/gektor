# Setup and saved data

## Model server

Start an inference server independently and wait for the model to load.
GEKTOR defaults to llama.cpp at `http://127.0.0.1:8080` in both CLI and the
first-run TUI profile. Configure other addresses in Settings → Model or with
`-backend`; use `-backend-kind openai` for an OpenAI-compatible API.
KoboldCPP remains available through `-backend-kind kobold`.

GEKTOR does not install models or start, stop or supervise inference servers.
Readiness, server logs and recovery belong to your server setup. A lost
connection is reported as a request error.

## Windows launchers

`run-tui.cmd` launches the UI and makes Chrome automation available on request.
In a source checkout it rebuilds the binary first; release archives use the
included executable without requiring Go.

Additional helpers live in `scripts/`: `run-task.cmd`, `run-plan.cmd` and
`run-resume.cmd`. They run from the application directory and forward flags
to the same executable. Development helpers include eval and llama-bench runners.
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
`GEKTOR_API_KEY` is the generic API key variable.

A session stores the full conversation archive and its compacted model context.
The archive allows resume, fork and historical reads. It is not a backup of
workspace files. Output retention and session controls are described in the
[terminal guide](tui.md).

## Distribution

Release archives contain a Windows or Linux amd64 binary, profiles, skills and
documentation. Windows archives also include the UI launcher and CLI helpers.
Models, server runtimes, Node.js and browsers are external dependencies.
Browser requirements and first-use preparation are documented in
[browser setup](browser.md).
