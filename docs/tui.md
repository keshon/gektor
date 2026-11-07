# Terminal guide

GEKTOR has two ways to work:

| Mode | Use it when | What you save |
|---|---|---|
| **Agent** | You want to discuss a task, explore a project or ask the model to do work | A conversation session |
| **Workflows** | You want to define steps and run them again with different inputs | A recipe and a separate record of each run |

Both use the selected workspace: the folder GEKTOR works in.
Agent's **Plan / Act** choice is separate from these two modes.

## Start and choose a workspace

On Windows, run `start-gektor-agent.cmd`, or `start-gektor-workflows.cmd`
to open Workflows directly. On other systems, run `gektor -tui`;
add `-tui-screen workflows` to open Workflows.
Source checkouts rebuild through the Windows launcher; release archives use
the supplied binary.

For a local model, start its server first and wait for it to load.
Open **Settings**, then **Model**, to set the protocol, address and model.
For the ChatGPT connection, run `gektor auth login` first.
See [setup](bootstrap.md) and [connection options](cli.md).

Choose your project folder under **Workspace** in Settings.
In the folder browser, **Select current folder** selects the displayed folder.
Opening a folder in the browser only browses it.

Choose **Apply**, or press `Ctrl+S`, to save settings.
**Cancel** discards the pending edits.
Explicit command-line flags can override saved settings for a launch.
Browser, debug and permission flags keep normal settings behavior.
Workspace, profile, model, generation and identity overrides stay temporary
until you apply them in Settings. A new installation starts in Plan even when
extra flags are supplied; `-plan=false` explicitly starts in Act.

Click the top menus, or press `F9` and use the arrow keys.
The bottom row shows actions for the current screen. Press `F1` for help.

### Other settings

| Section | What it changes |
|---|---|
| Model | Connection and model selection |
| Generation | Response limits and sampling settings supported by that connection |
| Profiles | Load or save a group of settings |
| Workspace | Project folder |
| You | Your displayed name and persona |
| Prompts | Response style and base instructions |

Loading a profile stages its values; choose Apply to use them.
Changing settings does not send a task to the model.
See [profiles](cli.md#profiles) for the file format and command-line use.

## Agent

Agent is the chat screen. You describe a task; the model chooses its next steps.

### Do a task

1. Check the workspace and model in the header.
2. Choose **Plan** in the Agent menu to discuss and inspect, or **Act** to allow changes.
   Some launch paths call this menu **Run**.
3. Type your request and press `Enter`. Use `Ctrl+O` for a new line.
4. Read the reply and tool summaries. Answer questions or permission requests.
5. Review the result. Send a follow-up in the same conversation if needed.

Plan uses read-only tools. Act allows changes under the current permissions.
Switching between them keeps the conversation.

For example, start with:

> Find where this app saves settings. Explain what happens when saving fails.

After reviewing the answer, switch to Act and ask:

> Fix that failure path and run the relevant tests.

### Read history and tool results

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
| Agent thinking | Show full thinking; otherwise show a one-line summary |
| Agent tools | Show tool details; otherwise show compact rows |
| System messages | Show system notices |
| Speaker names | Show participant names |
| Message labels | Show tags such as THINKING and RESPONSE |
| Outlined frame | Show pane borders |

`Ctrl+G` toggles full thinking. It does not shorten the normal reply.
View choices and the theme are saved. Hidden content remains in saved history.

### Stop, retry and continue

Press `Esc` to stop an active turn. Stopping does not undo completed changes.
If a popup is open, Esc may close it first.

You can send another message while Agent is working.
It waits for a model/tool boundary and does not interrupt a running tool.
`Ctrl+E` brings the latest pending message back to an empty draft.
`Ctrl+X` removes the latest pending message.

The **Steering** setting, or `/steering on|off`, controls delivery.
With steering enabled, new input can withhold tools that have not started.
With it disabled, the current tool batch finishes first.
A new message never grants permission.

After a failed turn, `/retry` continues from the saved checkpoint.
It does not roll back files. Read the error and check completed work first.

### Permissions

A permission request shows the operation and its target.
In Agent's permission prompt, `Y` allows once. `A` opens confirmation for
allowing the same tool and target for this run. `N` lets you add a redirect
note. Esc at the first prompt denies the request.
An explicit deny rule cannot be overridden here.

Shell commands, Git commands and external tools run with your account's
permissions. The workspace folder is not a sandbox for those programs.
See [security](../SECURITY.md).

### Save and reopen a conversation

Conversations are saved under `~/.gektor/sessions/` in the normal TUI launch.
Choose **Open** in the Session menu, or press `Ctrl+P`, to search and reopen one.
Opening a session does not send a new model request.
Use `Ctrl+R` in the session list to rename it, or `Ctrl+D` to confirm deletion.

Choose **New** in the Session menu, or press `Ctrl+N`, for a fresh conversation.
Unsent drafts are kept while the app is open, but are not saved across exit.

Long conversations may be compacted to fit the model's context window.
The full saved archive remains available to read and search.
You can also use `/compact` while idle.
A session is not a backup of project files.

## Workflow workspace

A workflow is a set of saved steps.
The runner follows those steps instead of asking a model to choose each action.

| Term | Meaning | Available today |
|---|---|---|
| **Recipe** | Instructions: steps, connections, inputs and outputs | A JSON file in the workspace |
| **Run** | One execution with its input values and results | Saved separately in your GEKTOR user directory |
| **Template** | A reusable starting point copied into a new recipe | No template library yet; copy a recipe manually |
| **Schedule** | A trigger that starts a workflow at set times | Schedule card in the editor; executed by the workflow service |

You can edit and run workflows without a model connection.
AI change requests need a model. A model-generation node also needs its configured model.

### Open or create a recipe

Finish or stop the active Agent turn first.
In the **Workflows** menu:

- **Open recipe…** opens a workspace recipe.
- **New recipe…** starts one.
- **Open workflows**, or `/workflows`, enters the editor.

The editor has **Workflow / Node / Run / View / Settings / Help** menus.
Choose a recipe path inside the workspace.
For example, `workflows/image-report.json` is a useful convention,
not a required folder.
There is no automatic recipe library or shared user-directory recipe lookup yet.

In the editor, `Ctrl+O` opens, `Ctrl+N` creates and `Ctrl+S` saves.
Save before replacing a draft.
New/Open asks before discarding unsaved changes.

### Read the nodes and connections

Each card shows its name, operation, input and output ports, and status.
The selected card has a double border.

- Inputs are on the left. They receive values.
- Outputs are on the right. They pass results to other steps.
- Solid lines carry data, such as text from one step to another.
- Dashed lines set order. The diamond ports **then** and **after**
  mean “wait for this step,” without passing a value.
- Filled ports are connected; empty ports are not.
- A crossing is not a connection.
  A `!` at an output means the wire could not be drawn.
  The recipe still keeps the connection.

**Not run** means there is no run result on that card.
It does not mean the recipe has passed validation.
Steps execute one at a time, even when the graph branches.

Click a card or use the arrow keys to select it.
Drag empty space to pan. Use `Shift+wheel` for horizontal movement,
or `Alt+arrows` to pan with the keyboard.

**Lock node positions** in View (`G`) prevents accidental card movement.
You can still pan, inspect and edit.
When unlocked, drag cards or use `Shift+arrows` to move them.
`L` arranges them automatically; `F` shows an overview.

### Inspect a step

Select a node and press `Ctrl+T`. The inspector has three tabs:

| Tab | What it answers |
|---|---|
| **Properties** | What values does this step use? Where do they come from? Who uses its outputs? |
| **Result** | What happened in the selected run? What values and errors were recorded? |
| **Code** | What script does this step use, if it has one? |

Click a tab or press `1`, `2` or `3`.
`Tab` moves focus between canvas and inspector.
In the focused inspector, left/right changes tabs and up/down scrolls.

Properties uses the same port names as the cards.
Result needs a selected run; a draft alone has no recorded result.
`Ctrl+J` shows raw data when you need the full record.
Close the inspector with its close button or focused `Esc`.

### Change a recipe

1. Press `A`, or choose **Add node…** in Node, to select an operation.
2. Press `Enter` on a draft node to edit its arguments.
3. Click an output port, then an input port, to set their connection.
   Review the mapping in the form before applying.
4. Choose **Behavior…** in Node (`E` on the canvas) for conditions,
   repeat-for-each, ordering, review and retry settings.
5. Choose **Edit workflow…** in Workflow to change its name, inputs or outputs.
6. Save with `Ctrl+S`.

`B` opens connections for editing or removal. `C` opens a connection form.
`Delete` removes a node. `Ctrl+Z` undoes a recipe change; `Ctrl+Y` redoes it.
These controls do not undo an executed operation.

In the focused inspector, `E` edits Properties or Code instead of node behavior.
Code has a separate script editor. Complex values may still need JSON.
`J` on the canvas opens the full recipe JSON.

Press `I` to describe a change to the model.
Review its proposed changes before applying them with `S`.
Esc discards the proposal.
Applying changes updates the draft; it does not save or run it.
Save and validate after reviewing it.

### Validate and run

1. Press `V`, or choose **Validate…** in Run, to check the recipe.
   Supply input values if asked. Fix any reported errors.
2. Press `R`, or choose **Run…** in Run, to execute it.
3. If it declares inputs, a form shows their defaults.
   Change the values for this run, then continue.
4. Read and answer any permission requests or step reviews.
5. Select a node and open **Result** to inspect what happened.

Inputs are values the recipe expects from you, such as a folder, a label
or a list of numbers.
They let you reuse the same steps with different data.
Text fields take plain text.
List fields accept a JSON array such as `[2, 3, 5]`,
or comma-separated JSON values such as `2, 3, 5`.
Other complex values use JSON. Recipes without inputs skip the form.

**Run with JSON…** in Run is an advanced option for entering the complete input
object. It is not needed for ordinary runs.

Validation checks the recipe and input types.
It does not prove that a later file, server request or model call will succeed.
The TUI runs the current draft; save it to reopen those edits later.

Press Esc during execution to request cancellation.
Completed writes and requests remain.
A failed run stops the remaining work; inspect results before running again.

### Schedule a workflow

Save the recipe first. In **Node**, choose **Schedule trigger**.
You can also press `A` and choose **Schedule trigger** from the node types.

If the recipe has inputs, fill in the values to use for scheduled runs.
Then choose **Every hour**, **Daily**, or **Cron**. For Daily, enter a time
such as `09:00`. For Cron, enter a five-field expression such as `*/15 * * * *`.
Set the time zone explicitly; the default is `UTC`.
Choose **Save schedule** to enable it.

A Schedule card appears on the canvas. It starts the whole recipe; its
connections are automatic. Select it and press Enter to change the timing,
input values or enabled setting. Delete removes the trigger without deleting
queued jobs or saved runs. **Refresh schedules** in Run reloads CLI changes.

Keep the workflow service running for this workspace:

```powershell
.\serve-gektor-workflows.cmd -workspace "D:\path\to\project"
```

The service uses its own permission rules. Closing the TUI is fine while the
service stays running. The inspector's **Service** tab explains this setup.

A schedule stores a copy of the recipe and input values. Saving later recipe
edits does not change that copy. Edit and save the trigger again to update it.
Schedules belong to the workspace and are stored in your user data, outside
the recipe JSON. Copying a recipe to another workspace does not enable its schedule.

### Review and reopen a run

Press `H`, or choose **Saved runs…** in Run, to open an earlier run.
It shows the recipe captured for that run and its recorded results.
Editing the current recipe does not change that record.

A step may pause for review before it executes.
Press `P`, or choose **Review…** in Run, to approve or reject it.
Approval continues that saved run without repeating completed steps.
Permissions still apply.

Saved runs are read-only.
Press `D`, or choose **Return to draft** in Run, to edit the draft again.
There is no general “resume any failed step” action.

### Try a small example

In a source checkout, the [transform-and-review example](../examples/workflows/transform-and-review/README.md)
lets you try a run without a model call:

1. Set the workspace to `examples/workflows/transform-and-review` in the checkout.
2. Open `recipe.json` in Workflows.
3. Press `V` to validate and keep the default numbers.
4. Press `R` to run, again keeping the defaults. Answer permission requests if shown.
5. The run pauses before the **double** step. Press `P` to review it and approve.
6. Select **double**, open the inspector and choose Result.
   Its output contains the doubled numbers: `4, 6, 10`.
7. Press `D` to return to the draft. Run it again with different input numbers.

This example changes data in memory and stores run results; it does not edit
project files. You do not need to change its script to try different numbers.

### Save, leave and reuse

`Ctrl+S` saves the recipe file.
Card positions and the position lock are stored separately in your user directory.
Moving a card does not change execution.

Choose **Return to chat** in Workflow, or press Esc on the canvas, to return
to Agent. The draft stays in memory while the app is open.
Save before quitting; this in-memory draft is not crash recovery.

To reuse a recipe in another project, copy it into that workspace.
Check its paths and required profiles.
Paths inside called recipes are also relative to the workspace,
not the recipe's own folder.
There is no automatic shared-template installation.

Background queue management and webhook setup use the
[CLI/service](cli.md#workflow-queue-and-service). Schedules can be managed
from the editor or the CLI.
Closing the TUI does not keep an ordinary foreground run alive.

See the [workflow reference](workflows.md) for available operations,
data mappings, execution rules and examples.

## Attach files and images

Type `@` to browse workspace files. For example:

```text
Review @README.md
Compare @"docs/design notes.txt" with @README.md
Describe @screenshot.png
```

Use arrows to choose a suggestion and Tab to insert it.
File suggestions insert a path; they do not send the message.

Text files must be UTF-8, up to 32 KiB each and 64 KiB combined per message.
Invalid, binary, missing or out-of-workspace file references stop submission
and keep the draft. A repeated reference attaches one copy.
The model receives file snapshots.
Queued references are read when their turn begins; retries reuse that turn's snapshot.
Attach only content you intend to send to the selected backend.

On Windows, copy a screenshot and press `Ctrl+Alt+V`, or use `/paste-image`.
Ctrl+V also works when the terminal forwards it.
Some terminals paste an image path instead.
A single pasted absolute image path can be imported, including from outside
the workspace. Typing a path or using a file tool keeps the workspace restrictions.

The composer accepts up to eight images, 20 MiB each and 40 MiB combined.
Clipboard images also have a 32-megapixel limit.
`Alt+1` through `Alt+8`, or `/detach N`, removes an attachment.
`Ctrl+U` clears the draft and attachments.

Image understanding needs a vision-capable model and backend.
ChatGPT accepts PNG, JPEG, WebP and non-animated GIF; convert BMP to PNG first.
llama.cpp vision also needs its vision projector.
GEKTOR's KoboldCPP connection currently rejects image attachments.

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
| Switch Plan / Act | F5 in a direct run, or the Agent menu |
| Inspect tools | Ctrl+T |
| Show full thinking | Ctrl+G |
| Latest message | Ctrl+End |
| Help / menus | F1 / F9 |
| Retry failed turn | /retry |
| Compact context | /compact |
| Show current run facts | /status |
| Quit | Ctrl+Q |

Type `/` for local commands.
Unknown commands show an error; they are not sent as model requests.
Ctrl+Q stops an active run first; press again after stopping to quit.
Ctrl+C aborts and quits immediately.

## Learning from a task

These Agent commands are experimental:

- `/eval` asks a model to review the last task's saved history.
- `/learn` asks it to propose one reusable lesson.
- Add `N` to select an execution, or `session` for the full conversation.
  For example, `/learn session`.
- `/learn review` reopens the latest proposal or saved review diagnostic.

The reviewer reads saved history. It cannot inspect current files or run tests.
Its report is a model assessment, not a fresh verification.
Keep the review dialog open to receive the result; Esc cancels it.

For a lesson, `S` saves, `E` edits first, and Esc/Enter skips.
Saved lessons become workspace `skills/learned-<id>/SKILL.md` files with
`evidence.json` beside them. Review both before sharing through Git.
They can guide later tasks but grant no extra permissions.

`/learn undo` retires the latest lesson saved in this app instance and workspace
if its guide has not been edited.
To retire an older one, rename its `SKILL.md` to `SKILL.retired.md`.
Start a new session to exclude guidance already loaded into the conversation.

These commands are separate from the developer's [eval runner](../eval/README.md).

## Troubleshooting

| Problem | What to check |
|---|---|
| Cannot connect to a local model | Is the server loaded? Do protocol and address in Model settings match it? |
| Mouse does nothing | Check that the terminal sends mouse events; in ConEmu, check its mouse settings |
| Ctrl+V does not attach a screenshot | Use Ctrl+Alt+V or /paste-image |
| Cannot edit a workflow node | Are you viewing a saved run? Return to draft with D |
| Cannot move nodes | Turn off Lock node positions in View |
| A workflow input asks for JSON | Use the expected type; lists can use comma-separated values |
| Need full failure details | Open the step's Result tab or the Agent tool inspector |

Agent sessions keep `diagnostics.jsonl` beside `state.json`.
It records request phases and tool boundaries, without message bodies or tool
arguments/results. It rotates at 2 MiB and retains one previous file.

Terminal margins belong to the terminal application.
For matching margins, use background `#F2F4F6` for the light theme or
`#11161C` for the dark theme in your terminal profile.

See [setup and saved data](bootstrap.md), [workflow reference](workflows.md)
and [security](../SECURITY.md) for more detail.
