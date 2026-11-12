# Architecture

GEKTOR uses one coding-agent loop. Plan explores without changing anything; Act
edits files, runs commands and delegates bounded subtasks. Print, JSON, TUI
and stdio RPC use the same loop.

## Package map

Paths below are relative to the repository root.

| Package | Responsibility |
|---|---|
| cmd/gektor | Resolve launch choices and own launch resources |
| internal/app | Construct backend and workflow services |
| internal/roles | Choose tools, budgets and policy for each agent role |
| internal/agent | Request model turns, order tools and recover from failures |
| internal/llm | Define messages and client contracts; subpackages handle protocols |
| internal/providers | Describe providers, inspect models and manage accounts |
| internal/harness | Expose agent execution and operator gates to frontends |
| internal/agentrpc | Frame stdio RPC requests and own per-request lifetime |
| internal/events | Serialize frontend events |
| internal/conversation | Save conversation history, model context and run records |
| internal/workspace and internal/permission | Resolve file access and authorize effects |
| internal/tools | Compose Agent tools and session capability discovery |
| internal/capabilities | Implement shared feature services and their tool/node contracts |
| internal/httptransport, internal/process and internal/executables | Own network policy, process lifetime and executable discovery |
| internal/workflow | Validate, execute, store and schedule workflow runs |
| internal/tui | Own screen state and connect controls to application actions |
| internal/tui/components | Render reusable controls and handle their local interaction |
| cmd/release and internal/release | Adapt packaging arguments and stage/verify release payloads |

## One agent turn

1. Accept pending user input and background notifications.
2. Prepare the model's context and tool definitions. Compact history if needed.
3. Request one response through the selected protocol adapter.
4. Save the response. Reject malformed or interrupted tool calls.
5. Validate and authorize calls, execute them in the required order, then attach
   their results. Continue with another model turn when needed.
6. Before returning an answer, check for newly arrived work. Save accepted work
   even if the step limit prevents another turn.
7. At run end, verify if configured, observe workspace changes and persist the
   outcome. Cancellation follows the same finalization path after initialization.

Start at `internal/agent/agent.go` for the Agent API. Execution begins in
`internal/agent/run.go`; setup and finalization live in
`internal/agent/run_lifecycle.go`. The run package exposes execution to callers;
the session package owns its saved data. They do not implement separate loops.

## Composition

cmd/gektor resolves flags, preferences and provider profiles. Each launch owns
its backend client, MCP connections and background-process registry.
One prepared run configuration carries identity, generation settings, checks,
permissions and role selection into CLI, TUI and RPC. Frontends attach their own
event and gate transports. They do not reconstruct subsets of the role environment.
All three use internal/harness.Session for execution, resume, budget continuation,
mutation checks and final reporting. Workflow authoring is an explicit Session
configuration; its restricted tools and resume policy stay with that role.
CLI and first-run TUI settings use internal/config.DefaultProfile: llama.cpp
at http://127.0.0.1:9931. The eval runner uses the same provider defaults.

internal/app prepares validated profiles and compiled identity as PreparedProfile.
Runtime clients remain in the composed Backend and role environment.
internal/roles builds Planner, Interactive and Subagent from Env.

Subagents
share the workspace and permission policy and cannot delegate again or ask the
operator. Their finite step allowance defaults to 12; operators can configure a
default and maximum, and a task can request an allowance within that maximum.
Errors retain a bounded report with measured effects and child archive references.
There is no automatic continuation after exhaustion.

internal/harness provides execution, events and operator gates for CLI, TUI and RPC.
Events retain their Go values in process; JSON serialization happens only at the
CLI/RPC boundary. The same event sequence and fields define the wire projection.
Conversation snapshots and title files belong to internal/conversation.
internal/llm defines provider-neutral messages, replay envelopes, errors and retries.
internal/llm/chatcompletions owns Chat Completions transport and request-format dialects;
internal/llm/responses owns the current ChatGPT Responses integration.
internal/providers declares provider metadata without importing implementations.
Its llamacpp, koboldcpp and openaicompat subpackages inspect backend metadata;
they depend only on a shared internal HTTP probe helper, never protocol clients.
internal/app constructs connections, binds credentials, and orchestrates discovery.
internal/tools supplies tools; internal/workspace resolves paths and measures
file changes; internal/permission authorizes each tool call.

The application model in internal/tui owns screen selection, shared controls,
terminal input, and the lifetime and quit policy of its peer screens.
internal/tui/agent owns the Agent conversation, composer, inspector, sessions,
and Agent execution. It reports navigation and quit intents to the parent.
internal/tui/workflow owns recipe editing, graph interaction, run inspection,
message dispatch, queued commands and asynchronous workflow execution. Its
public API exposes navigation state and operations; the host does not reach into
the recipe or run state. Workflow also supplies its menu commands,
availability rules, toggles, and shortcuts. screen.Host supplies shared dialogs and
screen.Surface supplies current geometry and styles. The shared screen package
also owns menu and overlay state, rendering, interaction, and theme mappings. Workflow receives execution
services and owns its cancellation; it has no access to Agent or transcript state.

The model server is external. GEKTOR does not install or supervise it.
API-compatible providers share the Chat Completions transport; local dialects handle
server-specific grammar and sampling. The catalog selects an inspection strategy
separately from the wire dialect. Application composition binds both to the same
HTTP connection; model listing, capability probes and backend detection never
run inside the protocol client.

## Execution and limits

Each step requests one model response. Tool calls execute and their results
append to history; a complete text response ends the run. Adjacent concurrent
tools may overlap. Exclusive tools are barriers in model call order. Results
retain that order, and errors preserve the command output alongside the error.

Arguments are validated before execution. Invalid or truncated JSON is retained
as text in the archive and explained to the model through a harness message;
it is not replaced with an invented empty call. Tool results carry failure
metadata separately from their text. SYSTEM identifies harness messages and
local UI notices; local notices are excluded from model context.

ChatGPT sign-in and credential renewal live under `internal/providers/openai`.
The Responses adapter implements the same LLM client boundary as local servers;
GEKTOR owns tool execution, permissions and compaction for every backend.
OAuth credentials stay in the protected user store. Each client binds one account
registration and verified identity; changing the selected account cannot redirect
an existing connection. Token renewal remains serialized across processes.

Responses replay preserves ordered native items separately from display text.
The envelope is replayed only when its integration, model, account scope, and
canonical message projection match. Mutation or incompatible native state falls
back to the normal transcript without restoring removed calls. Interrupted output
is archived as partial text/reasoning, never as executable tool calls.

Application composition supplies a model-facts callback on roles.Env. Sessions
refresh local facts before Run/Resume, and direct CLI execution refreshes before
constructing the parent agent. Delegated roles inherit the run snapshot. Unknown
probe results preserve known facts; settings relaunch replaces client and probe
together. The settings form receives discovery as a callback from its TUI owner.

Native process tools pass an executable and literal arguments directly.
Shell tools use pwsh.exe, then powershell.exe on Windows, or sh elsewhere.
Windows shell source travels through UTF-8 stdin with profiles disabled.
The prompt names the selected shell. Shell commands retain that shell's error
semantics; native tools report the process exit code without shell parsing.

The operator sets the step limit, default 25; explicit max-steps 0 is unlimited
for the main agent. Todo is checklist data: it cannot
grant steps or prevent an answer. On exhaustion, an operator may explicitly
approve one 25-step continuation. Step exhaustion does not request an extra
closing model call. The separate run-time limit can reserve a tool-free closing
request; see [runtime limits](docs/reference/cli.md#backend).

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

Before each agent request, `internal/agent` records archive ID membership and
captures instruction, tool and retained skill-load metadata for its tool batch.
`inspect_context` reads that fixed view; it does not reconstruct a request from
the archive or inspect backend internals. `internal/conversation` saves system prompt
revisions and the latest request manifest. Changed results and calls are marked
as rewritten; omitted messages are absent from the included ID ranges.

`read_history` searches the original archive. For the agent's own session it
stops before the calling response, so its own query cannot create a search hit.
Reading a child archive uses that child's saved history instead.
See [session context usage](docs/guide/sessions.md#session-history-and-context).

Switching between plan and act appends a stance turn rather than rewriting the
system prompt. The roles share the same tool definitions and enforce read-only
access at execution. This preserves the prefix for backend cache reuse; actual
cache hits depend on the server and its slot state.

Snapshots write atomically after responses and tool execution. Resume restores
history and composes the current launch's instructions. Fork copies history into a fresh
task directory; it does not create a Git checkout.

## Tool composition

`internal/tools/packages_builtin.go` defines native package membership, per-agent
instances and Plan eligibility. Base and ReadOnly select from those same entries.
Composition fills defaults and wires permissions locally; `Dependencies` contains
only caller inputs. Standalone Workflow construction wires its own permissions
and recipe registry without setting up unrelated services.
Native tools enter the registry directly, retaining their streaming, image,
checklist, question and context-reuse interfaces. Stateful tool instances belong
to one agent; adapter connections keep their existing launch lifetime.

The capability manager also activates native packages. Web tools remain eager
under `native_web`; an internal role-environment override supports lazy-web
evaluation. There is no new user-facing exposure setting. Saved activations
restore before eager preparation. Native definitions are checked immediately;
external adapters still reconnect and validate on use. Eager native schemas can
be previewed for context reports without publishing them or making network calls.
Discovery and a bounded prompt map share one optional-package catalog.
Optional targets use a fixed `invoke_tool` envelope. Their schemas enter history
on activation rather than changing the leading tool list. The Agent resolves
the target before permission checks and scheduling; archives retain the original
model call. Saved catalogs restore the exact schemas after compaction.
Llama-server requests also carry a transient attribution footer derived from the
saved operator messages and current run mode. It is not a new archived user turn.

### Capabilities and workflow tools

Built-in `native_images`, `native_workflows`, `native_files`, `native_documents`
and `native_media` packages load on demand. The
[capability development pattern](docs/development/capabilities.md) defines the
service and adapter boundaries for additions and changes.
`capabilities/images` stages rooted inputs for the external Pixelita Go library;
`workflow/agenttools` adapts manual recipes to `internal/workflow`. The runner has
no model dependency and dispatches registered node descriptors. Model access is
an explicit node with its own bounded context.

Native file operations live in `internal/capabilities/files`. Their agent and
workflow adapters share operations, schemas and limits. ZIP and stream archives
share extraction planning and rooted output publication. Document readers live
in `internal/capabilities/documents`, with the same adapter split. Its parsers
receive bounded bytes through `internal/subprocess`, which also hosts script worker
process support. The parent enforces deadlines and output size; the child sets
platform memory limits. This is not an operating system sandbox.

The service in `internal/capabilities/social` handles bounded FxTwitter requests
for both Agent and Workflows. Its contract owns JSON and resource handling;
Agent activation stays in tool composition. Provider code does not import Agent.

### Workflow execution

Nested steps retain direct-call permissions. Runs and schema-validated records
live in the workspace-scoped user store, outside task files. OS locks identify
active runs and serialize record revision checks; interrupted runs never retry
automatically. Schema registration is an operator action.
Version-3 recipes bind typed inputs and dependency JSON outputs; resolved values
are validated and authorized before their operation. `workflow validate/run`
requires no LLM backend unless the recipe contains a model node. `roles.WorkflowAuthor` has a fixed tool set
and can save only an operator-selected recipe; schema lookup is on demand.

### Role permissions

Main-only sources are excluded for children before loading; role constructors
reject forbidden direct extras. Plan can activate native packages without IO,
but external activation stays blocked. Every resulting call still goes through
the Plan allowlist and permission gate.

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

Browser profile selection is saved separately from activated tool catalogs.
A profile can be selected before the browser is enabled; restoring it must not
connect a transport or activate tools. Older session snapshots retain this
separate profile field.

Explicit -mcp servers still connect at launch. Configured capability packs share
the browser's deferred activation mechanism and support native Go, executable
JSON and MCP adapters. Environment details and skill metadata are queried through
system_info; load_skill reads guides and supporting resources from configured
libraries or project skill directories. See [Extensions](docs/guide/extensions.md).

`internal/mcp` owns MCP protocol sessions, stdio process lifetime and restart,
HTTP session renewal, and bounded JSON-RPC/SSE decoding. The adapters in
`internal/tools` own authorization, credentials and redaction, Agent tool names,
and result adaptation. A disconnected tool call is never replayed automatically.
See [MCP protocol sessions](internal/mcp/README.md) for lifetime contracts.

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
by that command. `internal/checks` collects language-independent, report-only
findings from file edits and a final pass over changed paths. Both passes skip
files over 256 KiB with an explicit notice and cap findings at 20 plus a limit
notice. GEKTOR does not choose language-specific checks or formatters; projects
supply their own verification command.

`LogGate` in `internal/permission` observes operator gate decisions when `-audit` is set.
It preserves gate context and results; logging failures never block execution.
Automatic policy decisions are outside this operator log.

## UI and settings

Harness instructions live in internal/prompts/text and are embedded at build
time. Interactive and Planner share the same base prompt and tool definitions;
their current Plan/Act stance is a trailing harness message. Subagents reuse
the base with an explicit task scope and budget. Provider adapters own wire
formatting; selecting a backend does not select a different behavioral prompt.
Profile identity and response presets are composed separately from capabilities.

The Bubble Tea update loop owns UI state. Components own local interactions;
the terminal host routes focus, messages and effects. Workers return messages rather
than changing the live model.

Reusable geometry, popup, form, composer, selection and transcript models live
under `internal/tui/components`. The application supplies styles and data;
components do not own backend services. The active input owner determines
keyboard, paste, mouse and caret routing. Session scans return revision-tagged
results; stale results cannot replace the current session list.

The [TUI design reference](docs/development/tui.md) defines the target component
contracts and migration plan for shared controls, layouts and interaction.

Settled transcript blocks are cached by content and presentation settings.
The viewport retains separate rendered parts and draws only visible lines.
Colors live in `internal/tui/screen/styles.go`; changing the theme invalidates cached rendering.
Width calculations use terminal cells, including frames, gutters and scrollbars.

Applying settings returns ModeChange with saved preferences, history, draft
and session directory. The CLI composes a fresh launch from structured options;
execution policy survives the handoff. Provider profiles decode strictly.

## Evaluation and boundaries

cmd/eval runs selected probes in disposable seed workspaces. Its core suite is
a fixed seven-scenario baseline. Frozen criteria check workspace contents,
tool usage and answers independently of the model checklist.

The experimental TUI /eval command is a separate retrospective, implemented in
internal/retrospective. Session snapshots retain execution ranges alongside the full
archive; compaction changes only the working projection. Retrospective constructs
its own reviewer with archive-reading and structured-submission tools, without
task capabilities or a dependency on the task-role environment.
Reports remain proposals until the user saves a lesson through `/learn` or the
`/eval` result. Each approved procedure is an ordinary discoverable workspace
skill with pinned evidence excerpts; retirement removes its guide from discovery.
Literal evidence and read coverage are checked mechanically; the model's judgment
and the general usefulness of a lesson are not independently verified.

File tools validate paths and edits. Shell and MCP run with the user's process
permissions; the workspace is not a process sandbox. See [SECURITY.md](SECURITY.md).
Usage and flags live in [README.md](README.md) and [docs/cli.md](docs/reference/cli.md).
House rules are enforced by tests described in [docs/conventions.md](docs/development/conventions.md).

### Automation boundaries

`automation` owns node descriptors, recipe/run values and the node catalog.
`automation/recipe` binds typed values and validates graph edges;
`automation/engine` handles execution, skips, iterations, attempts and review
boundaries. Neither package imports agent tools. `automation/store` owns durable
runs, records, queue snapshots, schedules and immutable run artifacts. `automation/service` coordinates
workspace access, catalog validation, resource authorization and persistence.

CLI and agent adapters call the service. `app` assembles the built-in catalog.
The TUI calls the same service.

Composition loads frozen dependencies, authorizes
recipe reads, rewrites scoped inputs and expands nested calls. A compiler owns
its step count and input bindings for one compilation; these are not global state.

`automation/api` provides optional authenticated HTTP ingress to the durable
queue. Operator-configured routes freeze recipe dependencies at startup;
route-scoped webhook tokens can only submit inputs. The API does not execute
nodes or approve reviews. The existing daemon owns execution and its policy.
Submission deduplication is atomic with enqueueing in `automation/store` and
lasts while the original job remains retained.

Artifact access is scoped to a run and producing step during node execution,
including review continuation. Nodes receive an `ArtifactAccess` interface through
the execution context; they never receive the store path or choose another run.
The store publishes bytes and metadata together, verifies reads, bounds storage,
and uses the run lease for export and retention. Artifact IDs travel through the
existing step-output references. Operator pruning removes payloads from old terminal
runs while retaining run history; it does not run implicitly.

Pixelita operations live under `capabilities/images`; its `tools` and
`nodes` packages adapt the same operations for their respective callers.
Image operations stage bounded scratch copies, process them and verify the
original sources before publication. Every destination is authorized before
workspace outputs are created. Reports distinguish processing restrictions from
publication failures and retain source-verification evidence.
JavaScript workers and bounded model nodes have explicit inputs and outputs;
they do not inherit the parent agent conversation or tool registry.

Adding a node means providing a descriptor (version, schemas, resources,
validation and execution), registering it in composition, and testing its
contract. Execution and recipe schemas do not maintain separate name switches.

HTTP nodes and native network tools share `httptransport`, including
public-address validation and DNS checks. `automation/credentials` stores only
origin-bound environment references. The HTTP node has no dependency on agent
tools. Queued starts and operator continuations share one service execution
lifecycle for cancellation and final status persistence; recovery respects run leases.

`process` owns shared process-group cleanup and environment scrubbing.
Native agent tools and the automation `run_program` node use those primitives.
The workflow node supplies its workspace directory and optional artifact stdin,
enforces a finite deadline and output bounds, and persists stdout/stderr artifacts
even on failure. It does not depend on `tools` or provide an OS sandbox.

Workflow composition snapshots the recipe dependency closure and expands calls into
qualified steps in one graph. Compiler-owned input binding and output collection
nodes are omitted from authoring schemas. Input binding preserves typed contracts;
the engine's gate dependency remains separate from `merge: any` branch semantics.
Compiler-owned output collectors wait for dependencies without inheriting branch
skip rules; a child's collector still requires its call gate. Missing referenced
outputs fail collection rather than silently disappearing from the result.

Nested calls share the parent run lease, review decisions, cancellation and artifact
store. No second worker or child-run lifecycle is created. Queues persist the source
bundle; review continuation uses the saved compiled definition.
