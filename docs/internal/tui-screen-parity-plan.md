# TUI screen ownership and parity plan

Date: 2026-11-11
Status: Complete on `codex/tui-screen-split`; scope is `internal/tui` and this document.
Baseline: `49f4a39` on `codex/tui-screen-split`.

This document records the completed ownership cleanup and settings decisions.
The broader terminal guide lives in [Terminal architecture](../development/tui.md).
Apply the project's [Readable Go skill](../../.agents/skills/go-readable/SKILL.md)
when implementing this plan.

## Completed ownership and settings behavior

The parent owns navigation, settings transactions, overlay lifecycle, terminal
modes, and final composition. Agent and Workflow each provide their own view,
presentation data, commands, and cleanup. Workflow no longer derives its
presentation from Agent. Equivalent construction, update, keyboard, and rendering
responsibilities have matching file homes; shared-control forwarding layers and
the `externalControls` arrangement have been removed.

Settings is available identically in both modes:

- Application: Appearance.
- Shared model: Model connection, Response settings, and Launch profiles.
- Agent configuration: Identity and persona, and Agent instructions.

These are conceptual groups, separated by menu rules. The menu has no inert
heading rows that resemble clickable items.

Session and Recipe retain their current-object scope. View contains local screen
display controls. Change workspace remains explicit application navigation.
Agent's Run menu exposes permission and MCP inspection alongside its other
execution controls.

There is one active application configuration, remembered across launches.
Appearance applies immediately to both screens. Model settings apply to subsequent
requests, including Workflow AI drafting; Agent identity and instructions apply to
subsequent Agent turns. Existing messages and explicit recipe node settings are
not rewritten. Reopening a conversation uses its saved history with the current
configuration, rather than restoring a separate configuration snapshot.

Configuration changes requiring new services are accepted only when both screens
are idle. The existing relaunch path retains conversation history, unsent input,
attachments, and saved-session identity. Unsaved Workflow drafts block relaunch;
Appearance can still change without losing either draft. History is read before
writing a custom profile, preserving failure ordering. Delayed Agent UI results
cannot take over Workflow input or overlays.

## File and test ownership

Agent and Workflow both use `presentation.go` for their presentation data and
`mouse.go` for pointer interaction, alongside the existing matching `model.go`,
`config.go`, `api.go`, `update.go`, `keys.go`, `view.go`, `menus.go`, and `actions.go`.
Feature-specific files remain different where responsibilities differ.

Tests follow the same ownership boundaries:

- Parent tests cover navigation, composed frames, application settings, relaunch,
  and shared overlay/input routing. `workflow_integration_test.go` stays here
  because it exercises the assembled application, including both screen types.
- Screen tests cover local behavior. The Workflow test host now uses production
  `screen.Controls`, including its forms, input handling, and close callbacks.
  It no longer implements a second form/dialog system.
- Component tests live beside their components. Popup overlay regressions and
  transcript repeat-update behavior were moved out of Agent.
- Preference-write caching is tested beside `settings.Application`; package
  dependency checks live at the TUI root.

Agent's catch-all `tui_test.go` was redistributed into feature test files, and
Workflow's extraction-era `screen_*_test.go` files now use feature names.
Optional parent render exports are collected in `previews_test.go`. Unused
fixture helpers were removed.

Three source-shape checks tied to palette filenames and a hardcoded list of
rendering method names were removed. Behavioral rendering tests and dependency
checks remain. The compaction regression now drives the Agent compaction action,
checking reduced context and preserved archive; the transcript repeat-update
test describes its actual content-preservation guarantee. A failing temporary
directory cleanup exposed two tests that left turns running; they now deliver
completion through the real parent update loop before cleanup.

## Baseline problem

Agent and Workflow have separate execution models, but their application
integration remains asymmetric:

- The parent starts presentation updates with `agent.Presentation()`, even while
  Workflow is active. Global settings, appearance, and shared help still come
  through Agent, which also installs shared-control lifecycle callbacks.
- Agent renders application menus, toolbar, and overlays inside its view.
  Workflow supplies content and the parent adds those controls.
- Agent retains shared-control wrappers and aliases, plus `externalControls`
  branches supporting different ownership arrangements.
- Equivalent responsibilities have different file homes: Agent has `keys.go`
  and `update.go`, while Workflow keeps both in `model.go`. Rendering lives in
  Agent's `screen.go` and Workflow's `view.go`; configuration also has different
  homes.

Feature-specific files are expected to differ. Sessions, attachments, and gates
belong to Agent; recipes, connections, and scheduling belong to Workflow. The
work below addresses ownership and navigation through equivalent responsibilities.

## Target ownership

| Owner | Responsibilities |
|---|---|
| Application parent | Terminal lifetime, active-screen selection, global settings and appearance, shared menu and overlay composition, application quit and relaunch policy |
| Agent screen | Conversation content, composer, sessions, tool inspector, gates, local interaction, Agent execution and cleanup |
| Workflow screen | Recipe editor, graph and inspector, local interaction, workflow execution and cleanup |
| Shared controls | Reusable control state, layout, rendering, and input handling; feature operations remain with their owners |

Choose the concrete settings owner after tracing its callers. Avoid a large host
interface, parallel configuration types, or a callback bundle that mirrors the
old Agent implementation. Screens can retain different APIs where their behavior
requires it; parity does not require a common interface.

## Original implementation sequence (completed)

### Scoped construction sketch

Keep `tui.Model` as the concrete parent of Agent and Workflow. It owns the
application settings editor, profile-change transaction and shared-control
lifecycle. `settings.Application` owns preference persistence; Agent supplies a
relaunch snapshot and applies display changes to its own render caches. The
existing relaunch payload remains the caller-facing handoff contract.

Both feature views supply content/cursor information. One shared controls
composition operation adds menus, toolbar, overlays and painting in the parent.
Workflow retains its editor, cancellation and result admission. Agent retains
its conversation, gates, sessions and task execution. No screen interface or
host callback bundle is added. Equivalent state/config/update/key/mouse/view
files are aligned within the existing packages.

### 1. Verify the proposed boundary

Sketch each owner's entry type, construction inputs, results, and cleanup before
moving files. Trace an Agent task, a Workflow edit/run, a settings apply/relaunch,
and quit with a retained dirty Workflow. Include failure and cancellation paths.

### 2. Move application settings out of Agent

Give global settings, theme/appearance persistence, and profile/workspace changes
a neutral owner. Preserve conversation state through an explicit Agent snapshot
operation when relaunching. Connect Agent-specific display and execution choices
through clear operations without exposing its mutable model.

Preserve staged settings, validation, temporary-launch preference behavior,
profile-copy persistence, and failure ordering. In particular, a failed history
read must not leave a newly saved custom profile behind.

### 3. Use one application composition path

Both screens supply content, local menu/action data, overlay bounds, and cursor
information where needed. The parent consistently adds application menus,
toolbar, shared overlays, final painting, and terminal modes.

Workflow presentation must no longer begin with `agent.Presentation()`. Shared
control lifecycle and input ownership must be coordinated explicitly. Hidden
screens may receive asynchronous results, but must not receive another screen's
paste or keystrokes.

### 4. Remove extraction leftovers

Review Agent's shared-control wrappers, aliases, and `externalControls` branches.
Call shared implementations directly where appropriate. Keep adapters only where
they express a real feature operation. Migrate tests to the actual owner rather
than preserving a second ownership arrangement for old fixtures.

### 5. Align equivalent file responsibilities

| File | Responsibility in each screen package |
|---|---|
| `model.go` | Screen state |
| `config.go` | Construction inputs |
| `api.go` | Operations exposed to the application |
| `update.go` | Message dispatch |
| `keys.go` | Keyboard interaction |
| `mouse.go` | Pointer interaction |
| `view.go` | Screen content and cursor |
| `menus.go` | Local commands, availability, and shortcuts |

Create these files only where there is meaningful code. Keep domain-specific
files different. Normalize constructor naming where it improves the caller's
reading path without creating unnecessary compatibility wrappers.

### 6. Migrate tests and documentation

Keep local behavior tests beside each screen. Exercise navigation, settings,
shared controls, and lifecycle through the real parent and constructors. Preserve
exact draft and submitted-value assertions when splitting transport, feature,
and parent-routing coverage.

Cover screen switching and retained drafts, hidden unsaved work, paste ownership,
resize and mouse geometry, settings relaunch, and completion after cancellation.
Preserve permissions, event ordering, cleanup on construction failure, and
cancellation of blocked receivers.

The completion record above documents the scoped changes. Broader architecture
and development guides were left outside this task's authorized file scope.

## Acceptance and verification

The work is complete when both screens can be understood independently, the
parent owns application decisions, equivalent responsibilities have predictable
homes, and no broad forwarding layer conceals the former structure. Review the
reader's path as well as the dependency direction; passing tests alone does not
establish readability.

Verification completed from the repository root:

```sh
go test ./cmd/... ./internal/...
go vet ./cmd/... ./internal/...
go build ./cmd/... ./internal/...
go test -race ./cmd/... ./internal/...
go run ./cmd/eval -dry
```

The source-package checks passed, and the dry evaluation catalog validated all
44 probes without model requests. Repository-wide `./...` discovery also includes
saved inputs under `eval/results` that import removed packages; those result files
were left untouched. The subsequent settings-menu changes were checked with TUI
race tests and source-package vet/build checks.

The ownership cleanup was committed as `b8e8f16` before the approved settings
navigation changes. All changes remain on the existing branch and within
`internal/tui` plus this completion record.
