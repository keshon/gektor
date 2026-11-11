# Working on run sessions

`harness` runs prepared task roles for CLI, TUI, and RPC. It owns run/resume
coordination and reporting; callers own the workspace, model client, frontend,
and operator transport.

## Reading path

- `session.go`: validate a launch, select a role, run or resume, then report the
  measured outcome. `Session` owns its inbox and question budget.
- `observer.go`: translate low-level Agent observations into existing events,
  attribute nudge replies, and invoke launch observers after event delivery.
- `diagnostics.go`: open a metadata-only run log, coalesce progress, rotate it,
  and restore the frontend sink when the run ends.
- Gate and suspension files: operator questions and permissions; keep their
  execution decisions separate from observation.

## Contracts

`Config.Events` borrows an `events.Sink`. Delivery is synchronous and ordered.
A sink must not call back into its publishing emitter or mutate published nested
payloads. CLI/RPC emit directly; the TUI uses its existing cancellable queue.
No additional event-delivery goroutine is needed.

Transient tool output precedes execution-finished and recorded-result events.
Child envelopes retain the parent call ID and never become parent usage or
transcript evidence. `sessionObserver` owns nudge-reply state for one run.

Diagnostics failures are best effort. The run owns `diagnosticLog.Close`, called
after publishers have stopped; closing restores the original frontend sink.
Keep sensitive text and arguments out of the metadata log.

Run `go test ./internal/harness` and use `-race` for delivery or lifetime changes.
API changes also require CLI, TUI, RPC, roles, and event tests. Follow the updated
[Readable Go skill](../../.agents/skills/go-readable-keshon/SKILL.md).
