---
name: code-navigation
description: Locate declarations, trace callers and imports, and assess symbol usages in Go, TypeScript/JavaScript and Python using existing file tools and small scripts. Read when tracing behavior or planning a refactor across files.
---

# Navigate code with existing tools

1. Use `list_files` to locate the relevant package and its configuration:
   `go.mod`/`go.work`, `package.json`/`tsconfig.json`, or `pyproject.toml`.
   In a monorepo, identify the target subproject before searching everything.
2. Use `grep_files` with a narrow path, symbol name and filename glob.
   Find declaration candidates, then `read_file` the relevant code and imports.
   If output is capped, narrow the search; do not interpret a partial result
   as a complete list of usages.
3. Trace the actual entrypoint, callers and state ownership. Follow import
   aliases, re-exports, receiver types and registration code as needed.
   Prefer a few relevant reads over dumping whole directories into context.
4. Use `run_shell` for installed language tools or a small read-only script
   only when ordinary search cannot answer the question reliably. Python is
   useful for joining search results or parsing Python, not mandatory for
   every task. Do not build an index or start a language server for this.
5. Report concrete `path:line` locations and distinguish text matches,
   syntax declarations and resolved symbol references. State unresolved
   ambiguity instead of claiming all callers were found.

## Searches

These are `grep_files` arguments, not shell commands. Its patterns use Go
regular expressions: no lookbehind or backreferences.

```json
{"path":"internal","pattern":"\\bLoadState\\b","glob":"*.go"}
```

```json
{"path":"src","pattern":"\\bexport\\b.*\\bloadSession\\b","glob":"*.ts"}
```

The export pattern only finds candidates; multiline declarations and
re-exports need follow-up inspection. Search `.tsx`, `.js` or `.jsx` separately
when the project uses them. Do not assume a tool's glob supports brace syntax.

Use `read_file` with its 1-based `offset` and an appropriate `max_bytes` to
inspect a match in context. Follow up on a truncated read only as needed.

## Language-specific precision

- **Go:** check the package, imports and receiver type. Same-name methods on
  different types are different symbols. Interface calls, build tags and
  generated code can hide relevant usages from a simple name search.
  A small helper using standard `go/parser` can list syntax declarations;
  it does not by itself resolve calls or interface implementations.
- **TypeScript/JavaScript:** inspect aliases, barrel exports and `tsconfig`
  path mappings. If the project already has `typescript`, a Node script may
  use its compiler API and the project's configuration. Regex or a Python
  text scan is not a TypeScript type checker.
- **Python:** use standard `ast.parse` for definitions, imports and call
  syntax when useful. Read sources without importing or executing the project.
  AST alone cannot resolve dynamic attributes, monkey patches or all imports.

## Scripts and validation

- Use the current platform's shell: TARS uses `cmd.exe` on Windows and `sh`
  elsewhere. Do not send Bash heredocs to Windows `run_shell`. If quoting is
  fragile, `write_file` a short temporary script, run it, then remove only
  that script. Keep its output bounded and include source paths and lines.
- Reuse installed tools and project dependencies. Do not install packages
  or download tooling just to improve navigation without authorization.
- Plan has no shell or write tools. Use `list_files`, `grep_files` and
  `read_file` there; report any remaining ambiguity without bypassing the role.
- For a refactor, search the old name again and run the relevant existing
  build/typecheck/tests. A text search is not proof of a safe rename, and a
  passing build does not prove that every runtime caller was covered.
