# Harness work record

Completed on 2026-11-08. Current behavior is documented in the
[CLI reference](../cli.md), [architecture](../../ARCHITECTURE.md),
[delegate measurements](../../eval/prompts/delegation.md) and
[terminal smoke procedure](../tui-smoke.md).

## Why the work was needed

- Sequential delegation of one tightly coupled change required repeated handoffs.
  One delegate exhausted its step budget after editing files and returned no
  useful completion report. The parent recovered changes through Git inspection.
- Partial delegation reports left integration, old fixtures, and documentation
  unfinished. Passing checks before subsequent edits were not final validation.
- A workspace error combined a low-level error and recovery instructions into a
  single clipped TUI line. The short replacement was made after a real screenshot.
- The static Figcraft review did not exercise the running terminal interface.
- Every delegate receives 12 model steps, regardless of task size. A step is
  one model request, including the final answer. The session shows exhaustion;
  it does not establish a suitable default for future tasks.

These findings describe the starting behavior. Recovery work also
fixed unsent drafts lost across relaunches, reassignment confirmations following
the wrong row after refresh, and `/new task` reusing a reopened session's file.

These observations do not establish a compaction defect. Do not redesign memory
or add a Project abstraction as part of this work.

## Terminal validation

Validation on Windows: the running TUI was checked at 80 and 40 columns for
session navigation, destination readability and assignment cancellation. The
80-column check also covered cross-workspace opening, draft restoration,
current-workspace filtering, missing-root refusal and assignment confirmation.
Opening and recovery made zero mock model requests. Sending a new task at 40
columns produced the fake reply. Regression tests also protect the original
session when `/new task` follows a workspace relaunch.
