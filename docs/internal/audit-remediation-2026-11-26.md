# November 26 audit remediation

Follow-up to the [audit](project-audit-2026-11-26.md). The user approved all five
fixes and stale documentation cleanup, including retiring the FigCraft project.

Automatic code reformatting was removed rather than replaced with a custom
formatter. Markdown parsing and syntax highlighting remain. Fences preserve
source text, including invalid syntax; terminal wrapping and tab presentation
remain display operations. Formatter bundles, WASM warm-up and formatter-only
dependencies/license entries were removed. Goja remains for workflow scripts.
Single-row tool badges and their inspection targets are unchanged.

The same local four-fence JavaScript probe measured 99.4 and 96.7 ms after removal,
versus 897.7 and 903.8 ms before. Ten tiny fences measured 1.1 ms versus 82.5 ms.
These measurements isolate the chosen fixture, not a general latency guarantee.
Parsing and highlighting still take time; unchanged live fences may benefit from
caching if future measurements justify it. Invalid Rust no longer gains invented
semicolons in the view.

Answered resume now closes pending tool calls before adding supplied user notes
and images. Checklist recovery uses accepted execution evidence from the original
archive instead of replaying the latest attempted update from working context.
Regression coverage includes failed, cancelled, withheld and unanswered updates,
reused call IDs, older successful records and compacted archives.

The separate-consumer check now includes non-Go package inputs in its snapshot
and fingerprint, with embedded-asset regression coverage and a CI gate.

Documentation now describes upfront titles, mechanical compaction fallback,
saved-key authentication, incomplete outcomes, structured verification receipts,
and checks that include public controls. The obsolete FigCraft project, mockup
exports and update scripts were retired. Current gallery files/exporter and the
independent FigCraft runtime skill remain.

Validation passed:

- Full `go test -p 1 ./...`, `go vet ./...` and executable build.
- Focused race tests for agent, harness, Agent TUI and Markdown.
- All three original recovery probes, now passing.
- External-consumer tests/vet/build and embedded-input checker regressions.
- Documentation conventions, website checks/build and 45 dry eval definitions.
- Existing packed-tool-row wrapping, inspection and hit-target tests.

An early integrated vet attempt caught a test caller still using the old answered
resume signature. That caller was updated before the final passing vet and full
suite. No live model or native terminal acceptance was performed. The executable
was built at `bin/gektor.exe`.
