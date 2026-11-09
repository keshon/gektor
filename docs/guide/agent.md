# Work on a project with Agent

Describe a task, inspect the proposed approach in Plan, then switch to Act when you want changes. Start with [Get started](../start.md) if you have not selected a workspace and model.

## Do a task

1. Check the workspace and model in the header.
2. Choose **Plan** in the Run menu to discuss and inspect, or **Act** to allow changes.
3. Type your request and press `Enter`. Use `Ctrl+O` for a new line.
4. Read the reply and tool summaries. Answer questions or permission requests.
5. Review the result. Send a follow-up in the same conversation if needed.

Plan uses read-only tools. Act allows changes under the current permissions.
Switching between them keeps the conversation.

For example, start with:

> Find where this app saves settings. Explain what happens when saving fails.

After reviewing the answer, switch to Act and ask:

> Fix that failure path and run the relevant tests.

## Read history and tool results

History contains messages, replies, thinking, tool calls and system notices.
A successful tool call does not mean the whole task has been verified.

Compact tool rows show the action and its target.
Click the tool name to inspect its arguments and output.
From the keyboard, use `Ctrl+T`, select a call and press `Enter`.

The inspector opens beside history, or fills the screen in a narrow terminal.
`Tab` changes focus. Arrow keys, page keys and the mouse wheel scroll it.
`Esc` closes the focused inspector; its close button works too.

Scrolling up lets you read without following new messages.
`Ctrl+End` or `F7` returns to the latest message.

The **View** menu controls how history looks:

| Option | When checked |
|---|---|
| Thinking | Show full thinking; otherwise show a one-line summary |
| Tool calls | Show tool details; otherwise show compact rows |
| System messages | Show system notices |
| Speaker names | Show participant names |
| Message labels | Show tags such as THINKING and RESPONSE |

`Ctrl+G` toggles full thinking. It does not shorten the normal reply.
View choices and the theme are saved. Hidden content remains in saved history.

Settings > Appearance offers Dark, Light, One Half Dark, One Half Light, Solarized
Dark and Solarized Light. Choose a theme and press Apply to save it.
The `/theme <name>` command saves and applies it immediately; use `/theme` to
list the names.

## Stop, retry and continue

Press `Esc` to stop an active turn. Stopping does not undo completed changes.
If a popup is open, Esc may close it first.

You can send another message while Agent is working.
It waits for a model/tool boundary and does not interrupt a running tool.
The line above the draft shows how many messages are queued and previews the
latest one. Click the count or press `Alt+Q` to open **Message queue**.
The Queue shortcut stays visible during a run, including when nothing is waiting.
You can also open it from Run > Message queue.
Messages appear oldest first. Select any message, then use:

- `Enter` to read its full text and image names.
- `Ctrl+E` to edit it in a separate window. Save keeps its position and images.
- `Delete` to remove it.

Your current draft and its images stay intact. Cancel leaves the queued message
unchanged. Delivery does not wait for you to open the queue: a quick reply can
leave little time to edit. Once the agent receives a message, it is read only.

A row marked **No longer queued** is also read only; its disappearance alone does
not prove receipt. Closing the list drops these old rows.

If it is received while you are
editing, Save explains what happened and keeps your edit visible so you can copy
it. If a permission request or question interrupts the editor, `Alt+Q` reopens
it after you answer.

The latest-message shortcuts still work in the composer: `Ctrl+E` brings the
latest queued message back to an empty draft; `Ctrl+X` removes it.
A follow-up marked **Next run** will start after the current turn ends.

**Apply new messages during run** in Run, or `/steering on|off`, controls delivery.
With steering enabled, new input can withhold tools that have not started.
With it disabled, the current tool batch finishes first.
A new message never grants permission.

After a failed turn, `/retry` continues from the saved checkpoint.
It does not roll back files. Read the error and check completed work first.

## Background processes

Open **Run > Background processes…**, or use `/processes`, in Agent or Workflows.
The searchable table shows processes started by this GEKTOR launch, including
their managed ID, status, PID, session and command. It does not list arbitrary
system processes or reconnect to processes from an earlier launch.

Press `Enter` for exact arguments, workspace, owning session and captured output.
Output is a bounded snapshot; reopen it for newer output. Reading it does not
consume the Agent's output cursor or completion notifications. `R` refreshes the
list. `S` asks to stop the selected process and its children; confirmation starts
on Cancel. Stopping runs asynchronously. Exited entries remain available for
inspection, with Stop disabled.

Background processes can outlive an Agent turn. Closing GEKTOR or restarting it
to apply settings stops the processes owned by that launch. A saved workflow
schedule remains saved, but needs a running workflow service to execute.
