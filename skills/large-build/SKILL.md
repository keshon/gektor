---
name: large-build
description: Build features in verified increments; delegate only work worth a fresh context.
---

# Large builds

1. Identify the existing structure and smallest useful increment. Implement and
   check it before expanding; do not replace working code with placeholders.
2. Keep tool arguments within the response budget. Use focused patches or split
   large new files into smaller writes. Inspect results before continuing.
3. Track remaining requirements with todo when useful. Phases do not end the
   task: continue until complete or blocked.
4. Use delegate_task only when available and fresh context justifies its cost.
   Supply requirements, paths and expected checks. Keep small/coupled work local.
   Delegates share the workspace and run sequentially; this isolates context,
   not parallel execution. Keep overlapping edits under one owner, verify
   returned evidence and integrate the work. User constraints take precedence.
