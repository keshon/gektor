# skills/

Lean, checklist-style guides for THIS agent — not general documentation
copied from elsewhere. Written for a small local model: short, directive,
no prose padding, no theory.

Convention: `skills/<name>/SKILL.md`. The workspace adds a bounded metadata
catalog to the initial context. Full guides load through visible `load_skill`
calls by ID; workspace guides also work with `read_file`. The agent should name
the skill it applies. User libraries and project directories are supported,
with project guides overriding the same ID. Launching from this repository
does not install its guides into another workspace. See
[skill roots and precedence](../docs/extensions.md#skill-libraries).
Project/ancestor AGENTS.md instructions load automatically.

Baseline tool discipline lives in the system prompt. Skills cover
task-specific workflows (git, dev servers, large builds, language quirks).

Rules for writing one:
- Lead with a checklist, not an essay.
- Name this agent's actual tools (`patch_file`, `move_file`, `run_shell`,
  `delegate_task`) — generic advice is dead weight here.
- Keep each guide focused. Move lengthy reference material out of the checklist
  when needed; do not split guides merely to meet a line count.

## Choose a guide

Read this catalog first, then only the SKILL.md relevant to the current task.
More than one guide can apply; do not load the whole directory.

- Trace behavior, locate declarations or assess usages across files:
  [code-navigation/SKILL.md](code-navigation/SKILL.md).
- Write or edit Go: [go/SKILL.md](go/SKILL.md).
- Write or edit TypeScript/TSX: [typescript/SKILL.md](typescript/SKILL.md).
- Inspect Git history, blame or diffs, or make commits:
  [git/SKILL.md](git/SKILL.md).
- Exercise a web UI, inspect browser errors or capture screenshots:
  [browser/SKILL.md](browser/SKILL.md).
- Start or test a local server or watcher:
  [dev-server/SKILL.md](dev-server/SKILL.md).
- Build a large feature incrementally or decide whether to delegate:
  [large-build/SKILL.md](large-build/SKILL.md).
- Inspect image assets, measure compression, resize, convert or compare:
  [pixelita/SKILL.md](pixelita/SKILL.md). Uses the external pixelita CLI;
  measurements and optimization work with text-only models.

Read-only roles cannot execute shell commands or write files; follow only
steps their available tools support.
