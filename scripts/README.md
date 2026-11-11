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

Use [the demos](../examples/demos/README.md) to try complete examples without remembering
their inputs and permission flags.

## Development helpers

These require a source checkout:

- `build.cmd` builds packages and writes `gektor.exe` at the project root.
- `build.cmd dist` stages a runnable Windows package under `dist/windows-amd64/`.
- `build.cmd release vMAJOR.MINOR.PATCH` builds Windows/Linux archives and checksums
  from the same staged payloads under `dist/`. It requires a clean checkout.
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
- `go run ./cmd/release dist` stages a package for the host OS; use
  `-target windows-amd64` or `-target linux-amd64` before `dist` to choose a target.
- `go run ./cmd/release vMAJOR.MINOR.PATCH` builds both release archives and
  `SHA256SUMS.txt`. It packages files; publishing to GitHub is a separate action.
- `go test ./internal/release` checks payload selection, offline links and archives.

Invoke helpers under `scripts/` as `scripts/NAME.cmd`.
`ensure-build.cmd` builds in the application folder without changing the caller's
working directory. `set-env.ps1` supports custom dotenv launchers;
GEKTOR itself does not load `.env`.
