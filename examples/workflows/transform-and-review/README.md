# Transform and review

From the repository root, build the current branch and select the example workspace:

```powershell
go build -o gektor.exe ./cmd/gektor
.\gektor.exe workflow validate -workspace examples/workflows/transform-and-review recipe.json
.\gektor.exe workflow run -workspace examples/workflows/transform-and-review -yes -allow "workflow_run=*,script_transform=*" recipe.json
.\gektor.exe workflow inspect -workspace examples/workflows/transform-and-review <run-id>
.\gektor.exe workflow review -workspace examples/workflows/transform-and-review -decision approve -allow "workflow_run=*,script_transform=*" <run-id> double
.\gektor.exe workflow inspect -workspace examples/workflows/transform-and-review <run-id>
```

The first command only validates. The run transforms the input into three sets
of node arguments, then waits for review. Approval executes those three items;
the final output is `{"items":[4,6,10]}`. The completed preparation step is not
repeated. Use `-decision reject` to stop instead. No model is involved.
