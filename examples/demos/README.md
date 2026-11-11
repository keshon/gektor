# Demos

These launchers show complete tasks. They reuse the maintained
[workflow examples](../workflows/README.md); they do not keep separate
copies of recipes. Model reliability is checked by [evals](../../eval/README.md).

These launchers require Windows. Run them from the extracted release folder
or a built source checkout. For a model-neutral first Agent task, use
[Get started](../../docs/start.md).
Each demo creates a new workspace under `tmp/demos/` and prints its location.
Existing example files are not edited.
Demo windows wait at the end so you can read the result. Add `--no-pause`
when running them from an automated check.

| Script | What it shows | Needs a model |
|---|---|---|
| `agent-task.cmd` | Agent reads a small note in Plan mode and returns its code | Signed-in ChatGPT |
| `workflow-review.cmd` | A workflow pauses for review, then returns 4, 6, 10 | No |
| `workflow-draft.cmd` | Read the public Moltbook feed and save a local draft | Signed-in ChatGPT |

For the ChatGPT demos, run `gektor.exe auth login` first if needed.
They use `gpt-6.1-sol`; edit `profiles/chatgpt.json` for the Agent demo or
`examples/workflows/moltbook-draft/chatgpt.json` for the draft demo
to select another available model.

```powershell
.\examples\demos\agent-task.cmd
.\examples\demos\workflow-review.cmd
.\examples\demos\workflow-draft.cmd
```

Use the review demo for a quick engine check without a model or network.
The review demo automatically approves its known arithmetic step after checking
that the run paused. It does not approve arbitrary recipes.

The draft demo makes a public GET request and saves a run artifact.
It never publishes, comments or votes. An API error may produce a completed
run with no draft; the demo reports that status.

The workflow examples are also available for manual editing in the TUI.
Open Workflows with `start-gektor-workflows.cmd`, select a demo workspace in
Settings, and open its `recipe.json`.
The workflow demos print their run ID so you can reopen their saved results.

See [launchers](../../scripts/README.md) for general commands and
[the terminal guide](../../docs/reference/terminal.md) for using Agent and Workflows.
