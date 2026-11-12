# Discovery and external-path access audit

Recorded: 2026-11-12. Baseline: `d51ae84`.
Status: baseline investigation complete. Discovery and composition changes are
implemented in the follow-up below; external-path access remains a proposal.
The baseline sections are historical evidence, not the current access contract.

## Scope and verdict

Reviewed agent-facing capability discovery and activation, optional-tool dispatch,
MCP catalog discovery, skill metadata and loading, host/PATH/browser inspection,
catalog restoration and compaction, workspace paths, scoped instructions,
permissions, shell execution and TUI file references. Model-provider selection
and settings-picker discovery are separate from this audit.

The discovery/execution separation is sound. Finding the right feature and its
configured resources needs improvement. Keep the existing catalog and dispatcher;
there is no evidence here for a new discovery framework, persistent SSH shells,
automatic command retries or an embedding-based search service.

Source session `22db8cc4` completed the requested remote listing correctly in
59.3 seconds: eight model requests and nine tool calls, including about 6.1
seconds for the SSH call. It performed no transfer, retry, cancellation or
output-overflow test. Its subsequent feedback describes some untested concerns
and incorrectly calls SSH overflow truncation.

## Findings

| Priority | Finding | Evidence and consequence |
|---|---|---|
| High | SSH has no connection inventory | The three SSH tools require a connection name, but activation points to configuration rather than exposing names. The session read that configuration through PowerShell after the file tool rejected it. |
| Medium | Matching semantics differ and reject ordinary queries | Capabilities require every whitespace-delimited term to occur in ID/description. Skills require one contiguous substring across ID/path/description. Word order and extra words can turn relevant results into an empty result. |
| Medium | Search does not include known tool names | Searching for `image_view` returns no pack. Native definitions and configured executable definitions can be known without opening an adapter, but capability search only reads ID and description. |
| Medium | Imported integrations can have almost no searchable meaning | MCP import initializes descriptions to `<id> MCP service`. A user asking about what an opaque server does cannot find it by those activities unless an operator improves the description. Discovering remote tool details requires activation; do not silently connect every server to compensate. |
| Medium | Discovery omits lifecycle state | Results contain ID and description, unchanged after activation. They do not distinguish configured, activated, or known failed connections. Being discoverable also does not establish permission or readiness. |
| Medium | Capability results have no paging | The startup map is bounded, but the full discovery response and activation schemas are not paged. Generic output storage can preserve oversized tool output through `read_output`; this is a recovery mechanism, not a convenient discovery interface. Catalog reinjection can also add substantial context. This is a scaling concern, not an observed failure in the SSH session. |
| Medium | External files have no normal authorization route | Rooted file tools and explicit TUI references reject paths outside the workspace. An ordinary permission approval cannot widen the root. Shell execution can access those paths under the user's account. |
| Low | Browser entry points and status add avoidable branching | Browser activation exists both as `enable_browser` and capability activation. They return different text; the catalog-recovery path supplies missing schemas. Host browser inspection may recommend capability discovery merely because a catalog exists, without establishing that a browser pack is configured. |

Relevant implementations:
[capability search](../../internal/tools/extension/extension.go),
[catalog entries and startup map](../../internal/tools/extension/native.go),
[native metadata](../../internal/tools/packages_builtin.go),
[MCP import](../../internal/mcpmanager/mcpmanager.go),
[skill and host search](../../internal/tools/host/host.go),
[SSH operations](../../internal/capabilities/ssh/contract/contract.go),
[browser activation](../../internal/tools/browser/browser.go).

### Search probe

A temporary test called the shipping composition and discovery methods, with no
remote connection or model inference. It was removed after execution.

| Query | Observed result |
|---|---|
| Capability `ssh` | `native_ssh` |
| Capability `ssh connect host` | Empty |
| Capability `sftp upload` | Empty |
| Capability `pdf` | `native_documents` |
| Capability `read pdf documents` | `native_documents` |
| Capability `image_view` | Empty |
| Capability `crop` | `native_images` |
| Capability `png` | Empty |
| Capability `workflow schedule cron` | `native_workflows` |
| Capability `schedules` | Empty |

A fixture skill named `go-review`, described as `Review readable Go packages.`,
was found by `go`, `readable` and `review readable`, but not by `go review` or
`review go`. SSH discovery returned exactly the same response before and after
activation. Empty results are not proof of a missing feature.

### Safeguards worth keeping

- Metadata discovery does not start adapters. Activation and tool actions are
  separate. Native activation publishes existing instances.
- Optional dispatch resolves the actual target before argument validation,
  Plan admission, permission checks, scheduling and execution. The dispatcher
  is not an authorization shortcut.
- Registry additions are atomic, reject name collisions and validate schemas.
  Failed activation does not publish a partial active catalog.
- Main-only packs are excluded from child discovery, activation and restoration.
- Saved catalogs restore lazily. External execution checks the loaded catalog
  fingerprint; incompatible native catalogs are rejected during restoration.
- Missing optional schemas and instructions are restored from saved activation
  state after compaction. Arbitrary user text is not accepted as catalog delivery.
- Skills expose metadata before full guides, have paging and warning reporting,
  and confine supporting resources to their package. Project overrides and
  malformed entries have existing coverage. New skills can be discovered during
  a session without reopening the workspace.
- MCP tool enumeration follows pages and rejects repeated cursors, more than
  100 pages, or more than 4096 definitions.
- PATH discovery lists executable candidates without launching each candidate;
  it preserves shadowed paths and reports scan warnings. It is not an inventory
  of agent tools, all installed packages, or proof that an executable works.

Evidence lives in [dispatch tests](../../internal/agent/dispatch_test.go),
[role policy tests](../../internal/roles/catalog_policy_test.go),
[capability tests](../../internal/tools/extension/capabilities_test.go),
[skill tests](../../internal/workspace/skill_library_test.go),
[host tests](../../internal/tools/host/host_test.go) and
[MCP enumeration tests](../../internal/mcp/discovery_test.go).

## Recommended discovery changes

1. Add a read-only SSH connection inventory. Return connection names and minimal
   identifying metadata, excluding credential references, secrets and private-key
   paths. Use the service's current profile snapshot. Do not ask the model to
   inspect application configuration. Unknown-name errors should point to it.
2. Use consistent, deterministic token matching for capability and skill
   metadata. Prefer exact IDs and known tool names, then rank token matches;
   preserve stable ordering for ties. Return a bounded page and continuation.
   Empty results should suggest a shorter query or unfiltered inventory.
   PATH filename filtering can remain a literal filter.
3. Improve native descriptions with actual activities and common formats. Search
   already-known native/configured executable tool names. Remote definitions
   become searchable after activation; discovering them must remain an explicit
   action. Let operators supply meaningful descriptions for imported MCP servers.
4. Include activation state in discovery. Separate configured/activated state
   from connectivity and readiness; report unknown readiness honestly. Retain
   permission checks on actual calls. Make active saved catalogs inspectable even
   when their source is no longer configured, with an actionable unavailable state.
5. Align browser activation responses and status wording. Preserve the existing
   convenience entry point if useful; this does not require a new browser API.
6. Add discovery-focused tests and decision probes: multiword/reordered queries,
   known tool names, opaque integration descriptions, no-match recovery, activation
   state, large catalogs and an SSH task that never reads local configuration.
   Existing release-skill and media probes are useful, but do not establish this
   whole discovery contract. Run live model probes separately from unit tests.

## Recommended external-path model

Keep one primary workspace for relative paths, default process working directory,
Git and project identity. Add operator-visible file access scopes alongside it;
there is no need to change the workspace to a drive root.

| User intent | Proposed behavior |
|---|---|
| Read one external document or image | Explicit attachment creates a bounded snapshot, with its source identified. It grants no sibling access or later write access. |
| Repeatedly inspect an external file or folder | Grant read access to that exact file or folder for this session. Ordinary file tools use the grant. |
| Modify an external target | Grant write access separately, covering the requested target and required operation. |
| Reuse a trusted reference folder across chats | Offer an explicit operator action to remember the read scope for this workspace. Display remembered scopes on launch and allow revocation. |
| Run commands in another repository | Choose its working directory explicitly and authorize the command. File scopes alone do not constrain the process. |
| Inspect configured SSH hosts or other application services | Use dedicated metadata tools rather than granting access to application configuration. |

A plain path in conversation is useful intent, but should not silently grant the
parent directory or turn a read request into write access. Explicit attachments
can authorize their exact snapshot; broader scopes need a concrete operator
choice. A model may request a scope through the normal permission interface; it
must not approve its own request or derive approval from tool/document content.

Implementation constraints:

- Resolve relative paths against the primary workspace. External absolute paths
  resolve only against matching approved scopes. Keep existing file tool names.
- Check scopes before scoped-instruction discovery as well as actual I/O.
  Currently [argument instruction loading](../../internal/tools/workspace_paths.go)
  can reject an external path before the tool and permission gate can handle it.
- Use rooted handles for approved directories and exact-name restrictions for
  file grants. Preserve symlink/junction containment at the operation; a lexical
  prefix check followed by an unrestricted open is insufficient.
- Canonicalize permission resources consistently within each scope, so alternate
  path spellings cannot evade a deny rule. Keep sensitive-read policies in effect.
- Apply scopes to read/write/search/preview, attachments and capability local
  inputs/outputs, including SSH transfers. Keep scope state outside model history;
  compaction and replay cannot grant access. Revocation must close/restrict scopes.
- Define instruction provenance explicitly: a document attachment is data; a
  grant to edit another repository must account for that repository's applicable
  project instructions rather than treating the primary project's rules as global.
- Distinguish remembered access configuration from run-local permission answers.
  Child agents and workflow runs must receive only the scopes they need, not an
  accidental global access map.

### Shell boundary

The current [security contract](../../SECURITY.md) deliberately confines file
APIs while processes run with the operator account's permissions. The default
[permission policy](../../internal/permission/permission.go) allows most commands
and asks for selected dangerous patterns. It is not a filesystem sandbox.

For ordinary trusted local use, add an honest behavioral rule: use approved scopes
for intentional external file operations; after an access rejection, obtain scope
access instead of changing tools to read the same target. Make errors explain the
supported recovery. This improves consistency but cannot constrain arbitrary code.

Actual enforcement across shells, native executables, background jobs and local
adapters requires OS-level isolation with approved paths mounted or otherwise
exposed. Do not attempt to enforce it by parsing command strings. Keep that as a
separate execution-isolation project if that guarantee is required. File scopes
are worthwhile without claiming that they isolate child processes.

## Delivery order and validation

Deliver discovery/search/SSH inventory first, then exact external attachments,
then session file/folder scopes across the normal tools. Add remembered scopes
after session scope behavior and revocation are stable. Process isolation is a
separate decision, not a prerequisite for these usability improvements.

Validation performed on this baseline:

```text
go test ./internal/tools/... ./internal/agent ./internal/roles ./internal/workspace ./internal/mcp ./internal/permission ./internal/conversation -count=1
go vet ./internal/tools/... ./internal/agent ./internal/roles ./internal/workspace ./internal/mcp ./internal/permission ./internal/conversation
```

Both passed. The temporary search probe also passed and produced the results
above. Tests cover mechanics, not model decision quality. No fresh live SSH,
browser or LLM exercise was run for this audit; no production Go or skill files
were changed. The earlier local session supplies the observed SSH behavior.

## Discovery delivery on 2026-11-12

Implemented after the baseline investigation:

- One shared metadata matcher for skill and capability discovery. Matching query
  words may appear in any order; matching-word count determines rank, with exact
  fields ranked first and stable ordering for ties. No synonym service or model
  call is involved.
- Capability search includes native, configured executable and already-known
  remote tool names. Results use the same collection/total/more/next_offset
  pattern as host discovery. Default page size is 50, maximum 100. Empty matches
  include a recovery hint. Activation state is visible; it does not claim readiness.
- Saved catalogs without a configured source remain inspectable as unavailable.
- `ssh_connections` returns configured names and endpoints from the execution
  service's immutable snapshot, without resolving credentials or connecting.
  SSH guidance directs the model there instead of configuration files.
- Browser convenience activation returns the same catalog result as generic
  activation. Host inspection distinguishes PATH executables from Agent tools.
- `builtinPackages` declares packs; defaults and feature-specific wiring are
  separate functions in the same file. Operation entry builders return the tool
  together with its own read-only declaration, removing repeated policy callbacks.

No per-pack types or files were added. The shared matcher has one small package
because both adapters use the same ranking behavior. Transport, permissions,
Plan admission, workspace containment and process execution boundaries remain.
The SSH catalog change requires a fresh session if an old saved activation is
rejected by the existing native-catalog compatibility check.

### Token measurements

Measured through `TestStartupContextLive` against the running llama.cpp backend
with the same workspace, skill libraries, task and one-token response budget:

| Measurement | Before | After |
|---|---:|---:|
| Backend-reported startup prompt tokens | 5294 | 5411 |
| Local estimate | 4859 | 4967 |
| Leading tool definitions | 31 | 31 |

The measured increase is 117 tokens, about 2.2%. The browser-enabled startup
estimate allowance moved from 5000 to 5200; it remains an estimate check rather
than a promise that backend tokenization matches it. Lazy activation still keeps
optional schemas out of the leading tool definitions. Discovery pages prevent
whole inventories entering every result. This does not solve the pre-existing
cost of activating an unusually large remote tool catalog; activation and
compaction recovery can still deliver that catalog's full schemas.

### Mechanical and model checks

Passed `go test ./...`, `go vet ./...`, and `go build ./...`. Targeted race checks
passed for extension discovery, host discovery, SSH metadata/service and roles.
Tests cover ranking, reordered terms, exact names, invalid paging requests,
continuation, a 200-pack inventory, activation state, missing restored sources,
executable metadata without process launch, credential omission and snapshot use.
Existing role/dispatcher tests continue to check target permissions and unchanged
leading definitions across activation.

The opt-in `TestDiscoveryLive` uses the shipping Interactive role with isolated
SSH profiles and 40 unrelated fixture skills. No remote SSH host or personal
configuration is needed. The backend reported Ornith 1.5 35B-A3B Q5_K_M.

| Live check series | Result | Interpretation |
|---|---|---|
| Initial three scenarios | 3/3 passed | SSH inventory, tool-name search/pagination and skill search/load worked. The SSH run made an unnecessary PATH query. |
| Two repeats after clarifying PATH discovery | 4/6 passed | SSH and skill checks passed. Both pagination checks failed because the model ignored the requested two-item page size, using 10 or 50 instead. |
| Two repeats with explicit `limit=2` in the pagination request | 6/6 passed | Both runs followed the returned cursor; both SSH runs avoided file/PATH detours; both skill runs loaded the guide. One skill run first tried capability discovery before finding skill metadata. |

The final series establishes working mechanics with explicit paging parameters;
it does not establish reliable interpretation of every natural-language page-size
request. The earlier failures are retained here rather than pooled into a pass
rate across different prompts. Test logs include request tokens and tool calls.
Reproduction instructions are in the [eval guide](../../eval/README.md#discovery-integration-checks).

External attachments, additional file/folder scopes, remembered scopes and OS
process isolation are not implemented in this delivery.
