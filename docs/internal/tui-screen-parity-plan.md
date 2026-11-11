# TUI screen ownership and parity plan

Date: 2026-11-11
Status: Planned; implementation has not started.
Baseline: `49f4a39` on `codex/tui-screen-split`.

This is an internal implementation plan. The maintained description of the
current terminal architecture lives in [Terminal architecture](../development/tui.md).
Apply the project's [Readable Go skill](../../.agents/skills/go-readable/SKILL.md)
when implementing this plan.

## Problem

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

## Implementation sequence

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

Update [Architecture](../../ARCHITECTURE.md),
[Terminal architecture](../development/tui.md), and affected testing guidance
to describe the completed implementation. Keep this document marked as a plan
until the work is complete.

## Acceptance and verification

The work is complete when both screens can be understood independently, the
parent owns application decisions, equivalent responsibilities have predictable
homes, and no broad forwarding layer conceals the former structure. Review the
reader's path as well as the dependency direction; passing tests alone does not
establish readability.

Run focused checks during the work, then verify the completed change from the
repository root:

```sh
go test ./...
go vet ./...
go build ./...
go test -race ./...
go run ./cmd/eval -dry
```

Inspect the final diff for unrelated changes and report any skipped verification.
Commit the completed pass on the existing branch. Preserve the user's skill-file
move and reinforcement; avoid unrelated documentation or skill changes.
