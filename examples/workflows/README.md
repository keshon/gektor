# Workflow examples

Start with **transform-and-review**. It runs without a model or network.
Each folder is a workspace with a recipe and instructions for running it.
Copy a folder before changing it. Runs and artifacts are saved separately from
the recipe; exporting an artifact creates a file in the workspace.

| Example | What it teaches | Requirements |
|---|---|---|
| [Transform and review](transform-and-review/README.md) | Inputs, per-item work and human approval | None |
| [Collection report](collection-report/README.md) | Bounded parallel items and explicit failure collection | None |
| [Log report](log-report/README.md) | Capture a file and save a report artifact | None |
| [Subworkflow](subworkflow/README.md) | Call a reusable recipe and return named outputs | None |
| [Local program](process-report/README.md) | Feed captured data to a program and retain diagnostics | Python |
| [Repository report](repository-report/README.md) | HTTP data, review and revision-checked records | GitHub network access; optional model |
| [Webhook note](webhook-note/README.md) | Submit jobs to the workflow service without duplicates | Local service and tokens you create |
| [Feed draft](moltbook-draft/README.md) | Combine a public feed, model output and saved state | Network and a model |

These are teaching examples. They are not acceptance tests and a successful
demo does not measure model reliability. Each covers a different part of the
engine; the feed example is a local draft, not a publishing bot.

[Demos](../demos/README.md) launch selected examples in fresh workspaces.
[Evals](../../eval/README.md) test the Agent with isolated fixtures and scored
results. Engine behavior is tested in Go, without depending on a model or public
API. Use the [workflow reference](../../docs/reference/recipes.md) for recipe fields.
