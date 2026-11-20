# Reading-list workbench

A small offline app that uses the shared terminal controls for a book list,
Markdown notes, and an edit form. Books and edits exist only in memory. The app
does not load GEKTOR settings, sessions, workflows, roles, or model providers.

From the repository root:

```sh
go run ./examples/workbench
go test ./examples/workbench
```

The first Go build may download module dependencies. Running the app makes no
network requests and writes no files.

| Key | Action |
| --- | --- |
| Up / Down, Enter | Select a book and read its notes |
| E | Edit the open book |
| Tab / Shift+Tab | Move between fields and form buttons |
| Ctrl+G | Select all text in the focused field |
| Ctrl+S | Save a valid draft |
| Esc | Cancel the draft, return to the list, or quit from the list |
| Ctrl+R | Refresh the list, sorting books by title |
| Ctrl+T | Switch this app between dark and light palettes |
| Ctrl+Q | Quit from any screen |

List rows, form controls, and bottom shortcuts also accept mouse input.

Refresh runs on a snapshot with a 250 ms delay to make cancellation observable.
Starting another refresh, opening a book, or quitting cancels the current job.
The app rejects results from replaced jobs and joins all workers on exit, even
if the terminal driver never executes the returned command. Editing is separate:
controls own a draft and emit an intent; the app validates it before updating a
book. Closing the form discards its draft.

`examples/workbench/workbench.go` owns navigation, books, the palette, and control event handling.
`examples/workbench/refresh.go` owns asynchronous sorting and cancellation. The example imports only
`internal/tui/screen` and `internal/tui/components` from GEKTOR. Its tests exercise
editing through key and paste messages, replaced dialogs and jobs, shutdown,
selection after sorting, terminal dimensions, and the Bubble Tea input driver.

This is a second consumer inside the repository, not a published Go module:
`internal` imports prevent use from unrelated modules. The host routes application
shortcuts and passes remaining input to `DialogInput`; controls select the active
widget. The host retains an opaque `DialogID` and matches intent events by ID.
An opaque `TableID` retains list selection, filtering, focus and scrolling across
detail/edit overlays; the host supplies copied row snapshots and never retains a
mutable table widget.
Markdown detail content is refreshed in the same overlay lifetime on resize and
theme changes, preserving scroll and expansion. Forms retain their drafts and
focus. The example has no persistence, add/remove actions, or background service.
Controls still expose some dialog and form presentation state. Closing those
remaining ownership gaps and proving a pinned external consumer remain
prerequisites for a stable public API.
