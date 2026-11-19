# TUI reuse and interaction audit

Date: 2026-11-19. Revision: `f20dbf6` on `codex/workflow-runtime-v4`.

## Assessment

GEKTOR has a reusable component core and a coherent visual language. It does not
yet have an application-independent TUI library. The main obstacle is the shared
dialog/form layer's dependence on GEKTOR settings. The main interaction problem
is the amount of chrome around the content, particularly in narrow terminals.

Keep the existing palettes, title/focus distinction, framed task dialogs, explicit
Apply/Cancel actions, top menus and keyboard/mouse parity. Improvements should
make these conventions easier to use and apply consistently across projects.

The initial audit made no production changes. The implementation follow-up is recorded below. The existing TUI tests pass. Rendered
fixtures and temporary probes provide the evidence below; they are not a manual
test of every screen in a native terminal.

## What is already suitable for reuse

| Code | Assessment |
|---|---|
| `components/cell`, `selection` | Small responsibilities, terminal-cell measurement and explicit state. |
| `components/popup`, `pane` | Shared framing, layout and hit geometry; callers supply styles. |
| `components/form`, `table`, `schemaform` | Useful generic cores. Address feedback, focus and layout behavior before publishing an API. |
| `components/composer`, `completion`, `transcript`, `graph` | Optional specialized components, with no imports of GEKTOR execution services. |
| `screen` | Useful menu/dialog composition, but imports `settings` and contains lesson/evaluation behavior. Requires separation. |
| `settings` | Combines generic form/picker behavior with provider, prompt and profile rules. Keep application-specific rules in GEKTOR. |
| root `tui`, `agent`, `workflow`, `tools` | Application consumers. The root launch config embeds Agent configuration and constructs GEKTOR services. |

All these packages currently live under `internal/`, so other repositories cannot
import them as a supported Go library. Moving code to a public module should
follow a second consumer that demonstrates the necessary API.

Existing strengths worth preserving include stable dialog identities, rejection
of stale replies, copied form inputs, intent events instead of retained feature
effects, shared drawing/hit geometry, and screen-owned cancellation with joined
cleanup. Agent and Workflow remain peer consumers.

## Findings

### 1. Generic forms inherit GEKTOR field-name behavior

Priority: high for reuse. See `screen/screen.go:81`, `settings/catalog.go:17`,
`settings/picker.go:89` and `screen/requests.go:9`, relative to `internal/tui`.

`screen.FormState` embeds `settings.Model`. A field called `model` opens provider
discovery; `app-workspace` selects special filesystem behavior. The same path
imports GEKTOR configuration, provider and prompt catalogs. A probe with a plain
field labelled "Vehicle model" confirmed that opening its picker starts provider
discovery solely because its key is `model`.

The screen layer also has `Review`, `Save` and `Evaluating` flags with default
wording such as "save lesson" in `screen/dialog_controls.go:84`.

Make field and picker behavior explicit data supplied by the consumer. Generic
forms should accept fields, choices and picker results without interpreting
application field names. GEKTOR settings should adapt provider/profile catalogs
to those controls. Dialogs should receive action labels and IDs from their owner.
The existing event/identity mechanism can remain; no plugin framework is needed.

### 2. Rendering a table changes interaction state

Priority: medium. See `components/table/layout.go:85`.

`Layout` closes More and moves focus to the fallback action when the terminal is
too short. Restoring the size does not restore the previous focus. The same method
also clamps selection and updates the search editor. It is invoked by rendering,
status hints, action availability and mouse handling.

A probe started with filter focus and an open action menu, rendered at height 4,
then restored height 24. Focus remained `2`, More was closed, and Enter activated
the first action. No input event requested that focus change.

Keep interaction normalization in an explicit update/resize operation and preserve
the prior focus while displaying an unusable-size fallback. Drawing and hit
testing should consume the same measured layout without changing the input owner.

### 3. Modal chrome repeats instructions and wastes narrow-screen space

Priority: medium. See `agent/composer.go:20`, `agent/status.go:21`,
`screen/dialog_actions.go` and `components/table/layout.go`.

The MCP fixture at 40 by 20 displays one server. Around it are the modal hint,
multiple button rows, an underlying composer hint, "Draft preserved", the pane
frame and the bottom action bar. Some repeated chrome belongs to the application
around the table, not to the table alone.

A settled root-model preview confirms the repetition in Theme at 80 by 24 too:
the modal contains shortcuts and buttons, the underlying composer advertises
Esc/Tab/Ctrl+O, and the action bar repeats Apply/Cancel. Ctrl+O newline is advertised
even while a radio choice has focus.

Give each active overlay one authoritative action presentation. Keep explicit
buttons; show additional editing hints only when they describe the focused
control. Reduce the underlying composer to a compact retained-draft indication
when that fact is useful. Let narrow tables move secondary actions into their
existing More menu so data receives the recovered rows.

### 4. Form errors wrap, then silently lose their remaining text

Priority: medium. See `components/form/view.go:70` and `components/form/form.go:12`.

Feedback is wrapped and sliced to the available row budget. There is no disclosure
or scroll path for the omitted text. A 40-by-20 probe with a long validation error
confirmed that its final actionable sentence never appears. Fields also have no
associated error value; the renderer only accepts one form-level error.

Retain the complete error and offer a details view when it cannot fit. Add field
feedback where a caller can identify the field, keeping form-level errors for
cross-field or persistence failures. Preserve the draft and the current editing
context. This implements the existing design reference's stated error behavior.

### 5. Table selection looks focused while another control owns input

Priority: medium. See `components/table/layout.go:100`.

The current row always receives the selected fill and `›` marker. Its appearance
does not change when focus moves to Filter or an action button. A probe with
`Focus = 1` still rendered `› One` with the selected style.

Keep the selected row identifiable, but give the active input owner the strongest
focus treatment. A quieter retained selection should differ from a focused row.
Preserve non-color markers for both facts. This matters especially when the
filter caret or a button is far from the selected row.

### 6. Layout APIs impose the same width on different reading tasks

Priority: medium. See `components/popup/popup.go:20`,
`components/table/layout.go:27` and `components/pane/pane.go:30`.

Tables, pickers and short forms all use the 80-cell modal maximum. A table in a
120-column area still measures 80 columns. Split panes always divide the area in
half, while the side-by-side threshold is supplied separately.

The 80-cell default is appropriate for short forms. Let a content-heavy table or
inspector request a wider or maximized view, clamped by the common layout owner.
Start with that simple choice; add drag resizing only if a real consumer needs it.
This also avoids making every application invent its own geometry workaround.

### 7. Help, hints, menus and bindings describe actions separately

Priority: medium as an improvement. See `workflow/keys.go:159`,
`workflow/actions.go:345`, `workflow/menus.go`, `screen/navigation.go` and
`screen/dialog_actions.go`.

Workflow help is a long literal string. Menus and the action bar have separate
labels and availability, while direct keys have separate dispatch. Agent has
slash-command discovery, but that is not a shared action browser for both screens.
The empty Workflow preview also offers Edit/Connect in the wide footer despite
having no node; its empty-state guidance is more useful than those actions.

Reuse action IDs, labels, bindings and availability already supplied by each
feature. Derive contextual help and hints from that data where practical, keeping
the actual effect in the feature. Add a searchable "Actions…" entry available in
both screens. Choose its shortcut after checking existing text-editing bindings.
Avoid a universal command bus or a new hierarchy of command types.

### 8. Theme and terminal assumptions need an explicit library policy

Priority: lower for GEKTOR; resolve before publishing the shared shell.
See `screen/styles.go:88` and `screen/paint.go:11`.

The selected palette is global process state. It is race-safe, but two preview or
host instances cannot select independent themes. Low-level components already
accept styles; the shared screen layer consults the global palette while drawing.
`Paint` also emits truecolor sequences unconditionally, independently of a host's
color policy. The probe confirmed this emission; limited-color terminals were not
tested. No claim of an observed native-terminal failure is made here.

Let a reusable host own a palette snapshot and terminal color policy, while
GEKTOR retains its saved-theme behavior. Add measured palette checks and a reduced-
color rendering case before claiming broad portability. Tiny-area guards also
need consistency: `ModalWidth(2)` returns 4, although later composition clips it.

## Competitor patterns that fit the design language

- **Lazygit:** search/filter follows the focused view, with explicit next/previous
  match navigation. Its keybinding menu and normal/half/fullscreen modes suggest
  searchable contextual help and a larger reading view. Keep GEKTOR's existing
  keys while adopting the discoverability and space allocation.
  [Search behavior](https://github.com/jesseduffield/lazygit/blob/master/docs/Searching.md),
  [keybindings](https://github.com/jesseduffield/lazygit/blob/master/docs/keybindings/Keybindings_en.md).
- **K9s:** visible context and resource navigation, active-key help and Escape
  back to the previous view. A concise context path for nested lists/details/forms
  would make GEKTOR's retained overlay state easier to understand. The existing
  dialog identity/lifetime machinery is a useful foundation.
  [Navigation and help](https://github.com/derailed/k9s#key-bindings).
- **Yazi:** explicit selection versus opening, and direct actions for copying a
  full path, directory or filename. GEKTOR already distinguishes browsing from
  selecting a folder; extend that clarity to clipped paths, IDs and record values.
  [Navigation and copy actions](https://yazi-rs.github.io/docs/quick-start/).

These are recommendations based on the projects' documented interactions, not
side-by-side hands-on usability measurements. Preserve GEKTOR's menus, palettes
and plain symbols; wholesale keymap or visual replacement is unnecessary.

## Order of work

1. Fix table render-time focus changes, incomplete error access and misleading
   modal hints. Verify resize, focus transfer and keyboard/mouse action parity.
2. Simplify overlay chrome and narrow action groups. Keep enough rows for useful
   content at 80 by 24 and a usable reduced view at 40 by 20.
3. Separate generic forms/pickers from GEKTOR's settings catalogs and replace
   lesson-specific shared-dialog defaults with caller-supplied actions.
4. Build a small second consumer: a CLI with a task list, detail pane, edit form,
   confirmation, asynchronous refresh and theme selection. It must use the shared
   controls without importing GEKTOR services, providers or configuration.
5. Use that consumer to choose a small public module boundary and add concise
   examples. Then add searchable actions and wider reading views as shared features.

The second consumer should demonstrate actual reuse before an API is declared
stable. Existing Agent and Workflow screens remain application code.

## Verification and limits

`go test -p 1 ./internal/tui/...` passed across the root shell, both screens,
settings, controls and all component packages. Existing preview exporters produced
temporary fixtures under `D:/Temp/gektor-tui-audit-2026-11-19`. Rendered views
included light/dark dialogs, MCP at 80 by 24 and 40 by 20, the queue editor and
composer; additional root-model probes exported settled Theme and empty Workflow
views at widths 40, 80 and 120.

Small temporary probes reproduced findings 1, 2, 4, 5 and 6 and the unconditional
color emission in finding 8. Probe source files were removed from the checkout;
copies and render artifacts remain in that temporary directory. Rendering ANSI
fixtures to images is an approximation of font/color presentation; native console
decoding, live mouse feel, screen readers, limited-color terminals and Linux/macOS
were not manually exercised. No live model or external service was used. No new
production concurrency was introduced, so no additional race run was needed.

## Implementation follow-up

The user chose to retain the existing chrome. Finding 3 and the proposed chrome
reduction are excluded from this pass: palettes, frames, button groups and footer
structure remain. The changes address interaction and ownership within that design.

- Forms and pickers now live in `components/fields`, below `screen`; neither
  imports settings, providers, profiles or execution services. GEKTOR's settings
  adapter supplies its own behavior. Neutral field-name tests cover the keys that
  previously triggered application-specific behavior.
- Shared information dialogs accept action IDs, keys and labels from their owner.
  Lesson/evaluation and Workflow proposal behavior no longer lives in controls.
- Table layout preserves owner state through resize and drawing. Retained row
  selection differs from active keyboard focus, including while More is open.
- Complete form errors and field-specific feedback are available through Ctrl+E.
  The details view scrolls and returns without replacing the dialog or draft.
- F11 expands tables, information dialogs and both inspectors. Short forms keep
  their default width. Width requests and split geometry are explicit and bounded;
  tiny modals no longer exceed their available width.
- F12 and Help > Actions expose enabled commands from the current menu data.
  The application rechecks availability before dispatch and ignores stale results.
  Empty Workflow menus and footers disable actions that need an existing node.
- Hosts can use independent palette snapshots and terminal color profiles. Tests
  cover two differently themed hosts and ANSI, ANSI256 and color-free composition.

These changes prepare reuse inside the current repository. The second CLI consumer
and public module are still separate work; no stable external API is claimed.
Native-terminal interaction, alternate operating systems and screen-reader behavior
remain manual verification items. Automated verification is recorded with the
completed change; live workflow/model acceptance remains paused independently.

### Follow-up verification

- `go test -p 1 -timeout 180s ./cmd/... ./internal/...`
- `go test -race -p 1 -timeout 180s ./internal/tui/...`
- `go vet -p 1 ./cmd/... ./internal/...`
- `go build -o D:/Temp/gektor-tui-fixed-2026-11-19/gektor.exe ./cmd/gektor`
- `npm run check` in `website` (documentation paths, anchors and publication tests).

Opt-in preview tests exported theme fixtures plus Actions and complete-error views
at widths 40, 80 and 120 under `D:/Temp/gektor-tui-fixed-2026-11-19`. Inspection of
the ANSI output and approximate images checked narrow action names, the final
error remedy and preserved theme controls. This is rendered-fixture verification;
it does not replace testing keyboard and mouse behavior in a native console.

### Scrollbar follow-up

Forms, pickers, tables and complete-error details had click-only scrollbars.
They now capture thumb drags through motion and release outside the control.
Shared table dialogs also forward motion and release in local coordinates.
Form tracks use the visible field window, including wrapped choices and hidden
fields, rather than assuming three terminal rows per field.

Information dialogs, completion popups and the Workflow inspector share the same drag handling.
A changed track cancels its grab; terminal resize also cancels transcript,
Agent inspector and session-browser grabs. Track clicks page without activating
buttons or accepting picker choices. One-cell tracks can reach either end by
dragging above or below the track.

Regression tests cover drawn thumb positions, both screen hosts, widths 40–140,
card and table rows, expanded and minimal inspectors, out-of-bounds movement,
buttonless release events, resize cancellation and preservation of drafts.
Existing completion, transcript, Agent inspector and session-browser drag tests
remain part of the TUI suite. Native-console mouse feel remains a manual check.
