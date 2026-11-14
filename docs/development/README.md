# Build and contribute

Use a source checkout to change GEKTOR. Downloaded releases already contain a
binary and need no Go installation.

## Build

Install Go 1.26.11 or newer. From the repository root:

```powershell
go build -o gektor.exe ./cmd/gektor
.\gektor.exe -tui
```

On Linux or in Git Bash, `bash build.sh` builds the root binary.
Use `bash build.sh help` for all modes, including `test`, `check`, `fmt`,
`vuln`, `dist` and `clean`. Start it with `./gektor -tui` on Linux.
Start a model server separately for Agent work; workflow examples without model
steps run without one.

## Find the right part of the project

- [Architecture](../../ARCHITECTURE.md) explains runtime ownership and common change paths.
- [Conventions](conventions.md) contains enforced project rules.
- [Built-in capabilities](capabilities.md) defines service, adapter and support-package boundaries.
- [Testing](testing.md) covers automated checks and terminal recovery verification.
- [Terminal architecture](tui.md) covers components and interaction contracts.
- [Eval runner](../../eval/README.md) explains model scenarios and scoring.
- [Writing documentation](writing.md) defines article ownership and editing checks.
- [Website](../../website/README.md) explains publication and local previews.
- [Scripts](../../scripts/README.md) covers build, benchmark and release helpers.
- [Editable terminal designs](../design/tui/README.md) stay beside their native assets.

## Check and package

Run the checks in [Testing](testing.md) before submitting a change. On Windows,
`build.cmd check` runs the local suite. Browser smoke checks and model evals are
opt-in and require their documented dependencies.

`build.cmd dist` (or `bash build.sh dist`) builds the host platform's release
folder under `dist/gektor-<os>-<arch>/`, a ZIP of that folder and a `.zip.sha256`
checksum. It uses the current checkout and verifies archive contents before
reporting completion. The underlying packager also stages `dist/<os>-<arch>/`.
The current packager supports Windows/Linux amd64.
`build.cmd run -- -tui` builds and starts the TUI. See [Scripts](../../scripts/README.md)
for the shared command set. `dist` already creates the release artifacts; the
unified helper has no separate `release` mode and does not publish anything.

For a clean, versioned Windows/Linux release, the existing low-level command
`go run ./cmd/release vMAJOR.MINOR.PATCH` still builds both archives and
`SHA256SUMS.txt` under `dist/`. Those archives are verified against staged
contents and executable permissions. Versioned builds require a clean checkout
and use committed payload bytes, independent of checkout line endings. Go and
Git are the build requirements. Packaging uses tracked files, so commit new
runtime files and documentation before expecting them in a GEKTOR package.
Internal work records and native design assets are excluded. Links to omitted
contributor sources point to the packaged revision on GitHub; guides and reference
links remain local. Frozen skill files are copied without rewriting.

Development distributions permit tracked working-tree changes; untracked files
are excluded. Commit new runtime files before expecting them in a package.
The unified helper replaces only distribution directories marked as its own.
Move unmarked output aside before rebuilding. Keep deployed user data outside
these disposable build folders. `build.cmd clean` removes root app binaries only.
