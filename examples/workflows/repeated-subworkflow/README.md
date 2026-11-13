# Repeated subworkflow

Run from the repository root:

```text
gektor workflow run -workspace . examples/workflows/repeated-subworkflow/recipe.json
```

This model-free fixture produces three numbered pages. Each child writes one
artifact, then returns a boolean `done` and the next complete input object.
There are no network calls. A real pagination child would obtain those fields
from its source instead.

The final declared `last` output has `done: true`, `next.cursor: 3` and the third
artifact. Inspection retains all three child scopes and their artifacts under
IDs such as `pages[0]/save`. To produce five pages, save `{"count":5}` in a workspace file and pass its path
using `-inputs inputs.json` before the recipe path. The fixture accepts
counts 1–10 and the repeat itself is capped at ten iterations.

Completed iterations survive a later review pause. A process interrupted during
an external operation is not automatically replayed. `delay_ms`, when supplied,
is a short cancellable in-process delay, not a durable wait.
