# Launchers and helper scripts

## Windows entry points

The five main launchers live beside `gektor.exe`:

| Script | Purpose |
|---|---|
| `start-gektor-agent.cmd` | Open the Agent TUI with saved settings |
| `start-gektor-workflows.cmd` | Open the Workflows TUI with saved settings |
| `run-gektor-agent.cmd [flags] "task"` | Run one Agent task in the CLI |
| `run-gektor-workflow.cmd [flags] recipe.json` | Run a saved recipe once |
| `serve-gektor-workflows.cmd [flags]` | Run the workflow worker until stopped |

All launchers build first in a source checkout.
Release archives use the included binary and do not require Go.
They do not start or manage model servers.

**Start** opens a screen, **run** executes once, and **serve** stays running.
These words do not select a backend.

The TUI restores saved settings and workspace.
The Agent CLI uses its normal flag/profile defaults.
A workflow uses the profiles named by its model nodes; many workflows need no model.
For CLI profiles, see [the reference](../docs/cli.md#profiles).

Run these examples from the application folder:

```powershell
.\start-gektor-agent.cmd
.\start-gektor-agent.cmd -profile profiles/chatgpt.json
.\start-gektor-agent.cmd -browser chrome -browser-profile work
.\start-gektor-workflows.cmd
.\run-gektor-agent.cmd -profile profiles/chatgpt.json -workspace D:\Projects\my-app "Explain this project"
.\run-gektor-workflow.cmd -workspace examples/workflows/transform-and-review recipe.json
.\serve-gektor-workflows.cmd -workspace D:\Projects\my-app
```

The ChatGPT profile uses your signed-in account. Connect with `gektor.exe auth login`.
Edit the profile to change its model or context budget.

Relative CLI paths use the caller's current directory, even when the launcher
is installed elsewhere. The build helper temporarily enters the installation
folder and then restores the caller's directory.
Keep flags before the Agent task text or workflow recipe path.
A worker still needs permission rules for the operations it will execute;
starting it does not grant access.

The new TUI launchers do not enable browser access by default.
Pass `-browser chrome` or `-browser msedge` when needed.
This makes browser tools available for lazy activation.

## Existing names

The older CLI helpers still work:

- `scripts/run-task.cmd` forwards to `run-gektor-agent.cmd`.
- `scripts/run-plan.cmd` runs an Agent task with `-plan`.
- `scripts/run-resume.cmd [id or path]` lists saved CLI tasks or resumes one.

Use [the demos](../demos/README.md) to try complete examples without remembering
their inputs and permission flags.

## Development helpers

These require a source checkout:

- `build.cmd` builds packages and writes `gektor.exe` at the project root.
- `build.cmd test [flags]` runs tests.
- `build.cmd check` runs vet, builds, race tests, conventions and dry eval probes.
- `build.cmd fmt` lists files that gofmt would change; `fmt fix` formats them.
- `build.cmd vuln` runs govulncheck, installing it if needed.
- `build.cmd clean` removes generated root executables only.
  It leaves `bin/`, scratch folders, saved history and eval results alone.
- `scripts/tui-smoke.go` starts an isolated session-recovery check with a fake backend.
  See the [terminal smoke procedure](../docs/tui-smoke.md) for build and run commands.
- `eval-run.cmd [flags]` runs model eval probes.
- `eval-dry.cmd [flags]` checks probes without model calls.
- `bench-llama.cmd [quick] [model.gguf]` runs external llama-bench.
  It reads benchmark settings from the root `.env`; see `.env.example`.
- `bench-report.ps1 -Dir PATH` reports on an existing benchmark directory.
- `release.py vMAJOR.MINOR.PATCH` builds Windows/Linux archives and checksums
  under `tmp/`. It does not publish them.

Invoke helpers under `scripts/` as `scripts/NAME.cmd`.
`ensure-build.cmd` builds in the application folder without changing the caller's
working directory. `set-env.ps1` supports custom dotenv launchers;
GEKTOR itself does not load `.env`.
