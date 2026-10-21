---
name: dev-server
description: Read before testing anything that runs a local server (npm run dev, a Go http server, etc).
---

# Testing a dev server

A server process never exits on its own — that's the whole point of it.
Treat it differently from a normal command:

1. `start_background` with the actual run command (`npm run dev`, `go run .`,
   etc). Never `run_shell` for this — it blocks until the command exits,
   which a server never does, so run_shell will just time out and kill it.
   Keep the returned process id and next_cursor.
2. Wait a couple seconds, then `check_url` against the URL it's supposed
   to serve. A startup banner saying "Local: http://localhost:5173/" is
   not proof it's reachable — `check_url` is.
3. If `check_url` fails: call `check_background` with the process id and
   `cursor` set to the latest `next_cursor`. It returns only new stdout/stderr
   plus the next cursor. Use `cursor: 0` only to deliberately replay retained
   output; an eviction marker means part of the log is no longer available.
   Real errors (port already in use, missing dependency, syntax error) show up there. Diagnose from that, not from
   guessing.
4. Fix the real cause, then repeat from step 1 (start a new background
   process — the old one may need `stop_background` first if it's still
   holding the port).
5. Use `stop_background` when checks no longer need the server. Managed
   background processes are cleaned up when the frontend exits; do not promise
   that a server will survive GEKTOR closing. For a persistent server, give the
   user the startup command to run in a separate terminal.
