# MCP and browser management unification

Date: 2026-11-13
Status: Implemented locally and verified on 2026-11-13. Compatibility aliases were
excluded by the owner; experimental conversations may be discarded.

## Decision

Treat Playwright as a bundled MCP integration. User-added servers such as
YouGile and bundled integrations use the same management and activation rules.
Keep browser behavior in its existing adapter.

Use these terms consistently:

- Built-in tools are implemented by GEKTOR.
- External tools are supplied by MCP servers or executable capability packs.
- Bundled MCP integrations ship with GEKTOR's adapter and configuration defaults.
- User-added MCP integrations come from saved operator configuration.
- On load and on demand describe tool exposure, independently of origin.

Playwright is bundled but external: its tools run through an external MCP server.
Media tools remain built-in even when their implementation invokes FFmpeg.

An executable pack is another external adapter path already supported by GEKTOR.
For example, an operator could register a custom crm-helper.exe exposing
find_customer. GEKTOR starts the program per call, writes
`{"tool":"find_customer","arguments":{"name":"Acme"}}` to stdin and reads
one response such as `{"output":{"customer_id":"123"}}` from stdout.
The explicitly selected capability configuration supplies tool names and schemas.
This is an illustrative integration, not a shipped CRM tool.

## Starting implementation (before this change)

[Role composition](../../internal/roles/tools.go) already adds browser and
configured MCP sources to the same capability catalog. The separate
enable_browser entry point delegates to that catalog.

[Browser](../../internal/tools/browser/browser.go) owns preparation, the pinned
Playwright package, selected profile, profile locking, connection and graceful
shutdown. Capability() currently constructs a new source on each call.

[The extension layer](../../internal/tools/extension/mcp.go) supplies shared MCP
transport. StartBrowserMCP additionally grants access to browser-generated
workspace artifacts. Generic MCP servers do not receive that grant.

[Capabilities](../../internal/tools/extension/extension.go) separates a
launch-owned source/connection from each conversation's active catalog. Saved
schemas can restore without connecting; tool calls reconnect and check the
catalog fingerprint.

[The MCP manager](../../internal/mcpmanager/mcpmanager.go) owns saved definitions
and launch connections. Connect currently sets Eager=true; Disconnect sets
Eager=false. The old saved Startup field also controlled initial eagerness. Connection actions
and exposure policy are therefore coupled today.

[Browser profile restoration](../../internal/tools/browser/browser_profile.go)
selects the saved profile before restoring the catalog. Profiles persist outside
conversations; browser transport is not isolated per conversation.

The existing [Tools management plan](tools-management-plan.md) owns the shared
Tools destination. Extend that direction without another settings surface or
universal plugin framework.

## Expected behavior

Configuration, connection and activation are distinct:

1. Configuration determines availability and exposure. Saving, listing or
   inspecting configuration performs no connection or installation.
2. Connection establishes the launch-owned MCP transport and discovers tools.
   Connect, Disconnect and Reconnect never change exposure policy.
3. Activation publishes tools and instructions into one conversation's registry.
   It does not activate another conversation or a delegated agent.

On load means activation when a new conversation catalog is assembled, including
connection/discovery when required. On demand means discovery lists the
integration but its tools enter model context only after explicit activation.
Role restrictions apply to both policies.

Reopening a conversation restores saved activations lazily, subject to current
availability and catalog compatibility. A saved catalog does not prove a live
connection. Preserve explicit errors for incompatible catalogs.

Connections remain shared within one GEKTOR launch. This provides no server-side
session isolation: YouGile may keep state on its connection, just as tabs and
cookies belong to the selected browser profile. Separate connections per
conversation are outside this change.

Management mutations run while the agent is idle, using the existing guard.
Disconnect removes transport, not saved activation. The next tool call reconnects
if the source remains enabled. Disable prevents calls through previously
activated wrappers as well as new activation. Re-enable permits normal loading
and fingerprint checks. No usable stale adapter may survive disable or editing.

Connection failures remain visible and retryable; they do not silently change
on load into on demand. Startup failure must not block unrelated coding work.

## Ownership and target layout

Keep the existing packages:

- internal/mcpmanager: managed entries, origin metadata, availability, exposure,
  connection actions and status. Owns the single shutdown route for registered
  sources; does not implement browser behavior.
- internal/tools/browser: typed browser settings, package preparation, profiles,
  locking, observations and graceful adapter cleanup.
- internal/tools/extension: transports, capability loading, conversation
  activation, catalog compatibility and artifact validation.
- cmd/gektor: constructs one browser adapter and one stable capability source,
  registers the source with the manager and supplies effective settings.
- internal/roles: obtains MCP sources through the common composition path.
- internal/tui: shared Tools/MCP presentation and existing action guards;
  browser-specific settings remain feature-owned.

Add a bounded manager registration path for a bundled source. It accepts an
existing Capability and explicit display metadata. Do not rebuild browser
configuration as an editable raw MCP command. Keep bundled entries separate from
imported definitions so import/replace cannot overwrite them.

Use one stable source object for management, discovery, activation and status.
Remove the separate browser source append from roles and Tools composition.
Manager operations call the source; its adapter retains transport cleanup.
Remove competing direct launch cleanup paths for the registered browser.

Profile changes must invalidate both the browser adapter cache and the shared
source cache before restoring or activating a catalog. Preserve current
channel/profile compatibility checks. A reconnect uses the selected profile and
reacquires its lock. Do not introduce a lock-order cycle between source loading
and profile selection.

Keep browser flags as optional launch preferences. Register the bundled
integration in every launch, on demand by default, with Chrome as its default
channel. Registration performs no preparation, process launch or schema loading.
The owner removed the flag prerequisite on 2026-11-13 after testing management.
Keep existing user-added MCP settings storage. No new browser settings file is
required; permanent browser configuration retains its existing flag path.

Bundled entries show origin and effective settings. The generic editor cannot
modify their generated commands. Delete/import-replace applies only to user-added
definitions. Browser availability can be toggled for the current launch.

Preserve the browser capability ID, MCP tool names, package version and saved
fingerprint inputs wherever possible. Metadata-only changes must not invalidate
existing conversations.

## Browser behavior that remains

Retain persistent named profiles, OS locking, graceful browser_close before
transport shutdown, prerequisites and pinned npm preparation, headless selection,
browser_observe, image delivery, bounded snapshot normalization, manual-login
instructions and first-navigation verification.

Artifact access is an explicit grant from the bundled adapter. Neither an
imported server ID nor browser-prefixed tool names confer that grant. Custom
browser MCP servers retain their existing generic behavior.

Connection success means handshake and catalog discovery succeeded. It does not
prove Chrome launched, website authentication succeeded or a page action worked.

## Migration sequence

### 1. Separate connection from exposure

Remove Eager mutation from manager Connect/Disconnect. Derive exposure only from
configured policy. Store the preference as on_load (default false). Remove the
old startup field without a fallback or automatic migration. Definitions using
that field must be saved again; this experimental application has one consumer.

Document the intentional behavior change: Connect no longer automatically exposes
tools in subsequent conversations. Add coverage for both policies, connection
failure, disconnect/reconnect and conversation activation isolation first.

### 2. Register the bundled browser source

Construct one source at launch and register it with the manager. Reject collisions
with configured packs, explicit MCP flags and saved servers before starting any
process. Do not silently rename, delete or replace saved definitions.

Use the manager source list for roles and Tools. Profile changes clear both
caches; shutdown and failure paths release locks. Avoid holding the manager-wide
mutex during multi-minute npm preparation so unrelated status remains readable.

Audit existing activated wrappers when editing/disabling a source. Ensure they
consult current availability and cannot execute a disconnected superseded source
that reconnects itself after replacement.

### 3. Unify activation and management

Advertise discover_capabilities and enable_capability as the only path. Remove
enable_browser and all special activation handling. Existing experimental
conversations that depend on the removed entry point require a fresh conversation.

Show Playwright and YouGile in the same management list with the same connection
actions. Show origin, availability, exposure, connection and current-conversation
activation separately. Listing must not prepare npm, create profiles, connect,
expose schemas or send model requests.

Keep user-added import, credentials, editing and deletion semantics. Hide or
reject generic actions that do not apply to bundled entries.

### 4. Documentation and verification

Update browser/extensions guides, affected configuration/CLI references, role
instructions and offline evals. Add no compatibility aliases or migration layers.

## Acceptance checks

- YouGile and Playwright follow the same on-load/on-demand rules.
- Connect never changes exposure or another conversation's activated tools.
- Activating A does not activate B or a child agent.
- Resuming saved on-demand activation restores schemas without connecting until use.
- Disconnect/reconnect preserves activation and checks catalog compatibility.
- Disable blocks already activated wrappers; re-enable and retry work.
- Editing cannot execute through a stale cached source.
- Status remains responsive during preparation; cancellation cleans up.
- Preparation failure, handshake failure, rejected catalogs, profile changes,
  disconnect and exit release resources exactly once.
- Profile restore, explicit override and channel mismatch preserve current rules;
  a profile change cannot return tools from the previous cache.
- YouGile definitions use the current schema; credential references remain intact.
  No compatibility reader or automatic migration is introduced.
- Collisions fail before side effects; imports cannot replace bundled sources.
- Generic MCP servers cannot acquire browser artifact access by choosing an ID.
- On-demand sources keep initial model context small.
- A real browser check verifies navigation, controls, observations, screenshot
  attachment, profile persistence and cleanup. Handshake alone is insufficient.

Run focused tests under internal/mcpmanager, internal/tools/extension,
internal/tools/browser, internal/roles, internal/tui and cmd/gektor as affected.
Then use the required checks in [development testing](../development/testing.md):
go test ./..., go vet ./..., go test -race ./... and go run ./cmd/eval -dry.

Opt-in browser tests live under internal/tools/browser. Verify their actual names
and environment gates before running them; record live checks separately from
offline checks. Reviewing this draft requires no live YouGile or browser calls.

## Scope boundary

No arbitrary browser-provider framework, generic plugin SDK, new workflow browser
nodes, per-conversation MCP processes, automatic browser installation or redesign
of executable packs. The implementation changes management and activation while
preserving browser-specific behavior.


## Verification result (2026-11-13)

- Source-tree tests passed with race detection: go test -race ./cmd/... ./internal/...
- Source-tree vet passed: go vet ./cmd/... ./internal/...
- Offline eval validation passed: 45 probes.
- Real Chrome fixtures passed through generic capability activation, including
  bundled manager registration, navigation/actions, observations, screenshots,
  failed-save detection, simulated login handoff and persistent profile reuse.
- MCP table/form tests and layout checks passed across themes and terminal sizes;
  wide and narrow rendered terminal captures were inspected.
- Changed Go files are formatted; diff whitespace and documentation links pass.

The repository-wide ./... checks include historical copied Go files under
eval/results/20261110-123946/inputs with imports of moved packages and fail setup.
Those archived files were left untouched; the actual cmd and internal source
trees passed. Live YouGile requests were not made. Executable packs retain their
existing implementation.
