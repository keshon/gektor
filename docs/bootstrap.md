# Bootstrap and runtime ownership

Status: GEKTOR connects to a model server that someone else starts. It does not
ship, launch or supervise one. The source repository therefore contains no
model weights and no runtime bundles.

## Current setup

Start a backend, then configure **Settings → Model**. The launchers in the
repository root are the supported way to do this:

```powershell
.\run-server-llama.cmd      # llama.cpp on 127.0.0.1:8080
.\run-server-kobold.cmd      # KoboldCPP on 127.0.0.1:5001
```

Each reads `.env` (copy from `.env.example`), applies defaults, validates the
values it needs, refuses to start if the port is already serving, and then
execs the server in the foreground so its logs stay where you can see them.
Run one in its own terminal, wait for the model to load, then launch the UI:

```powershell
.\run-tui.cmd
```

There is no installer. Point `LLAMA_MODEL` or `KOBOLD_MODEL` at whatever weights
you already have.

## Configuration

`.env` holds machine-specific paths and keys and is gitignored. The launchers
name each setting they read, and anything they do not name goes in the
`*_ARGS` escape hatch.

`*_ARGS` is placed **first** on the command line. llama.cpp and KoboldCPP take
the last occurrence of a repeated flag, so every named setting wins over it.
That is deliberate: a documented setting the script defaults, validates and
reports should not be silently overridable by a string it never inspects.

## Directory ownership

- `cmd`: executable entry points; `internal`: Go implementation.
- `profiles`: small, versioned configuration examples; personal overrides stay private.
- `scripts`: reproducible build operations.
- `bin`: built application executables.
- `lab`: experiments only; shipping code must have no dependency on it.

Everything GEKTOR persists for the user -- preferences, sessions, missions,
task state, spilled tool output -- lives under `.gektor` in the user's home
directory. The repository is never written to except where the agent is
working on it.

## Service contract

GEKTOR starts no backend process. That means:

- It never terminates a server it did not start.
- A server that exits mid-run surfaces as a connection error on the next
  request, not as an automatic restart. Restart it yourself.
- An occupied port is the launcher's problem to report, which is why it probes
  before starting and says so instead of letting the server fail obscurely.
- Readiness, logs and shutdown belong to whoever owns the terminal the server
  runs in.

The last point is the trade being made. Nothing is supervised, so nothing can
be supervised badly — but there is also no automatic recovery, and no bundled
way to get a server running on a new machine.

## If you want a managed setup

`llama.cpp` and KoboldCPP both start with a handful of flags and keep running in
their own window. A process supervisor, a scheduled task, or a container image
built from your own base will cover what GEKTOR used to do here, and each of those
is a better fit for the job than an application-level supervisor with no
restart policy of its own.