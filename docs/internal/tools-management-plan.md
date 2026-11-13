# Tools management: configuration, exposure and ownership

Date: 2026-11-12
Status: Approved; phase zero package boundaries are implemented. Existing SSH work and this plan
were committed as `caa586c`. Tools inventory, Media executable preferences, and reference-only SSH profile
management and Windows protected SSH secret input are implemented. Consolidated MCP
presentation remains planned; Unix encrypted SSH storage is a separate future feature.

Current migration: the first package extraction was committed as `2f4bb47`.
The follow-up, committed as `0f77869`, groups related operations under one feature owner: files, git, run,
background, web, images and host. Output and command remain shared resource
owners; read_output lives in its own tool adapter package. Browser and extension/MCP now
have dedicated owners as well, committed as `b71db5c`. Existing workflow Agent adapters and shared file
capabilities retain their cohesive feature packages.
All concrete tools, including thin shared-capability adapters and the write-check
wrapper, now live in feature packages. Root retains three files: composition,
builtin declarations and resource routing. The tool package tree below is implemented. Tools inventory and Media preferences now use the existing shared controls.

Test placement now follows those owners. Files, foreground execution, background
processes and Windows command-runtime tests live with their implementations.
Root retains eight test files for composition, permission/resource routing,
Agent/Workflow integration and their local fixtures. Subprocess fixtures and
isolated test homes are owned by the feature test binaries that use them.

## Decision

Add one application-level **Tools** destination, available identically from
Agent and Workflows. It lists toolsets and their dependencies, opens specific
settings, and explains which execution frontends support each operation.
First establish the tool, node and capability boundaries below, then start the UI
with SSH and Media and bring existing MCP management into the same entry point.
Reuse existing operation contracts, services, forms and settings transactions.

The shared UI provides navigation and status. Each feature owns the meaning of
its settings and the effects of applying them. No universal tool configuration
schema, credential factory, plugin registry or form-generation framework is needed.

## Vocabulary and boundaries

- **Capability**: reusable domain behavior such as SSH or media processing.
  Existing code also uses Capability for an Agent extension source; retain those
  internal names for now, and use more precise labels in the UI and documentation.
- **Operation**: one task with typed inputs/results, validation, limits and
  permission resources, such as `ssh_upload` or `media_convert`.
- **Agent tool**: the Agent-facing adapter for an operation, including its
  concurrency mode and session attachment handling where applicable.
- **Workflow node**: a workflow-facing descriptor for an operation, including
  node version, template validation, result validation and retry safety.
- **Toolset**: a group shown in Tools, such as Media or SSH. Existing Agent
  exposure packs remain the source for discovery and activation. A toolset is
  a user-facing grouping, not a replacement domain abstraction.
- **Dependency**: an executable, connection profile or credential reference needed
  by particular operations. A missing dependency need not disable the entire toolset.

A toolset may have no workflow nodes. Session support, MCP adapters and built-in
workflow-management tools do not become domain capabilities just for symmetry.
One-to-one pack/capability/node mappings are not a requirement.

The ordinary execution path remains:

```text
Agent tool ───────┐
                 ├─ shared operation contract ─ domain capability ─ dependencies
Workflow node ───┘
```

Application composition supplies the same effective feature settings to both
paths. The parent TUI presents and edits those settings; it never implements an
operation or calls one frontend through another.

Evidence: [capability pattern](../development/capabilities.md),
[Agent composition](../../internal/tools/packages_builtin.go),
[node adapters](../../internal/capabilities/nodes/nodes.go),
[workflow composition](../../internal/app/workflow.go).

## What the user sees

**Tools** opens a searchable list: SSH, Media, Images, Documents, Web, Workflows
and configured extensions. Show native toolsets and MCP/executable extensions
in distinguishable groups. Individual tools/nodes appear in a selected toolset's
operation list; they do not crowd the main list.

A selected toolset shows:

- Description and supported operations, with Agent/Workflow availability.
- Configuration source and readiness: configured, setup required, unavailable
  dependency, disabled where supported, or a specific configuration error.
- Agent exposure: always available, startup or on demand, derived from existing
  declarations. Show current-session activation separately when that session is
  available; otherwise say it is not attached rather than showing inactive.
- Feature-specific settings and explicit actions such as Check executable,
  Test connection or Open configuration.

Do not reduce these facts to a single Enabled/Disabled switch. Configured does
not mean activated; activated does not mean connected or authorized. SSH has no
idle connected state because every call closes its connection. A successful
check is a dated result, not a permanent guarantee.

Opening Tools must not activate packs, start processes, connect to servers or
send work to a model. Report unchecked dependencies as unchecked. An explicit
check performs a bounded effect and reports what was actually tested.

On smaller terminals the list and detail become successive views. Preserve
selection on Back, reuse existing keyboard/mouse and dialog behavior, and keep
Save, Cancel and error handling consistent with current settings. Settings remain
available without a configured model. Browsing is allowed during execution;
applying configuration waits until affected work is idle.

## Media preferences

Media owns the screen describing FFmpeg, ffprobe and yt-dlp. Each dependency
offers **Find on PATH** by default, an explicit absolute executable path, and
Disabled where the existing resolver supports it. Clearing an override restores
PATH lookup. The file picker selects an executable; it does not run it.

Keep the existing `~/.gektor/applications.json` format and
[executable resolver](../../internal/executables/executables.go). Add validated,
atomic save support there; do not write the file directly from a TUI component.
The resolver owns resolution, probing, bounds and cache invalidation.

An explicit Check executable uses the existing bounded version probe. Display
the resolved path, version and failure reason. Do not install software, change
PATH, execute arbitrary user-provided check commands or fall back from an invalid
explicit path silently.

These settings are shared by all operations using that executable, including
workflow nodes. They are not copied into each clip/conversion/download tool.
Media's Go-native operations remain available without FFmpeg or yt-dlp; report
the affected operations instead of declaring all Media unavailable.

## SSH preferences and credentials

SSH owns a connection list and an explicit connection editor: name, host, port,
user, known_hosts path and authentication method. Start with private-key file
(optional passphrase) and password. Preserve the existing `ssh.json` format and
environment references when adding editing support.

Offer an existing environment reference and, when implemented safely, a masked
secret entry with an explicit storage choice. Never put literal secrets in
`ssh.json`, recipes, application preferences or conversation state.

The existing [MCP credential store](../../internal/mcpmanager/credentials/credentials.go)
already protects stored values using Windows DPAPI. Its Unix implementation uses
owner-only files rather than encryption. Reuse the storage/protection mechanism
under a neutral credential owner; keep MCP origin/header checks, HTTP origin
binding and SSH authentication validation in their protocol-specific owners.
Do not pretend the current environment lookup already supplies protected storage.

Implemented boundaries for saved SSH passwords:

1. Extract only reusable secret storage/protection into `internal/credentials`;
   retain existing MCP credential IDs, files and origin validation.
2. Add explicit SSH secret-reference fields alongside existing environment
   fields. Validate mutually exclusive sources; never reinterpret an environment
   reference as a different secret source.
3. Pass the resolver through application composition to both execution paths.
   Missing/deleted secrets must fail without displaying their value.
4. Use a masked control whose submitted value is not retained in ordinary form
   drafts, undo/history, archives or diagnostic messages. Cancel clears it.
5. Make the platform's protection visible. Do not silently save a password in
   plaintext when the user expects protected storage. Environment references and
   key files remain the portable baseline; a Linux protected-storage backend is
   separate work before promising encrypted persistence there.

Session-only secret entry is a possible later option, not a first-release
requirement: it needs explicit process ownership and cannot silently supply a
separately running workflow worker. Avoid process-global `os.Setenv` as a TUI
secret store.

Test connection verifies the configured host key and authentication, then closes
the connection. Do not run an arbitrary remote command, transfer a file or accept
an unknown host key implicitly. It must use the same SSH connection code and
credential resolution as execution. Check SFTP/hard-link support separately and
label it separately; authentication success does not establish transfer support.

The [credential setup guide](../reference/operations.md#supplying-credentials-in-the-cli-and-tui)
covers both reference-only UI/CLI management and manual profile editing.

## Configuration, activation and application lifetime

Configuration is user-owned machine state. Recipes contain operation arguments
and connection names, not executable installations, passwords or copies of tool
preferences. Existing explicit extension files remain explicit operator inputs;
Tools must not discover and authorize workspace executable configurations merely
because a file exists.

Lazy activation remains an Agent/session concern. Workflow nodes are registered
by workflow composition and require no Agent pack activation. Initial Tools work
should display the exposure policy, not add a persistent on-demand/startup/disabled
preference that only one execution path would honor. If toolset disabling is added
later, its Agent and Workflow effects must be specified and enforced together.
Executable Disabled and MCP Enabled already have owners; retain their semantics.

Saving changes must not mutate services used by running operations or rewrite a
saved conversation's active catalog. Use the existing application settings/relaunch
handoff at an idle boundary to rebuild Agent and Workflow services together.
Preserve drafts and history, and retain existing catalog compatibility checks.
An independently running workflow worker keeps its snapshot until restarted.
Show that consequence before Apply. Do not add hot reload or connection pooling
as part of settings management.

## Prerequisite: decide tools, nodes and capabilities together

Before migration, `internal/tools` had 129 Go files, including 68 production
files and approximately 8,500 production lines (inspection on 2026-11-12).
Composition, transports, filesystem operations, activation and resource lifetimes
were mixed together. The migration separates those owners before the management
UI takes dependencies on them.

A package owns a cohesive feature, not one model-facing tool name. Keep related
operations together even when their permissions, schemas or output formats differ.
Split only for a substantial independent feature or resource lifetime. Preserve
those distinctions inside ordinary concrete operations; do not add dispatch
frameworks or split every operation into another directory.

Target shape:

```text
internal/tools/
  tools.go, packages_builtin.go   composition and exposure declarations
  workspace_paths.go             cross-tool resource/instruction routing
  documents/                     document tool adapters
  media/                         media tool adapters
  social/                        public-post tool adapter
  ssh/                           SSH tool adapters
  readoutput/                    retained-output paging adapter
  files/                         file tools, archive/download adapters and write checks
  git/                           inspection, staging, commits and Git invocation rules
  run/                           finite shell and native program tools
  background/                    start/check/wait/stop and owned process lifetime
  web/                           readable/raw fetch, URL checks and search
  images/                        attachments/view/download and image Agent adapters
  host/                          system_info, PATH/host/browser discovery and probes
  browser/                       browser activation, observations, profiles and lifetime
  extension/                     discovery, configuration, activation and adapters
  askuser/                       clarification interaction
  delegate/                      delegated-agent execution
  history/                       saved conversation retrieval
  skill/                         guide loading and completeness proof
  context/                       model-request inspection
  todo/                          checklist state
  command/                       shared bounded command execution and process cleanup
  output/                        retained logs, bounded capture and paging
```

Shared-capability Agent adapters live in their matching tool feature packages;
image adapters remain with attachment grants. Domain contracts stay under
`internal/capabilities/<feature>`. The read_output tool lives in
`internal/tools/readoutput`, using the neutral output.Store. The files owner also
contains archive/download adapters and post-write check observation.

### Feature decisions and preserved boundaries

- **Files:** one workspace file feature includes reads, directory listings, grep,
  writes, patches and moves. Keep read paging, UTF-16 handling, binary detection,
  scan budgets, mutation validation and approval evidence with their operations.
  Write and patch share their existing path lock; listing, reading and moving do
  not acquire that lock merely because they now share a package. Approval consumers
  call files.Preview and files.Proposal. Preserve rename destination and fallback
  behavior.
- **Git:** Inspect, Add and Commit live together, with one private bounded Git
  invocation path. Preserve literal pathspecs, revision validation, separate tool
  names, read-only/mutation modes, attribution and message-file cleanup.
- **Finite commands:** Shell and Program live together in run, using command's
  existing runtime. Preserve shell source versus literal native argument shapes.
- **Background commands:** one owner retains process IDs, notifications, cursors,
  output handles and stop/join. Its lifetime outlives an individual tool call and
  remains separate from finite execution.
- **HTTP:** readable/raw fetching, reachability checks and search live in web.
  Preserve their separate limits and URL policies: check_url permits local dev
  servers; fetch/search do not gain loopback access. Keep search parsing and the
  shared bounded fetch/spill path local without another tool-package dependency.
- **Images:** attachment listing, viewing, downloads, MCP image publication and
  image-operation adapters belong together. Preserve session grants, queues,
  image validation and the distinction between Agent adaptation and shared image
  transformations.
- **Host:** system_info and executable probes belong with host/PATH discovery.
  Read-only consumer interfaces expose catalog snapshots and browser tool names;
  host does not import root composition. Keep discovery separate from installation
  and preserve the existing probe verdict semantics.
- **Browser:** setup, activation, observations, profiles and shutdown share one
  browser owner. The extension adapter normalizes MCP browser responses and
  resolves artifacts only after the built-in launcher grants workspace access;
  configured MCP servers receive no such filesystem grant.
- **Extensions:** configuration, source identity, activation compatibility,
  credentials, MCP and executable adaptation move together behind real operations.
  Keep lifetime and resource ownership explicit; do not export private transport
  fields just to make a move or its old tests compile.
- **Independent interactions:** ask_user, delegation, history, skills, request
  inspection and todo retain distinct owners. Their different session contracts
  do not become one generic session-tool dispatcher.

Concrete support owners needed before child packages can stop importing root:

- Extract `OutputStore`, its context grant, reservation limits, spill publication
  and bounded output into `internal/tools/output`. Producers and read_output consume
  this same owner; no copied stores or unguarded filesystem reads.
- Extract the existing Agent command runtime into `internal/tools/command`: shell
  selection/building, native invocation, streamed combined output and spill/tree
  cleanup. Its API accepts concrete command inputs and returns output/errors, not
  Agent tool calls. Preserve its distinct output policy from `internal/process.Run`,
  which retains separate bounded streams for workflow/media execution. Continue
  reusing the existing process-group primitives rather than duplicating them.
- Keep arbitrary executable probing with its actual caller or executable owner.
  `ProbeCommand` and `executables.Resolver` have different verdict semantics;
  relocating one does not authorize merging them or changing those verdicts.
- Keep operation-specific argument guards local. The existing top-level field
  check belongs with Agent schema validation as `agent.RejectUnknownFields`, so
  independent tools do not copy it or import root composition. JSON reduction,
  URL wrappers and formatting helpers follow their real consumers; do not create
  a shared utilities package. Verify public callers before deleting a helper;
  CLI and live-test entry points count.

Dependency rules:

- The root composes child packages. Child packages must not import the root to
  obtain types or unexported helpers; that would create a cycle or a facade.
- Children consume `agent.Tool` and existing neutral domain/support packages.
  Do not move the Tool interface into the composition package.
- The new command/output support owners must not import Agent, tools or workflow
  packages. Move resource primitives and implement their bounded formatting with
  those owners; keep model/session adaptation in the tool adapters. Reusing an
  Agent formatting helper is not a reason to reverse this dependency direction.
- Trace shared output storage, credential scrubbing, process launch and browser
  activation before moving files. Put shared resource behavior with its actual
  owner, reusing `internal/mcp`, `internal/process`, `internal/credentials` and
  `internal/workspace` where their contracts fit. If retained tool output needs
  its own owner, extract that concrete store for its real readers/writers;
  do not create a miscellaneous shared-helper package.
- Extension source/config/activation types move together so browser and MCP
  management can depend on that smaller owner. Browser integration must not
  depend on root tools composition. Retain catalog fingerprints and lifecycle.
- Update roles, harness, CLI, TUI and MCP-manager imports to the actual owners.
  Do not retain aliases or forwarding constructors solely to preserve internal
  imports, and do not expose private fields solely to make extraction compile.
- Tests for feature behavior move beside that feature; composition, public
  tool-definition baselines and cross-frontend parity tests stay at composition.
  Update dependency checks to enforce the new boundaries.

Perform the work in bounded slices: freeze the current registry/policy baselines;
extract retained-output and command owners; move independent interactions; group files, run, web and Git;
move background and browser owners; move extension configuration/catalog/adapter
behavior; then consolidate thin glue and workflow adapters. Adjust the order for
actual dependencies rather than introducing a temporary facade to force it.

Preserve names, JSON schemas, ordering, pack IDs, activation instructions,
permissions, Plan restrictions, concurrency, retry declarations, output limits,
saved-catalog compatibility and cancellation/cleanup behavior. Compare the
registered definitions and policies before/after; run lifecycle and contract tests
plus the configured project checks. This phase changes code organization, not
configuration formats or management behavior.

### Workflow-facing collections

Keep `internal/workflow/agenttools` together as the Agent adapter for one workflow
service. Schema, save, run/validate, inspection, status, schedules and record
operations retain separate concrete types and permission contracts within that
package. Do not create a directory per operation. Keep execution and persistence
in the existing service, engine and store owners; adapters should remain thin.

For node packages, the inspected decisions are:

- **artifact_import / artifact_write / artifact_read_text:** keep `nodes/artifact`.
  All operate on the same immutable run-owned artifact access, not ordinary files.
- **http_request:** keep `nodes/http`. It owns its one bounded request policy and
  origin-bound credential protocol. Do not merge with fetch_raw/webfetch, whose
  redirects, outputs and status semantics differ. Extract a neutral HTTP operation
  only when a second frontend actually needs this exact contract.
- **model_generate:** keep `nodes/model`; application composition resolves the
  connection, while the node owns its request/output contract. It does not own
  provider accounts or model setup UI.
- **run_program:** keep `nodes/process`. Its separate output streams and run
  artifacts differ from the Agent command tools; sharing the name does not justify
  sharing the whole implementation. Reuse `internal/process` for actual execution.
- **record_read / record_write:** keep `nodes/record` thin over `workflow/records`;
  no new record service layer or per-method package.
- **script_transform:** keep `nodes/script` thin over the isolated workflow script
  runtime. Do not move worker lifetime or sandbox rules into the node adapter.
- **workflow_call / internal workflow_bind and workflow_value:** keep `nodes/workflow`. These are
  compiler-expanded recipe operations with binding semantics, not ordinary tools.
  Keep both internal nodes hidden from authoring catalogs.
- **Shared capability nodes:** keep `capabilities/nodes` as descriptor assembly.
  It has no independent execution implementation or retained mutable state to
  split. Adjust its concrete imports when a capability moves; no package per loop.

Agent and Workflow retain the same shared operation contracts and domain owners.
Sibling parity means equivalent semantics and explicit ownership; it does not
require matching directory counts. Extract a node package when a cohesive
responsibility warrants it, using the same rule applied to tools.

### Domain capability decisions

- **files: keep.** Downloads and archive create/list/extract share the bounded
  workspace-file service, path validation, publication and size limits. Keep their
  distinct request and archive-entry validation paths in the same feature package.
  Preserve the native_files exposure ID and the existing operation names/contract.
- **documents: keep.** DOCX, XLSX and PDF share bounded workspace reading, isolated
  parser-worker dispatch and text continuation/output budgets. DOCX and XLSX also
  share Office archive guards. Keep format-specific implementation files, rather
  than exporting parser internals just to form three new packages.
- **images: keep.** Input staging, Pixelita execution, publication and output
  evidence are coherent domain behavior. Session image grants stay in Agent
  adapters. Inspect/preview/variants/compare do not need separate packages.
- **media: keep the package, separate request paths.** Video backends and local
  FFmpeg work actually reuse temporary directories, input staging, output
  preflight/publication and bounded child-process work-directory monitoring.
  The wide request previously mixed unrelated validation fields in
  `validation.go`. Video and local-processing requests/validation are now
  separate, retaining the wire schemas, limits and backend decisions. Do not split by
  executable name or make the status operation a second registry. Dependency
  requirements must remain available to both status and management UI from the
  same feature-owned facts.
- **social: keep.** One public-post reading contract and provider behavior; no
  package per metadata field or hypothetical future network.
- **ssh: keep.** Profiles, authentication, host verification, per-call connection
  lifetime and SFTP transfers form one protocol capability. Keep exec/transfer
  files together; shared profile/credential resolution is stronger than a shared
  workspace pointer. Frontend adapters remain thin.
- **workflow records: keep their existing owner.** They live under workflow
  because storage belongs to its user store. Optimistic revisions and schema
  validation remain shared with Agent; relocating them under capabilities for
  directory symmetry would not improve ownership.
- **contracts: retain one contract per real operation owner.** Agent and node
  adapters consume it. Tables may enumerate operations; they must not become
  protocol handlers or a plugin framework. Keep each cohesive feature's contracts
  together, including downloads and archive operations.

Media request separation is implemented within the existing feature package:
`VideoRequest` and `RunVideo` own video work; `LocalRequest` and `RunLocal` own
workspace processing; `Status` owns dependency inspection. Validation follows
those same request families. Both publication paths share only a destination and
byte cap. The wire schemas, backend selection and execution limits are preserved.
No generic capability framework or additional media packages are introduced.

## Management UI code homes and ownership

Target locations, introduced only as their phase is implemented:

```text
internal/tui/tools/
  tools.go          toolset list, detail state and user intents
  media.go          executable preferences and check presentation
  ssh.go            connection list/editor and check presentation
  ...tests beside the behavior they verify
internal/tui/       parent navigation, settings transaction and relaunch handoff
internal/app/       inspection/composition and concrete management actions
internal/executables/   executable config, atomic saves, resolution and probing
internal/capabilities/ssh/  profile validation/store and bounded connection check
internal/credentials/      environment references and reusable secret protection
internal/mcpmanager/       MCP definitions, protocol checks and connection lifetime
internal/tools/            Agent composition and cross-tool resource routing
internal/tools/<feature>/  independent Agent tools and justified lifecycle groups
internal/capabilities/nodes/ workflow descriptors
```

Keep operation validation, schemas and execution in their current homes. The
management list should project existing metadata into small display values; it
must not become a second operation registry. Add concrete typed methods for
Media and SSH management, with explicit application wiring. Reuse form/list/file
picker controls and return intents to their owner. Do not give shared controls
feature callbacks or make TUI import Agent/workflow internals to execute settings.

Forms own editable drafts. Feature stores own validation and atomic persistence.
The application owns apply/rebuild decisions. Commands that probe or connect own
their cancellation and completion; closing the screen joins or cancels the work.
Check errors are scoped to the selected item, not fatal to the whole Tools screen.

## Delivery sequence and acceptance

0. **Tool/node/capability boundaries.** Complete the prerequisite above before adding
   management UI dependencies. Preserve behavior, move feature tests with their
   owners, update callers and enforce import direction. Apply the node and
   capability decisions explicitly, retaining downloads and archives in their
   shared files owner. Media request cleanup follows as its own internal API slice.
1. **Current setup docs.** Already documented: manual SSH profiles, password/key environment
   references, masked terminal input, TUI/CLI launch and worker inheritance.
2. **Tools list + Media.** Implemented: shared navigation and read-only inventory,
   then PATH/override forms and explicit executable checks. Reuse the resolver
   and verify that the selected override reaches Agent and Workflow operations.
3. **SSH profile management.** Implemented: validated profile updates, confirmed
   TUI deletion and bounded authentication checks, with matching CLI management. Supports
   existing environment references and key files, without inventing raw-secret
   CLI flags. Profile edits apply at the same idle/relaunch boundary.
4. **Protected secret input.** Implemented: shared opaque storage/protection under
   `internal/credentials`, with existing MCP binding and file compatibility retained.
   SSH has distinct saved password/passphrase references and a separate masked
   prompt. Windows uses account-bound DPAPI; SSH saved-secret writes are refused
   without encryption. Environment references and key files remain portable.
5. **MCP and extension presentation.** Reuse the current MCP manager under Tools.
   Keep older navigation/CLI entry points as routes to the same owner. Show
   explicitly selected executable extension definitions and their provenance;
   defer a general extension editor and configurable exposure policy.

For each phase verify empty/missing/invalid configs, cancellation and Cancel,
both themes and small terminals, keyboard/mouse parity, and Agent/Workflow
settings consistency. Opening the screen performs no execution/network effects.
Checks are bounded and explicit. Secrets never reach regular drafts, logs or
archives. Test no-overwrite/profile-save failures and settings edits while a run
is active. Existing permission checks, Plan restrictions, retry declarations,
catalog restore checks and startup context budgets remain in force.

Run the project-configured `build.cmd check` after implementation changes.
Documentation-only work needs link/flag/source verification rather than runtime
tests. Passing offline tests does not establish a remote server's compatibility.

## SSH UX correction

The default editor presents Password or Private key. New secrets are entered in
the isolated masked prompt on Save; existing ones have keep/change states.
Port, trusted hosts and environment sources live under Advanced. Shared saved
credentials are selected by connection name rather than raw ID. Hidden fields
retain their draft values; changing presentation does not change authentication.


## Conversation tool controls (2026-11-13)

Approved follow-up: move Tools to Run beside MCP servers. Use Toolset, Source,
Loading and Status; provide Load/Unload, Details and feature settings. Preserve
one stable task order and alphabetize remaining integrations. Always-loaded
packages cannot be unloaded. Without an attached idle Agent conversation, session
controls are disabled; workflow configuration remains separate.

The capability catalog owns loading and unloading. Save an explicit unloaded
selection in conversation capability metadata so on-load defaults do not undo it
on the next turn. Unload removes registry definitions and active instructions,
retains the immutable archive and does not close shared MCP connections. Existing
role composition supplies native bindings and source adapters to manual loading;
no model request or tool action is executed. The Agent screen owns selecting and
saving its conversation; the parent Tools view routes user intent and displays
status. Load runs as bounded background work and prevents concurrent turns.

Verify loaded/unloaded state across resume, explicit reload, on-load suppression,
MCP connection retention, Always guards, role/policy enforcement, no-session
controls, stable ordering and narrow/wide terminal layouts.


Implemented locally: Tools now lives under Run, with stable task ordering,
Loading/Status columns, explicit Load/Unload, Details and feature Settings.
Conversation metadata preserves unloaded selections across resume; shared MCP
connections remain open. Filters, selection, history and unsent drafts survive
changes. Source-tree race tests, vet, 45 dry eval probes and theme/size layout
checks passed. Rendered wide and narrow inventory captures were inspected.

Pre-session selections use the existing in-memory draft and the same capability metadata. The first message saves them into the initial conversation snapshot before constructing its session. Background loading returns metadata to the UI loop; it never mutates the draft from a worker.
