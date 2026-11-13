# Map a reusable workflow

Run the same frozen child recipe for each input object, with separate execution
records and ordered outcomes. This example needs no model, network or external
application.

From a built checkout or extracted application folder in PowerShell:

```powershell
$workspace = 'examples/workflows/mapped-subworkflow'
.\gektor.exe workflow validate -workspace $workspace recipe.json
.\gektor.exe workflow run -workspace $workspace -yes -allow 'workflow_run=*,workflow_call=double.json,script_transform=*' recipe.json
```

The default input `[-1, 2, 2, 3]` returns `completed: 3`, `failed: 1` and
`values: [4, 4, 6]`. The negative input deliberately fails. Both occurrences of
`2` execute independently. Inspection names the first child's operation
`batch[0]/double` and retains its failure in that item's child scope.

Calls execute serially. Leaf collections inside a child can run concurrently.
All children share the run's invocation and artifact budgets. A child review
pauses at that item; approval preserves preceding outcomes and continues the
saved snapshot. Rejecting a review cancels remaining items. Permission, validation,
checkpoint and output-contract failures stop the map even with `on_error: "collect"`.

On Linux use `./gektor` and equivalent shell syntax. See the
[recipe reference](../../../docs/reference/recipes.md#reusable-subworkflows).
