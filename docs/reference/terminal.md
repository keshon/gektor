# Terminal controls

Find menus, settings, keyboard bindings and local commands here. For a first task, use [Get started](../start.md).

## Common terminal entry

    .\gektor.exe -tui

First use opens in Plan and connects to the default external server described
in [Setup](../guide/models.md#model-server). Later launches reuse saved settings.
GEKTOR does not install model weights or manage the server.

Run > Plan or Act selects execution mode; Act enables tool calls under the
existing permission gates. GEKTOR > Change workspace chooses the workspace.
Settings > Launch profiles loads or saves setups; Settings > Model connection
selects the connection and model. GEKTOR also switches between Agent and Workflows.
Enter opens shared pickers.

Applying settings restarts the launch rather than reconfiguring in place, so the
new profile is composed into fresh services. The composer draft and the session
directory carry across the restart.

Choices and effective profile settings are saved under `~/.gektor/application.json`.
Explicit workspace, profile, connection, generation and identity flags override
saved settings for that launch. Applying settings through the UI saves them.
Runtime limits do not make a launch temporary: main-agent and delegate step
limits carry across TUI relaunches and are saved with preferences.
Direct API-key flags stay in memory across a restart and are never written
to preferences.

## Tools and Media preferences

Open **Run > Tools…** from Agent or Workflows, or type `/tools` in Agent.
Tools lists toolsets in a stable task order. Loading shows the default policy;
Status shows the current conversation. Load/Unload changes only that conversation.
Always-loaded toolsets cannot be unloaded. Unloading MCP tools keeps the server
connected. Details lists operations; Settings opens SSH or Media preferences.
Without an attached idle Agent conversation, session actions are disabled.

The searchable list shows built-in toolsets and configured extensions. Open a
row for Agent operations, matching workflow nodes, configuration source and loading policy. Inspection does not activate toolsets, connect extensions or run checks.

Select **Media**, then **Settings**, to inspect yt-dlp, FFmpeg, ffprobe, Deno and Node preferences.
**Operations** lists the supported Agent tools and workflow nodes. **Preferences**
edits the selected executable: leave its path blank to use PATH, select an
absolute executable path to override it, or mark it Disabled. Cancel discards the
draft. Apply validates and atomically saves `~/.gektor/applications.json`, then
relaunches at an idle boundary with the conversation and unsent input retained.
Save or discard an unsaved workflow before applying changes.

**Check executable** (`C`) explicitly runs the selected saved executable's bounded
version probe. Its result shows the path, version, failure reason and UTC check
time. Closing Tools cancels and joins a pending check. A version check does not
establish codec or remote-service compatibility. Native YouTube metadata,
transcripts and single-stream downloads do not need these local applications.
These preferences are shared by Agent and Workflow operations.

Open **SSH** for named connections. **Add** and **Edit** start with a connection
name, host, port, user and **Authentication: Password / Private key**. Port defaults
to 22 and stays visible in the basic form. Key authentication
shows a file chooser and an optional key passphrase. Saving a new password or
passphrase opens a separate masked prompt, then saves and restarts GEKTOR.

Existing credentials show **Saved — keep password/passphrase** with a **Change**
action. Keep leaves the credential untouched. **No passphrase** removes the
passphrase from that connection. Cancel leaves both the connection and its
credentials unchanged. There are no credential IDs to manage in the TUI.

**Advanced settings** contains the trusted hosts file (`~/.ssh/known_hosts` by
default), environment-based authentication and reuse of a
saved credential from another connection. Reuse selects the connection by name;
password and passphrase choices are kept separate. Existing environment-based
connections open Advanced automatically. Hiding it preserves their authentication.

**Test connection** verifies the saved profile's host key and authentication,
then closes. Unknown host keys are rejected. Its dated result does not establish
SFTP or hard-link support. Closing the view cancels pending work. Delete requires
typing the connection name. An independent workflow worker keeps its profile
snapshot until restarted.

On Windows, the masked prompt encrypts values for the current Windows account.
Ctrl+S saves; Esc discards the pending edit. Values never enter ordinary forms,
conversation history or application preferences. Terminal paste is accepted;
control characters and oversized input are rejected. Other platforms use key
files or environment variables under Advanced; there is no plaintext fallback.
Workers need the same account and credential directory for saved secrets, or must
inherit the selected environment variable. See [credential setup](operations.md#supplying-credentials-in-the-cli-and-tui).

Old credentials are retained when profiles change or are deleted, since other
connections and worker snapshots may still reference them. Session-only secret
storage is not implemented.
In Agent, the MCP manager is also available through `/mcp`.

## Start and choose a workspace

On Windows, run `start-tui-agent.cmd`, or `start-tui-workflows.cmd`
to open Workflows directly. On other systems, run `gektor -tui`;
add `-tui-screen workflows` to open Workflows.
Source checkouts rebuild through the Windows launcher; release archives use
the supplied binary.

For a local model, start its server first and wait for it to load.
Open **Settings**, then **Model connection**, to set the protocol, address and model.
For the ChatGPT connection, run `gektor auth login` first.
See [setup](configuration.md) and [connection options](cli.md).

Choose your project folder under **GEKTOR**, **Change workspace…**.
In the folder browser, **Select folder** selects the displayed current folder.
Opening a folder in the browser only browses it.

Choose **Apply**, or press `Ctrl+S`, to save settings.
**Cancel** discards the pending edits.
Small choices stay in the form. Use Left/Right or Space to change them, or click
an option. Checkboxes use Space to switch on or off. These changes wait for Apply.
When space is tight, a choice shows its current value with previous/next arrows.

Larger lists have a filter and Select/Cancel buttons. Click a row to select it;
press Enter or Select to accept it. Page Up/Down move through results, and
Ctrl+Home/End jump to the first/last result. Tab moves focus to the buttons.

A checkmark identifies the form's current value; the highlighted row is the
choice you are browsing. Scrolling over the footer does not move the list.

Folder browsing keeps its separate Open and Select folder actions.
Use `Ctrl+L` to enter a path. `Esc` returns to the previous filter and selection.

In multiline form fields, Enter inserts a new line and Tab moves to the next control.

Explicit command-line flags can override saved settings for a launch.
Browser, debug and permission flags keep normal settings behavior.
Workspace, profile, model, generation and identity overrides stay temporary
until you apply them in Settings. A new installation starts in Plan even when
extra flags are supplied; `-plan=false` explicitly starts in Act.

Click the top menus, or press `F9` and use the arrow keys.
The bottom row shows actions for the current screen. Press `F1` for help.

### Menus

The GEKTOR menu switches between Agent and Workflows, changes the workspace,
and quits the application. A checkmark shows the active mode.

| Menu | Agent | Workflows |
|---|---|---|
| Session / Recipe | New, open, rename and delete sessions; compact saved history | New, open and save recipes; edit recipe settings |
| Node | | Add, edit, connect and delete nodes; add schedule triggers |
| Run | Plan or Act; Ask permission or Full access; Live steering, message queue and background processes; permissions, MCP, stop and retry | Validate, run and stop; Ask permission or Full access; inspect run history and set up schedules |
| View | Show transcript details and jump to the latest message | Show the inspector and arrange the canvas |
| Settings | Appearance, model connection, response settings, launch profiles, identity and persona, agent instructions | The same shared settings |
| Help | Keyboard shortcuts, commands, agent instruction preview and About | Keyboard shortcuts for Workflows and About |

At narrow widths, More opens the menus that do not fit on the top row.
Keyboard and mouse reach the same actions. Disabled actions cannot run.

Appearance groups theme and panel frame choices. Apply saves them together;
Cancel discards the draft. The View menu cycles thinking and tool-call display
modes and toggles system messages, speaker names and message labels. See
[history display](../guide/agent.md#read-history-and-tool-results).
The agent instruction preview shows identity instructions, not the full model
request or its tool definitions.

The Compact history menu action asks for confirmation before changing saved
history. The `/compact` command remains a direct action.

### Other settings

| Section | What it changes |
|---|---|
| Model connection | Connection and model selection |
| Response settings | Response limits and sampling settings supported by that connection |
| Launch profiles | Load or save a group of settings |
| Identity and persona | Your displayed name and persona |
| Agent instructions | Response style and base instructions |

Loading a profile stages its values; choose Apply to use them.
Changing settings does not send a task to the model.
See [profiles](cli.md#profiles) for the file format and command-line use.

### MCP servers

In Agent, open **Run > MCP servers…**, or use `/mcp`. The table shows each server's
status and tool count. Use `/` to filter, arrows to select a row, and Tab to
reach its action buttons. Enter opens Details. Esc leaves the filter first,
then closes the table.

Connect starts a connection; Disconnect closes it. Edit changes the saved
definition. **More…** opens an attached menu for Enable/Disable, Delete, Add,
Paste JSON and Import file. Press `M` to open it, arrows to choose, and `Esc`
to return to the same server. Direct shortcuts still work outside the filter.
Delete asks for confirmation. Changes are available between agent turns.

Add, Paste JSON and Import file save server definitions. Importing does not
connect them. Closing a detail or edit dialog returns to the filtered list.

## Useful keys and commands

Shortcuts depend on the active screen.
For example, `Ctrl+O` inserts a new line in Agent but opens a recipe in Workflows.

| Agent action | Key or command |
|---|---|
| Send | Enter |
| New line | Ctrl+O |
| Clear draft | Ctrl+U |
| Recall submitted input | Alt+Up / Alt+Down |
| New session | Ctrl+N |
| Open sessions | Ctrl+P |
| Switch Plan / Act | F5 while idle, or the Run menu |
| Inspect tools | Ctrl+T |
| Cycle short, full and hidden thinking | Ctrl+G |
| Latest message | Ctrl+End |
| Help / menus | F1 / F9 |
| Retry failed turn | /retry |
| Compact context | /compact |
| Show current run facts | /status |
| Message queue | Alt+Q |
| Background processes | /processes |
| Quit | Ctrl+Q |

Type `/` for local commands.
Unknown commands show an error; they are not sent as model requests.
Ctrl+Q stops an active run first; press again after stopping to quit.
Ctrl+C aborts and quits immediately.

Tools can also be loaded or unloaded before the first message. These choices stay with the new task draft and are saved when the conversation starts. Existing conversations keep their own selections.
