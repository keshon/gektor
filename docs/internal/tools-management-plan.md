# Tools management: configuration, exposure and ownership

Date: 2026-11-12
Status: Proposed implementation plan. Only the current SSH setup documentation
has been changed; the management UI described here is not implemented.

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

Before offering saved SSH passwords:

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

The [current manual setup](../reference/operations.md#supplying-credentials-in-the-cli-and-tui)
remains accurate until this UI and the matching CLI management commands ship.

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

The current `internal/tools` root has 129 Go files, including 68 production files
and approximately 8,500 production lines (inspection on 2026-11-12). It combines
composition, browser setup/lifetime, process execution, filesystem operations,
Git, extension transports, activation and session support. These responsibilities
justify package boundaries before the management UI takes dependencies on them.

The initial seven broad tool-family buckets are superseded by this decision.
Most substantive standalone tools get narrow packages. Keep operations together
when they form one resource lifecycle or already share an entire execution path;
sharing a workspace pointer or a small helper is not sufficient justification.
Thin adapters remain compact. User-facing toolsets are independent of Go package
boundaries, so an Agent pack may compose several packages.

Target shape, with exact file allocation confirmed by tracing each operation's
dependencies before its move:

```text
internal/tools/
  tools.go, packages_builtin.go   explicit composition and exposure declarations
  operations.go                  thin documents/files/media/social/SSH adapters
  workspace_paths.go             cross-tool resource/instruction routing
  readfile/                      read_file and its continuation/decoding behavior
  listfiles/                     list_files
  grep/                          grep_files and its scan implementation
  edit/                          write_file and patch_file, locking and edit evidence
  movefile/                      move_file
  shell/                         run_shell
  program/                       run_program
  background/                    start_background/start_program/check/wait/stop
  git/                           git inspection
  gitadd/                        git_add
  gitcommit/                     git_commit
  fetch/                         webfetch + fetch_raw, common bounded fetch path
  checkurl/                      check_url
  search/                        search_web
  browser/                       enable_browser, browser_observe and browser lifetime
  images/                        attachment listing/view/download + image adapters
  extension/                     discovery, activation and extension adapter lifetime
  askuser/                       ask_user
  delegate/                      delegate_task
  history/                       read_history
  skill/                         load_skill
  context/                       inspect_context
  todo/                          todo and its checklist state
  systeminfo/                    system_info and host discovery
  readoutput/                    read_output over the shared output store
```

Keep the small shared-capability adapters together as composition glue, except
image adapters which belong beside their real attachment-grant handling. Their
domain code and contracts remain under `internal/capabilities/<feature>`.

### Tool-by-tool decisions and evidence

- **read_file:** extract with its UTF-16 decoder, binary detection, continuation,
  image attachment behavior and read tests from `fs.go`/`utf16.go`. It owns a
  substantial read interaction independently of directory listing and editing.
- **list_files:** extract the directory listing operation from `fs.go`; borrowing
  the same workspace is not shared lifecycle with read_file.
- **grep_files:** extract `grep.go`/`grep_scan.go` together; scanning, context and
  bounds form one operation. Reading and grep may share a small text-sniffing
  primitive with a concrete owner, or modest duplication; neither imports the
  other's tool implementation for it.
- **write_file + patch_file:** keep one edit package. Both use `withPathLock`
  from `mutation.go`; patch matching, newline preservation, write guards and
  approval previews/evidence must agree with actual mutations. Move the relevant
  parts of `fs.go` and `mutation.go` together. Expose only the operation-level
  proposal/preview behavior needed by actual approval consumers, not locks or
  mutable buffers. The cross-tool `PreviewArgs`/`FullApproval` routing and stale
  evidence checks remain root glue initially because they also cover move_file;
  move_file must not import the edit tool just to get an approval formatter.
  Their shared mutation semantics justify this grouping, not the word file.
- **move_file:** separate. It currently does not use the write/patch lock; do not
  describe all filesystem tools as one synchronized owner. Cross-filesystem copy
  behavior and existing destination behavior stay unchanged in a package move.
- **run_shell / run_program:** separate adapters. Shell parsing and literal native
  argument rules remain distinct. They consume the same foreground command runtime
  rather than borrowing a tool from one another.
- **start_background / start_program / check_background / wait_background /
  stop_background:** keep one background owner. The shared `BackgroundProcesses`
  map, notifications, cursors, stop/join and output handles must retain one lifetime.
- **git / git_add / git_commit:** separate adapters. The current shared `runGit`
  function is a small invocation helper, not mutable Git state owned by this package.
  Reuse the command runtime; keep repository argument safeguards in each operation.
  A few repeated Git prefix arguments do not warrant a new generic Git framework.
- **webfetch + fetch_raw:** keep together. `fetch.go` owns their complete bounded
  request/spill path, with explicit extraction and size differences. This is more
  substantial than sharing an HTTP client and already prevents behavior drift.
- **check_url / search_web:** separate from fetch and from each other. Their
  reachability and search parsing/result semantics differ; reuse `httptransport`.
- **enable_browser + browser_observe:** keep with browser setup, profiles, locks,
  artifacts and connection shutdown. The same browser owner supplies the source
  registered with extension activation; browser never imports root composition.
- **image_attachments / image_view / image_download / image operation adapters:**
  keep one Agent image integration package because grant resolution, returned
  image attachments and vision admission are frontend-owned. The shared image
  domain must remain independent of those session grants.
- **discover_capabilities / enable_capability:** keep beside source configuration,
  catalog snapshots, native-pack exposure and extension MCP/executable adaptation
  in `extension`. Preserve the distinct launch-owned source and session-owned
  catalog lifetimes. `internal/mcp` remains the protocol transport owner.
  Do not create an independent package per remotely advertised tool.
- **ask_user / delegate_task / read_history / load_skill / inspect_context / todo /
  system_info:** give each its own narrow package. Inspection found no shared
  session state that requires this collection to remain in a `session` bucket.
  Checklist state belongs to todo; question suspension and delegation keep their
  existing Agent/harness contracts and cancellation behavior.
- **read_output:** separate the adapter from retained output storage, which is
  also written by commands, background jobs, HTTP fetches and extensions.
- **Shared capability adapters:** documents, archive/download, media, social and
  SSH wrappers are currently roughly 10–16 lines each. Consolidate these wrappers
  in root glue rather than adding a package for every forwarding type. Registration
  still consumes their existing shared contracts; no generic execution dispatcher.

Concrete support owners needed before child packages can stop importing root:

- Extract `OutputStore`, its context grant, reservation limits, spill publication
  and bounded output into `internal/tooloutput`. Producers and read_output consume
  this same owner; no copied stores or unguarded filesystem reads.
- Extract the existing Agent command runtime into `internal/command`: shell
  selection/building, native invocation, streamed combined output and spill/tree
  cleanup. Its API accepts concrete command inputs and returns output/errors, not
  Agent tool calls. Preserve its distinct output policy from `internal/process.Run`,
  which retains separate bounded streams for workflow/media execution. Continue
  reusing the existing process-group primitives rather than duplicating them.
- Keep arbitrary executable probing with its actual caller or executable owner.
  `ProbeCommand` and `executables.Resolver` have different verdict semantics;
  relocating one does not authorize merging them or changing those verdicts.
- Keep strict argument decoding local where small. `validate.go`, JSON reduction,
  URL wrappers and formatting helpers must follow real consumers; do not move them
  wholesale into a shared utilities package. Verify public callers before deleting
  an apparently unused helper; CLI and live-test entry points count.

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
extract retained-output and command owners; move independent tools; move edit,
background and browser owners; move extension configuration/catalog/adapter
behavior; then consolidate thin glue and workflow adapters. Adjust the order for
actual dependencies rather than introducing a temporary facade to force it.

Preserve names, JSON schemas, ordering, pack IDs, activation instructions,
permissions, Plan restrictions, concurrency, retry declarations, output limits,
saved-catalog compatibility and cancellation/cleanup behavior. Compare the
registered definitions and policies before/after; run lifecycle and contract tests
plus the configured project checks. This phase changes code organization, not
configuration formats or management behavior.

### Workflow-facing collections

Apply the same independence test to `internal/workflow/agenttools`, not only the
root tools collection:

- Extract schema inspection, recipe saving, run inspection and worker status into
  narrow `schema`, `save`, `inspect` and `status` adapter packages respectively.
- Keep workflow_run/workflow_validate together in `run`: they use the same
  service/source validation and differ in whether execution is requested.
- Keep workflow_schedule/workflow_schedules together in `schedule`: one schedule
  identity, revision and snapshot contract owns both operations.
- Keep record_read/record_write as compact adapters to `workflow/records`;
  their shared store/optimistic-revision behavior already has a proper owner.
  The root `workflow/agenttools` becomes small adapter glue, not another collection
  that accumulates substantial operation implementations.

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

- **files: split download from archives.** The current Service combines HTTP
  transport with archive parsing/publication, sharing mostly WS and a size bound.
  Move download_file to `capabilities/download` with its own contract; move
  archive_list/create/extract and ZIP/TAR/GZIP machinery to `capabilities/archive`
  with its own contract. Preserve native_files as one exposure pack and existing
  operation names. Small path/cancellation checks can be duplicated or use the
  existing workspace API; no artificial shared files facade. All three archive
  operations retain one format/entry validation and extraction-planning owner.
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
  The weakness is the wide Request plus validation of unrelated fields in
  `validation.go`, not lack of directories. Follow package moves with a separate
  internal API change for video versus local-processing requests/validation,
  retaining the wire schemas, limits and backend decisions. Do not split by
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
  protocol handlers or a plugin framework. Split the files contract alongside
  its domain split; keep the remaining cohesive contracts together.

The archive/download split is an ownership change separate from mechanical tool
moves, and Media request separation is a later internal API cleanup. Verify each
slice independently. No runtime behavior changes are implied by this decision
ledger, and the management UI must not be used to hide unresolved domain APIs.

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
internal/tools/            Agent composition and thin capability adapters
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
   capability decisions explicitly, including splitting downloads from archives.
   Keep Media request cleanup in its own subsequent slice.
1. **Current setup docs.** Already documented: manual SSH profiles, password/key environment
   references, masked terminal input, TUI/CLI launch and worker inheritance.
2. **Tools list + Media.** Add the shared navigation and read-only inventory,
   then PATH/override forms and explicit executable checks. Reuse the resolver
   and verify that the selected override reaches Agent and Workflow operations.
3. **SSH profile management.** Add validated atomic profile updates and bounded
   authentication checks; expose the same operations to the CLI. First support
   existing environment references and key files, without inventing raw-secret
   CLI flags. Profile edits apply at the same idle/relaunch boundary.
4. **Protected secret input.** Extract the existing protection mechanism without
   weakening MCP binding, then add masked SSH password/passphrase entry and
   secret-reference persistence. Make platform support and storage explicit.
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
