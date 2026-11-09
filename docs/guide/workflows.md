# Run and edit a workflow

Open a saved recipe, supply its inputs and inspect each step's result. For a first run without a model, follow [Get started](../start.md#run-a-workflow-without-a-model).

## Recipes and runs

A workflow is a set of saved steps.
The runner follows those steps instead of asking a model to choose each action.

| Term | Meaning | Available today |
|---|---|---|
| **Recipe** | Instructions: steps, connections, inputs and outputs | A JSON file in the workspace |
| **Run** | One execution with its input values and results | Saved separately in your GEKTOR user directory |
| **Template** | A reusable starting point copied into a new recipe | No template library yet; copy a recipe manually |
| **Schedule** | A trigger that starts a workflow at set times | Schedule card in the editor; executed by the workflow service |

You can edit and run workflows without a model connection.
AI change requests need a model. A model-generation node also needs its configured model.

## Open or create a recipe

Finish or stop the active Agent turn first.
Choose **GEKTOR > Workflows** to enter the editor, then use **Recipe**:

- **Open recipe…** opens a workspace recipe.
- **New recipe…** starts one.
You can also use `/workflows` to enter the editor.

The editor has **Recipe / Node / Run / View / Settings / Help** menus.
Choose a recipe path inside the workspace.
For example, `workflows/image-report.json` is a useful convention,
not a required folder.
There is no automatic recipe library or shared user-directory recipe lookup yet.

In the editor, `Ctrl+O` opens, `Ctrl+N` creates and `Ctrl+S` saves.
Save before replacing a draft.
New/Open asks before discarding unsaved changes.

## Read the nodes and connections

Each card shows its name, operation, input and output ports, and status.
The selected card has a double border.

- Inputs are on the left. They receive values.
- Outputs are on the right. They pass results to other steps.
- Solid lines carry data, such as text from one step to another.
- Dashed lines set order. The diamond ports **then** and **after**
  mean “wait for this step,” without passing a value.
- Filled ports are connected; empty ports are not.
- A crossing is not a connection.
  A `!` at an output means the wire could not be drawn.
  The recipe still keeps the connection.

**Not run** means there is no run result on that card.
It does not mean the recipe has passed validation.
Steps execute one at a time, even when the graph branches.

Click a card or use the arrow keys to select it.
Drag empty space to pan. Use `Shift+wheel` for horizontal movement,
or `Alt+arrows` to pan with the keyboard.

**Lock node positions** in View (`G`) prevents accidental card movement.
You can still pan, inspect and edit.
When unlocked, drag cards or use `Shift+arrows` to move them.
`L` arranges them automatically; `F` shows an overview.

## Inspect a step

Select a node and press `Ctrl+T`. The inspector has three tabs:

| Tab | What it answers |
|---|---|
| **Properties** | What values does this step use? Where do they come from? Who uses its outputs? |
| **Result** | What happened in the selected run? What values and errors were recorded? |
| **Code** | What script does this step use, if it has one? |

Click a tab or press `1`, `2` or `3`.
`Tab` moves focus between canvas and inspector.
In the focused inspector, left/right changes tabs and up/down scrolls.
Tabs stay above the scrolling content. The active tab has brackets;
its color shows whether the inspector has focus. Help uses the same tabs,
with mouse clicks or Tab and left/right to choose a category.

Properties uses the same port names as the cards.
Result needs a selected run; a draft alone has no recorded result.
`Ctrl+J` shows raw data when you need the full record.
Close the inspector with its close button or focused `Esc`.

## Change a recipe

1. Press `A`, or choose **Add node…** in Node, to select an operation.
2. Press `Enter` on a draft node to edit its arguments.
3. Click an output port, then an input port, to set their connection.
   Review the mapping in the form before applying.
4. Choose **Behavior…** in Node (`E` on the canvas) for conditions,
   repeat-for-each, ordering, review and retry settings.
5. Choose **Recipe settings…** in Recipe to change its name, inputs or outputs.
6. Save with `Ctrl+S`.

`B` opens connections for editing or removal. `C` opens a connection form.
`Delete` removes a node. `Ctrl+Z` undoes a recipe change; `Ctrl+Y` redoes it.
These controls do not undo an executed operation.

In the focused inspector, `E` edits Properties or Code instead of node behavior.
Code has a separate script editor. Complex values may still need JSON.
`J` on the canvas opens the full recipe JSON.

Press `I` to describe a change to the model.
Review its proposed changes before applying them with `S`.
Esc discards the proposal.
Applying changes updates the draft; it does not save or run it.
Save and validate after reviewing it.

## Validate and run

1. Press `V`, or choose **Validate recipe…** in Run, to check the recipe.
   Supply input values if asked. Fix any reported errors.
2. Press `R`, or choose **Run recipe…** in Run, to execute it.
3. If it declares inputs, a form shows their defaults.
   Change the values for this run, then continue.
4. Read and answer any permission requests or step reviews.
5. Select a node and open **Result** to inspect what happened.

Inputs are values the recipe expects from you, such as a folder, a label
or a list of numbers.
They let you reuse the same steps with different data.
Text fields take plain text.
List fields accept a JSON array such as `[2, 3, 5]`,
or comma-separated JSON values such as `2, 3, 5`.
Other complex values use JSON. Recipes without inputs skip the form.

**Run with JSON inputs…** in Run is an advanced option for entering the complete input
object. It is not needed for ordinary runs.

Validation checks the recipe and input types.
It does not prove that a later file, server request or model call will succeed.
The TUI runs the current draft; save it to reopen those edits later.

Press Esc during execution to request cancellation.
Completed writes and requests remain.
A failed run stops the remaining work; inspect results before running again.

## Review and reopen a run

Press `H`, or choose **Run history…** in Run, to browse saved runs, newest first.
Press `/` to filter by recipe, status, date or run ID. Choose **Open run**, or
press `Enter`, to inspect the selected record.
It shows the recipe captured for that run and its recorded results.
Editing the current recipe does not change that record.

A step may pause for review before it executes.
Press `P`, or choose **Review run…** in Run, to approve or reject it.
Approval continues that saved run without repeating completed steps.
Permissions still apply.

Saved runs are read-only.
Press `D`, or choose **Return to recipe** in Run, to edit the draft again.
There is no general “resume any failed step” action.

## Try a small example

The bundled [transform-and-review example](../../examples/workflows/transform-and-review/README.md)
lets you try a run without a model call:

1. Set the workspace to `examples/workflows/transform-and-review` in the application folder.
2. Open `recipe.json` in Workflows.
3. Press `V` to validate and keep the default numbers.
4. Press `R` to run, again keeping the defaults. Answer permission requests if shown.
5. The run pauses before the **double** step. Press `P` to review it and approve.
6. Select **double**, open the inspector and choose Result.
   Its output contains the doubled numbers: `4, 6, 10`.
7. Press `D` to return to the draft. Run it again with different input numbers.

This example changes data in memory and stores run results; it does not edit
project files. You do not need to change its script to try different numbers.

## Save, leave and reuse

`Ctrl+S` saves the recipe file.
Card positions and the position lock are stored separately in your user directory.
Moving a card does not change execution.

Choose **Agent** in GEKTOR, or press Esc on the canvas, to return
to Agent. The draft stays in memory while the app is open.
Save before quitting; this in-memory draft is not crash recovery.

To reuse a recipe in another project, copy it into that workspace.
Check its paths and required profiles.
Paths inside called recipes are also relative to the workspace,
not the recipe's own folder.
There is no automatic shared-template installation.

Background queue management and webhook setup use the
[CLI/service](../reference/cli.md#workflow-queue-and-service). Schedules can be managed
from the editor or the CLI.
Closing the TUI does not keep an ordinary foreground run alive.

See the [workflow reference](../reference/recipes.md) for available operations,
data mappings, execution rules and examples.

## Run a record recipe without AI

Save this schema as `notes.schema.json`:

```json
{"type":"object","properties":{"text":{"type":"string"}},"required":["text"],"additionalProperties":false}
```

Save this recipe as `note.json`:

```json
{
  "version": 3,
  "name": "Save and read a note",
  "inputs": {"text": {"type": "string", "default": "First workflow"}},
  "steps": [
    {"id": "save", "tool": "record_write", "args": {
      "kind": "notes", "id": "first", "expected_revision": 0,
      "data": {"text": {"$input": "text"}}
    }},
    {"id": "read", "tool": "record_read", "args": {
      "kind": "notes", "id": {"$step": "save", "pointer": "/id"}
    }}
  ]
}
```

From that workspace:

```powershell
gektor workflow register-schema notes notes.schema.json
gektor workflow validate note.json
gektor workflow run -yes -allow "record_write=notes/*" note.json
gektor workflow record notes first
gektor workflow runs
gektor workflow inspect <run-id>
```

No model configuration or TUI is involved. The record survives process restarts.
Running this create recipe again fails with a revision conflict; it does not
replace the saved note. To update it, use its current revision in
`expected_revision`, or reference the revision returned by a preceding read step.

## TUI editor

The [workflow workspace](workflows.md#recipes-and-runs) opens from `/workflows` or GEKTOR, Workflows. It edits the same version 3 recipes used by the CLI and uses the same execution, permission and review services. Canvas layout is user-local metadata, not part of the executable recipe.
