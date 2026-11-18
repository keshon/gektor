# Test a workflow with a fixture

This example supplies a recorded HTTP response and executes the script against
it. No HTTP request or model call is made. Run from this folder:

```powershell
gektor workflow test -workspace . -fixture fixture.json -yes recipe.json
```

The result is `mode: fixture`, `passed: true`, with status 200 and value 6 in
`outputs`. Change the script to multiply by 3 and the test fails. Changing the
URL also fails because the HTTP arguments no longer match the fixture.

Tests run in memory and create no saved run or jobs. The recipe remains a
normal version 4 recipe; ordinary `workflow run` executes its real operations.
For fixture fields and capture commands, see the
[reference](../../../docs/reference/recipes.md#fixture-tests).
