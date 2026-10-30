---
name: go
description: Go conventions for this agent. Read before writing or editing .go files.
---

# Go

1. Format changed Go files only; preserve unrelated edits and line endings.
2. Run relevant tests, build and vet. Across package boundaries run
   `go test ./...`, `go build ./...`, `go vet ./...`.
3. Cover changed behavior/bugs with meaningful regressions, not implementation
   mirrors. Report actual checks; distinguish pre-existing failures and avoid
   unrelated fixes merely to get green.
4. Follow project conventions. Group code by responsibility without giant files
   or needless fragmentation. Prefer plain Go, explicit dependencies and direct
   control flow; use generics only for useful duplication. Avoid unnecessary
   mutable globals. Wrap propagated errors with fmt.Errorf and %w.
5. Use patch_file for focused edits; write_file for new files or intentional
   full replacements. Preserve unrelated user changes.
