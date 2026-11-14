# Launchers and helper scripts

## Application launchers

See [CLI launchers](../docs/reference/cli.md#windows-launchers) for application
entry points and syntax. Releases use the included binary; source checkouts
build first. Relative paths use the caller's current directory.

## Task helpers

- `scripts/run-task.cmd` forwards to `run-cli-agent.cmd`.
- `scripts/run-plan.cmd` runs an Agent task with `-plan`.
- `scripts/run-resume.cmd [id or path]` lists saved CLI tasks or resumes one.

Use [the demos](../examples/demos/README.md) to try complete examples without remembering
their inputs and permission flags.

## Development helpers

These require a source checkout:

Use `build.cmd <command>` on Windows or `bash build.sh <command>` on Linux
and in Git Bash. Both call the same Go helper from the repository root.

| Command | Result |
|---|---|
| `build` | Build all packages and write application executables at the root. |
| `run [target] [-- args]` | Build one application, then start it with the supplied arguments. |
| `dist` | Build the native release folder, ZIP archive and `.zip.sha256` checksum under `dist/`. |
| `test [flags]` | Run tests with optional Go test flags. |
| `vet [flags]` | Run `go vet`. |
| `check` | Vet, build, optional lint, race tests and project checks. |
| `fmt [fix]` | List formatting changes, or apply them with `fix`. |
| `vuln` | Run an installed `govulncheck`; print installation instructions if absent. |
| `clean` | Remove generated root application executables, leaving `dist/` and user data. |
| `help` | Show commands, application targets and output paths. |

For example, `build.cmd run -- -tui` rebuilds and starts the TUI;
`bash build.sh test -run TestConventions` runs selected tests. The target is
`gektor`. Dist builds `dist/gektor-<os>-<arch>/`, its ZIP and checksum for the
host platform (Windows/Linux amd64). The underlying packager also stages `dist/<os>-<arch>/`.
Test modes use a temporary home directory while preserving the Go caches.
`check` includes dry eval probes; race tests require a C compiler. Lint is
skipped with a message because GEKTOR has no golangci-lint configuration.

The low-level `go run ./cmd/release vMAJOR.MINOR.PATCH` command still creates
clean, versioned Windows/Linux archives. This is separate from the unified
local helper; publishing remains a separate action.

Other project helpers:

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

Invoke helpers under `scripts/` as `scripts/NAME.cmd`, or `bash scripts/NAME.sh` in Git Bash.
The application launchers, task helpers, and eval helpers have Bash counterparts.
Benchmark helpers remain Windows scripts.
`ensure-build.cmd` and `ensure-build.sh` build in the application folder without changing the caller's
working directory. `set-env.ps1` supports custom dotenv launchers;
GEKTOR itself does not load `.env`.

The root build scripts forward to `cmd/build`; maintain command handling there
so Windows and Bash stay consistent. `go test ./cmd/build` covers arguments,
archive integrity, launcher permissions and owned output directories.
