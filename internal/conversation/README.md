# Conversation snapshots

`conversation` owns durable message history and its compacted model view.
Start with `conversation.go`: `Snapshot`, its metadata, construction, and context
projection. It is caller-owned data, not a background session service.

- `storage.go`: load, decode, validate, and replace snapshots; read archive or context.
- `context.go`: prompt revisions and request inspection metadata.
- `history.go`, `children.go`, and `failure.go`: archive browsing and recorded results.
- `title.go`, `text.go`, and `workspace.go`: display titles, text, and workspace lookup.

The archive retains original messages; compaction changes the model projection.
Keep persisted field names, run ranges, and inspection validation intact.
Construction and context projection copy message slices, not every nested value;
callers must not assume a deep copy of attachment or tool-call data.
Saving stages a complete candidate before replacing the destination.

Follow [Go style](../../.agents/skills/go-style/SKILL.md) and
[Go architecture](../../.agents/skills/go-architecture/SKILL.md).
