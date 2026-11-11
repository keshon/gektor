# Working on permissions

`permission` owns tool/resource policy evaluation, operator answers, and optional
gate logging. Callers supply the tool identity, actual resources, and gate.
This package imports no other internal package.

## Reading path

- `permission.go`: effects, ordered rules, shipping defaults, pattern matching,
  `Authorize`, and operator-answer decoding with `Decide`.
- `log.go`: `LogGate` decorates an operator gate; `GateRecord` defines its JSONL
  record. CLI, RPC, and TUI callers install the decorator at their gate boundary.

## Contracts to preserve

- The last matching rule wins, regardless of effect. A zero policy and an
  unmatched call allow execution; callers explicitly choose shipping defaults.
- `Authorize` validates rules, evaluates policy, and calls the gate only for
  `Ask`. A missing gate, a gate error, or a final effect other than `Allow` blocks.
- Match resources after resolving the operation's actual arguments. Permission
  matching is separate from rooted filesystem access and process isolation.
- `Decide` shares operator-answer semantics across frontends. Preserve denial
  notes and context/error propagation when adapting gates.
- `LogGate` records operator answers only, not automatic policy decisions.
  Empty paths disable logging. Logging failures never replace the gate's effect
  or error; preserve the existing JSON field names when changing Go types.

## Tests

Policy, pattern, and answer tests live in `permission_test.go` and `glob_test.go`.
`log_test.go` checks actual JSONL output, disabled logging, failure handling, and
unchanged context/results. Keep caller integration tests with their frontends.

From the repository root, run `go test ./internal/permission`.
Follow the [Readable Go skill](../../.agents/skills/go-readable/SKILL.md).
