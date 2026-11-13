# Durable deadline wait

Run from the repository root:

```text
gektor workflow run -workspace . examples/workflows/durable-wait/recipe.json
```

The call returns `waiting` with a run ID and a `resume_at` deadline after writing
one artifact. It does not keep a process sleeping for the workflow. In another
terminal, start the worker with permission to run the workflow and write artifacts:

```text
gektor workflow serve -workspace .
```

Inspect the original run ID to see completion. The final declared outputs contain
two artifact references, `before` and `after`. The first operation is preserved;
continuation does not write it again. No model or network service is used.

If the worker is stopped before the deadline, restart it to continue the waiting
run. A run interrupted during active execution is not automatically replayed.
The worker uses its own current permission rules. An overdue deadline does not
bypass those rules or turn failed authorization into a retry loop.
