# Terminal architecture and interaction contracts

Use this reference when changing terminal components or shared interactions. For user controls, see [Terminal controls](../reference/terminal.md).

## TUI design and components

This is the design reference for GEKTOR's terminal UI.
It covers Agent and Workflows, including their menus, dialogs and inspectors.
The component boundaries below describe the implementation. Verification lists
the checks needed before accepting a screen; it does not claim every screen
has passed them.

For current shortcuts and everyday use, see the [terminal guide](../reference/terminal.md).
For the application's state and service boundaries, see
[architecture](../../ARCHITECTURE.md#ui-and-settings). This document owns shared UI
appearance, interaction and layout decisions. It is not a record of past changes.

The editable [FigCraft project and mockups](../design/tui/README.md) show proposed
controls, screen layouts, interaction states and palette studies. Review them
before migrating a screen. Static mockups do not verify terminal input behavior.

## Scope and ownership

Reusable layouts are components too. They own spacing, alignment, scrolling
boundaries and hit regions, rather than leaving every screen to calculate them.

An application-independent piece is a candidate for reuse. Extract it when it
has a clear responsibility and removes duplication or prevents inconsistent
behavior. A helper used once can stay next to its caller. Reuse does not require
a separate package for every control.

| Level | Examples | Owns |
|---|---|---|
| Primitives | Text, button, field, checkbox, shortcut, divider | Drawing, local state and local interaction |
| Layouts | Modal, panel, split pane, scroll area, footer | Geometry, spacing, clipping and hit regions |
| Composed controls | Form, searchable list, file picker, inspector | Related controls and their navigation |
| Application screens | Agent, Workflows, Settings, MCP | Domain data, permissions, persistence and effects |

The same split layout can hold a conversation or a graph. The same list can
display models or sessions. Their application actions remain separate.
A component reports an intent such as accept, cancel or activate; its owner
decides whether that saves settings, opens a session or starts a connection.

`internal/tui` owns the application model and terminal runtime. It selects the
active screen, routes input and results, constructs both screens, and coordinates
quit protection across active runs and retained Workflow drafts.
`internal/tui/agent` owns its conversation, transcript, composer, inspector,
sessions, preferences, and Agent execution. Start at
[agent.go](../../internal/tui/agent/agent.go) for the package contract, launch
configuration, and constructor. Agent emits navigation and quit
intents to the parent; it does not import Workflow.

Agent transcript code stays private to the screen: [transcript.go](../../internal/tui/agent/transcript.go) connects
screen events to transcript state, [transcript_blocks.go](../../internal/tui/agent/transcript_blocks.go) normalizes entries,
[transcript_block_view.go](../../internal/tui/agent/transcript_block_view.go) renders individual blocks, [transcript_render.go](../../internal/tui/agent/transcript_render.go)
owns caching and tool hit regions, and [transcript_stream.go](../../internal/tui/agent/transcript_stream.go) handles live text.
The renderer receives presentation values instead of the application model.

The Agent session browser keeps selection, search, and its rename input in
[sessions.go](../../internal/tui/agent/sessions.go); keyboard and mouse routing
live in [sessions_input.go](../../internal/tui/agent/sessions_input.go).
[sessions_actions.go](../../internal/tui/agent/sessions_actions.go) coordinates
rename, delete, and workspace assignment, while
[session_store.go](../../internal/tui/agent/session_store.go) handles their disk rules.
[session_open.go](../../internal/tui/agent/session_open.go) owns active-session
replacement; [transcript_history.go](../../internal/tui/agent/transcript_history.go)
rebuilds saved messages into transcript blocks. The session library root is
captured when the Agent model is constructed and shared by scanning and creation.

Settings pickers emit `settings.DiscoveryRequest` values for remote model and
reasoning catalogs. The parent executes discovery with the launch context and
returns a `ScanMsg`. Opaque request IDs reject results from replaced pickers or
forms. Directory and detail commands capture their own input snapshots; keyboard
interaction, layout, and filesystem effects have separate files in `settings`.

Start at [screen.go](../../internal/tui/screen/screen.go) for shared controls,
[settings.go](../../internal/tui/settings/settings.go) for settings interaction,
and [workflow.go](../../internal/tui/workflow/workflow.go) for Workflow construction.

`internal/tui/workflow` owns its own model, recipe editor, graph, inspector,
run state, menu commands and availability, message dispatch, permissions, and
asynchronous operations. The parent supplies execution services, its concrete
`screen.Controls`, and a `screen.Surface` value. It updates the surface snapshot
before input and presentation so rendering and hit testing use the same geometry.

Shared controls own draft editing, table navigation, and presentation state. They
copy initial form choices and emit `screen.Event` values for submissions, table
activation, and overlay lifetime changes. Events retain the producing dialog's
identity; replaced dialogs cannot apply changes or close their replacements.
The shell drains events on the update loop and coordinates overlay focus.
Each owner retains one pending form: its dialog identity and a private submission
function beside the form fields. Replacing a form replaces that pending value;
late events cannot clear or submit the newer form. Agent's suspended queue editor
retains its own pending form until it resumes or is discarded. Permission reply
channels remain separate because multiple requests can await lifecycle delivery.
Shared controls retain no feature callbacks.

Feature owners prepare table rows and action availability on the update loop;
rendering does not refresh domain state. Controls preserve search and selection
when rows change. A retained table model returns to controls when its details
close. Replacement discards the old return action; dismissal runs it once.
Workflow denies unresolved permission requests on dismissal, replacement, or
shutdown. Agent can suspend a queued-message draft for a permission prompt and
resume it only in the same session.

The generic graph component renders supplied labels and tones; workflow status
names and presentation belong to Workflow.

Each screen owns its cancellation and cleanup. The terminal host closes Workflow
when the launch ends. Settings changes restart the launch with the new client
and policy. Returning to Agent retains the workflow draft and the Agent composer.

## Visual language

### Color and emphasis

Define styles by purpose, then map them to the light and dark palettes.
Components receive those styles from the application. Screens do not choose
their own colors or reuse warning styles to mean keyboard focus.

| Role | Use |
|---|---|
| Surface and text | Normal content, enabled controls and values |
| Border | Frames, structural dividers and scrollbar track |
| Title | Strong normal text; independent of focus or warning colors |
| Secondary | Descriptions, metadata and supporting explanations |
| Focus | Current keyboard target, with a marker or outline as well as color |
| Selection fill and text | The current row in a focused list |
| Chosen value | Checkmark or radio mark identifying the draft's selected value |
| Disabled | An unavailable control; distinct from an enabled, unfocused control |
| Success | Confirmed successful result, paired with a label or symbol |
| Warning | A condition requiring attention |
| Danger | Failed operations and destructive actions, with explicit wording |
| Match | Search matches, readable on both normal and selected rows |

Focus, selection and a saved value are different facts. Moving through choices
does not silently save one. A checkmark identifies the chosen value; a row
highlight identifies the current navigation target.

All text must remain readable on its actual surface in every theme. Disabled
controls and secondary text have different roles. Normal buttons must not look
disabled just because another control has focus. Theme changes refresh existing
content and open overlays, including cached transcript and inspector content.

### Theme set

The theme set is Dark, Light, One Half Dark, One Half Light, Solarized
Dark and Solarized Light. The FigCraft project contains all six. The latter four
are GEKTOR adaptations of the referenced terminal palettes; they keep readable
text and the shared state meanings rather than copying every ANSI color slot.

All six are available in Settings and through `/theme <name>`. Apply saves the
Settings choice; the command saves it immediately. Existing Dark and Light
preferences remain valid. All themes use the same controls,
layout, shortcuts and semantic roles. Adding a theme changes its palette mapping,
not individual screen renderers.

### Geometry and spacing

- Measure widths and coordinates in terminal cells, including styled text.
- Keep the existing 80-cell task-modal width as the default. Clamp it to the
  available area; a minimum width must never push content off screen.
- Allow a wider content view when a table or long record needs it. The shared
  layout owns this choice; individual screens do not invent width formulas.
- Use one cell of horizontal padding inside frames, as the current popup does.
- Use one blank row between logical groups and at the top and bottom of a
  modal body. Related label, value and error rows stay together.
- Reserve one empty cell between scrolling content and its scrollbar.
- Titles, body content and footer content share the same left alignment.
- Split panes share the same top and bottom bounds. Opening an inspector does
  not introduce a differently aligned inner window.
- Keep necessary actions visible. Scroll the body before hiding the footer.
- Use spacing between shortcut groups. Do not add middle-dot separators.

Rounded frames identify floating task modals. Attached menus and pickers use
compact surfaces near their control. Main panes retain the application's
outlined or unoutlined frame preference. These are deliberate surface types,
not different drawing rules invented by each screen.

Use a small shared set of symbols for selection, disclosure, ports and status.
Pair status symbols with words. Avoid decorative emoji and font-specific icons.
Do not use an arrow as a decorative separator in explanatory text.

## Control states

Every interactive control defines its normal, focused, disabled and error
appearance where those states apply. Choices also define selected and
unselected states. Asynchronous controls define loading and failure states.
Only the component holding focus displays an editing caret.

Disabled controls cannot be activated through mouse clicks or shortcuts.
When the reason matters, explain it near the control. Removing unavailable
actions from a menu can be clearer than presenting a long disabled list.

Errors retain the user's input. Field validation appears next to the affected
field; a form-level error appears above the footer. Errors wrap instead of
being cut into an unreadable fragment. Clearing an error does not move focus.

## Primitive catalog

This catalog defines the shared vocabulary. Implement a primitive when a
screen uses it; the checklist is not a requirement to build unused controls.

| Primitive | Contract |
|---|---|
| Label and text | Normal, title, secondary and status roles; explicit wrapping or clipping |
| Button | Text action, enabled state, visible focus and a matching hit region; Enter or Space activates a focused button |
| Primary button | One main action per action group; label states the result: Save, Open, Connect or Import |
| Destructive button | Explicit action and target; danger styling; confirmation starts on Cancel |
| Text input | Visible label, value, caret, horizontal scrolling and an optional placeholder; placeholder does not replace the label |
| Multiline input | Wrapped text, bounded height and internal scrolling; Enter inserts a newline; Tab moves focus |
| Search input | Text input with filter semantics; preserves the query while moving through results |
| Number input | Text editing with type and range validation; no silent conversion of invalid text |
| Read-only value | Readable value without an editing caret; long values can be inspected or copied |
| Checkbox | Boolean value, visible checked state; Space or click toggles the draft |
| Radio choices | A few short alternatives shown together; one chosen draft value, separate from focus |
| Picker field | Current value and a clear opening affordance; opens a larger choice list |
| List row | Stable identity, main label and optional metadata; focus and chosen-value marks are separate |
| Table cell | Declared alignment and overflow behavior; identifiers and full errors remain inspectable |
| Tab | Active tab and keyboard focus are separate; selecting a tab preserves each page's local state |
| Shortcut hint | Structured key/action pair; shared key emphasis and secondary action text |
| Divider | Structural separator owned by the layout, not a row of application text |
| Status label | State word with an optional symbol; color is supporting information |
| Busy indicator | Shows ongoing work without claiming progress or success; cancel remains available where supported |
| Progress indicator | Uses measured progress only; shows an indeterminate state when totals are unknown |
| Scrollbar | Shared track, thumb, gutter, paging and drag behavior; only appears for overflow |
| Disclosure | Shows whether details are expanded; activation changes presentation, not the underlying data |
| Attachment item | Name, type and removal action; retains the original attachment identity |

Keep display labels separate from stored values and IDs. A server's status,
for example, is a separate field, not text appended to its ID and parsed back
when the operator chooses an action.

Small choices normally use a checkbox or inline radios. Two to four short
alternatives are a useful starting point; actual available width decides whether
they fit. Larger sets, long labels and discovered values use a searchable picker.

The conversation composer is an explicit text-input variant: Enter sends and
the existing newline shortcut inserts a newline. Its behavior is documented in
the terminal guide and must not leak into ordinary multiline form fields.

## Shared layouts

### Agent composer and message queue

During a run, show one summary row above the draft: queued count, received count
when present, and the latest message preview. The count and ALT+Q open the queue.
The running toolbar keeps Stop, Send and Queue visible at 40 columns, even when
the queue is empty. Run > Message queue provides the same entry point. Help and
Newline stay available by keyboard. Do not repeat those shortcuts above the input.

The queue uses the table component's list layout: main text, a detail row and
space between messages. Its actions wrap to a second footer row when needed.
Read shows full text and image names. Edit uses a separate form and preserves
the composer draft. Save updates the message in place; Cancel leaves it unchanged.
Received messages are read only. A vanished row is labeled No longer queued
unless receipt was observed; removing a message must not imply delivery.
Selection follows stable IDs. Edit and Remove
check receipt atomically; a failed Save keeps the editor text visible.

### Modal

A task modal has a header, a scrollable body and a fixed footer.

```text
Header: title and close control
Divider
Body: optional description, fields, list or document
Divider
Footer: relevant shortcuts and optional action row
```

The header has one title row. Search and explanatory text belong in the body.
The footer has a divider even for a read-only dialog. Hints and action buttons
use separate rows when both are present. Buttons align to the right; the main
action comes before Cancel. Read-only dialogs use Close instead of Cancel.

The footer removes optional hints first when space is limited. It never cuts
the only useful instruction into text such as `Ctrl+S app...`. Empty descriptions
do not leave an unexplained extra block of space. If a terminal cannot fit the
minimum controls, show a compact size message with a working way to close.

### Panel and split pane

A panel has a header, body and optional status/footer area. A split layout
places sibling panels within the same bounds and calculates their rectangles
together. Both Agent and Workflows use it for the main view and inspector.

Pane focus is visible. Each pane keeps its own selection and scroll position.
Closing the inspector returns focus to the item that opened it. A narrow screen
shows one active pane with a clear way back, rather than squeezing two unreadable
panes side by side. The shared layout owns the breakpoint and size calculation.

An inspector footer is exactly one text row plus its separator and frame edge.
Do not add blank rows above or below the shortcuts. The canvas hint footer uses
the same compact height. The Agent composer keeps the space needed for its input;
the neighboring inspector does not inherit that height. Pane outer bounds stay
aligned while the inspector body gains the unused space.

### Scroll area

The scroll area owns the viewport, visible range, content clipping, gutter and
scrollbar geometry. Headers, tabs and action footers stay outside it. A bounded
form editor may scroll internally; mouse position decides which area scrolls.

The wheel moves the content under the pointer. It does not commit a choice or
change a list in a different part of the screen. Track clicks page; thumb drags
retain ownership until release, including when the pointer leaves the track.

### Form and action footer

A form lays out label, control, optional help and optional error as one field
group. Hidden fields take no space. Focused fields scroll into view without
rearranging unrelated groups. The form owns draft values, validation state and
control navigation; the screen owns loading, saving and application effects.

The footer receives structured actions and shortcuts. It draws them and returns
their hit regions. A caller does not pass a preformatted hint string or compute
button coordinates separately.

### Menus, tabs and toolbars

Top menus and context menus share row selection, disabled states and keyboard
navigation. Menus stay attached to their origin and clamp to the screen.
Use an ellipsis for an action opening another dialog. Show checkmarks for
independent toggles and radio marks for mutually exclusive choices.

A toolbar is a group of direct actions. The global hotkey bar and modal
shortcuts share key formatting, but do not duplicate commands that cannot run
in the current context. Display key labels in uppercase, including named keys
and modifiers: `T`, `DELETE`, `ENTER`, `ESC`, `CTRL+S`. This affects presentation;
the accepted key bindings stay unchanged. Action labels keep normal casing.
The hotkey bar keeps one blank terminal column between
action blocks, using the surrounding surface color. The gap stays visible at
narrow widths and never activates an action when clicked. This spacing belongs
to the shared component, not each screen.

Tabs use the same title and focus roles in both modes,
with one blank row before their page content.

Management tables keep frequent actions visible and put additional actions in
an attached More menu. The menu preserves the selected row and filter. Escape
returns to the table; clicking outside dismisses the menu without activating
anything beneath it. Optional metadata yields before names and status in narrow
tables, so management remains usable at 40 by 20 cells.

## Composed controls

| Control | Required behavior |
|---|---|
| Searchable list | Filter, selection, stable scrolling, result count and inspectable full labels |
| Picker | Searchable list plus chosen-value mark, explicit acceptance and cancellation |
| File browser | Separate current location and filter; opening a folder browses it; selecting it is explicit |
| Table | Main label and distinct metadata columns; preserve useful columns before optional metadata |
| Inspector | Shared panel shell and tabs; human summary first, full/raw data available separately |
| Document viewer | Wrapped text, scrolling, optional copy action and a fixed Close footer |
| Confirmation | Clear effect and target, explicit action and safe default focus |
| Permission/question gate | Readable request, bounded choices and existing permission semantics |
| Completion list | Anchored to the composer; selecting a suggestion inserts or runs according to its declared action |
| Notice | Information, warning or error with an appropriate dismissal policy; important failures remain inspectable |
| Transcript block | Shared spacing, speaker/status roles, markdown and thinking presentation |
| Tool row | Consistent action, target, state and details affordance; measured results stay separate from claims |
| Graph node | Shared title, ports, body and status areas; selection independent of execution state |
| Canvas | Pan, viewport, node placement, connection rendering and hit testing within bounded visible work |

Notices use the theme's secondary text, success, warning and error roles.
Warnings and errors include a text label, so color is not their only signal.
A status footer stays one row tall. Each new message replaces both the text
and its severity; a normal update must not keep the previous error's color.

Tool rows share state markers between compact and detailed views. Compact rows
keep the state word visible before optional technical names and long targets.
Returned, skipped and failed calls must not look completed. Full arguments and
output remain available in the detailed view.

Every list defines loading, empty, no-match and failed-load states. A failed
request offers retry where useful and preserves the query. Loading does not
replace the list with a successful-looking empty result. Async replies carry
an identity or revision so an old result cannot replace a newer request.

The inspector summary explains the selected item in human terms. Input sources,
output values, errors and completed effects are distinct sections. Raw JSON,
long code and full traces belong in explicit detail views. A selected graph node
and its inspector use matching labels and port names.

Existing transcript, permission and workflow execution behavior stays with its
current owner. A visual migration must not change approvals, recipe execution,
run state or what is sent to the model.

## Keyboard, mouse and focus

| Context | Contract |
|---|---|
| Form | Tab and Shift+Tab move controls; Ctrl+S submits an ordinary editable form; Esc cancels |
| Multiline field | Enter inserts a newline; navigation keys edit text; Tab leaves the field |
| Inline choices | Left/Right move between alternatives; Space selects; click selects the draft value |
| List or picker | Up/Down move rows; Page Up/Down page; Home/End reach the boundaries |
| Searchable list | Typing filters; list navigation keeps the query intact; text-caret Home/End remain available when editing the search field |
| Picker acceptance | Click selects a row; Enter or Select accepts it; selection does not immediately close the picker |
| Table/list screen | Click selects a row; Enter performs its displayed main action |
| Menu | Up/Down move; Enter activates; Esc closes and restores the origin |
| Tabs | Left/Right change tabs while the tab strip has focus; they do not steal editor or canvas keys |
| Destructive confirmation | Enter activates the focused button; Ctrl+S does not confirm deletion |
| Read-only dialog | Esc or Close closes it; Enter may close only when no focused control uses Enter |
| Close control | Same cancellation behavior as Esc for that surface |

For searchable lists, distinguish search editing from result navigation in the
focus model. Display the applicable keys. Do not bind one key to both caret
movement and list movement in the same focus state.

The topmost active surface owns input. Esc dismisses its child picker first,
then the parent form on a later press. Cancelling a picker preserves the parent
draft, focused field and scroll position. Opening another surface preserves the
conversation draft. Restoring focus must not leave a hidden editor receiving
typing or paste.

Outside clicks do not save or execute anything. An attached, temporary picker
may dismiss without changing the draft; a task modal stays open. Terminal key
availability varies, so essential actions also have ordinary keys or visible
controls. Scope global shortcuts so they cannot trigger unrelated work behind
a dialog.

## Applying the library to screens

| Screen | Composition and changes |
|---|---|
| Agent | Main panel with transcript and composer; shared split layout for tool inspector |
| Workflows | Main panel with canvas; same split layout for node/run inspector; shared tabs and action controls |
| Settings | Form in a task modal; staged edits, inline small choices and contextual hints |
| Theme | Inline theme choices and Apply; copy must state when the change takes effect |
| Open session | Searchable global list; each card separates title, workspace path and metadata/preview. Ctrl+F toggles all/current workspace; Ctrl+W reassigns with confirmation. Opening restores the saved workspace; missing or unassigned paths require explicit selection, never a silent fallback. Rename/delete remain available. |
| Permissions | Read-only policy table with filtering, visible rule order and a fixed Close footer |
| MCP servers | Server list with separate status column and direct Details, Connect/Disconnect and Edit actions; Add and Import are screen actions |
| Workflow inputs | Typed form with useful labels and defaults; raw JSON is an explicit alternative |
| Saved recipes/runs | Searchable list with readable names first; IDs and status are separate metadata |
| Help and diagnostics | Document viewer; shared tabs where needed; full errors remain available |

Use the same nouns across menus, titles, buttons and help. Labels come from
human-readable field metadata rather than internal keys such as `kind`.
An action button states its effect. Use Continue only for an actual next stage
whose purpose is explained. Avoid repeating the title as a field label when it
adds no information.

Different screens can have different save contracts, but they must say what
they do. Settings changes remain a draft until Apply. MCP actions execute when
explicitly chosen. Merely highlighting a row or navigating a folder has no
external effect.

## Menu ownership

The [terminal guide](../reference/terminal.md#menus) lists the menus and their user-facing roles.
GEKTOR owns mode switching, workspace selection and application exit. Session
and Recipe own saved content. Run owns execution. View owns visibility and
navigation. Settings owns configuration; Appearance owns theme and panel frame.
Help owns instructions and diagnostics. Node is specific to the workflow editor.

Menu definitions and their command metadata live in
[presentation.go](../../internal/tui/presentation.go), with Agent actions in
[menus.go](../../internal/tui/agent/menus.go) and Workflow actions in its own
package. Application navigation dispatch lives in
[update.go](../../internal/tui/update.go). The shared header handles layout,
focus and input, using the shared popup component. Both keyboard and mouse
activation use the same enabled-state checks and dispatcher. At narrow widths,
More exposes menus that would otherwise be clipped.

Settings sections use one title catalog for tabs and form headings. Internal
provider and profile fields retain their domain names; changing a display label
does not change the stored profile format or existing slash commands.

## Implementation boundaries

Build on [the existing components](../../internal/tui/components). Prefer plain Go
structs, functions and explicit message handling. Keep related primitives in
the same package. Introduce another package only when its responsibility is
clear and it has a real consumer.

| Existing home | Migration responsibility |
|---|---|
| [cell](../../internal/tui/components/cell/cell.go) | Terminal-cell measurement, fitting and clipping |
| [popup](../../internal/tui/components/popup/popup.go) | Shared surface/layout rules, header/footer, shortcut groups, attached menus and overlay geometry |
| [pane](../../internal/tui/components/pane/pane.go) | Main-panel frames, split regions, body/footer bounds, close targets, tab strips, action toolbars and notice rows |
| [form](../../internal/tui/components/form/form.go) | Typed controls, field groups, draft and focus behavior |
| [selection](../../internal/tui/components/selection/selection.go) | Shared selection, paging and visible-range behavior |
| [table](../../internal/tui/components/table/table.go) | Searchable tables or two-line list cards, direct actions and shared render/hit layout |
| [schemaform](../../internal/tui/components/schemaform/schemaform.go) | Convert schemas to those controls; preserve unsupported values for explicit JSON editing |
| [composer](../../internal/tui/components/composer/composer.go), [completion](../../internal/tui/components/completion/completion.go), [gate](../../internal/tui/components/gate/gate.go) | Use common styles and geometry while keeping their specific input contracts |
| [transcript](../../internal/tui/components/transcript/transcript.go) | History rendering and viewport behavior |
| [graph](../../internal/tui/components/graph/graph.go) | Node/canvas presentation and graph interactions, independent of workflow execution |
| [settings](../../internal/tui/settings/settings.go) | Settings values, discovery and intents; move reusable presentation into the components it uses |
| [styles.go](../../internal/tui/screen/styles.go) | Semantic styles and theme mappings, supplied to components |

The Bubble Tea loop remains the state owner. Components own their local
selection, focus and editing state. Async services return messages; components
do not open backend connections or save application preferences themselves.

`popup.Frame` wraps rows that already have their styles. Render every content
row with `popup.Row`, including title padding and shortcut footers. A foreground
style alone leaves the canvas visible behind the text. Check cell backgrounds
after ANSI rendering, including compact layouts and selected rows.

Pass keyboard hints as `popup.Shortcut` values, including custom dialog actions
and Agent status controls. The shared renderer formats keys and keeps each
key/action pair together. Application code still owns message content, tool
output formatting and workflow explanations.

A layout calculation returns the rectangles used by rendering, clipping,
caret placement and hit testing. Compute those from one set of inputs.
Do not infer a divider from a string of line characters or derive list hit
positions from a second hardcoded row count. Scrollbars and footer controls
use the same geometry as their rendered targets.

Keep repeated rendering work bounded by the visible area. Cache only with keys
that include every presentation input that can change the result. A node far
outside the viewport must not make dragging or repainting progressively slower.

## Implementation status (2026-11-09 snapshot)

The shared components listed above are implemented. They provide modal frames,
forms, lists and tables, panel layouts, shortcuts and graph presentation.
Settings, session search, MCP management and Permissions use these components.

Component adoption and usability are separate checks. When changing a screen,
use the existing components and run the relevant verification tasks below.
Record a specific missing behavior when one is found, rather than treating an
entire component family as unfinished.

## Verification

The following checks were recorded for visual acceptance on 2026-11-09:

- Check workflow input, recipe/run selection, confirmation and detail dialogs
  against the shared controls and fix any remaining differences.
- Review representative screens in all six themes and the sizes below, including
  both inspector layouts, node selection, status colors and resized dialogs.
- Run the usability tasks and remove leftover drawing and input paths found
  during those checks.

These are acceptance criteria for the migration, not claims about current test
coverage. Existing tests cover parts of frame sizing, hit testing, focus,
themes and draft preservation. A passing layout test does not establish usability.

Use all six themes at 80 by 24, 120 by 40 and 160 by 50 cells. Also check 40 by 20
for safe clipping, reachable actions and a usable close path. Resize while a
picker, multiline editor and inspector are open. Check Unicode labels and long
paths in terminal cells.

Automated checks should cover:

- Header/body/footer geometry and shared render/hit/caret rectangles.
- One-row inspector/canvas footers without blank padding; independent composer height.
- Normal, focused, chosen, disabled, error and loading states.
- Enabled buttons staying readable without focus; disabled shortcuts doing nothing.
- Keyboard navigation, paging, mouse selection and explicit acceptance.
- Scrollbar dragging, nested scroll areas and wheel events outside a control.
- Draft preservation on child cancellation, modal cancellation and resizing.
- Multiline entry, paste, Unicode and exactly one focused input owner.
- Stale async results, empty lists, no matches and failed discovery with retry.
- Existing destructive-action, permission and workflow execution protections.
- Distant graph coordinates, clipping and bounded rendering work.

Keep representative visual previews for a form, session list, picker, policy
table, MCP list, confirmation and both inspectors in all six themes. Review their
spacing and state distinction; string-width assertions alone cannot judge them.

Usability tasks:

1. Change the theme and explain when it is saved.
2. Find a model in a long list, choose it, then cancel the parent settings form.
3. Choose a workspace while distinguishing browsing from selection.
4. Find an MCP server and connect it without an Action dropdown.
5. Filter permission rules and explain which matching rule takes precedence.
6. Open a saved workflow run, find a failed step, inspect its inputs and return
   to the graph without losing selection or position.
7. Open an Agent tool result, read its error, close the inspector and continue
   the existing draft.

Observe where people hesitate, use the wrong control or cannot explain whether
a change was saved. Fix those interactions before extending the component catalog.
