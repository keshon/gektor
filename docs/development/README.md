# Build and contribute

Use a source checkout to change GEKTOR. Downloaded releases already contain a
binary and need no Go installation.

## Build

Install Go 1.26.10 or newer. From the repository root:

```powershell
go build -o gektor.exe ./cmd/gektor
.\gektor.exe -tui
```

On Linux, use `go build -o gektor ./cmd/gektor` and `./gektor -tui`.
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

`python scripts/release.py vMAJOR.MINOR.PATCH` builds Windows and Linux archives
under `tmp/` and checks their contents. It does not publish a release. Packaging
uses tracked files, so new documentation must be tracked before a release build.
Internal work records and native design assets are excluded. Links to omitted
contributor sources point to the packaged revision on GitHub; guides and reference
links remain local. Frozen skill files are copied without rewriting.
