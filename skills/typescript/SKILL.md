---
name: typescript
description: TypeScript conventions for this agent. Read before writing or editing .ts/.tsx files.
---

# TypeScript

1. Locate and read package.json for actual typecheck/lint/test scripts; do not
   invent command names. Run relevant checks with project configuration and
   installed dependencies. Never download a compiler implicitly through npx;
   report missing tools. Use run_shell for shell/package scripts, run_program
   for native executables with literal args.
2. Fix introduced regressions; identify pre-existing failures and avoid unrelated
   cleanup. Report actual validation.
3. Follow compiler/framework/export conventions. Prefer unknown plus narrowing
   over any, and functions/objects over classes unless state/behavior belong
   together. Use explicit return types when they clarify exported contracts.
4. Use patch_file for small edits, write_file for new files or full replacements.
