# Architecture

GEKTOR uses one coding-agent loop. Plan explores without changing anything; Act
edits files, runs commands and delegates bounded subtasks. Print, JSON, TUI
and stdio RPC use the same loop.

## Composition

cmd/gektor resolves flags, preferences and provider profiles. Each launch owns
its backend client, MCP connections and background-process registry.
CLI and first-run TUI settings use internal/config.DefaultProfile: llama.cpp
at http://127.0.0.1:8080. The eval runner uses the same provider defaults.
internal/app compiles identity and profile settings into Services.
internal/roles builds Planner, Interactive and Subagent from Env. Subagents
share the workspace and permission policy, have 12 steps and cannot delegate
again or ask the operator. Their results return through the parent tool call.

internal/run provides Session events and operator gates for TUI and RPC.
The print CLI supplies console observers to the same role constructors.
internal/llm owns provider dialects, streaming, transport retries and usage.
internal/tools supplies tools; internal/workspace resolves paths and measures
file changes; internal/permission authorizes each tool call.

The model server is external. GEKTOR does not install or supervise it.
API-compatible providers share the OpenAI transport; local dialects handle
server-specific grammar, sampling, capability discovery and telemetry.

## Execution and limits

Each step requests one model response. Tool calls execute and their results
append to history; a complete text response ends the run. Adjacent concurrent
tools may overlap. Exclusive tools are barriers in model call order. Results
retain that order, and errors preserve the command output alongside the error.

Arguments are validated before execution. Invalid or truncated JSON is retained
as text in the archive and explained to the model through a harness message;
it is not replaced with an invented empty call. Tool results carry failure
metadata separately from their text. SYSTEM displays model-facing harness
messages; local UI notices use STATUS.

Native process tools pass an executable and literal arguments directly.
Shell tools use pwsh.exe, then powershell.exe on Windows, or sh elsewhere.
Windows shell source travels through UTF-8 stdin with profiles disabled.
The prompt names the selected shell. Shell commands retain that shell's error
semantics; native tools report the process exit code without shell parsing.

The operator sets the step limit, default 25; explicit max-steps 0 is unlimited
for the main agent. Todo is checklist data: it cannot
grant steps or prevent an answer. On exhaustion, an operator may explicitly
approve one 25-step continuation. There is no additional closing model call.

Repeat detection compares tool calls and their results. Three consecutive
identical step signatures trigger one answer turn without tools. Potentially
mutating calls reset repeat tracking; the step limit still bounds such runs.

Recovery handles incomplete generations, malformed leaked calls, empty
responses and context overflow. Well-formed leaked calls use the normal
tool/permission path. An empty response gets one retry; overflow gets one
compact-and-retry. Permission, cancellation and tool timeouts remain enforced.

Follow-up user messages enter an inbox while the agent is running. The loop
accepts them at turn boundaries, preserving tool-call/result ordering; it does
not run a second model request concurrently for the same conversation.

## Context and persistence

Ordinary steps append to history, preserving the backend prompt prefix.
The saved snapshot separates the full archive from the model's working
projection. Automatic compaction makes a bounded LLM checkpoint, retains user
instructions verbatim and keeps recent complete step groups. An invalid
checkpoint leaves the previous context intact. Explicit /compact uses the
extractive transformation without a model call. A cooldown limits repeated
automatic compaction; overflow recovery may compact immediately.

Switching between plan and act appends a stance turn rather than rewriting the
system prompt. The roles share the same tool definitions and enforce read-only
access at execution. This preserves the prefix for backend cache reuse; actual
cache hits depend on the server and its slot state.

Snapshots write atomically after responses and tool execution. Resume restores
history and composes the current launch's instructions. Fork copies history into a fresh
task directory; it does not create a Git checkout.

## Optional browser

The browser flag exposes only enable_browser initially. Its first successful
call checks dependencies, prepares the pinned Playwright MCP package and connects
the transport. Instructions arrive as the tool result, and the registry adds
the actual MCP tools. Tool names cannot replace existing registry entries.

The transport belongs to the launch; activation belongs to each agent's registry.
New sessions start without browser schemas. Snapshots preserve activated tool
catalogs independently of compaction. Resume restores schemas before the first
model request and reconnects only on actual use, checking the configuration and
catalog fingerprint before execution. Older archives migrate from successful
activation results. The expanded catalog stays present across turns and Plan/Act
switches. Activation can invalidate the backend's prompt cache once.

Explicit -mcp servers still connect at launch. Configured capability packs share
the browser's deferred activation mechanism and support native Go, executable
JSON and MCP adapters. Environment details and skill metadata are queried through
system_info; load_skill reads guides and supporting resources from configured
libraries or project skill directories. See [Extensions](docs/extensions.md).

## Effects and verification

The tracked/unignored workspace file set is inventoried at start and compared
once at end, including failed and cancelled runs. The final scan has an
independent 30-second limit so
cancellation does not erase evidence of prior edits. It captures shell, MCP and
delegate effects without scanning every step. Unchanged file metadata reuses
the previous digest. This records observed file changes, not backups or rollback.

An optional verify command runs once after a completed model answer. Its real
output and error determine the recorded verdict, without another model call.
The final workspace comparison follows verification, including files generated
by that command. internal/checks collects report-only findings from file edits
and a final pass over changed paths; internal/rules supplies language checks.

## UI and settings

Harness instructions live in internal/prompts/text and are embedded at build
time. Interactive and Planner share the same base prompt and tool definitions;
their current Plan/Act stance is a trailing harness message. Subagents reuse
the base with an explicit task scope and budget. Provider adapters own wire
formatting; selecting a backend does not select a different behavioral prompt.
Profile identity and response presets are composed separately from capabilities.

The Bubble Tea update loop owns UI state. Components own local interactions;
the root routes focus, messages and effects. Workers return messages rather
than changing the live model.

Applying settings returns ModeChange with saved preferences, history, draft
and session directory. The CLI composes a fresh launch from structured options;
execution policy survives the handoff. Provider profiles decode strictly.

## Evaluation and boundaries

cmd/eval runs selected probes in disposable seed workspaces. Its core suite is
a fixed seven-scenario baseline. Frozen criteria check workspace contents,
tool usage and answers independently of the model checklist.

The experimental TUI /eval command is a separate retrospective, implemented in
internal/retrospect. Session snapshots retain execution ranges alongside the full
archive; compaction changes only the working projection. The Retrospective role
has archive-reading and structured-submission tools, without task capabilities.
Validated reports can append quoted historical observations to a workspace skill.
Literal evidence and read coverage are checked mechanically; the model's judgment
and the general usefulness of a lesson are not independently verified.

File tools validate paths and edits. Shell and MCP run with the user's process
permissions; the workspace is not a process sandbox. See [SECURITY.md](SECURITY.md).
Usage and flags live in [README.md](README.md) and [docs/cli.md](docs/cli.md).
House rules are enforced by tests described in [docs/conventions.md](docs/conventions.md).
