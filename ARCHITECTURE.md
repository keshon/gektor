# Architecture

TARS uses one coding-agent loop. Plan explores with read-only tools; Act
edits files, runs commands and delegates bounded subtasks. Print, JSON, TUI
and stdio RPC use the same loop.

## Composition

cmd/agent resolves flags, preferences and provider profiles. Each launch owns
its backend client, MCP connections and background-process registry.
internal/app compiles identity and profile settings into Services.
internal/roles builds Planner, Interactive and Subagent from Env. Subagents
share the workspace and permission policy, have 12 steps and cannot delegate
again or ask the operator. Their results return through the parent tool call.

internal/run provides Session events and operator gates for TUI and RPC.
The print CLI supplies console observers to the same role constructors.
internal/llm owns provider dialects, streaming, transport retries and usage.
internal/tools supplies tools; internal/workspace resolves paths and measures
file changes; internal/permission authorizes each tool call.

## Execution and limits

Each step requests one model response. Tool calls execute and their results
append to history; a complete text response ends the run. Adjacent concurrent
tools may overlap. Exclusive tools are barriers in model call order. Results
retain that order, and errors preserve the command output alongside the error.

The operator sets the step limit, default 25. Todo is checklist data: it cannot
grant steps or prevent an answer. On exhaustion, an operator may explicitly
approve one 25-step continuation. There is no additional closing model call.
Repeated reads, failed commands and whole-file replacements remain legal.

Recovery handles incomplete generations, malformed leaked calls, empty
responses and context overflow. Well-formed leaked calls use the normal
tool/permission path. An empty response gets one retry; overflow gets one
compact-and-retry. Permission, cancellation and tool timeouts remain enforced.

## Context and persistence

Ordinary steps append to history, preserving the backend prompt prefix.
Compaction retains the system prompt, initial task, recent complete step groups
and a bounded extractive summary. A cooldown prevents compaction on every step
when the retained working set remains large. Explicit compaction and overflow
recovery use the same history transformation.

Snapshots write atomically after responses and tool execution. Resume restores
history and refreshes workspace instructions. Fork copies history into a fresh
task directory; it does not create a Git checkout.

## Effects and verification

The workspace is inventoried at start and compared once at end, including failed
and cancelled runs. The final scan has an independent 30-second limit so
cancellation does not erase evidence of prior edits. It captures shell, MCP and
delegate effects without scanning every step. Unchanged file metadata reuses
the previous digest.

An optional verify command runs once after a completed model answer. Its real
output and error determine the recorded verdict, without another model call.
The final workspace comparison follows verification, including files generated
by that command. internal/checks collects report-only findings from file edits
and a final pass over changed paths; internal/rules supplies language checks.

## UI and settings

The Bubble Tea update loop owns UI state. Components own local interactions;
the root routes focus, messages and effects. Workers return messages rather
than changing the live model.

Applying settings returns ModeChange with saved preferences, history, draft
and session directory. The CLI composes a fresh launch from structured options;
execution policy survives the handoff. Provider profiles decode strictly.
Preferences accept the two retired harness advice fields on load and omit them
on the next save.

## Evaluation and boundaries

cmd/eval runs 15 scenarios in disposable seed workspaces. Frozen criteria check
workspace contents, tool usage and answers independently of the model checklist.

File tools validate paths and edits. Shell and MCP run with the user's process
permissions; the workspace is not a process sandbox. See [SECURITY.md](SECURITY.md).
Usage and flags live in [README.md](README.md) and [docs/cli.md](docs/cli.md).
House rules are enforced by tests described in [docs/conventions.md](docs/conventions.md).
