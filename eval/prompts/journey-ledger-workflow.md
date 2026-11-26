Create `ledger-review.json`, a reusable version 4 GEKTOR workflow for this repaired
ledger project. Discover the available workflow authoring tools and operation
schemas, save the recipe, and validate it. Do not run or approve it; the operator
will supply inputs and review the report after you finish.

The recipe must take required inputs `amounts` (an array of strings) and
`report_name` (a string). Every execution must run the real project's
`go test ./...`, then run `go run ./cmd/ledger` with the supplied amounts as literal
arguments. Use workflow `run_program` operations, in the workspace root. A failed
check or invalid amount must stop execution without publishing a report.

After both commands succeed, read the actual CLI stdout artifact and publish its
text, unchanged, as an immutable artifact named by `report_name`. The
`artifact_write` step that publishes this report must have `review: true`, so the
operator explicitly approves publication. Declare an output named `report` that
returns the published artifact's metadata. Neither the input amounts, report name,
nor calculated total may be hardcoded. Preserve the repaired library and CLI.
