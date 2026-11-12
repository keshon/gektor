# Project conventions

These rules apply to changes in GEKTOR. The conventions test reads this canonical file; keep rule tags intact.

## Conventions

> How the system works is in [ARCHITECTURE.md](../../ARCHITECTURE.md). Usage is in
> [README.md](../../README.md). This document is the house rules.

## How to read this

Every rule carries an enforcement tier:

- **[enforced: &lt;name&gt;]** — a check fails, and the name says which one.
  `internal/conventions` is an ordinary Go test, so `go test ./...` runs it
  with everything else. `TestDocumentAndChecksAgree` checks that the tags here
  match the registered checks.
- **[invariant]** — nothing checks it, and violating it breaks something
  nameable. Each states the failure it prevents.
- **[practice]** — how things are done here. Nothing breaks if you deviate.

`internal/conventions` reads this file during tests. Update the documentation
and registered check together when changing an enforced rule.

**Adding a rule.** Give it a tier. If it would be [enforced], write the check
first and let it fail, then record the baseline.

## Package layers

**[enforced: package-layers]** `internal/llm`, `internal/prompts`,
`internal/workspace`, `internal/permission` and `internal/events` import no other
internal package. The `internal/llm/chatcompletions` and `internal/llm/responses`
protocol packages import only `internal/llm`. The `internal/providers` catalog
and `internal/providers/openai` account package import no other internal package.
Provider inspection packages (`llamacpp`, `koboldcpp`, `openaicompat`) import only
`internal/providers/internal/probe`, whose HTTP helpers import no internal package.

`internal/credentials` imports no other internal package and owns environment lookup
and opaque platform-protected storage. `internal/mcpmanager/credentials` imports
only that owner; MCP retains origin/header binding and its existing payload format.
SSH owns password/passphrase purpose checks and rejects unencrypted persistence.

Request dialects format inference requests; inspection belongs to providers.

`internal/conversation` imports only `internal/llm`.
`internal/agent` imports only `internal/llm`, `internal/prompts`,
`internal/permission` and `internal/conversation`. This is what
lets `internal/tools` implement `agent.Tool` without an import cycle, and what
keeps the loop testable against a stub client with no filesystem in sight.
`internal/harness` owns role selection, run/resume, checks and reporting for every
frontend. It imports only agent, checks, events, LLM contracts and roles.
`internal/agentrpc` owns RPC framing and request lifetime; it imports only
agent, events, permission, harness, workspace and conversation. Frontends pass a complete prepared
run configuration instead of rebuilding a subset of the role environment.

`internal/checks` owns language-independent content findings and bounded file
scanning. It imports only workspace access. Do not add built-in language checks,
formatter selection or language discovery; projects configure their verification
command. `internal/permission` also provides optional best-effort gate logging
through `LogGate`; logging preserves the operator's decision and error.

Capability services do not import Agent or workflow execution. Image services
import only workspace access; attachment grants are resolved by their Agent
adapter. Document readers import workspace access and the shared worker package.
File services import workspace access and HTTP transport. Social readers import
HTTP transport. Media services import workspace access, HTTP transport, process
execution and program discovery.

Shared HTTP transport and process support import no internal packages. Program
discovery imports process support. Worker support imports no internal packages.
Release packaging imports no internal runtime packages; its command only adapts
arguments to the standard-library packager and the installed Go and Git tools.
Every capability service and contract has an explicit import rule. Workflow
adapters share `internal/capabilities/nodes`; agent adapters live in
`internal/tools`. Adding an unregistered capability package fails the check.
All concrete Agent tools, including thin capability adapters and wrappers, live
in feature packages. Root tool composition does not declare tool Mode methods.
Feature behavior tests live beside their implementations. Root tool tests cover
composition, permission/resource routing and Agent/Workflow integration. Test
helpers stay local to those owners; subprocess fixtures and temporary homes
belong to the test binary that uses them.
Tool feature packages do not import root tool composition. The shared
`internal/tools/command` runtime imports only process support and
`internal/tools/output`; retained output imports only user-directory support.
Agent tool adapters do not import workflow node adapters. Application composition
assembles both. Shared record operations live in `workflow/records`, which
imports only workflow definitions, record storage and workspace access; the record
node adapter imports only definitions, shared records and workspace access.
Workflow agent adapters receive the operation registry factory from composition;
they do not import `internal/app` or reconstruct application services themselves.
Tool factories receive policy, gates and store dependencies at construction.

Reusable TUI components may import other components, but no application packages.

Workflow definitions import no internal packages. Recipe preparation imports only
workflow definitions; the engine imports definitions and recipe preparation;
the store imports only definitions. The service coordinates these packages and
workspace access. The daemon imports only the service and store. Node adapters
and frontends stay outside these core workflow packages.

**[invariant]** `internal/agent` never learns which backend it is talking to.
A backend quirk handled inside the loop is a quirk that will be handled twice
the next time a backend is added, and inconsistently the second time.

**[enforced: structured-output]** A constrained `llm.ChatRequest` sets both
`Grammar` and `JSONSchema`. The two are one contract in two encodings, and each
backend reads only one of them: koboldcpp takes GBNF on the chat endpoint,
llama-server takes `response_format` and ignores a grammar field there without
complaint. Sending one encoding leaves the other backend generating free text
that happens to be asked for JSON, which fails later and somewhere else.

## Dependencies

**[practice]** Built-in capabilities follow the
[capability development pattern](capabilities.md): execution stays in a shared
service, JSON contracts stay in its adapter, and Agent and workflow wrappers
connect those contracts to their callers. Shared HTTP, process and application
support are separate responsibilities. Existing package differences need review;
matching directory names alone does not establish a shared boundary.

**[enforced: cgo-free]** No package imports `C`. cgo turns a single `go build`
into a toolchain problem, breaks cross-compilation, and is not worth it for a
tool whose whole job is to run somewhere unattended.

**[practice]** The standard library first. A third-party package is considered
only if it is cgo-free, widely used, and earns its weight — `golang.org/x/sys`
qualifies because Windows job objects have no standard-library equivalent and
without them a stopped dev server can survive, still holding its port.

The Charm terminal stack (Bubble Tea v2, Lipgloss v2, Bubbles v2,
and their input/ANSI helpers) qualifies for `-tui`
only: writing a fullscreen renderer on raw ANSI escapes is a second project,
and the agent loop and model transports do not import the terminal stack.
Process cleanup in the tools package uses the Windows system dependency.

## Building agents

**[enforced: agent-construction]** `internal/roles` constructs task agents;
`internal/retrospective` constructs its isolated archived-session reviewer.
Only these owning packages call `agent.New`; frontends call their entry points.
Agent roles were once configured at their call sites, and they
drifted — one harness ended up without `ask_user` and `delegate_task` while the
shipping CLI had both, so it was scoring a different agent than the one under
test. One answer per role, in one place.

Role constructors return composition errors directly. Callers must handle them
before preview or execution; do not defer a captured setup error into a lifecycle
callback. Dynamic tool catalogs own activation, preview, and persisted state;
Agent consumes their lifecycle contract. Static registries need no catalog.
Construction and preview must not connect optional integrations.

Pass the output store and background owner directly to Agent. The output store
binds execution context and retains oversized results for the same session.
Workspace path interpretation owns scoped instruction lookup and canonical
permission resources; tool-specific resource contracts still take precedence.
Keep verification as measured execution, separate from result observers.

Session frontends implement `events.Sink.Handle` over the existing event vocabulary.
Delivery is synchronous and ordered; publishers must not mutate nested payloads
after emission. Harness and child adapters own event translation and identities;
the TUI alone bridges to its existing cancellable screen queue. Diagnostics owns
its log lifetime and must not change execution outcomes.

Shared provider contract tests in `internal/llm/provider_contract_test.go` exercise
both real protocols against local HTTP fixtures. They test normalized cancellation
without imposing identical provider-specific replay or response formats.

**[invariant]** A capability is removed by the harness, never by asking the
model not to use it. The plan role keeps the acting role's system prompt and
tool list — a narrower one would change the prompt prefix, so a backend that
caches prompts would re-read the whole conversation on every mode switch — and
refuses every call outside the read-only set before it runs. A subagent gets a
registry without delegation, so delegation cannot recurse.

## Verification

**[invariant]** File effects and verification results are measured, never inferred
from answer wording. An explicit verify command supplies mechanical evidence.
Verification records a real command verdict without requesting a model self-check.

**[invariant]** Reads are always refreshed: an editor or background process can
change their result without an agent mutation. Three consecutive steps with the
same calls and results trigger a final tool-free answer. Changed results are
progress. The step budget still bounds execution.

**[invariant]** Invalid arguments are never executed or replaced by a fake empty
object. Keep original input as archive text and send an explicit diagnostic to
the model. SYSTEM displays model-facing harness messages and their request
delivery; local UI statuses are labelled STATUS. Hiding SYSTEM changes display
only.

## Prompts

**[enforced: prompt-registration]** Every `internal/prompts/text/*.txt` is
referenced by `internal/prompts/prompts.go`. An orphaned prompt file is either
dead weight or a prompt someone forgot to wire up, and both look the same from
outside.

**[enforced: skill-paths]** Every `skills/.../SKILL.md` a prompt names exists in the
repository, and every prompt that names one says "if it exists". A model told
to read a file that is not there spends a turn reasoning about the
contradiction — live, one concluded the instruction itself was an error — and
the agent's workspace is often not this repository.

**[practice]** Prompts live as `.txt`, not Go string constants. A one-sentence
wording change should be a one-line diff, not a change buried in a
multi-line concatenation.

## Evals

**[enforced: probe-prompts]** Every probe's `prompt` field names a file that
exists in `eval/prompts/`. A probe whose write-up has been renamed or deleted
is a criterion nobody can check the intent of.

**[enforced: cli-flags]** Every flag `cmd/gektor` registers is named in
`docs/reference/cli.md` (as `` `-name` ``). A reference that rots one flag at a time
is how five flags went undocumented unnoticed.

**[invariant]** A probe's pass criteria come from its write-up in
`eval/prompts/`, not from what seems reasonable while writing the JSON. A
criterion invented at the probe is a number the harness will one day fail on
for no stated reason.

**[invariant]** A run that never reached the model is not scored. A dead
backend produces failed workspace checks that look exactly like a model that
got worse.

## Documentation

**[enforced: doc-identifiers]** Backticked Go names in root guides, maintained docs outside docs/internal,
skills/README.md, eval/README.md, scripts/README.md and README files under examples
are checked against parsed declarations.
The check recognizes mixed-case names and package-qualified exported names.
Bare acronyms and key labels are excluded; JSON fields use quoted JSON notation.
Bare names may match fields, methods or test declarations; qualified names
must belong to the named package. Standard-library names use its exported
declarations. This is an existence check, not a full Go type check.

**[enforced: doc-paths]** Backticked paths starting with internal/ or cmd/,
and repository-relative filenames ending in .go, must exist. Globs must match
at least one path. This covers root guides, maintained docs outside docs/internal,
skills/README.md, eval/README.md, scripts/README.md and README files under examples.
Write repository paths from the root. Absolute paths, commands, placeholders
and other workspace example paths are outside the check's scope.

**[enforced: comment-dates]** Comments in non-test Go files do not cite dates.
A comment saying a rule came from an incident on a particular day is
unverifiable to every reader who was not there. Keep the rule; the incident
belongs in a test name. Existing occurrences are recorded in `baseline.json`
and may only decrease.

**[practice]** Comments explain why, not what. The code already says what.

**[practice]** Give state and resources one owner. Keep connection cleanup beside
creation, and make cancellation behavior clear. Frontends call shared services;
they do not copy execution or permission rules. Group code by responsibility
before creating packages, interfaces or nested configuration. A short helper
should name a useful operation; a line-count target is not a reason to split it.

## Testing

**[enforced: doc-and-checks-agree]** Every `[enforced: x]` tag here has a check
in `internal/conventions`, and every check has a tag here.

**[practice]** A bug found in a live run gets a test named after its shape
before it gets a fix. The test name is where that history belongs.
