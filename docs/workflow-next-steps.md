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

## Next: finish connection editing

Connections can be created, but removing or changing them still requires editing
node arguments, dependencies or the complete recipe.

- Add a connection list for the selected node, showing source, destination,
  argument and JSON pointer.
- Support removing an ordering dependency or data reference, and changing a
  mapping through the same controls used to create it.
- Show the current destination value before replacing it. Removing a required
  argument must leave a visible validation issue, not silently invent a value.
- Offer source fields from output schemas where available. Keep manual JSON
  pointers for dynamic outputs; schema hints are not runtime guarantees.
- Preserve cycle checks and recipe undo/redo for every connection change.

Acceptance: build, change and remove a two-node data connection without opening
the full recipe JSON; undo restores the exact previous definition.

## Then: reduce reliance on full recipe JSON

- Add dedicated recipe name, description, input and output controls.
- Reuse schema forms and JSON fallback fields rather than writing separate
  editors for each node type.
- Make validation errors easy to associate with a node and argument. Focus the
  affected node when the error contains a usable step identifier.
- Improve run input forms using declared recipe inputs; preserve a JSON option
  for complex values.

Advanced JSON editing should remain available. It should not be necessary for
routine recipe setup.

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

The outline/error-recovery fixes and latest chooser/connection improvements are
still uncommitted at this snapshot. Review the combined diff, finish the targeted
acceptance checks, then commit. Push/release is a separate requested action.

Recommended next implementation: connection inspection, editing and removal.
It closes the clearest gap in the current canvas without expanding engine scope.
