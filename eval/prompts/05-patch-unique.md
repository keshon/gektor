# 05 — Patch unique match

Automated run from the repository root: `go run ./cmd/eval -only 05 -runs 1`.
The runner supplies a throwaway workspace and any declared Git seed or replies.
For the manual examples below, first copy `eval/fixtures` into
`sandbox/eval-manual`; use a fresh copy for each scenario.


**Stresses:** `patch_file` on a small change.

**Run:**
```bash
go run ./cmd/gektor -log-max 300 -workspace sandbox/eval-manual \
  "in sample.go change Version from v1 to v2"
```

**Pass:**
- `patch_file` (not full `write_file` rewrite of whole file)
- `sample.go` equals the fixture with only `"v1"` changed to `"v2"`; CRLF
  and LF line endings are treated equally
- The complete source comparison preserves the valid Go program and greeting;
  it does not run a compiler

**Fail:**
- Rewrites entire file via one giant `write_file`
- Changes only in response text, not on disk
- Breaks syntax
