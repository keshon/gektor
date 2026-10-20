---
name: large-build
description: Build a large feature in working increments and decide when a fresh subagent context is useful.
---

# Large builds

1. Identify the existing structure and the smallest useful increment.
2. Implement and check that increment before expanding it. A skeleton can help
   for a new module, but do not replace working code with placeholders.
3. Keep generated tool arguments small enough to complete within the response
   budget. Use focused patches; split a large new file into smaller writes
   when needed. Check tool results before proceeding.
4. Track unfinished requirements with todo when helpful. A phase is not the
   end of the task: continue until the requested work is done or blocked.

Use delegate_task only when it is available and a self-contained subtask
benefits from fresh context enough to justify another model run. Include its
requirements, relevant paths, and expected checks. Small tasks and tightly
coupled edits usually belong in the parent context.

Delegates share the workspace and run sequentially; delegation is context
isolation, not parallel execution. Keep ownership of overlapping edits in one
place. Read the returned result, verify important claims as needed, and integrate
the work. Explicit user constraints take precedence over these heuristics.
