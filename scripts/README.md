# Launchers and helper scripts

## Application launchers

See [CLI launchers](../docs/reference/cli.md#windows-launchers) for application
entry points and syntax. Releases use the included binary; source checkouts
build first. Relative paths use the caller's current directory.

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
  See the [terminal smoke procedure](../docs/development/testing.md) for build and run commands.
- `eval-run.cmd [flags]` runs model eval probes.
- `eval-dry.cmd [flags]` checks probes without model calls.
- `bench-llama.cmd [quick] [model.gguf]` runs external llama-bench.
  It reads benchmark settings from the root `.env`; see `.env.example`.
- `bench-report.ps1 -Dir PATH` reports on an existing benchmark directory.
- `release.py vMAJOR.MINOR.PATCH` builds Windows/Linux archives and checksums
  under `tmp/`. It does not publish them.
- `python scripts/release_test.py` checks documentation selection and offline-link rewriting.

Invoke helpers under `scripts/` as `scripts/NAME.cmd`.
`ensure-build.cmd` builds in the application folder without changing the caller's
working directory. `set-env.ps1` supports custom dotenv launchers;
GEKTOR itself does not load `.env`.
