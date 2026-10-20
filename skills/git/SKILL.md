---
name: git
description: Inspect Git history, blame and diffs, or make requested commits with run_shell.
---

# Git via run_shell

Run git from the workspace root. Do not pass .git as a log path: it is
repository metadata, not the path whose history you are inspecting.

1. Start with a bounded command matching the question, such as
   git log --oneline -20 or git log --format="%h %an %ar %s" -20.
2. Narrow by path, revision or date when needed; expand the history only when
   the task calls for it. Truncated output is not a complete result.
3. Inspect status and the relevant diff before changing Git state. Preserve
   unrelated work and stage only the intended files for a requested commit.
4. If a command fails, read stderr and correct the actual cause. Do not repeat
   the same failed command with cosmetic changes; report a blocker when stuck.

Use list_files for directory contents and grep_files for source searches.
Use Git's own file/index/history commands when those semantics are needed.
Report observed results; do not claim a commit or push succeeded without
checking its command result. Use quoting appropriate to the current shell.
