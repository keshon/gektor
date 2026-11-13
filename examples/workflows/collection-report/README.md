# Collect item outcomes

Run a bounded parallel collection and report both successful and failed items.
This example uses the bundled JavaScript interpreter. No model, network or
external application is required.

From a built checkout or extracted application folder in PowerShell:

```powershell
$workspace = 'examples/workflows/collection-report'
.\gektor.exe workflow validate -workspace $workspace recipe.json
.\gektor.exe workflow run -workspace $workspace -yes -allow 'workflow_run=*,script_transform=*' recipe.json
```

The default input `[-1, 2, 3]` produces a report with `completed: 2`, `failed: 1`
and `values: [4, 6]`. The negative input deliberately fails. Its error remains in
the map's item results and saved execution record; the report still runs.

Up to three transforms overlap. Results retain their input order. Permission,
output-contract and persistence errors still stop execution. Remove
`on_error: "collect"` to stop on the first observed operation failure.

On Linux use `./gektor` and the equivalent shell syntax. Each execution saves a
separate run. See the [recipe reference](../../../docs/reference/recipes.md).
