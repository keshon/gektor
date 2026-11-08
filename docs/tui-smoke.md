# Session recovery smoke check

Use a real terminal. The helper starts a local fake backend and gives the child
process a temporary home directory. It creates two workspaces and four saved
sessions: A, B, an unassigned session, and a session whose folder is missing.
No personal sessions or real model service are used.

```text
go build -o bin/gektor.exe ./cmd/gektor
go run scripts/tui-smoke.go -binary bin/gektor.exe
```

On Linux or macOS, build `bin/gektor` and pass that path instead. Add `-keep`
to retain the printed temporary directory for inspection. Otherwise the helper
removes its fixtures after closing GEKTOR.

1. Open `/sessions`. Open B and type an unsent draft.
2. Open A. Confirm the header changes to workspace A and B's draft is absent.
3. Reopen B. Confirm its draft returns. Clear it before sending anything.
4. Reopen A, start `/new`, and send a short message. The fake backend answers
   `Local smoke reply.`. Reopen that new session from B and check the A header.
5. In the session list, use CTRL+F to switch between all and current workspaces.
6. Open the missing and unassigned sessions. Both must stay in the current
   workspace and offer CTRL+W to assign one.
7. Press CTRL+W. Check the destination, cancel with N, and confirm nothing
   changed. Repeat and confirm with Y. The selected session must now open.
8. Repeat the session-list checks at 40 columns. OPEN, CLOSE and navigation
   must remain visible. The assignment must show the end of the folder path.

Quit with CTRL+Q. The helper prints the number of mock model requests. Opening,
filtering and reassigning saved sessions should make none; sending a message
should increase it. This counter does not include model-list discovery.

Automated coverage in `internal/tui/session_workspace_test.go` checks 40- and
80-column rendering, scope filters, missing and legacy roots, draft transfer,
confirmation cancellation, and a refresh that removes or reorders a session.
These tests exercise state and layout, not native terminal input.
