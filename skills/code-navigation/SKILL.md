---
name: code-navigation
description: Trace declarations, callers, imports and state ownership across Go, TypeScript/JavaScript and Python.
---

# Code navigation

1. Locate the subproject and its configuration with list_files: go.mod/go.work,
   package.json/tsconfig.json, or pyproject.toml. Narrow the scope first.
2. Search with grep_files using a path, symbol and filename glob; read relevant
   declarations/imports. Narrow capped results instead of treating them as complete.
3. Trace entrypoints, callers, state ownership, aliases, re-exports, receiver types
   and registration. Read selected files rather than dumping directories.
4. When text search is insufficient, use installed language tools or a small
   read-only script. No new index, language server or downloaded dependencies
   without authorization. Plan permits only its existing read-only tools.
5. Report path:line evidence and unresolved ambiguity. Distinguish text matches,
   syntax declarations and resolved references. For refactors, search the old name
   again and run existing build/typecheck/tests; passing builds do not cover every
   dynamic caller.

grep_files uses Go regex (no lookbehind/backreferences); do not assume brace globs.

```json
{"path":"internal","pattern":"\\bLoadState\\b","glob":"*.go"}
```

Read matches with read_file's 1-based offset and bounded max_bytes. Follow
truncated reads only as needed.

- Go: verify package, imports and receiver; same-name methods may differ.
  Interfaces, build tags and generated code need follow-up. go/parser supplies
  syntax, not type resolution.
- TS/JS: inspect aliases, barrel exports and tsconfig paths; search .tsx/.js/.jsx
  when used. Reuse an installed TypeScript compiler API for type-aware questions;
  regex/Python scans are not a type checker.
- Python: ast.parse can inspect definitions/imports/calls without executing the
  project; it cannot resolve every dynamic attribute or import.

Prefer run_program for native tools with literal args; run_shell uses PowerShell
on Windows and sh elsewhere. Avoid Bash heredocs on Windows. If quoting is
fragile, write a temporary script, run it, then remove only that script. Bound
its output and include source locations.
