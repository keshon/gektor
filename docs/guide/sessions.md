# Reopen a conversation and manage context

Use the session list to reopen a conversation. Compaction shortens the model's working context while retaining the saved archive. Sessions do not back up project files.

## Save and reopen a conversation

Conversations are saved under `~/.gektor/sessions/` in the normal TUI launch.
Choose **Open session…** in the Session menu, or press `Ctrl+P`, to search and reopen one.
The conversation opens at the latest reply. If you open a match from saved
message history, it jumps to that message. Opening a session does not send a
new model request.

For a new conversation, Agent tries to generate a short title from the first
message before requesting the first reply. If naming fails or times out, the
session uses the first line of the message.

Choose **Copy session ID** or **Copy session path** in the Session menu to copy
a reference to the current conversation. The path is the full session folder
path, containing `state.json` and its logs. These actions also work during a run.
On narrow terminals, use the Session menu (under **More…** when needed).

The list searches session titles and saved message history across workspaces.
Use `Ctrl+F` to toggle the current-workspace filter; each result shows its assigned
workspace. Opening a session restores that workspace, not the current directory.

Use `Ctrl+R` in the session list to rename it, or `Ctrl+D` to open the deletion
prompt. Press Enter to save a rename or `Y` to confirm deletion. Modern also
shows clickable **Save**, **Delete** and **Cancel** buttons in these prompts.

Choose **Session > Add to favorites** or **Remove from favorites** for the
current conversation. Favorites persist across launches and appear first
among the results that match your search and workspace filter.

Choose **Session > Delete sessions…** to select saved sessions for deletion.
Press `Insert` to toggle the current mark and move down one row, or click
the checkboxes to mark rows directly. `+` marks every matching session
and `-` clears those marks, including rows below the visible list. These
shortcuts work in both session pickers. Marks outside the current filter stay
selected. Paste a plus or minus sign to include it in a search.

Press `Delete` or click **Delete** to replace the picker with a confirmation.
It shows the session name for one session or the count for a batch. With no
marks, it uses the current row. Choose **Delete** to proceed, or press
`Esc` to return to your marks. **Cancel** has initial focus.

Choose **New session** in the Session menu, or press `Ctrl+N`, for a fresh conversation.
Unsent drafts are kept while the app is open, but are not saved across exit.

### Restore a missing workspace

If the saved directory is missing, or an older session has no workspace assigned,
opening stops with an explanation. Choose the intended workspace with **GEKTOR > Change workspace…**,
then press `Ctrl+W` on the session and confirm with `Y` or Modern's **Assign**
button. Press `N` or `Esc` to cancel. Reassignment updates the session's
workspace metadata and leaves project files where they are.

An explicit workspace switch also updates the active saved session
once the replacement services have been initialized, without needing another
agent run. A failed initialization does not stamp the new workspace.

### Session titles

New Agent conversations first use the opening message as a title. Before the
first task request, GEKTOR asks the selected backend once for a short title in
the user's language. This separate request has no tools or conversation history
and an eight-second timeout, so the first turn can take longer to begin.
A failed, timed-out or cancelled title request keeps the original title; manual
renames take precedence. Existing sessions and forks keep their titles.

## Compaction and earlier decisions

Long conversations may be compacted to fit the model's context window.
If the model cannot produce a usable checkpoint, automatic compaction can
continue with recorded facts and recent turns. The full saved archive remains available to read and search.
You can also use `/compact` while idle.
A session is not a backup of project files.

Ask the Agent to find an earlier decision or tool result, for example:
"Find where we discussed the queue bug and quote the original message."
It can search this session's saved archive even after compaction. It can also
inspect which messages and instructions were in its current model request.
Searching other sessions is not supported by these tools.
See [session history and context](sessions.md#session-history-and-context) for tool
examples and the limits of prompt version records.

## Session history and context

The Agent can search its saved conversation, including records removed from its
working context by compaction. Retrieval runs locally without calling a model
or a separate memory service. The Agent reads the tool result on its next turn.

Two read-only tools answer different questions:

| Tool | Use it for |
|---|---|
| `read_history` | Earlier user requests, decisions, messages and tool results in the saved archive |
| `inspect_context` | Which archive records are included in the calling model request, its instructions, advertised tools and recorded system prompt versions |

### Read earlier messages

For example, `read_history` with `{"query":"queue","count":3}` finds matching
records. Each result has a stable, one-based message ID. Open an original with
`{"start":42,"count":1}`. To continue a search, keep `query` and pass `next_start`
as `start`. For a long message, keep its ID and pass `offset` from `next_offset`
with `count:1` and no `query`.

Search is literal and case-insensitive, not semantic or a regular expression.
Queries allow up to 256 characters. `count` defaults to 8 and allows at most 10
messages. During an agent call, reading its own session stops before the calling
response, so the search call cannot become its own match. Child archive reads
use the child's saved history.

### Inspect the current request

Start context inspection with `{}`. The overview lists available instruction
sections and included archive ID ranges. Use `{"view":"instructions","section":"identity"}`
to read the current persona section, when present. Follow `next_offset` to read
the rest.

`{"view":"revisions"}` lists saved system versions;
`{"view":"instructions","revision":1}` reads one. Instructions return at most
2,000 characters per page; tool, skill and revision lists are paged too.

The overview also pages its archive ranges and rewritten IDs with `next_offset`.
Pass that value as `offset`, keeping the same view, section and revision.
Offsets start at zero and count characters for instructions, items for lists.
The `directive` and `summary` sections describe only the current request and
cannot be combined with a historical `revision`.

Archive presence does not prove current context inclusion. Included records may
also have shorter reused tool results or rejected calls removed; the manifest
marks those IDs as rewritten. Skill inspection lists retained successful load
records, not every available skill or proof that full skill text remains.

### Interpret saved prompt records

System prompt tracking starts with this feature. A prompt recovered from an old
archive is marked `saved_archive`, with original delivery unknown. New versions
are recorded before the provider call; a saved record alone does not confirm
that the backend received it. These records show text, not how a
settings change was applied. They do not expose provider templates, caches or
private reasoning. The latest request manifest is saved; full request bodies are
not duplicated on every step.

Search stays within the current saved session, or its own child archive selected
with `delegate_call_id`. A delegate gets its own archive. Cross-session search
is not provided. Without a saved archive, `read_history` reports that limitation;
`inspect_context` still works during an agent tool call.

## Remembering facts and preferences

`/memorize` saves your text directly, without a model request or a lesson review:

```text
/memorize "Use pnpm for this project"
/memorize project "Preserve single-row tool calls"
/memorize user "Prefer concise explanations"
```

The default scope is the current project. User memories apply across workspaces;
project memories apply only to the opened workspace. Changing branches or running
commands in a subdirectory does not change the project. Separate checkouts and
worktrees remain separate even when they share a Git remote.

`/memory` opens the memory list; `/memory query` filters it. The list shows scope
and whether an entry is selected for the next request. Open an entry to edit it,
move it between user and project scope, or delete it. `/memory undo` removes the
latest newly saved entry from this app instance if it has not changed. Saving an
exact duplicate does not create a second entry.

Memory stays in local GEKTOR user data, outside the repository. Project identities
are registered by canonical workspace path. After moving a folder, open its new
location and use `/memory reconnect` to enter the previous path. Reconnection is
explicit and does not merge two registered projects.

New and resumed task turns load a bounded snapshot of user and current-project
memory. Edits and deletions take effect on the next turn. Entries outside the
snapshot remain stored and can be found through the read-only `memory_search`
tool. Memories provide context; current instructions and permission rules take
precedence. Deleting an entry excludes it from persistent recall but does not
erase its text from previous conversations or their archives.

Use `/memorize` for an explicit fact, preference or constraint. Use `/learn` below
to derive a reusable procedure from session evidence. Memory is not automatically
extracted from chats, and saving a memory does not create a skill.

## Learning from a task

These Agent commands are experimental:

- `/eval` asks a model to review the last task's saved history.
- `/learn` asks it to propose one reusable lesson.
- Add `N` to select an execution, or `session` for the full conversation.
  For example, `/learn session`.
- `/learn review` reopens the latest proposal or saved review diagnostic.

Add plain text to steer the review: `/learn focus on recovering from failed tests`
or `/learn "how we verified the migration"`. Combine it with a scope, such as
`/learn session focus on deployment` or `/learn 2 "the retry procedure"`.
`/eval` accepts the same scope and focus syntax. Without a scope, only the last
run is reviewed. An initial unquoted `last`, `session`, or integer selects the
scope; quote the whole focus if it starts with one of those words or a number.
One enclosing pair of single or double quotes is removed when there are no
interior quotes of the same kind; backslashes and other quotes remain literal.
Exact unquoted `/learn review` and `/learn undo` are
controls; `/learn "review"` is a new review focused on that word.

The focus directs what to examine, but cannot serve as evidence for a lesson.
It is saved with the proposal and shown when reopened. Selecting `/learn` in
command completion leaves the composer open for this text; submit again to run.

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

These commands are separate from the developer's [eval runner](../../eval/README.md).
