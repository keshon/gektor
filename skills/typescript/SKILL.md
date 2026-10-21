---
name: typescript
description: TypeScript conventions for this agent. Read before writing or editing .ts/.tsx files.
---

# TypeScript

## Before finishing any TS change
1. `list_files` to find `package.json`, then `read_file` it to find the
   real typecheck/lint/test scripts — don't guess command names.
2. Use `run_shell` for the existing typecheck script with the project
   configuration and installed dependencies; do not download a compiler
   implicitly through npx. Report missing tools rather than inventing scripts.
3. Run the configured linter and relevant tests. Fix regressions from the
   change; distinguish pre-existing failures and avoid unrelated cleanup.

## Style
- Follow the project's existing compiler settings and conventions. Prefer
  `unknown` + narrowing over introducing `any`.
- Use explicit return types where they clarify an exported contract.
- Prefer plain functions/objects over classes unless state and behavior
  genuinely belong together.
- Follow the framework and project's established export conventions.

## Editing
- Small fix: `patch_file`. New file or full rewrite:
  `write_file`.
