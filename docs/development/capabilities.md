# Built-in capability development

Use this standard when adding or changing an operation available to Agent or
Workflows. The current implementations live under `internal/capabilities`.
This page defines the development pattern; operation arguments and limits belong
in the [operation reference](../reference/operations.md).

## Separate capabilities from support code

A capability does a user task: read a document, process an image, download a
video or read a public post. HTTP transport, process cleanup and application
discovery support those tasks. They do not need tool and node adapters of their
own just to match a directory layout.

Name capability packages by the task they own. Keep provider names inside the
package when several providers serve the same task. Name support packages by
the resource they manage. Availability, authorization and successful execution
are separate facts.

## Capability layout

| Part | Owns | Does not own |
|---|---|---|
| Root package | Typed requests, validation, execution, results and resource cleanup | Agent loops, workflow execution or UI |
| `contract` | Operation names, descriptions, JSON contracts, argument decoding and permission resources | A second implementation of the operation |
| Feature packages under `internal/tools` | The Agent tool interface, attachment access and concurrency mode | Parsing, provider selection or file publication |
| `internal/capabilities/nodes` | Workflow descriptors, template validation, versions and retry declarations | Parsing, provider selection or file publication |

Each feature keeps its typed service and shared `contract` together. Agent
adapters live with their feature tool packages; related operations share one
owner. Workflow descriptors use the common node assembly above. Do not add a
subpackage for each operation. Composition supplies store and authorization
dependencies before any tool is registered, including derived output paths.

Shared support lives in `internal/httptransport`, `internal/process` and
`internal/executables`. Program configuration keeps its existing applications.json
filename. The capability package names are documents, files, images, media,
social and ssh. Provider names stay inside their implementations.

Agent composition lives in `internal/tools`. Concrete native operations belong to
feature packages such as `internal/tools/files`, `internal/tools/git` and
`internal/tools/web`; related operations share their owner. Browser setup,
profiles and connection lifetime live in `internal/tools/browser`.
`internal/tools/extension` owns discovery, activation, executable adapters and
MCP response adaptation. Only the built-in browser launcher grants its MCP
adapter access to browser artifacts in the workspace. Every concrete tool,
including shared-capability adapters, lives in a child package. Root contains composition and cross-tool resource routing. Child
packages do not import root composition. Command execution and retained output
are shared support owners under `internal/tools/command` and `internal/tools/output`.

The proposed [Tools management plan](../internal/tools-management-plan.md)
extends configuration UI across Agent and Workflows while retaining these
execution boundaries. Tools inventory, Media executable preferences and reference-only SSH editing are implemented.
protected secret entry and consolidated MCP management remain planned.

A provider-only reader may expose a typed function instead of a service. Do not
add empty adapter packages for a frontend that does not use it. When another
frontend needs the operation, give both callers the same contract.

Application-record operations are shared in `internal/workflow/records` because
they belong to the workflow user store. That package owns the operation contract
and execution; `workflow/agenttools` and `workflow/nodes/record` only adapt it.
Permission-resource routing uses the same shared operation package. This is the
current ownership boundary, not a second implementation or forwarding layer.

The root package must not import Agent, tool registries, workflow execution or
model protocols. Resolve session attachment grants at the adapter boundary and
pass explicit input access to the service. Document parsers may return worker
JSON across their existing subprocess boundary; they still receive typed requests.

Keep a single operation implementation and a shared contract. Both frontends use
the same defaults, normalized resources and result fields. Keep the operation
list in one place where both adapters need it. A new registry, plugin framework
or generic service interface is not required.

## Construction and lifetime

Use a plain service value when its dependencies are already explicit. Use a
constructor when defaults or owned state need initialization. Do not add a
constructor merely to make every package look alike.

Construction and capability discovery must not read task files, probe programs,
connect to providers or perform the operation. Inject dependencies at composition
time. A service that owns a cache or connection needs a clear lifetime; do not
silently create an independent copy for every helper call.

Reusable application discovery accepts its settings location from its caller.
Machine paths belong in user configuration; task paths belong to the workspace.
Keep model instructions and operation schemas lazy when their pack is lazy.

## Lazy exposure and prompt caching

Optional tools use a fixed `invoke_tool` definition. Activation installs their
execution targets and returns their exact argument schemas in conversation
content. It does not expand the leading model tool list. Eager tools remain
directly callable. The initial pack map is bounded to 700 characters with short
descriptions; discovery returns the full metadata.

`Registry.Defs` describes all execution targets. `ModelDefs` omits optional
targets. The Agent resolves an invocation before ordinary validation,
permissions, read-only admission, scheduling, attachments and mutation tracking.
Observers see the target name; the model replay retains its original envelope.
A dispatcher must never call an operation behind these checks.

Catalogs are marked by host metadata and restored from saved capability state
after compaction or resume. Restored schemas count toward the context budget.
Do not trust a summary or a matching substring of user/tool text to retain a
catalog. Startup estimates must stay within their existing budget.

Measure cached and newly processed tokens separately from total context size.
Schemas still consume context after activation. Appending them to the leading
tool list would invalidate the conversation prefix just as inserting them does.

The [media session findings](../internal/media-session-findings.md) record the
prefill measurements, implemented dispatcher, completed decision evals and
accepted model limitations. Earlier pending items there are historical.

## Validation and permissions

Decode strict JSON in the shared adapter. Reject unknown operations and fields.
The root service validates typed requests too, so direct Go callers cannot bypass
its limits. Validation normalizes values without executing the operation.

Permission resources must use the same normalized paths and URLs as execution.
Authorize every explicit input and destination. Authorize derived output names
before publication. Services receive an authorization callback when generated
paths are not known at the initial call boundary.

Template validation checks literal values without opening files or requiring
installed applications. Resolved values go through ordinary validation and
authorization before execution. References remain structured JSON objects.

## Execution and results

- Give file sizes, output sizes, item counts and execution time explicit bounds.
  Check the encoded result too; limiting text alone does not bound JSON overhead.
- Preserve cancellation and deadline errors. Check cancellation before publication
  and before returning success. A wrapper cannot interrupt a codec that ignores
  context; use a bounded worker when a hard execution deadline is required.
- Keep temporary files under an owned directory. Publish only complete results,
  preserve existing destinations and report failed cleanup alongside the main error.
- Choose providers explicitly. Do not install programs, add hidden retries or
  switch providers after failure without a documented operation contract.
- Keep network policy explicit. Native guarded HTTP and an external downloader's
  own requests have different boundaries. URL syntax validation alone is not DNS,
  redirect or subprocess network isolation.
- Return measured output paths, sizes, backend and format when relevant. Keep
  useful failure evidence with the error. A size-limited partial output is a
  failure, even if an external program exits successfully.

Descriptions and output schemas live in the shared adapter. Describe fields used
by later workflow steps, including nested items. Results may differ by operation;
a universal result envelope is not required.

Read-only, concurrent and retry-safe are separate declarations. A read-only
operation can still use mutable service state. A mutating operation may have
effects before failure. Declare each property from actual behavior and use the
same read-only decision in both frontends.
Shared operation contracts declare `RetrySafe` explicitly; node adapters consume
that declaration rather than deriving retries from `ReadOnly`. Contract parity
checks assert expected concurrency, read-only and retry policies separately.

## Files and names

Use lowercase Go package names and ordinary Go filenames. Name a file after what
it contains: a provider implementation, archive listing, output publication or
request validation. A file named after one format should not own dispatch for
every format. Keep operating-system suffixes on platform implementations.

The file-operation package provides naming examples for a
[service](../../internal/capabilities/files/files.go),
[shared JSON contract](../../internal/capabilities/files/contract/contract.go),
[tool wrapper](../../internal/tools/documents/documents.go) and
[workflow descriptors](../../internal/capabilities/nodes/nodes.go).
Use these roles when they exist. Split further only when a file has distinct
responsibilities. Avoid unrelated names such as helpers or common.

Use consistent exported names within a capability. Prefer `Request` and `Result`
for a single operation, and operation-specific names for several different
requests. Choose `Validate` for validation and normalization; avoid different
names for the same role across new services. Preserve public tool names, node
names and stored recipe compatibility when moving internal packages.

## Verification

Cover service limits, cancellation, malformed inputs, destination preservation
and failure cleanup. For generated outputs, test that authorization finishes
before the first write. Verify tool and node contracts, resource normalization,
read-only modes and lazy exposure together.

Keep deterministic tests independent of installed programs and public services.
Mark live compatibility checks separately. Model evals check discovery and tool
choice; they do not replace service tests or establish reliability from one run.

Package-layer checks must cover each capability and support package added to the
tree. The package-layers convention check covers every capability package and its
adapters, plus shared support packages. It also rejects a new capability package
without an import rule. The compiler alone does not enforce those boundaries.
See [testing](testing.md) for commands and [conventions](conventions.md) for the
existing enforced boundaries.

## Agent and workflow coverage

| Shared service | Agent tools | Workflow nodes |
|---|---|---|
| Documents | DOCX, XLSX and PDF readers | Same operations |
| Files | Downloads and archive operations | Same operations |
| Images | Inspect, preview, variants and pixel comparison | Same operations on workspace inputs |
| Media | Status, video metadata/captions/downloads and local media processing | Same operations |
| Social | Public-post reading | Public-post reading |

The social Agent name remains cap__social__read_post for saved activation
compatibility. Its node is social_read_post and uses that existing permission
identity. Their JSON contract and implementation are shared. A configured Agent
social pack may replace the built-in provider; it does not replace the built-in
workflow node.

Some Agent tools own session interaction: image attachment grants, displayed
pixels, questions, history and live background jobs. A workflow does not inherit
those objects. An equivalent node needs explicit inputs and lifetime ownership.
Artifact, review, record and scheduling operations also retain their workflow
ownership where it matters.
Records already share one contract and execution across both frontends. Their
schema registration and persistence remain owned by the workflow store.

Workspace text/file operations, structured HTTP requests and isolated data
transforms need a shared contract when a second frontend is introduced. This is
an extension rule, not missing implementation in the current feature set.
Extract the shared operation before exposing that frontend; preserve permissions,
credentials and output bounds. Do not implement a second version behind a
matching name.
