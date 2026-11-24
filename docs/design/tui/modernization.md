# TUI modernization experiment

Branch: `codex/tui-modernize`.

The Appearance form offers two styles: Commander and Modern. Missing style
preferences retain Commander. This experiment develops Modern and preserves
Commander's existing presentation.

Modern reduces the permanent controls around the conversation and
canvas. OpenCode's local TUI source informed the use of spacing and filled
surfaces. GEKTOR keeps its Agent/Workflows navigation, inspectors and component
library.

## First pass

- Remove the global bottom hotkey strip from both screens and dialogs.
- Keep keyboard bindings, F1 help, F9 menus and F12 action search.
- Put dialog actions inside their windows. Masked credential entry has local
  Save and Cancel buttons instead of relying on the global strip.
- Replace the minimal frame's full-width rules with blank rows. Preserve the
  geometry used by inspectors, scrollbars, close buttons and cursor placement.
- Give the composer a filled surface. Keep its draft visible and omit routine
  idle shortcut hints. Routine Stop, Queue, Latest and Scrolled up hints stay
  hidden in Modern.
  Their shortcuts remain available, and queue counts stay visible.
- Show a short invitation in an empty conversation.

Outlined frames remain an appearance choice. Other themes retain their semantic
roles. The shared library's toolbar stays available for other consumers;
`ControlProps.Modern` selects local controls for Modern while Commander keeps
the original strip.

## Visual direction

The default dark palette uses these roles:

| Role | Color |
|---|---|
| Canvas | `#11161C` |
| Input and popup surface | `#1D2731` |
| Main text | `#D5DEE7` |
| Secondary text | `#8E9CA9` |
| Focus and user accent | `#79C8D0` |
| Optional outlined pane border | `#526273` |

Use the terminal's selected font and cell metrics. Headings use weight rather
than a different typeface. Keep key names uppercase and action labels in normal
casing. Conversation content stays left aligned; whitespace separates regions.

```text
 GEKTOR  Session  Run  View  Settings  Help       session

 User request

 Assistant reply and compact tool activity

 [ filled composer surface                           ]
 [ draft                                             ]
 [ Act  model                             context    ]
```

## Review

Run `start-tui-agent.cmd` and `start-tui-workflows.cmd` from this branch. Select
Modern as the UI style and Borders: Hidden in Appearance to review the
borderless treatment. Both launchers
rebuild the executable.

The opt-in `TestModernPreview` exports actual shell rendering with fixture content
to `GEKTOR_TUI_PREVIEW_DIR`; it does not call a model. Raster previews show a
chosen font, not the user's terminal's exact font rendering. Keep temporary
exports in `tmp/`. The existing FigCraft project remains the earlier baseline.

Review wide and narrow screens, empty and active conversations, queue entries,
permission prompts, form buttons, inspectors and workflow navigation before
merging the experiment. Color and spacing previews do not prove live terminal
interaction or cursor rendering.


## Transcript pass

Reviewed OpenCode's `packages/tui/src/routes/session/index.tsx`, especially
UserMessage, AssistantMessage, ReasoningPart and InlineTool, plus
`packages/tui/src/ui/dialog.tsx`. Its visual hierarchy comes from different
surfaces and spacing for message roles, not just a different color theme.

Modern applies that principle through the existing renderer:

- User requests use a padded panel and accent rail; their text remains literal.
- Assistant prose stays on the canvas with a consistent reading inset.
- Thinking is subdued, without making whole paragraphs italic or displaying
  character counts. Expansion still uses the existing Thinking control.
- Optional speaker and role labels share one sentence-case heading.
- System notes use the same reading inset without the repeated accent rail.
  Request-inclusion metadata stays in Commander and the saved records, while
  Modern omits it from routine history.
- Successful tool activity is subdued. Failures remain distinct and tool names
  remain available in detailed output and inspection.
- Completion summaries retain status, verification state and relevant findings.
  **Run > Last run details** exposes the full receipt, including resume paths.
- Modern dialogs and popups use filled surfaces without visible frame rules;
  their padding and hit geometry stay unchanged. Shared dialogs dim their
  background. Modern does not add a second set of
  modal shortcut hints or a Draft preserved label outside the dialog.

Both render styles use the same stored blocks, tool IDs, queue, keyboard routing
and permissions. Style changes invalidate presentation caches and refresh pane
geometry; they do not edit saved history or call the model.

## Composer and inspector refinement

Appearance calls the frame setting **Borders**, with **Visible** and **Hidden**
choices. Existing `outlined` and `minimal` preference values remain compatible.

Modern places mode, model, access policy and context below the draft. Idle
screens omit the step counter and elapsed time; run facts remain available from
Run and the status controls. The conversation drops the old status header and
uses the recovered rows for history. Commander retains its header.

The shared pane measures a headerless layout for the Modern conversation.
Transcript scrolling, tool hit targets, search, suggestions and cursor placement
use those measured bounds. Agent and workflow inspectors keep their titles,
omit the footer region and indicate focus through color. Borderless inspectors use the input surface color to separate
them from the conversation. Modern inspection
renders string arguments literally, including embedded newlines, while preserving
all argument fields and their saved values.

Borderless Modern gives the composer blank top and bottom rows. Visible borders
replace those two padding rows. The input rail aligns
with the left edge of user panels; their text has one space after the rail.
Queue information shares that surface; clicking its visible summary row opens
the queue without changing the draft. Message accent rails use the same
background as their user panels, with no canvas-colored gap.

Modern action buttons use padded text on a filled background. Focus, disabled
state and destructive intent remain distinct. Forms, pickers, table actions and
credential dialogs use the same button renderer and measure its displayed label
for pointer targets. Commander retains bracketed action buttons.

## History interactions

Modern retains per-block display overrides in the Agent transcript. Thinking
headings toggle one stored thinking block. Tool status symbols toggle output;
tool names retain their inspector action. The History blocks list supplies a
keyboard route. Overrides are cleared when loading another session or changing
global thinking/tool display preferences. Stored history is not edited.

The shared composer can fold a large paste for display. Its textarea keeps the
full draft, including text entered before the paste. Explicit expansion or
ordinary editing restores the editor. Submission and input recall use the
original draft rather than a placeholder token.

Shared controls own temporary notifications and their versioned expiry messages.
The application routes timer messages without changing focus. Copy confirmations
use notices; errors retain their existing dialogs. Unified-diff rendering lives
in a reusable component with supplied styles and no filesystem access. Agent
uses it for returned tool output, including the inspector.

Workflow surfaces carry an immutable palette snapshot so frame, footer,
background and tab styles stay together. Modern workflow inspectors omit their
footer, use a separate background without borders, and use filled tabs without
brackets. Commander retains its footer and bracketed active tab. Inspector
paging and mouse scrolling use the inspector body height, including wrapped tab
rows, rather than borrowing the canvas height.

Workflow dialog bounds come from the canvas body layout in all four style/frame
combinations. Modern graph selection uses accent color with single-line card
borders; Commander retains double-line selection. Card borders and connection
ports remain visible when pane borders are hidden because they describe the graph.
The Modern empty canvas omits shortcut instructions. Native single-line and
multiline inputs share a blinking bar caret through the palette's input styles.
