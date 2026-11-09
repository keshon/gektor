# Diagnose a failed task

Start with the error on the failing tool or workflow step. Check completed work before retrying: stopping a run does not undo its effects.

## Troubleshooting

| Problem | What to check |
|---|---|
| Cannot connect to a local model | Is the server loaded? Do protocol and address in Model settings match it? |
| Mouse does nothing | Check that the terminal sends mouse events; in ConEmu, check its mouse settings |
| Ctrl+V does not attach a screenshot | Use Ctrl+Alt+V or /paste-image |
| Cannot edit a workflow node | Are you viewing a saved run? Return to draft with D |
| Cannot move nodes | Turn off Lock node positions in View |
| A workflow input asks for JSON | Use the expected type; lists can use comma-separated values |
| Need full failure details | Open the step's Result tab or the Agent tool inspector |

Agent sessions keep `diagnostics.jsonl` beside `state.json`.
It records request phases and tool boundaries, without message bodies or tool
arguments/results. It rotates at 2 MiB and retains one previous file.

Terminal margins belong to the terminal application.
For matching margins, use background `#F2F4F6` for the light theme or
`#11161C` for the dark theme in your terminal profile.

See [setup and saved data](../reference/configuration.md), [workflow reference](../reference/recipes.md)
and [security](../../SECURITY.md) for more detail.
