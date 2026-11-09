# Reopen a conversation and manage context

Use the session list to reopen a conversation. Compaction shortens the model's working context while retaining the saved archive. Sessions do not back up project files.

## Save and reopen a conversation

Conversations are saved under `~/.gektor/sessions/` in the normal TUI launch.
Choose **Open session…** in the Session menu, or press `Ctrl+P`, to search and reopen one.
Opening a session does not send a new model request.

The list searches session titles and saved message history across workspaces.
Use `Ctrl+F` to toggle the current-workspace filter; each result shows its assigned
workspace. Opening a session restores that workspace, not the current directory.

Use `Ctrl+R` in the session list to rename it, or `Ctrl+D` to confirm deletion.

Choose **New session** in the Session menu, or press `Ctrl+N`, for a fresh conversation.
Unsent drafts are kept while the app is open, but are not saved across exit.

### Restore a missing workspace

If the saved directory is missing, or an older session has no workspace assigned,
opening stops with an explanation. Choose the intended workspace with **GEKTOR > Change workspace…**,
then press `Ctrl+W` on the session and confirm with `Y` to reassign it (`N` or
`Esc` cancels). Reassignment changes metadata, not project files.

An explicit workspace switch also updates the active saved session
once the replacement services have been initialized, without needing another
agent run. A failed initialization does not stamp the new workspace.

### Session titles

New Agent conversations first use the opening message as a title. After the
first answer, GEKTOR asks the selected backend once for a short title in the
user's language. This runs separately from the conversation, with no tools or
history. A failed or cancelled request keeps the original title; manual renames
always take precedence. Starting another turn cancels a title request in progress.
Existing sessions and forks keep their titles.

## Compaction and earlier decisions

Long conversations may be compacted to fit the model's context window.
The full saved archive remains available to read and search.
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

These commands are separate from the developer's [eval runner](../../eval/README.md).
