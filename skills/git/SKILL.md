---
name: git
description: Inspect Git changes and history, stage requested paths and make authorized commits.
---

# Git

Prefer the dedicated Git tools when available. They pass literal arguments to
Git without a shell. Paths are relative to the workspace; do not pass `.git`
as a history path. Use `run_shell` for operations the tools do not expose,
with quoting appropriate to the current operating system.

1. Inspect `git` with `subcommand: "status"` and `subcommand: "diff"` before
   changing Git state. `staged: true` selects the staged diff.
2. For history, use `subcommand: "log"` with `max_items` up to 50, optionally
   narrowed by `path` or a commit `ref`. `show` requires a commit `ref`;
   `blame` requires a file `path`. Truncated output is not a complete result.
3. Stage only the intended files with `git_add`. Preserve unrelated work.
4. Commit only when the user authorizes it. Inspect the staged diff first:
   `git_commit` records everything already staged, including pre-existing
   staged changes. If unrelated changes are staged, resolve the scope before
   committing rather than silently including or unstaging them.
5. Write a concise imperative subject describing the resulting behavior.
   Follow the repository's commit conventions; add a body when the reason or
   tradeoff needs explanation. `git_commit` adds
   `Co-authored-by: Gektor <no-reply@keshon.ru>` and preserves Git's author
   configuration. Do not replace the user's identity with the agent's.

If a command fails, read its diagnostics and address the actual cause. Do not
repeat a failed command with cosmetic changes. Report observed results;
do not claim a commit or push succeeded without a successful command result.
The Git inspection tool remains read-only; `git_add` and `git_commit` are not
available for execution in Plan or read-only workers.
