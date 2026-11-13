# Working on workflows

This tree implements saved recipes, their execution, and durable records.
The terminal editor is a separate consumer in `internal/tui/workflow`.

## Ownership and reading path

- The root `workflow` package defines recipes, runs, nodes, registry contracts,
  schemas, and shared values. Start with `workflow.go` for recipe definitions and
  the handler contract, then `node.go` and `registry.go` for executable nodes.
  `run.go` defines execution records and attempts.
- `service` coordinates authorized workspace reads, frozen recipe composition,
  validation/execution, queue execution, and reviews. CLI, API, Agent tools, and
  TUI call it; it does not construct application clients or operator gates.
  Follow `runs.go`, `composition.go`, then the service operation being changed.
  `service/schema.go` declares recipe fields and limits using named input,
  retry, step, and root schemas; registered node names supply the tool enum.
- `recipe` prepares graphs, binds inputs, and resolves references.
- `engine` executes prepared definitions through a handler and checkpoint
  callback. It does not own application authorization or storage construction.
- `store` owns workspace-scoped records, artifacts, queue claims, schedules,
  and file locks. Callers close store handles; storage never executes nodes.
- `daemon` owns the worker lease, scheduling, and job claiming. It executes jobs
  through `service` and stops work before releasing its lease.
- `nodes` owns individual operation definitions. `script`, `records`, and
  `credentials` provide the corresponding execution/data support.
- `agenttools` owns Agent tool names, descriptions, argument schemas, decoding,
  and adaptation to the service. `api` owns HTTP transport and submission.

Application wiring in `internal/app/workflow.go` assembles concrete nodes and
authorization. Keep shared execution decisions out of the adapters, and keep
tool presentation out of the service. Recipe schemas are domain contracts and
remain available independently of any adapter.

## Contracts to preserve

- Submission freezes recipe dependencies before worker execution. Queued work
  must not silently pick up later edits to a recipe or its nested dependencies.
- Validate structure and known arguments before effects. Resolve output-dependent
  arguments before authorizing their actual resources; placeholders are not
  permission grants. Preserve node versions and exact argument binding.
- Checkpoint before effects. Uncertain operations are not automatically replayed;
  retries require the node's retry-safety contract. Resume is an explicit review
  operation, not a general recovery shortcut.
- Preserve cancellation, final persistence, and queue failure distinctions.
  An operation failure recorded on a job differs from failure to persist it.
- Store records retain full execution evidence. Compact caller summaries must
  not replace those records or discard declared outputs.
- Collection workers never mutate the run. The coordinator acknowledges durable
  checkpoints and joins all workers before returning. Concurrency requires the
  operation's explicit declaration; read-only and retry safety do not imply it.
- Only `ExecutionError` is collectable/retryable operation failure. Permission,
  validation, output-contract and persistence errors remain terminal.

## Tests and verification

Keep node, engine, store, service, and adapter tests with their owners. Test
authorization and composition through real registered nodes and temporary
workspaces; keep subprocess tests for script execution.

From the repository root:

```sh
go test ./internal/workflow/...
go test -race ./internal/workflow/...
go test ./cmd/... ./internal/...
```

For readability changes, follow the
[Go architecture skill](../../.agents/skills/go-architecture/SKILL.md).
