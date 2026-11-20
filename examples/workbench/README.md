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
`internal` imports prevent use from unrelated modules. The host still routes
input to the active control and reads `Controls.Dialog` as an identity token.
It does not modify dialog internals. Markdown notes are rendered before opening
the detail dialog; changing the terminal size or theme reopens that dialog and
resets its scroll position. The example has no persistence, add/remove actions,
or background service. It demonstrates the current reusable boundary without
declaring that boundary a stable public API.
