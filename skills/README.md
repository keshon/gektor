# skills/

Lean, checklist-style guides for THIS agent — not general documentation
copied from elsewhere. Written for a small local model: short, directive,
no prose padding, no theory.

Convention: `skills/<name>/SKILL.md`. The workspace adds a bounded catalog of paths and descriptions to the initial
context. Full guides are loaded through visible `read_file` calls only when
relevant; the agent should name the skill it applies. No special tool is needed. Skills must exist
in the task workspace: launching GEKTOR from this repository does not install
these guides into a different workspace. Copy the relevant skills there if
you want the agent to use them. Project/ancestor AGENTS.md instructions load
automatically; SKILL.md is read by the model when relevant.

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
