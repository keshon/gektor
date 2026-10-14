# Terminal guide

Start TARS with `go run ./cmd/agent -tui`, or use `run-tui.cmd` on Windows.
The UI uses Bubble Tea v2. [CLI flags](cli.md) configure the backend,
workspace, permissions, verification, and response budgets.

## Windows launchers

`run-kobold.cmd` starts KoboldCPP in the current terminal and keeps its output
separate from TARS. The shipped paths are specific to the original machine:

- Executable: `E:\Projects\ai-text\koboldcpp-extracted\koboldcpp-launcher.exe`
- Model: `E:\Projects\ai-text\models\unsloth\Qwen3.5-9B-GGUF\Qwen3.5-9B-Q4_K_M.gguf`

Edit both variables and the server working directory when installing elsewhere.
The script binds `127.0.0.1:5001`, uses CUDA GPU 0, requests GPU offload for
all layers, and sets a 16,384-token context. It detects an existing KoboldCPP
server on that port rather than starting another one.

Once the model is ready, run `run-tui.cmd` in another terminal. It checks
`/api/v1/model`, builds `agent.exe`, and connects with the `kobold` dialect.
Its workspace is this checkout and its response budget is 4,096 tokens.
Extra arguments are forwarded to the binary:

```bat
run-tui.cmd -workspace D:\Projects\my-app -verify-cmd "go test ./..."
```

Keep flags before any task text. Closing TARS leaves the independently
started KoboldCPP server running. Closing the server terminal stops that server.
The optional `run-remote.cmd` and `run-serve.cmd` files are local, gitignored
helpers; they are not required or distributed as part of the setup.

## Screen and focus

The unbranded one-row global menu exposes Session, Run, View, Settings and Help dropdowns. A separate one-row status header inside the chat pane shows Plan/Act mode, model, context, and run status.
During a run, a Braille spinner and step/context/elapsed metrics appear
at the top; the Run menu includes tool and file counts. Estimated context
usage has a `~` prefix.

At 110 columns and 18 rows or larger, the 34-column Sessions pane appears
unless hidden with F6 or Ctrl+B. Tab switches between the pane and input.
Gray rules stay neutral; the focused pane label is colored. A cyan `▌`
marks the current saved session; `›` marks the row selected in the focused
pane. Ages are gray. `*`, `?`, and `!` indicate working/stopping, input
needed, and failed/unreadable states.

The transcript distinguishes YOU, TARS, and SYS/ERR notices. New turns use
extra spacing; horizontal rules are reserved for pane boundaries. Thinking and
tool results can be collapsed or expanded with F3 or Ctrl+G. Collapsing is
visual only; it does not remove model history. Scrolling up stops automatic
following; F7 or Ctrl+End returns to the latest content.

## Header badges

The session title and clock sit above a badge strip: Act/Plan, Ready/Work/Wait/
Stop/Error, model, context, and run progress. Work includes the braille spinner.
Click a chat-status badge to inspect it, or use F9 to open the global menu without moving the conversation. Pane
rules remain gray. Narrow terminals omit run and model badges before context.

Mode offers Act/Plan while idle; Activity offers Stop or Retry when applicable.
Model shows the configured backend, sanitized endpoint, transport, context
window, and response budget. These are configuration details, not server-health
measurements. Context offers manual compaction when an idle chat has saved state.

The context badge retains the latest request's prompt usage between turns.
`~CTX` marks an estimate. Opening or compacting a saved chat recomputes an
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
- Shift+Enter adds a line. Ctrl+O is the fallback.
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
- Enter executes a selected command when idle; during a run it queues the
  completed command. For files and directories, Enter inserts without sending.
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
provider or llama-server with VL weights and `--mmproj` is required. TARS's
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
- F5 Mode: switch Plan/Act when idle in a direct chat.
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

F2 or Ctrl+P opens a centered popup over the chat, using the same palette as
input suggestions. Search matches session names/IDs and saved user messages
and assistant replies, case-insensitively. Title matches rank ahead of history
matches; empty search shows recent sessions. Results show gray ages and a
highlighted matching excerpt. Up/Down and PgUp/PgDn move through results;
Enter opens the session without generating a response. History matches jump
to the matching message. Esc closes and preserves the composer draft.

Search covers the saved `state.json` conversation: compacted-away messages,
system/harness instructions, reasoning, and tool output are not searched.
Stop a running turn before switching sessions.

Focus the Sessions pane with Tab, then use Ctrl+R to rename or Ctrl+D to
confirm deletion. The same shortcuts remain available in the search popup.
Type a follow-up to continue. Saved Plan/Act mode is restored when
opening a direct chat. Missions can be resumed from the CLI; the session
search popup does not resume them as direct chats.

Drafts, input history, and the queued follow-up are kept separately for each
chat while the UI is open. They are not persisted across app restarts. Saved
model history and display metadata live under `.tars/tasks/<id>/`.

## Running, stopping, and permissions

Only one follow-up can be queued. Ctrl+E moves it into an empty draft;
Ctrl+X cancels it. It starts after a successful run if the draft is empty.
Failure, interruption, or a nonempty draft leaves it queued for editing.
Esc stops the run and waits for cleanup; it does not quit or erase the chat.
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
- `/new [task]`: start a new task; without text, reset to an empty chat.
- `/mode plan|act`: switch direct-chat mode; `/mode` alone shows the mode.
- `/status`: append current run facts to the transcript.
- `/retry`: retry the last failed turn from its saved starting history.
- `/compact`: shrink the saved conversation history when idle.
- `/quit` or `/q`: quit.

Unknown commands show an error and are never sent to the model. These are
local UI commands; they are not tools or RPC methods.

## Troubleshooting

- Server not ready: start `run-kobold.cmd` first and wait for loading to finish.
- Shift+Enter inserts correctly in classic Windows consoles through native
  key records. Other terminals must preserve key modifiers; Ctrl+O remains
  available if a terminal maps Shift+Enter to plain Enter.
- Strange scrolling: keep server logs in their own terminal. Do not redirect
  model/server output into the full-screen UI.
- No streaming text on KoboldCPP: that dialect uses unary requests. OpenAI
  and llama stream automatically in the TUI; the spinner still reports activity
  in unary mode.
- Large reference rejected: use a smaller file or ask the agent to inspect
  it with bounded file tools. Raising the generation budget does not raise
  attachment limits.

### Commander-style frame

The global menu and shortcut footer each occupy one row. Sessions remain in the left pane; the chat status, transcript, and growing multiline composer stay in the right pane. Tab switches pane focus, F6/Ctrl+B toggles the sidebar, and F2/Ctrl+P opens session search. Top-menu clicks use the same function-key routing, including disabled actions during approval. Narrow terminals hide the sidebar and reduce visible footer controls; full bindings remain available through Help.

The top row is an unbranded dropdown menu: Session, Run, View, Settings, Help. Click a category or press F9, then use Left/Right to switch categories, Up/Down to select, Enter to apply and Esc to back out. Session exposes new/search/compact/quit, Run exposes Act/Plan/status/stop, View exposes sidebar/details/latest, and Settings opens direct inspectors for coding runs and editable forms for tool-free chat. The function-key and Ctrl shortcuts remain in the footer and Help. Chat configuration now includes character/user, prompts, generation/context, model connection, save-as-profile and effective-prompt inspection. Voice-capable chat profiles expose a Voice menu for enable/disable, recording, greeting playback, settings and service details.

## Tool-free conversation mode

`agent.exe -profile profiles/llama-external.json -chat -tui` uses the same
Commander layout with CHAT shown in the status strip. It creates no coding
workspace or tool registry. `@file` stays literal text; chat does not read it.
The session pane browses `var/chats` beside the executable, or `-chat-dir`.
These archives contain successful complete turns and are independent of task
checkpoints under `.tars`. Search, reopen, rename, delete, new conversation,
retry and stop use the existing UI flow. Reopen requires matching system
prompt and generation settings. Act/Plan and coding compaction are disabled.
The line-based chat CLI uses unary requests. The chat TUI streams text for llama/OpenAI dialects; kobold uses a complete-response fallback.

Chat character cards render their opening message at the start of a new
conversation and on reopen. The greeting is a pinned assistant message in
the model prompt, separate from complete user/assistant turns. `/status`
shows the character name. Character controls are currently CLI/profile
settings, also editable through the Settings menu.

## Common menu flows

Session opens New, Browse/search, Rename current and Delete current directly.
Rename edits only the title. Delete requires confirmation and starts focused
on Cancel; the delete action is unavailable while generating.

Chat replaces coding Act/Plan with Stop response, Retry failed response and
conversation details. Settings forms edit character/user, prompt preferences,
sampling/context budgets and the model connection. They are unavailable during
a response. Apply validates first and updates the current session and archive;
history and draft are preserved. Cancel discards the form without changing the session.
Changes are in-memory until saved as a new profile. Save-as refuses to overwrite
an existing file and stores only API-key environment references.

Up/Down or Tab/Shift+Tab moves between single-line fields and Apply/Cancel.
Multiline fields keep arrows for editing; Ctrl+Up/Down moves between fields. Enter moves to the next
field or activates a button; Shift+Enter inserts a newline. Ctrl+S applies a
non-destructive form. Mouse clicks focus fields or activate buttons. Esc closes
the form or dropdown in one step. Keyboard menu navigation skips disabled
actions. Read-only inspectors support arrows, PageUp/PageDown and wheel scrolling.
Coding model/context panels are explicitly inspectors; changing the coding
backend still requires a restart. There are no redirects between common menus
and status-badge menus.

Prompt override switches and protocol choices cycle with Space or a mouse click.
Forms expose their own Next/Apply/Cancel footer actions instead of unrelated
chat hotkeys. The rendered frame can be inspected in [menu-flows.html](reports/menu-flows.html).

## Voice conversation MVP

Run `agent.exe -profile profiles/chat-veyra-voice.json -chat -tui -voice`.
The thinking endpoint still belongs to its external server. `-voice=false`
opens the same profile in text mode. Voice enable validates installed assets
and audio utilities; it does not load the model until synthesis is requested.

Voice > Play character greeting checks output without a microphone or thinking
request and warms the resident speech model. F8 or Ctrl+R starts recording;
wait for the Recording indication before speaking. Press it again to finish.
Whisper transcribes the complete WAV, then the visible user turn is submitted.
Esc cancels capture/recognition or stops generation and playback. A late result
from a canceled recording cannot submit a turn. Finish/cancel actions appear
in the recorder footer. An unsent text draft is preserved while recording and after submitting the voice turn.

Replies use ordered sentence chunks while llama/OpenAI text generation
continues. Long sentences split at clause/word boundaries; there are no
mid-word cuts. One PCM player stays open across chunks and drains the final
samples. Completed text remains saved if audio fails or is stopped.
Whisper and Chatterbox stay resident across turns and prompt edits, and owned
processes close on disabling voice or quitting. External thinking/recognition
services are never stopped by the application.

Voice > Speech settings edits microphone, Nano/Turbo selection, model directory
and recording cap (5–120 seconds). Applying speech settings disables the old
service; enable Voice again to use the new settings. Ordinary character and
generation edits keep the current speech worker warm.

Push-to-talk remains available; Ctrl+L enables hands-free listening and application-controlled interruption. This is not native model full duplex. Recognition and speech are
configured for English. Non-Latin replies fall back to text with an explicit
speech error. Reasoning blocks, fenced code and bracketed stage directions
are excluded from speech. Provider-specific vocal-cue negotiation is not yet
connected. Physical microphone/speaker acceptance remains a separate check.

## Managed thinking

`agent.exe -profile profiles/chat-veyra-local.json -chat -tui -voice`
uses the imported llama.cpp runtime and local Qwen starter model. Speech is optional.
Settings > Model connection selects external/managed ownership, the executable,
GGUF file, GPU layers, protocol and URL. Managed mode currently supports llama only.
Set Model directory to any model folder, including an LM Studio root; focus the
GGUF field and use Space/click to cycle recursively discovered files. A direct
GGUF path works without a directory scan. Models are used in place, never moved.

Loading starts on the first thinking request, with a five-minute readiness limit.
Logs are in var/thinking. Changing provider/model/context stops the owned server;
prompt and sampling edits retain it. Exiting closes the owned process tree.
Occupied ports are refused; external servers are never terminated. Model loading
may take time before the first reply; the normal generation indicator remains visible.

## Shared settings workspace

Thinking, Generation, Character, Prompts and Speech share the Commander palette
and connected square frame. F6 focuses the category rail; arrows change category,
Enter returns to controls. Edits survive category switches. Ctrl+S applies all
staged pages to the current conversation. Esc cancels all staged edits; it never
clears history or the text draft. Archive persistence succeeds before runtime
ownership changes. Changing the model/context restarts its owned thinking service;
ordinary prompts and sampling edits retain it.

Thinking shows connection, model and GPU layers in local mode. External mode shows
protocol, endpoint and model. F4 reveals technical paths and authentication settings.
Enter on a selection opens a searchable list; arrows/Enter choose, Esc returns.
Local GGUF discovery runs asynchronously. Ctrl+B browses folders and model files;
Ctrl+L opens the folder-path control; Ctrl+R scans/refreshes. Full locations and file
size are shown below the list. Server model names are discovered through the
configured protocol; unsupported discovery falls back to manual entry.

Explicit-profile launches keep runtime edits in memory until saved as a profile.\nThe common launcher also remembers its effective configuration in var/application.json;\nsource profile files are not overwritten. Character and microphone controls use the shared picker. Profiles can be loaded
as staged settings and saved as new portable copies. Speech provides a sample of
the applied built-in Chatterbox voice; custom voice references are not connected.

### Settings libraries and speech checks

Character > Card opens searchable PNG/JSON choices. Ctrl+B browses folders;
Ctrl+L edits the location, Ctrl+D lists drives and Ctrl+R scans the current root.
The selected card's name/description is loaded asynchronously below the list.
Plain portrait PNGs and unsupported cards cannot be selected.

Speech > Microphone discovers DirectShow audio devices through FFmpeg without
opening capture. Enter selects a name. Nano/Turbo remains a model selection, not
an invented list of voice identities: each adapter currently uses its built-in
English reference voice. Ctrl+P plays/stops a sample of the applied settings;
apply staged speech changes first. Closing settings or quitting cancels playback.

Profiles > Load opens saved JSON profiles beside agent.exe. Choosing a chat
profile stages its configuration; Apply updates the current session and history.
Changing categories displays its staged values. Ctrl+D in Profiles saves a new
portable copy using the entered name. Existing files are never overwritten.
The picker has separate Ctrl+D drive browsing, distinguished by its footer.

## Hands-free voice

Ctrl+L or Voice > Hands-free toggles continuous capture. Wait for Listening before
speaking. Silero VAD runs on CPU; 1.2 seconds of pre-roll protects quiet first words.
Default confirmation is 200 ms and end silence is 1100 ms. Speech > F4 Advanced
changes the detector/onset/end timing; energy detection is an explicit fallback.

Confirmed onset cancels active generation/playback. The new audio waits until
that request has finished canceling, then Whisper produces a visible user turn.
A continuation while recognition is in progress retains the earlier audio rather
than submitting an outdated partial question. Unsent text drafts are preserved.

Opening settings pauses live capture; Ctrl+L re-enables it afterward. F8 switches
to push-to-talk. A stalled microphone is reported and Ctrl+L off/on reconnects it.
Capture, VAD and recognition stop on disabling live mode or quitting. Use headphones
for interruption testing: this DirectShow path does not provide acoustic echo
cancellation, so speaker output can be picked up by the microphone.

## Application modes and shared settings

    .\agent.exe -tui

The menu is Session, Mode, Chat/Agent, View, Settings, Voice, Help. Mode selects
Chat or Agent; the current choice is marked. Settings opens the same workspace
in either mode: Model, Generation, Speech, Profiles, Application. Character and
Prompts are available only in Chat. Application selects the mode, workspace and
voice input style. Enter opens choices or file/directory pickers. F6 moves to the
category list; unsaved values remain staged across categories. Apply without
changes does nothing. Cancel discards the staged values.

Mode switches preserve the session directory, title and composer draft. Entering
Agent requires a workspace and starts in Plan. Agent > Act enables tool execution
under the existing permission gates. Applying service/model settings in an existing
Agent session preserves Plan/Act; it never starts a run. Switching modes while a
turn is active is disabled until that turn stops.

Common-launch sessions live under var/chats for compatibility with existing Chat
archives. settings.json records the saved mode and service profile. conversation.json
stores Chat history; state.json stores the Agent checkpoint. Opening a session restores
its mode and profile; Agent opens in Plan. Existing archives without settings use the
current service profile. Old standalone coding sessions stay in their workspace task
store. Full structured Agent history remains in its checkpoint journal; Chat receives
text dialogue projections and never exposes coding tools.

Voice input works in both modes. Chat speaks streamed sentences; Agent speaks its
completed answer and keeps intermediate code/tool output in the transcript. Opening
settings pauses hands-free capture; Ctrl+L enables it again after closing settings.

## Panel status and manual compaction

A connected horizontal rule separates the panel titles from their contents.
The status row contains plain labels; clicking it does not open menus.
Session > Compact conversation (or /compact) works while idle in both modes.
Agent uses its existing extractive checkpoint compaction. Chat retains the opening
exchange plus eight recent complete turns and writes a before-compact-*.json backup
beside the active archive. Chat compaction does not generate a semantic summary.

## Themes and session rows

View > Theme selects Dark or Light. Themes use shared semantic colors for primary
text, secondary text, accent, selection, disabled controls and status. The choice
is saved as an application preference and does not change the session, model or
permissions. Session rows use one full-row selection highlight, including the age.
There are no activity prefixes or competing current-session bars; the lower detail
line names the selected session's status.
