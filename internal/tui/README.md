# Working on the TUI

The `tui` package coordinates two peer screens: Agent and Workflow.
Use the [Readable Go skill](../../.agents/skills/go-readable/SKILL.md) when
changing their implementation or boundaries.

## Ownership

- **`tui` (this directory):** terminal lifetime, screen selection, application
  settings transactions, shared overlay lifecycle, quit/relaunch policy, and
  final composition.
- **`agent`:** conversation, composer, sessions, tool inspector, approval gates,
  and Agent execution.
- **`workflow`:** recipe editing, graph and inspector, scheduling, and Workflow
  execution.
- **`screen`:** shared control state, menus, overlays, layout, and composition.
  The parent owns the `screen.Controls` instance; features supply their actions
  and presentation data.
- **`settings`:** reusable settings editors and installation preference
  persistence. Application decisions about applying settings stay in `tui`.
- **`components`:** reusable terminal controls and layouts, with their own local
  state and interaction.
- **`clipboard`:** desktop image acquisition and bounded PNG/DIB normalization
  for user-requested attachments, including platform-specific clipboard access.

Agent and Workflow do not import each other. Coordinate cross-screen behavior
through the parent. Share behavior when its semantics agree; equivalent screen
responsibilities have matching file names, without requiring identical APIs.

## Reading path

1. Start at [`tui.go`](tui.go) (`Run`) and [`model.go`](model.go)
   (`newModel`) for terminal lifetime and construction.
2. Follow [`update.go`](update.go) for message and input routing, then
   [`controls.go`](controls.go) and [`presentation.go`](presentation.go) for
   shared controls and final composition.
3. In either screen, start with `config.go`, `model.go`, and `api.go`.
   Follow `update.go`, `keys.go`, or `mouse.go` for the interaction being changed.
   `presentation.go` supplies shared-control data; `view.go` renders local content.
   Continue into the feature file that owns the operation.
4. For application configuration, follow [`preferences.go`](preferences.go),
   [`appearance.go`](appearance.go), and [`relaunch.go`](relaunch.go).

## Contracts to preserve

- Hidden screens may receive asynchronous results. Keyboard, paste, and pointer
  input follow the parent's active-screen, overlay, and approval-gate routing.
  A delayed result must not take over another screen's input or overlays.
- The parent owns shared overlays and restores their focus owner on close.
  Screen-local behavior stays with the screen that supplies the operation.
- Settings is available in both modes. Appearance applies immediately;
  model settings affect subsequent requests, and identity/instructions affect
  subsequent Agent turns. Existing history and explicit recipe node settings
  are not rewritten.
- Service-changing settings require both screens to be idle. Relaunch preserves
  conversation history and drafts; retained unsaved Workflow work blocks it.
  Appearance does not require relaunch. Workspace changes remain explicit.
- Trace startup, completion, cancellation, and close together when changing
  execution or lifetime ownership.

## Tests and verification

Keep local tests beside their screen or component. Root tests exercise application
composition: navigation, shared controls, settings, relaunch, and lifecycle.
`workflow_integration_test.go` belongs here because it tests the assembled parent.
Use production constructors and controls in fixtures; finish asynchronous work
before cleanup. Optional rendered previews live in `previews_test.go`.

Run from the repository root, choosing checks appropriate to the change:

```sh
go test ./internal/tui/...
go test -race ./internal/tui/...
go vet ./cmd/... ./internal/...
go build ./cmd/... ./internal/...
```

For interaction and layout contracts, see [Terminal architecture](../../docs/development/tui.md).
For the completed refactor's decisions and verification record, see the
[screen ownership plan](../../docs/internal/tui-screen-parity-plan.md).
