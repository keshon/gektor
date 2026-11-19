# Join users and roles

Run from this folder:

```powershell
gektor workflow test -workspace . -fixture fixture.json -yes recipe.json
```

The test executes the join without a model or network. Ada gets the editor role;
Lin has no matching role, so the right side is null. Output keeps the users' order.
Change the mode to inner and the fixture fails because Lin's row is omitted.
Full mode also appends the unmatched reader role with a null left side.

Duplicate keys produce all matching pairs. String and integer keys are distinct;
null, missing, decimal and exponent keys fail. Inputs are bounded by the existing
64 KiB argument limit, and output is limited to 10,000 pairs and 1 MiB.
See the [join reference](../../../docs/reference/recipes.md#keyed-joins).
