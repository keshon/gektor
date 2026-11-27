# Working on explicit memory

`Store` owns local SQLite connections for explicitly saved user and project facts.
Callers choose the directory and close the store. `DefaultDir` resolves the user's
`.gektor/memory` directory and rejects relative home-path fallbacks. `OpenExisting`
returns nil when there is no database, without creating a directory.

`memory.go` contains scoped reads and transactional writes. `storage.go` owns the
SQLite schema and connection. `recall.go` renders bounded, deterministic context
snapshots without cutting individual entries.

Project records belong to a stable registry ID associated with the canonical
workspace directory. Branches in the same directory share memory; other checkouts
and worktrees do not. Git remotes are not consulted. `Reconnect` moves a missing
workspace's registration to an existing unregistered directory; it cannot merge
projects or link two existing workspaces.

Writes use immediate transactions and expected revisions to reject stale edits,
deletes and scope changes. Exact active duplicates reuse the existing entry.
Updates preserve the original source references. Deletion marks the entry inactive;
it immediately disappears from lists, search and recall, but does not erase the
original conversation or securely erase the database file.

`List` returns only user entries and the selected project's entries, with optional
case-insensitive substring search. `Snapshot` uses stable scope/creation/ID order,
a Unicode-character budget and complete JSON entries. Omitted entries stay stored
and can be searched. No model inference, automatic extraction or embeddings occur.

Run `go test -race ./internal/memory` and `go vet ./internal/memory`. Tests cover
reopening storage, project isolation, user sharing, scope changes, stale revisions,
concurrent writers, deletion, bounded recall and moved-workspace reconnection.
