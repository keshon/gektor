# Bootstrap and runtime ownership

Status: profiles and owned llama.cpp processes are implemented. Automatic
fresh-machine downloading/installation is still pending. The source repository
intentionally excludes model weights and runtime bundles.

## Current setup

Start an external backend and configure **Settings → Model**. No Python
installation is required. For managed thinking, supply a compatible
`llama-server` executable and a GGUF model; Settings → Model supports nested model
folders and files outside the installation directory.

```powershell
.\run-server-llama.cmd      # or run-server-kobold.cmd
.\run-tui.cmd
```

For a new machine without an existing asset bundle, install the selected
runtimes and models manually. There is no online downloader.

The sections below describe the remaining release installation contract.

## Installation paths

External thinking service: copy the Go executable and a profile, supply the
endpoint/model/context size and any API key through an environment variable.
The service is started and stopped by its owner, not Tars.

Managed thinking service: the profile declares `ownership: managed` with a
runtime executable and a model file. Tars starts `llama-server` itself, waits for
readiness, and owns the process tree. This needs the llama.cpp bundle under
`assets/runtimes/llamacpp` and a GGUF under `assets/models/thinking`.

These are separate supported targets, not silent fallbacks that change the
experience.

## Directory ownership

- `cmd`: executable entry points; `internal`: Go implementation.
- `profiles`: small, versioned configuration examples; personal overrides stay private.
- `scripts`: reproducible build and installation operations.
- `assets/runtimes`: application-managed native runtimes.
- `assets/models`: explicitly selected downloaded weights, never Git source.
- `bin`: built application executables.
- `var`: sessions, diagnostics and runtime state.
- `lab`: experiments only; shipping code must have no dependency on it.

Workspace coding checkpoints remain under `.tars`.

## Installation contract

Each managed dependency needs a manifest: version, source URL, immutable
revision, platform/architecture, file checksums, expected installed files,
license and required GPU driver. Weight choices are explicit and download
sizes are displayed before installation. Never download every provider.

Install into a staging directory, verify checksums, then publish the completed
installation. Interrupted downloads must be resumable or safely replaceable.
The resolved runtime must work after moving the installation directory.

## Service contract

A managed service has one owner responsible for startup, readiness timeout,
logs, cancellation and shutdown of its process tree. Occupied ports are errors,
not permission to attach to an unknown process. External services are never
terminated by application shutdown. Readiness checks must identify the loaded
model and usable capabilities, not merely an open TCP port.

## Acceptance before calling it portable

1. Coding runs against an external backend with no managed assets at all.
2. A managed profile installs on a clean Windows machine.
3. The complete installation runs from a path containing spaces and survives relocation.
4. Offline restart works once all selected assets are installed.
5. Missing assets, bad checksums and occupied ports give actionable errors.
6. Shutdown leaves no owned child process; external services survive.

Current `-check-profile` is configuration validation only. It establishes none
of the managed-runtime acceptance above.

## Model layout

Models are grouped by their pipeline role, then by provider/model family:

```
assets/
  models/
    thinking/qwen/
  runtimes/
    llamacpp/
```

Runtimes remain technology bundles: a runtime can host several models and is not
itself a set of model weights. `profiles/llama-managed.json` selects these
locations. Managed models may instead point to any absolute GGUF path on another
drive. Model-directory discovery is recursive and independent of vendor folder
names. Older user-created profiles pointing at former flat asset paths should
select the current location and save a new copy.