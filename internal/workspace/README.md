# Working on workspace access

`workspace.Workspace` owns an open `os.Root`, project instructions, skill
discovery, and file inventory. Construct it with `New` or `NewWithSkillRoots`
and close it when its caller's work ends.

## Reading path

- `workspace.go`: construction and cleanup on construction failure.
- `access.go`: rooted file operations and path conversion.
- `instructions.go`: ancestor/root instructions, scoped descendant instructions,
  and the instruction sections supplied to a task.
- `skills.go`: discovery, routing metadata, lazy guide/resource loading, and
  library precedence. `builtin.go` supplies embedded skills.
- `files.go` and `changes.go`: Git-aware inventory and observation of changes.

Keep these responsibilities in this package while they share the rooted access
and project-context contract. Feature consumers decide what to do with the files
and instructions; this package does not run tools or apply permission policy.

## Contracts to preserve

- Use rooted methods for actual workspace reads and writes. `Resolve` supplies
  an absolute path for display or subprocesses; it does not establish containment
  for a later ordinary `os.Open` call. `os.Root` enforces containment when the
  operation happens, including symlinks and junctions.
- This is a file boundary. Shell, Git, and external processes retain the user's
  permissions; workspace access does not isolate processes or networking.
- Construction loads ancestor `AGENTS.md` files before the root file. Scoped
  instructions load only the target's descendant directory chain and label its
  scope. Keep instruction size limits and ordering when changing discovery.
- Skill libraries are supplied lowest priority first; later libraries and project
  roots override earlier entries. Guides and resources stay lazy: discovery
  supplies routing metadata, not every guide's body. Preserve bounded reads and
  partial discovery results with warnings.
- Change observation measures actual file effects. Its final scan can outlive
  run cancellation, with its own timeout. Preserve Git ignore/tracked-file
  behavior and the exclusion of application state directories.

## Tests

Tests live beside their responsibility: instructions, skills, rooted access,
inventory, and changes. Use real temporary roots for filesystem contracts.
Symlink/junction tests must retain their platform-specific limitations.

From the repository root, run `go test ./internal/workspace`; use
`go test -race ./internal/workspace` when changing shared state or lifecycle.
Apply the [Readable Go skill](../../.agents/skills/go-readable/SKILL.md).
