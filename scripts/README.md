# Helper scripts

The application connects to an already running model server. These scripts do
not start or manage inference backends.

Release archives include the three CLI launchers and ensure-build.cmd.
Eval, benchmark and release-building helpers require a source checkout.

Windows helpers change to the application root before running. Invoke them
from the repository root as `scripts\NAME.cmd`:

- `run-task.cmd "task"`: run one CLI task.
- `run-plan.cmd "task"`: run a read-only planning task.
- `run-resume.cmd [id or path]`: list saved CLI tasks or resume one.
- `eval-run.cmd [flags]`: run eval probes against a model server.
- `eval-dry.cmd [flags]`: validate probes without model calls.
- `eval-reset.cmd`: restore the committed manual fixture baseline.
- `bench-llama.cmd [quick] [model.gguf]`: run the external llama-bench tool.
  Reads benchmark settings from the root `.env`; see `.env.example`.
- `bench-report.ps1 -Dir PATH`: report on an existing llama-bench results directory.

`release.py vMAJOR.MINOR.PATCH` builds Windows/Linux amd64 archives and SHA-256
checksums under `tmp/`. It does not publish a release.

`ensure-build.cmd` is called by launchers after changing to the application root.
It rebuilds a source checkout or checks that a release binary is present.
`set-env.ps1` is available for custom local launchers that import a dotenv file;
the application itself does not load it.

`run-tui.cmd` and `build.cmd` remain in the root as the UI and build entry points.
