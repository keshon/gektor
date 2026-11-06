# Workflow editor: remaining work

Status: 2026-11-06. This is a working backlog, not a promise to implement every
possible workflow feature. The engine and first TUI editor are implemented;
the next work should make everyday editing reliable and comfortable.

## Already available

- Version 3 recipes, validation, sequential graph execution, conditions,
  iteration, retries, review gates and persistent run records.
- CLI execution, background queue, HTTP API and webhooks; reusable subworkflows,
  JavaScript transforms, optional model nodes and structured records.
- TUI canvas with selection, dragging, keyboard movement, panning, overview and
  automatic layout. Canvas positions are saved separately from recipes.
- Registry-backed node selection and schema-generated argument forms.
- Recipe open/save, JSON editing, behavior settings and recipe undo/redo.
- Run, cancel, inspect stored runs and approve/reject waiting operations.
- AI recipe proposals with explicit review before applying changes.
- Shared forms, pickers, scrollbars and themes. The latest changes add a JSON
  file chooser and schema-based destination argument selection for connections.

See [workflow behavior](workflows.md) and [editor controls](tui.md#workflow-workspace)
for the current contracts.

## Connection editing implemented

`B` or Node → Connections lists links for the selected node. Create, edit
and remove use the shared forms, including current-value previews. Nested
argument, condition and iteration references are included. Source output schemas
offer pointer hints; manual pointers remain available for dynamic results.

Cycle checks and one-change undo/redo apply to every connection mutation.
Removing a required argument surfaces a node validation error. Stored runs and
stale forms cannot edit the draft. Draft replacement decodes into a fresh value,
so removing optional fields does not retain their previous values.

Automated coverage exercises creating, changing and removing a two-node data
connection without full recipe JSON, exact undo/redo, nested references, stale
drafts, frozen runs, invalid pointers and cycles. Native interactive acceptance
remains outstanding.

## Recipe setup implemented

`M` edits the name, `U` manages individual typed inputs, and `O` manages named
outputs. Input declarations reuse schema forms; defaults and output values use
JSON fields for literals and references. Existing names stay fixed to avoid
implicitly rewriting contracts. Invalid defaults, duplicate names, stale forms
and edits to frozen runs are rejected. Setup changes support undo/redo.

The recipe format has no description field; this pass does not add one.
Advanced JSON editing remains available for larger changes.

## Run inputs and validation navigation implemented

Run/validate offer generated fields or JSON for declared inputs. Fields preserve
literal strings and numeric precision; default, provided and omitted values remain
distinct. Both formats use the engine's input checks before submission. A queued
invocation is rejected if its draft changed before execution.

Validation errors use structured schema paths or explicit step IDs to focus the
node and inspector. Child errors can focus the authored parent call. Value paths
are shown where available; unidentifiable errors retain the selection. Tests
cover typed inputs, empty-string overrides, omissions, defaults, stale invocations
and schema/step error navigation.

## Canvas UX pass implemented

Workflow mode now has contextual Workflow, Node, Run and View menus. Add node
is visible in Node and the footer even at narrow widths. Recipe setup is grouped
under Workflow → Recipe settings.

The canvas supports dragging empty space, middle-button panning, horizontal
wheel events and Shift+wheel. Node dragging remains separate from panning.
Input/output ports expose connection state and open the mapping form by clicks.
Routes avoid cards, deduplicate ordering/data pairs and use connected corner
and crossing glyphs. Routing has a bounded search; an unroutable output port
shows `!` rather than drawing through a card. Layout changes never edit recipes.

The inspector toggles with Ctrl+T, closes with focused Esc or its pinned mouse
control, and wraps long values. Wide windows split; narrow windows show a full
width inspector. Automated tests cover routing, port hits, dragging, horizontal
scrolling, menus, inspector closing and resize geometry. Native terminal checks
below still apply.

## Verify before treating the TUI as finished

- Exercise the complete create → connect → save → reopen → validate → run flow
  in Windows Terminal, using both keyboard and mouse.
- Check light/dark themes, outlined/minimal framing, small windows and resize
  while a picker or inspector is open.
- Verify review, rejection, cancellation and reopening a stored run. Confirm
  that completed effects are not replayed by review actions.
- Check dirty-draft handling across New, Open, return to Chat and application
  exit. Explicitly decide whether crash recovery/autosave is needed; an in-memory
  draft is not recovery storage.
- Try AI proposals with both a local model and the ChatGPT provider, including
  malformed output and stale drafts. Automated tests do not establish live model
  behavior or native terminal usability.

The latest TUI tests and executable build passed. Native interactive acceptance
of the latest chooser and connection changes remains outstanding.

## Later, only when needed

- TUI management for queue jobs, schedules and webhook configuration. Backend
  availability does not mean these have dedicated editor screens.
- Node duplication, richer source-output previews and canvas navigation polish.
- A dedicated web UI in a separate repository, using the existing service/API
  boundary rather than moving workflow ownership into the frontend.
- Parallel execution or a JavaScript package ecosystem would be separate engine
  projects. Neither is required to finish this editor; execution is sequential
  today.

## Delivery

The outline/error-recovery fixes and chooser improvements were committed in
`9d3424b`. Connection editing, recipe setup, run inputs, validation navigation and the
canvas UX pass are subsequent work. Finish
native interactive acceptance before treating the editor as complete.
Commit, push and release remain separate requested actions.

Next: exercise the complete editing-to-execution flow in a native terminal,
then test live AI proposals with local and ChatGPT providers. Queue/schedule UI
and further canvas polish remain later work, driven by those checks.
