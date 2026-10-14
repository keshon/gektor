# Bootstrap and runtime ownership

Status: migration contract. Profile loading and offline validation exist;
managed runtime installation and speech migration do not yet exist.

## Two installation paths

External thinking service: copy the Go executable and a profile, supply the
endpoint/model/context size and any API key through an environment variable.
No Python, CUDA toolkit, speech weights or microphone utility is needed for
text and coding. The service is started and stopped by its owner, not Tars.

Local voice installation: the application package must include or install its
own pinned speech runtimes and selected model assets. It must never resolve
Python through Kitten or any lab experiment. GPU runtime libraries belong to
that package; compiling CUDA tooling is not a normal user prerequisite.
CPU and GPU configurations must be separate supported installation targets,
not silent fallbacks that change the experience.

## Directory ownership

- `cmd`: executable entry points; `internal`: Go implementation.
- `profiles`: small, versioned configuration examples; personal overrides stay private.
- `scripts`: reproducible build and installation operations.
- `assets/runtimes`: application-managed native/Python runtimes.
- `assets/models`: explicitly selected downloaded weights, never Git source.
- `bin`: built application executables.
- `var`: sessions, audio, diagnostics and runtime state.
- `lab`: experiments only; shipping code must have no dependency on it.

These are migration destinations. Do not create empty service directories or
move existing Tars task checkpoints before compatibility readers exist.
Workspace coding checkpoints remain under `.tars` during migration.

## Installation contract

Each managed dependency needs a manifest: version, source URL, immutable
revision, platform/architecture, file checksums, expected installed files,
license and required GPU driver. Weight choices are explicit and download
sizes are displayed before installation. Never download every provider.

Install into a staging directory, verify checksums, then publish the completed
installation. Interrupted downloads must be resumable or safely replaceable.
The resolved runtime must work after moving the installation directory;
Python virtual environments with absolute base paths are not portable bundles.
Package a standalone runtime or recreate its environment on the destination.

For Chatterbox, first remove the current lab/Kitten Python dependency, pin the
Nano/Turbo worker dependencies and model revisions, and validate a clean
installation with Git, uv and Python absent from PATH. Bundle required audio
utilities or expose them as an explicit external dependency. Sharing resident
GPU memory with the thinking service must be checked before starting voice.

## Service contract

A managed service has one owner responsible for startup, readiness timeout,
logs, cancellation and shutdown of its process tree. Occupied ports are errors,
not permission to attach to an unknown process. External services are never
terminated by application shutdown. Readiness checks must identify the loaded
model and usable capabilities, not merely an open TCP port.

## Acceptance before calling it portable

1. Text/coding runs with an external backend without speech dependencies.
2. A selected speech profile installs on a clean Windows machine.
3. The complete installation runs from a path containing spaces and survives relocation.
4. Offline restart works once all selected assets are installed.
5. Missing assets, bad checksums, incompatible drivers and occupied ports give actionable errors.
6. Shutdown leaves no owned child process; external services survive.
7. Microphone, playback, cancellation and conversation are tested on the target machine.

Current `-check-profile` is configuration validation only. It establishes none
of the managed-runtime or physical-audio acceptance above.

## First migration result

Veyra now has a standalone Chatterbox runtime under `assets/runtimes/chatterbox`
and Nano/Turbo weights under `assets/models/speaking/chatterbox`. Its production profiles
no longer use the lab Python environment. The Go worker launches Python in
isolated mode. Short Nano and Turbo synthesis acceptance passed with the promoted
runtime. Local promotion and checksum checking are exposed through CMD wrappers.

This bundle can seed the Tars speech migration; the Tars branch does not yet
contain a speech adapter or an internet installer. Keep the package independent
of the old repository path and preserve its manifest/licenses during transfer.

## Speech migration installed locally

Tars now has an owned Chatterbox process adapter, Whisper recognition, native
process ownership and audio capture/playback under `internal/speech`.
`import-voice-assets.cmd -SourceAssets <prepared-bundle-assets>` imports the
verified Chatterbox bundle and Whisper CUDA runtime/weights into this project.
It validates source hashes and refuses mismatching destination files. On the
same NTFS volume it uses hardlinks for immutable assets; cross-volume imports
copy files. No source files are moved. Do not modify runtime/model files in
place: hardlinked installations share file contents. Install a new revision
in a new directory. Copying the completed assets to another machine transfers
ordinary files; it does not require the original Veyra directory.

FFmpeg and FFplay remain explicit external executables (default PATH lookup),
and thinking can use an external service or the imported managed llama.cpp runtime. This is a local bundle migration,
not yet an internet installer or complete one-file release. The tested GPU
runtime is the existing CUDA 12.4 Torch bundle; acceptance on the second
machine remains pending. Voices are English Nano/Turbo built-in references.

The local installed-model test synthesized speech, recognized the generated
audio through the owned Whisper server, canceled synthesis and generated a
fresh reply. Both owned processes exited afterward. The test used synthetic
audio and does not establish physical microphone or speaker behavior.

The local import now includes llama.cpp's executable, CUDA runtime DLLs and the
Qwen starter model. `profiles/chat-veyra-local.json` selects these assets;
`profiles/chat-veyra-voice.json` keeps its external thinking endpoint.
Managed models may instead point to any absolute GGUF path on another drive.
Model-directory discovery is recursive and independent of vendor folder names.
The shipped runtime passed a CPU load/reply/shutdown check on this machine;
GPU acceptance on the second machine is still separate.

## Asset roles

Models are grouped by their pipeline role, then by provider/model family:

```
assets/
  characters/
  models/
    listening/whisper/
    thinking/qwen/
    speaking/chatterbox/nano/
    speaking/chatterbox/turbo/
  runtimes/
    llamacpp/
    whisper-cuda/
    chatterbox/
```

Runtimes remain technology bundles: a runtime can host several models and is not
itself a set of model weights. Profiles and the local asset importer use this
layout. Models on other drives remain valid; this organization applies to the
application's managed assets. Older user-created profiles pointing at the former
flat asset paths should select the migrated location and save a new copy.

Hands-free requires the portable CPU Silero worker at
assets/runtimes/silero/vad-worker.exe, Sherpa/ONNX DLLs under
assets/runtimes/sherpa/lib, and assets/models/listening/silero/silero_vad.onnx.
The importer now includes these assets from the prepared Veyra bundle.
No CUDA toolkit, system Python or native compilation is used for VAD.
