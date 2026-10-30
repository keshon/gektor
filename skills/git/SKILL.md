---
name: git
description: Inspect Git changes and history, stage requested paths and make authorized commits.
---

# Git

1. Prefer dedicated Git tools with literal workspace-relative paths; .git is
   not a history path. Inspect status and diff before mutations; staged=true
   inspects the index. Use run_shell only for unsupported operations with the
   current platform's quoting.
2. log caps at 50 commits; narrow by path/ref as needed. show requires ref;
   blame requires path. Truncated output is incomplete evidence.
3. git_add only intended, authorized paths. Preserve unrelated work.
4. Commit only with user authorization. Inspect the entire staged diff:
   git_commit includes pre-existing staged changes. Resolve unrelated staged
   scope before committing; never silently include or unstage it.
5. Use a concise imperative subject and repository conventions; explain reasons
   or tradeoffs in the body when useful. git_commit adds
   `Co-authored-by: Gektor <no-reply@keshon.ru>`; preserve the user's Git author
   configuration and hooks.
6. Diagnose actual failures; avoid cosmetic retries. Claim commit/push success
   only after successful results. Plan/read-only workers have inspection tools,
   not git_add/git_commit.
