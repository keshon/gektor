# Terminal guide

Start GEKTOR with `gektor.exe -tui`, or use `run-tui.cmd` on Windows.
The UI uses Bubble Tea v2. [CLI flags](cli.md) configure the backend,
workspace, permissions, verification, and response budgets.

## Windows launchers

Start a model server in its own terminal before using GEKTOR. Machine-specific
paths and keys live in `.env` (copy from `.env.example`; gitignored).

**llama.cpp (default backend):**
1. Fill in `LLAMA_SERVER_EXE` and `LLAMA_MODEL` in `.env`.
2. Run `run-server-llama.cmd` and wait for the model to finish loading.
3. In another terminal, run `run-tui.cmd`. It builds `gektor.exe` and opens
   saved settings, defaulting to llama.cpp at `127.0.0.1:8080` on first launch.
   Extra arguments are forwarded to the binary:

```bat
run-tui.cmd -workspace D:\Projects\my-app -verify-cmd "go test ./..."
```

**KoboldCPP (secondary):**
1. Fill in `KOBOLD_EXE` and `KOBOLD_MODEL` in `.env`.
2. Run `run-server-kobold.cmd` and wait for the model to finish loading.
3. In another terminal, run `run-tui.cmd -backend-kind kobold -backend http://127.0.0.1:5001`.

The sibling launchers build and run the same binary for print-mode work:
`run-task.cmd "task"`, `run-plan.cmd "task"`,
`run-resume.cmd [id]`. Each prints its usage
when called without arguments.

Keep flags before any task text. Closing GEKTOR leaves the independently
started model server running. Closing the server terminal stops that server.
The optional `run-remote.cmd` is a local, gitignored helper; it is not
required or distributed as part of the setup.

## Screen and focus

The unbranded one-row global menu exposes Session, Run, View, Settings and Help dropdowns. A separate one-row status header inside the transcript pane shows Plan/Act mode, model, context, and run status.
Step/context/elapsed metrics remain visible while idle; a running turn adds
a Braille spinner. The Run menu includes tool and file counts. Estimated context
usage has a `~` prefix. Empty history shows `CTX 0/window` and `step 0/limit`.
Status badges are separated by two spaces; ratios have no spaces around `/`.

Thinking and response fragments share a 16ms repaint buffer. Each frame paints
everything received, with no character-by-character typing animation. Thinking
is visible even before response text exists; tool calls remain complete cards.
The activity badge distinguishes waiting (`WAIT`), server-reported prompt
processing (`READ`, with a percentage when supplied), thinking (`THINK`),
streamed response (`STREAM`), tools (`TOOLS`), and non-streamed reply (`REPLY`).
llama.cpp telemetry supplies live prompt progress and generated token counts;
servers without it show estimates marked `~`, not an invented read percentage.
The context meter includes cached input and generated output, including thinking
and tool arguments. Final usage replaces live estimates when the reply ends.

The pane titles are Sessions and History. Plan/Act and the step limit are
global preferences: opening old history or changing the model preserves them.
`/max-steps 0` means unlimited, displayed as `∞`; the CLI flag has the same
meaning when explicitly supplied.

Local llama.cpp and KoboldCPP servers supply the context window and loaded
model at startup and before each turn. Settings exposes a manual window only
for remote OpenAI-compatible providers. Local configured windows remain a
fallback when the server cannot report one. Model badges show the filename
without directory prefixes.

Opening a saved conversation immediately shows a gray history context estimate
and its last recorded model. The estimate includes messages and images but
excludes tool schemas. The model badge becomes current on the first response;
the context badge becomes current as the new request starts, initially estimated
until the server supplies counts. Older sessions without
model metadata show `—` until a new response.

At 110 columns and 18 rows or larger, the 34-column Sessions pane appears
unless hidden with F6 or Ctrl+B. Tab switches between the pane and input.
Gray rules stay neutral; the focused pane label is colored. A cyan `▌`
marks the current saved session; `›` marks the row selected in the focused
pane. Ages are gray. `*`, `?`, and `!` indicate working/stopping, input
needed, and failed/unreadable states.

The transcript reads as a name row, a label row and a body. A run of
blocks by one participant opens with its name in uppercase; the blocks
after it carry only their label rows. The label row is a bracketed tag
at the left edge with the time flush right, and the body starts in
column 0. Machine notices are all `SYSTEM`, so their tag is what
distinguishes them. Thinking and tool results can be collapsed or
expanded with F3 or Ctrl+G. Collapsing is visual only; it does not
remove model history. Scrolling up stops automatic following; F7 or
Ctrl+End returns to the latest content. A scrollbar in the right-hand column
appears once the transcript is taller than the pane; its thumb is sized to
the visible fraction and sits where you are in the document.

```text
YOU
[INPUT]                                       [14:51]
I want you to reply with a phrase....

ASSISTANT
[THINKING]                                    [14:53]
The user asked me to reply with a phrase...

[RESPONSE]                                    [14:54]
This is a textual response generated by AI.

[TOOL CALL]                                   [14:55]
✓ read_file {"path":"internal/config/profile.go"}
| file content ....
| file content ....
└─ file content ....

SYSTEM
[HARNESS]                                     [14:55]
You've spent several steps searching (.....)
```

The user name follows Settings > You and the assistant name comes from the
run's identity; both are uppercased. The tags are fixed:
`INPUT`, `RESPONSE`, `THINKING`, `TOOL CALL`, `TOOL RESULT`,
`QUESTION`, `MISSION`, `HARNESS`, `ERROR` and `WARNING`. Blocks are
separated by one blank row; horizontal rules stay outside the transcript
and belong to pane boundaries, so a turn reads `YOU`/`[INPUT]` followed by
the assistant's name and `[RESPONSE]`.
Reopened history shows no times, because the saved format records none.

A reply in flight is revealed a few characters per frame rather than
landing in whatever chunks the backend sent. The rate adapts: a slow
model queues one or two characters between frames and appears to type,
while a fast one drains a quarter of its backlog per frame and catches up
in well under a second instead of trailing. Scrolling up empties the
queue at once, because nobody is watching the tail. The streamed body is
shown as literal text — a bold span would show its own asterisks for
several frames before closing — and the finished block renders with full
markup when the step arrives.

Deliberation streams the same way, on a buffer of its own: a thinking
model fills the THINK block while it is still thinking, and the block
stays on screen as the answer streams underneath it. One merged buffer
would interleave the two instead of chaining them, and reasoning that
arrives at the end would blank the answer to make room.

## Header badges

The session title and clock sit above a badge strip: Act/Plan, Ready/Work/Wait/
Stop/Error, model, context, and run progress. Work includes the braille spinner.
Click a status badge to inspect it, or use F9 to open the global menu without moving focus. Pane
rules remain gray. Narrow terminals omit run and model badges before context.

Mode offers Act/Plan while idle; Activity offers Stop or Retry when applicable.
Model shows the configured backend, sanitized endpoint, transport, context
window, and response budget. These are configuration details, not server-health
measurements. Context offers manual compaction when an idle session has saved state.

The context badge retains the latest request's prompt usage between turns.
`~CTX` marks an estimate. Opening or compacting a saved task recomputes an
estimate from replayed text and tool calls, excluding tool schemas and image
tokens. Context details identify the source and show previous request usage,
which is saved in optional `usage.json` alongside `state.json`. Missing or
corrupt usage metadata does not prevent opening a session.

Session scans run in background commands, with revision checks preventing stale
results from replacing newer state. The sidebar loads session metadata; opening
Search prepares conversation text. Unchanged histories reuse the session cache.
Opening old history omits message timestamps because the saved format does not
record them; live messages retain their actual display times.

The ownership rules and their regression tests are documented in
[tui-ownership.md](tui-ownership.md).

## Input and suggestions

The composer sits below the transcript inside the conversation pane; its
suggestions and prompts share that pane's width. The Sessions divider extends
to the single-row global action toolbar. Session
details sit at the bottom of the pane without a separate divider. Hiding Sessions gives the composer the full
terminal width.

The prompt is `>` with an underline caret. The input grows to fit multiline
text, up to six visible rows (less in short terminals).

- Enter sends when idle, or queues one follow-up during a run. Empty Enter
  does nothing.
- Ctrl+O adds a line. Shift+Enter also works when the terminal preserves it.
- Pasted multiline text stays in the draft, including a trailing newline; Enter sends it.
- Up/Down moves within the draft, unless the suggestion picker is open.
- Alt+Up/Alt+Down recalls submitted input and restores the unsent draft.
- Ctrl+U clears the draft.

Type `/` at the start of the message for commands, or `@` at a token boundary
for workspace paths. The popup shows up to six matches and keeps the input
in place. It filters by prefix; files are browsed one directory at a time,
not by searching the entire repository. Hidden entries appear when you type
a leading dot. Suggestions are available in the idle and running composer,
not in permission dialogs, question prompts, or session search.

- Up/Down selects a match.
- Tab inserts without executing.
- Enter runs a selected command that takes no argument. A command that does
  take one — `/max-steps`, `/mode`, `/theme`,
  `/new`, `/rename` — is handed to the composer instead, so the value can
  be typed; during a run the completed command is queued. For files and
  directories, Enter always inserts without sending.
- Esc dismisses the popup; typing again can reopen it. Press Esc again to
  stop an active run.

An exact completed file path closes its suggestion, so Enter can send the
message. Directory completion adds `/` and keeps browsing. Paths containing
spaces are quoted automatically. `/mode p` and `/mode a` suggest Plan and Act.

## File references

```text
Review @internal/tui/input.go
Compare @"docs/design notes.txt" with @README.md
Describe @screenshot.png
```

Paths are relative to the selected workspace. Text files must be UTF-8 and
contain no NUL bytes. Each text file is limited to 32 KiB; all text references
in a message are limited to 64 KiB combined. Repeated references to the same
resolved file attach one copy. Directories, binary/invalid UTF-8 files,
oversized files, and paths escaping the workspace (including resolved
symlinks) produce an error. No partial attachment is sent on these errors.

The model receives the original text plus file snapshots. The transcript
shows the original message, including after reopening saved history. Queued
references are read when their turn begins; retries reuse that turn's snapshot.
Typing a reference is an explicit user action: it does not invoke the model's
read_file permission gate. Attach only content you intend to send to the
configured backend. See [the trust boundary](../SECURITY.md).

PNG, JPG/JPEG, WebP, GIF, and BMP use image attachments instead of text.
The backend caps each image at 20 MiB. A vision-capable OpenAI-compatible
provider or llama-server with VL weights and `--mmproj` is required. GEKTOR's
KoboldCPP dialect rejects image attachments. `-image` attaches images to
an opening task; inline text-file references are a TUI composer feature,
not syntax interpreted by print mode or RPC.

Bare unknown mentions such as `@someone` remain literal. A missing unquoted
image path also remains literal for compatibility; quoted missing paths and
missing text-file paths with an extension/path separator report an error.

## Action toolbar

Help (F1), Search (Ctrl+P), and New (Ctrl+N) keep fixed positions.
The middle actions follow focus: Send/Newline in the composer, Stop/Latest
during a run, Rename/Delete in Sessions, and Back/Choose in overlays.
Details/Less (Ctrl+G), Sessions (Ctrl+B), and Quit (Ctrl+Q) remain at the end. Ctrl shortcuts use
compact caret notation (for example, `^P`) in narrow cells. Below 120
columns the toolbar stays on one row, shortening labels and reducing visible actions when needed. Mouse clicks invoke the same key routing.

Existing function keys remain aliases:

- F1 Help: open/close the help modal.
- F2 Search: open/close the centered session search popup.
- F3 Details: expand/collapse thinking and tool output.
- F4 New: start an empty session when idle.
- F5 Mode: switch Plan/Act when idle in a direct run.
- F6 Sidebar: show/hide the pane when the terminal is wide enough.
- F7 Latest: jump to the latest message.
- F8: unused.
- F9: focus the global menu. Left/Right selects a category; Enter opens its dropdown.
  Up/Down browses details and actions; Enter chooses an action. Esc goes back
  or returns to the composer. You can also click badges and actions.
- F10 Quit: exit when idle; stop an active run first.

The footer can be clicked. Disabled actions are dimmed. Below 120 columns,
it remains one row with abbreviated labels; very small terminals omit it.

## Sessions

F2 or Ctrl+P opens a centered popup over the transcript, using the same palette as
input suggestions. Search matches session names/IDs and saved user messages
and assistant replies, case-insensitively. Title matches rank ahead of history
matches; empty search shows recent sessions. Results show gray ages and a
highlighted matching excerpt. Up/Down and PgUp/PgDn move through results;
Enter opens the session without generating a response. History matches jump
to the matching message. Esc closes and preserves the composer draft.

Search covers user and assistant messages in the complete saved archive,
including messages outside the compacted model context. System/harness
instructions, reasoning, and tool output are not searched.
Stop a running turn before switching sessions.

Focus the Sessions pane with Tab, then use Ctrl+R to rename or Ctrl+D to
confirm deletion. The same shortcuts remain available in the search popup.
Type a follow-up to continue. Saved Plan/Act mode is restored when
opening a direct run. Missions can be resumed from the CLI; the session
search popup does not resume them as direct runs.

Drafts, input history, and the queued follow-up are kept separately for each
run while the UI is open. They are not persisted across app restarts. Saved
model history and display metadata live under `.gektor/tasks/<id>/`.

## Running, stopping, and permissions

Only one follow-up can be queued. Ctrl+E moves it into an empty draft;
Ctrl+X cancels it. It starts after a successful run if the draft is empty.
Failure, interruption, or a nonempty draft leaves it queued for editing.
Esc stops the run and waits for cleanup; it does not quit or erase the session.
Ctrl+Q or F10 quits when idle. During a run, it first stops the run;
press again after stopping to quit. Ctrl+C aborts and quits immediately.

Permission requests show the tool and resource with a scrollable preview:
Y allows once, A asks for confirmation to allow the same tool/resource for
this run, and N opens an optional redirect note. Enter submits the note;
Esc returns from the note/confirmation step. Esc at the initial permission
prompt denies the request. Explicit policy denial cannot be overridden.
Question prompts accept a multiline answer with Enter.

## Help and local commands

F1 and `/help` open the same modal with Input, Navigate, Run, and Commands
categories. Tab/Shift+Tab or Left/Right switches category; Up/Down,
PgUp/PgDn, and the mouse wheel scroll. Esc, Enter, or F1 closes it.
The draft is preserved while Help is open.

- `/help`: help modal.
- `/sessions`: session search popup.
- `/new [task]`: start a new task; without text, reset to an empty task.
- `/mode plan|act`: switch direct-run mode; `/mode` alone shows the mode. The
  switch keeps the conversation and the cached prompt prefix, so it does not
  restart the session or make the backend re-read the history.
- `/status`: append current run facts to the transcript.
- `/retry`: continue the last failed turn from its latest saved checkpoint.
- `/compact`: shrink the model context when idle, preserving the conversation archive.
- `/quit` or `/q`: quit.

Unknown commands show an error and are never sent to the model. These are
local UI commands; they are not tools or RPC methods.

## Troubleshooting

- Server not ready: start `run-kobold.cmd` first and wait for loading to finish.
- Use Ctrl+O for a newline on Windows. VT input keeps bracketed-paste boundaries
  intact; some hosts encode Shift+Enter as plain Enter. Native mouse records
  continue through the same adapter.
- In ConEmu, enable Settings > Mouse > Send mouse events to console. The input
  adapter accepts XTerm character sequences as well as native mouse records.
  Window resizing uses the visible viewport rather than scrollback buffer size.
- Strange scrolling: keep server logs in their own terminal. Do not redirect
  model/server output into the full-screen UI.
- No streaming text on KoboldCPP: that dialect uses unary requests. OpenAI
  and llama stream automatically in the TUI; the spinner still reports activity
  in unary mode.
- Large reference rejected: use a smaller file or ask the agent to inspect
  it with bounded file tools. Raising the generation budget does not raise
  attachment limits.

### Commander-style frame

The global menu and shortcut footer each occupy one row. Sessions remain in the left pane; the run status, transcript, and growing multiline composer stay in the right pane. Tab switches pane focus, F6/Ctrl+B toggles the sidebar, and F2/Ctrl+P opens session search. Top-menu clicks use the same function-key routing, including disabled actions during approval. Narrow terminals hide the sidebar and reduce visible footer controls; full bindings remain available through Help.

The top row is an unbranded dropdown menu: Session, Run, View, Settings, Help. Click a category or press F9, then use Left/Right to switch categories, Up/Down to select, Enter to apply and Esc to back out. Session exposes new/search/compact/rename/delete/quit, Run exposes Plan/Act/stop/retry/permissions, View exposes the display toggles and jump/theme actions, and Settings lists the editable sections. The function-key and Ctrl shortcuts remain in the footer and Help.

Each Settings entry opens its own small modal with only that section's
fields. The modal used to be one two-column window with a rail of section
names down the left, so every section opened as a form three quarters full of
the other sections' names; the menu is the chooser and the modal is for the
thing chosen. It used to have its own renderer and its own full-screen frame,
so it was the one screen in the app that looked unlike the rest; it is drawn
by the shared form component on the shared overlay surface, inside the normal
app chrome, exactly like a confirmation. An unset temperature reads
`default (0.4)` rather than a bare `0`, which used to be indistinguishable
from a profile that had explicitly been set to zero.

## Common menu flows

Session opens New, Browse/search, Rename current and Delete current directly.
Rename edits only the title. Delete requires confirmation and starts focused
on Cancel; the delete action is unavailable while generating.

Settings forms edit the user name and persona, prompt preset and base
instructions, sampling/context budgets and the model connection. They are
unavailable during a run. **Apply restarts the launch** so the new profile is
composed into fresh services rather than being swapped under a session that is
already running; the composer draft and the session directory carry across. Cancel
discards the form without changing anything. Changes are staged until saved as a
new profile. Save-as refuses to overwrite an existing file and stores only API-key
environment references.

Up/Down or Tab/Shift+Tab moves between single-line fields and Apply/Cancel.
Multiline fields keep arrows for editing; Ctrl+Up/Down moves between fields. Enter moves to the next
field or activates a button; Ctrl+O inserts a newline. Ctrl+S applies a
non-destructive form. Mouse clicks focus fields or activate buttons. Esc closes
the form or dropdown in one step. Keyboard menu navigation skips disabled
actions. Read-only inspectors support arrows, PageUp/PageDown and wheel scrolling.

Choice fields and protocol switches cycle with Space or a mouse click. Forms
expose their own Next/Apply/Cancel footer actions instead of unrelated hotkeys.

## Thinking service

GEKTOR connects to a model server someone else started. Start one with
`run-server-llama.cmd` or `run-server-kobold.cmd`, wait for it to report the
model loaded, then point GEKTOR at it.

Settings > Model selects the protocol, server address, model identifier and,
in Advanced, the environment variable holding an API key. Focus the model
field and press Space for models the server reports; typing a name that is
not on that list works too, since a server need not enumerate what it serves.

GEKTOR does not start, stop or restart that server. If it exits mid-run the
next request reports a connection error.

## Shared settings workspace

Model, Generation, Profiles, Application, You and Prompts share the Commander
palette and connected square frame. F6 focuses the category rail; arrows change category,
Enter returns to controls. Edits survive category switches. Ctrl+S applies all
staged pages, which restarts the launch. Esc cancels all staged edits; it never
clears history or the text draft.

Model shows connection, model and GPU layers in local mode. External mode shows
protocol, endpoint and model. F4 reveals technical paths and authentication settings.
Enter on a selection opens a searchable list; arrows/Enter choose, Esc returns.
Local GGUF discovery runs asynchronously. Ctrl+B browses folders and model files;
Ctrl+L opens the folder-path control; Ctrl+R scans/refreshes. Full locations and file
size are shown below the list. Server model names are discovered through the
configured protocol; unsupported discovery falls back to manual entry.

Explicit-profile launches keep runtime edits in memory until saved as a profile.
The common launcher also remembers its effective configuration in
`~/.gektor/application.json`;
source profile files are not overwritten. Profiles can be loaded as staged settings
and saved as new portable copies.

A launch without `-profile` generates one from the flags it resolved, plus the
context window the backend reported, and says so at startup. Settings reads the
profile rather than the flags, so without one it was disabled outright and could
not be entered at all. Generated settings land in `profiles/custom` like any other
unsourced edit; the generated profile is never written back to a file that did not
exist.

### Profiles

Profiles > Load setup opens saved JSON profiles beside gektor.exe. Choosing one
stages its configuration without applying it; Apply is a separate decision, and a
loaded profile with no field edits after the load still commits. Changing
categories displays staged values. Ctrl+D saves a new portable copy using the
entered name. Existing files are never overwritten. The picker has separate
Ctrl+D drive browsing, distinguished by its footer.

A profile that predates the `conversation` → `identity` rename loads unchanged;
see [cli.md](cli.md#profiles).

## Common launcher

    .\gektor.exe -tui

The menu is Session, Run, View, Settings, Help. Run selects Plan or Act; the
current choice is marked. Settings opens the workspace as Model, Generation,
Profiles, Application, You and Prompts. Application selects the workspace. Enter
opens choices or file/directory pickers. F6 moves to the category list; unsaved
values remain staged across categories. Apply with no changes does nothing.
Cancel discards the staged values.

A launch requires a workspace and starts in Plan. Act enables tool execution under
the existing permission gates. Applying settings in an existing session preserves
the Plan/Act choice and never starts a run.

Common-launch sessions live under `~/.gektor/sessions`, with `state.json` as the
complete archive and compacted context projection. Global settings provide
the current service profile; the session restores its saved Plan/Act choice. Standalone CLI sessions live beside them in
`~/.gektor/tasks`.

## Panel status and manual compaction

A connected horizontal rule separates the panel titles from their contents.
The status row contains plain labels; clicking it does not open menus.
Session > Compact history (or /compact) works while idle and uses the same
extractive checkpoint compaction the loop applies at its own threshold. The
model receives a summary and retained tail. The full transcript remains
available for reading and search; a marker reports the context reduction.

## View menu

The View menu splits into what the reader hides and what they can do,
with a rule between the two zones:

```text
✓ Hide Sessions            Ctrl+B
✓ Truncate AI reply        Ctrl+G
✓ Hide SYS messages
  Hide labels
──────────────────────────────
  Jump to latest        Ctrl+End
  Select Theme…
```

A checkmark states the condition the row's own label names, so it
appears on "Hide Sessions" only while the sidebar is hidden. It fills the
label indent every menu row already carries, so a tick costs no width and
never moves the labels. Plan/Act share the same indent; a menu with nothing
to toggle simply leaves it blank.

**Hide Sessions** (`Ctrl+B`, `F6`) hides the Sessions pane. **Truncate AI
reply** (`Ctrl+G`, `F3`) collapses thinking and long tool output. **Hide
SYS messages** drops harness notices, and nothing else: errors,
warnings, permission questions, tool results stay,
because they are the record of what happened. **Hide labels** drops the
`[TAG]` rows and keeps the name rows, so every block still says who
spoke.

All four are display-only and session-only. Hidden blocks stay in the
model history and return unchanged when a filter is lifted; nothing is
persisted, so a restart always shows the full transcript. **Jump to
latest** (`Ctrl+End`, `F7`) scrolls to the newest content, and **Select
Theme…** opens the Dark/Light picker.

## Themes and session rows

View > Select Theme picks Dark or Light. Themes use shared semantic colors for primary
text, secondary text, accent, selection, disabled controls and status. The choice
is saved as an application preference and does not change the session, model or
permissions. Session rows use one full-row selection highlight, including the age.
There are no activity prefixes or competing current-session bars; the lower detail
line names the selected session's status.

## Message layout and custom setups

A name row opens each run of blocks by one participant; after it, blocks
carry only a label row and their body at the left edge. The user name
follows Settings > You > Your name. Asterisk emphasis
uses the theme's secondary grey in both Dark and Light; code spans retain code
styling. Display labels and colors are never added to model message content.

Applying model, identity, prompt or generation changes automatically saves a
setup under `profiles/custom` and selects it as the profile source. Further
changes update that custom profile atomically. Bundled and external source
profiles remain intact. Profiles can load these setups or save a named copy.
A workspace change alone does not create a custom profile.

## Prompt presets

Settings > Prompts > Prompt preset selects a reusable response style. Custom (the
default) keeps the base prompts and profile settings without adding preset
instructions. Concise, Coding, Investigation and Explanation add a style section
to the compiled instruction. Conversation adds a natural, direct conversational
style. Presets affect presentation; tools, permissions and Plan/Act constraints
remain controlled by the harness.

The picker describes each preset. Settings > Prompt preview shows the applied
preset and its compiled instructions — the profile's contribution to the system
prompt, not the agent's own Plan/Act instructions, which are not configurable.
Apply saves the selection in the custom profile as `identity.prompt_preset`.

Preset text can reference `{{assistant}}`, `{{user}}` and `{{persona}}`, which
are substituted when identity is compiled. Raw profile system/post-history text
is passed through rather than expanded as a template; recognized unresolved
name macros produce warnings and remain literal text.

## Experimental session retrospective

`/eval` reviews the last archived execution in a fresh model context while the
task is idle. `/eval N` selects an execution number; `/eval session` selects the
whole session. Add a focus after an explicit scope, for example
`/eval last check the test evidence`. Old sessions without recorded execution
boundaries require `/eval session`; GEKTOR does not guess task boundaries.

The reviewer reads the full JSON archive, including messages hidden by any number
of compactions, in bounded pages. Run boundaries use archive indices. Retries and
answers to `ask_user` retain the preceding task in scope, so ranges may overlap.
The dialog reports inspected messages and partial reads. Twelve model steps limit
the review; a long session can exceed that budget, so coverage is not guaranteed.
Esc cancels. Task history, draft, step budget and token meter stay unchanged.

The report is the model's assessment of historical evidence, not an independent
correctness verdict. The reviewer has only `read_history` and `submit_evaluation`:
it cannot run tests, inspect current files, edit code or delegate. Findings must
cite messages actually read in the selected scope. The latest report replaces
`evaluation.json` next to that session's `state.json`.

Optionally, up to three observations are appended to
`skills/project-memory/SKILL.md` and linked from `skills/README.md`, preserving
existing catalog entries. Each needs a condition, a lesson, and a literal quote
from an explicitly successful archived tool result. This gate checks provenance,
not the general truth of the lesson. Entries remain historical observations to
check against current files; they are not promoted to system instructions.
Duplicate observations are skipped, and memory is capped at 64 KiB. A review can
legitimately produce no memory. Cancellation or an archive changed during the
model review prevents saving its results.

This TUI command is distinct from `cmd/eval`, the repeatable scenario benchmark.

## Implementation boundaries

Reusable form, composer, list-selection and transcript viewport models live under
`internal/tui/components`. GEKTOR supplies styles and application data, routes focus,
and handles effects; components own their local editing/selection/scroll state.
Session indexing lives in `the TUI itself`, independently of terminal
rendering. See [TUI ownership](tui-ownership.md) for enforced boundaries and limits.

While the agent is running, Enter sends ordinary text to the current run.
Messages wait until the current model/tool step completes, then enter history
in submission order, including resolved file references and images. A pending
count stays visible until delivery. This does not reset the step budget or
interrupt a tool. Commands that replace the session or change run settings
still wait for the run to finish. Messages received during cancellation or
failure are saved in history for an explicit retry. A message racing normal
completion uses the existing follow-up queue instead.
