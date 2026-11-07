# Reduce coding-session friction

Status: proposed; not implemented. Based on the session-workspace feature task.

## Observed problems

- Sequential delegation of one tightly coupled change required repeated handoffs.
  One delegate exhausted its step budget after editing files and returned no
  useful completion report. The parent recovered changes through Git inspection.
- Partial delegation reports left integration, old fixtures, and documentation
  unfinished. Passing checks before subsequent edits were not final validation.
- A workspace error combined a low-level error and recovery instructions into a
  single clipped TUI line. The short replacement was made after a real screenshot.
- The static Figcraft review did not exercise the running terminal interface.

These observations do not establish a compaction defect. Do not redesign memory
or add a Project abstraction as part of this work.

## 1. Preserve a useful report when delegation exhausts its budget

Trace delegate_task execution and budget-exhaustion handling first. Return a
harness-generated envelope on every exit: termination reason, tools executed,
workspace-relative paths reported as mutated, and references to captured check
results. Clearly distinguish recorded evidence from a model's summary. Do not
infer changed paths from prose or claim tests passed from their invocation alone.
If a model summary is available, include it separately; do not require another
model call to produce the mechanical envelope.

Acceptance: tests cover normal completion, step exhaustion after a mutation,
tool failure, and cancellation. The parent receives useful evidence on each
path without silently continuing the delegate or discarding partial work.

## 2. Make handoff state easier to inspect

Use that same envelope in the parent-facing tool result. Include unresolved work
only when supplied by the delegate, explicitly labelled as such. Keep output
bounded and link to the original records for longer diagnostics. Avoid inventing
a second task tracker or automatically delegating follow-up work.

Acceptance: an integration test can recover the edited paths and last check
result after exhaustion; a failed check cannot appear as successful completion.

## 3. Add regression coverage for session-list recovery UX

Exercise unassigned and unavailable workspace messages, reassignment confirm /
cancel, scope filtering, and reopening another workspace at normal and narrow
terminal sizes. Assert that essential recovery hints remain visible and rendered
rows fit. Keep detailed filesystem errors available where a detail view exists,
rather than appending them to the one-line recovery hint.

Acceptance: tests cover a long Windows path and a 40-column view, with no silent
workspace fallback. Manually inspect a running TUI as well as static mockups.

## 4. Establish a small terminal smoke-check procedure

Document a reproducible run against temporary session directories and a test
backend: create in workspace A, restart in B, open A's session, filter, reject a
missing directory, then explicitly reassign. Never use personal session storage
or send a real model request merely to test opening a saved conversation.
Investigate existing terminal test facilities before adding a new dependency.

Acceptance: record what was checked in the running application and what remains
covered only by unit tests or Figcraft. No browser check substitutes for TUI QA.

## Execution order and scope

Implement steps 1–2 together under one owner, then steps 3–4. Delegate only an
independent review or design task, not repeated completion of the same Go flow.
This last point is an agent workflow improvement, not evidence that the harness
needs a scheduler rewrite. Run relevant regressions and go test ./..., go build
./..., go vet ./... after code changes. Keep each increment separately reviewable.
