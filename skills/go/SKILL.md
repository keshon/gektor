---
name: go
description: Go conventions for this agent. Read before writing or editing .go files.
---

# Go

## Validation
1. Format only the changed Go files with gofmt. Do not rewrite the whole
   workspace to normalize unrelated files or line endings.
2. Run the relevant package tests, build and vet checks. For changes across
   package boundaries, run go test ./..., go build ./... and go vet ./....
3. Add a regression test when behavior changes or a bug needs coverage; avoid
   tests that merely repeat a trivial implementation.
4. Report commands actually run and their results. Distinguish failures caused
   by this change from existing failures; do not fix unrelated code to get green.

## Style
- Follow the project's conventions. Organize packages and files around
  responsibilities; avoid both giant files and needless fragmentation.
- Prefer meaningful names and direct control flow over generic framework layers.
- Wrap propagated errors with context using fmt.Errorf("...: %w", err).
- Prefer explicit dependencies. Introduce generics when they remove useful
  duplication, rather than replacing straightforward concrete code.
- Avoid mutable global state when the dependency can be passed explicitly.

## Editing
- Use patch_file for a focused edit; write_file for a new file
  or an intentional full replacement. Preserve unrelated user changes.
