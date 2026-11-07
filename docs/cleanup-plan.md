# Codebase cleanup

This is the active cleanup plan. Pause new capabilities until this pass is
finished. Update the checklist as work lands. Keep lasting explanations in
ARCHITECTURE.md and conventions.md; remove this plan when it is complete.

## What we want

A reader should be able to find the code for a task, follow its execution and
see who owns its state and resources. Ordinary changes should touch a small,
predictable part of the project.

- Prefer direct calls and concrete types.
- Give each responsibility one owner.
- Share behavior across frontends where it must agree.
- Keep provider details out of the agent loop and application effects out of
  reusable UI controls.
- Extract a helper when it names a useful operation or removes duplication.
  Do not split code just to meet a line limit.
- Keep a small package when its boundary is useful. Do not create a package
  for every helper or a framework for every repeated pattern.

This is a cleanup, with one confirmed bug fix. Further behavior changes need
their own explanation and regression test. No new nodes, prompt expansion,
dependency upgrades or changes to eval scoring belong in this work.

## Boundaries to preserve

These are responsibilities, not a request to rename every package.

| Area | Owns | Keep outside it |
|---|---|---|
| cmd/gektor | Arguments, launch choices and console interaction | Agent rules and provider protocols |
| app | Constructing services and binding configuration | Terminal rendering and model decisions |
| roles | Tool selection, role limits and policy wiring | Frontend-specific behavior |
| agent | Model turns, tool ordering, steering and recovery | Browser implementations, concrete tools and provider quirks |
| llm and protocol adapters | Messages, requests, streaming and protocol errors | Workspace access and tool execution |
| providers | Provider metadata, inspection and accounts | Agent control flow |
| run and events | Programmatic execution, operator gates and frontend events | Terminal layout and durable conversation storage |
| session | Saved conversation, model context and run records | Running tools and opening connections |
| workspace and permission | Rooted file access and authorization decisions | Decisions based on model wording |
| tools and integrations | Tool adapters, external services and capability activation | Agent-loop policy and UI state |
| automation | Recipe validation, execution, records and scheduling | Terminal controls and a second agent loop |
| tui/components | Shared rendering, geometry and local interaction | Config persistence, network calls and workflow execution |
| tui screens | Screen state, domain actions and component composition | Copies of shared execution rules |

For every resource, identify who creates it, who closes it and what cancellation
does. For saved state, identify who writes it and how older sessions load.
Record these facts near the owner; do not add a parallel ownership framework.

## 1. Protect the starting point

- [x] Start implementation on a dedicated cleanup branch based on the completed
  native-file work. Preserve those capabilities.
- [x] Record the baseline commit and checks in the implementation report.
- [x] Identify existing tests for the boundaries being changed. Add missing
  behavior coverage before moving code that relies on subtle ordering.

Implementation starts on codex/codebase-cleanup at bbf5a2e. Agent history,
reliability, repeat, inbox, steering, overflow and prefix tests cover the first
lifecycle changes. New completion tests cover the gap found in review.

The reviewed baseline passed `go test -race ./...`, `go vet ./...` and
`go run ./cmd/eval -dry`. The dry run validated 28 probes; it did not execute a
model or measure its quality. Recheck the baseline if the starting code changes.

## 2. Fix the completion bug

- [x] Add a permanent regression test for a notification arriving during the
  tool-free answer used after repeated tool calls.
- [x] Check the same boundary with pending operator input, with and without an
  inbox, and near the step limit.
- [x] Make normal and forced answers apply the same completion rules. Preserve
  accepted input and count each model request correctly.

Done when the arriving notification is handled before completion, or retained
under an explicit existing limit/error outcome. It must not silently disappear
from the finishing decision. Commit this behavior fix separately from refactors.

## 3. Repair documentation checks

- [x] Remove stale internal/harness references from the conventions, layer check
  and configuration-test comment. Do not recreate the deleted package.
- [x] Add native file capabilities to the architecture description.
- [x] Check explicit repository paths in maintained documentation. Cover root
  guides, docs, skills/README.md, eval/README.md and example/demo guides.
- [x] Handle globs, example workspace paths, generated paths and external paths
  explicitly. Do not treat every backticked token as a repository filename.
- [x] Test that an obsolete package path fails the check.
- [x] Correct the identifier check's documented guarantees. If strengthened,
  use parsed declarations for the forms it supports rather than substring
  matches across source text. Test false positives as well as missing names.
- [x] Make the layer check reject a rule naming a package that no longer exists.

Done when checks catch the confirmed drift and their stated scope matches what
they actually validate. Do not silence ordinary mistakes with baseline entries.

## 4. Make the agent lifecycle readable

- [x] Extract finalization from agent.run without changing its ordering.
  Preserve verification, cancellation, workspace observation, warnings and
  persistence on every exit path.
- [x] Separate setup from model-turn execution. Keep ownership of deadlines,
  history and run state visible at the call site.
- [x] Consolidate completion handling after the bug fix is covered.
- [x] Make step accounting easy to follow. Treat changes to counting and limits
  as a separate change from moving code.
- [x] Review executeToolCalls by phase: validation and authorization, admission,
  execution, ordered result attachment and mutation observation. Keep exclusive
  barriers and cancellation behavior intact.
- [x] Review the 50-field Config and roles.Env together. Group fields only where
  callers become simpler. Do not replace explicit callbacks with a generic
  hook registry or deeply nested configuration.
- [x] Investigate browser save/restore hooks. Move browser-specific wiring out
  of the loop only if existing saved sessions and lazy activation still work
  through a small compatibility path. Otherwise document and defer this seam.

Done when setup, turn execution and finalization can be read separately, and
existing history, prefix, overflow, steering, timeout and ordering tests pass.

Keep Config and roles.Env explicit for this pass. Role constructors already own
the wiring; nested option groups would change many callers without removing a
second owner. Tool validation and resource selection now have named helpers;
admission, exclusive barriers and result ordering remain together.

Defer browser snapshot migration. The saved profile exists even when the browser
capability has never been activated. Capability records describe activated tool
catalogs, so folding the profile into them needs a state-format and compatibility
decision, rather than a behavior-preserving code move.

## 5. Simplify launch and transport code

- [x] Split runAgentLaunch into configuration resolution, service construction,
  frontend selection and final reporting. Keep cleanup beside resource creation.
- [x] Check failure during partial startup: connections and background resources
  must be released even when a later setup step fails.
- [x] Compare CLI, TUI, RPC and eval construction. Remove duplicated rules that
  can drift; retain frontend-specific input and display handling.
- [x] Split Chat Completions streaming into wire parsing, response accumulation
  and tool-call assembly where these have clear inputs and outputs.
- [x] Preserve partial responses, replay data, usage, finish reasons and the
  rule that incomplete tool calls never execute.
- [x] Simplify eval main/runOnce by setup, execution and reporting. Keep probe
  criteria, scoring, backend-unavailable handling and results unchanged.

Done when startup ownership is explicit and transport cleanup does not change
the requests or events visible to callers.

## 6. Clarify tools and integrations

- [ ] Trace registration, activation, permission checks and cleanup for built-in,
  browser and MCP tools. Give each lifecycle step one owner.
- [ ] Review browser/MCP code alongside mcpmanager before proposing package moves.
  Separate transport from adapters only where it reduces coupling and does not
  introduce forwarding layers or import cycles.
- [ ] Move the FxTwitter reader into integrations if the adapter and capability
  registration remain small. Preserve on-demand activation, provider disclosure
  and configured replacement by ID.
- [ ] Check existing file and image integrations for duplicated schemas, limits,
  path rules or permission logic. Keep shared operations below their agent and
  workflow adapters. Avoid a universal operation framework.
- [ ] Check package-level maps/slices and panics individually. Keep lookup data,
  bounded synchronized caches and embedded-asset invariants when justified.
  Fix actual unsafe mutation or recoverable failures, not counts.

Done when adding one integration has an obvious home and does not require
editing agent internals or copying permission logic.

## 7. Separate workflow responsibilities

- [ ] Review recipe composition in phases: loading, binding inputs, naming and
  scope, dependency validation and frozen executable definition.
- [ ] Keep execution in the engine, persistence in the store and orchestration
  in the service. Check API, daemon, agent tools and TUI use these same owners.
- [ ] Check schedule creation, daemon ownership and run records without adding
  a second scheduling mechanism.
- [ ] Keep permission decisions consistent for direct and nested workflows,
  retries and generated output paths.
- [ ] Confirm recipe, run, template and schedule retain distinct meanings in
  code and docs. Document current support; do not add missing features here.

Done when the same recipe has the same execution and permission rules through
the CLI, agent and TUI. Keep automation in the current Go module.

## 8. Simplify TUI screens around shared controls

- [ ] Review key, command and event routing. Extract substantial related actions;
  keep readable dispatch switches. Do not add a command framework.
- [ ] Separate workflow actions, async results, canvas interaction and rendering.
  Keep domain execution in the workflow service.
- [ ] Review picker navigation and shared modal/list/form behavior. Remove old
  parallel implementations when an existing component already owns the job.
- [ ] Check against tui-design.md and the FigCraft reference. Preserve uppercase
  shortcut labels, gaps between shortcut items, compact footers and theme roles.
- [ ] Keep components free of application effects. A control returns an intent;
  its screen performs the action.
- [ ] Recheck graph interaction far from the origin, scrolling and hit regions.
  Add targeted measurements only if performance changes or a regression appears.

Done when shared controls behave consistently and screen code mainly connects
domain state to components. Keep the TUI in the current module; extract no
standalone UI library during this pass.

## 9. Make the result easy to navigate

- [ ] Put a short package map and one-agent-turn explanation at the top of
  ARCHITECTURE.md. Split dense sections by responsibility; preserve invariants.
- [ ] Explain run versus session where their responsibilities are introduced.
  Rename a package only if the benefit outweighs changing every caller.
- [ ] Add short package comments where a boundary is hard to infer, especially
  workflow engine/service/store and integration adapters. Avoid boilerplate.
- [ ] Keep the useful small packages: conventions is test infrastructure; rules
  holds language checks; audit wraps gate decisions; gektor centralizes user
  paths. Change them only if a concrete ownership problem is found.
- [ ] Keep root launchers and the existing documentation paths. Remove dead code
  only after checking platform files, embedding, registrations and callers.
- [ ] Keep learning marked experimental. No redesign of retrospect is required.
- [ ] Check README and docs navigation, examples and demos after moves.
- [ ] Finish the checklist, move any remaining useful contracts into their
  canonical documents, then remove this temporary plan.

Done when a contributor can find where to add a tool, node, provider or control,
and where execution, permissions, persistence and cleanup are owned.

## How changes land

Use one concern per commit. Separate bug fixes, code moves and semantic changes.
Existing tests are evidence to preserve, not a substitute for understanding the
contract. Add regressions for discovered bugs; do not test helper structure.

Run focused tests for each change. Across package boundaries run
`go test ./...`, `go build ./...` and `go vet ./...`. Run the full race suite
after concurrency/lifecycle changes and at completion. Run eval dry validation
after eval, documentation or registration changes and at completion. Live-model
evals are needed when model-facing behavior changes; this plan does not authorize
changing prompts or scoring to improve a result.

Investigate new failures before continuing. Report skipped platform or live
checks explicitly. Keep package rules and their documentation aligned when a
boundary changes.

A conditional item can finish with a short evidence-based decision to leave it
alone. The pass is complete when every item has been fixed, checked and retained,
or explicitly deferred with a concrete reason. Do not turn it into an endless
refactor or a new permanent backlog.
