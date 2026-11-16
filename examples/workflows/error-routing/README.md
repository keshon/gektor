# Error routes and finalizers

Run from the repository root:

```text
gektor workflow run -workspace examples/workflows/error-routing -allow "workflow_run=*,script_transform=*,artifact_write=*" recipe.json
```

The script deliberately fails. The command exits with an error and returns a run
ID with status `failed`. No model or network service is used.

The `report` step runs because `transform` has a typed operation failure. Its
`failure.txt` artifact contains `script_failed`. The `finish` step writes
`finished.txt` after the error route settles, even though it appears first in the
saved recipe. Ordinary work stops: `after.txt` is never created. These are stored
run artifacts; they are not written into this example folder.

Inspect and list the existing run:

```text
gektor workflow inspect -workspace examples/workflows/error-routing <run-id>
gektor workflow artifacts -workspace examples/workflows/error-routing <run-id>
```

Neither the error route nor the finalizer turns failure into success. They still
need their own permissions. Cancellation, a denied operation, an invalid output
contract or a failed checkpoint stops execution without dispatching more work.

See [the recipe reference](../../../docs/reference/recipes.md#error-routes-and-finalizers)
for the failure envelope, ordering and pause/recovery rules.
