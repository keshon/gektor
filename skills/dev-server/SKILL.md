---
name: dev-server
description: Start, check and stop local development servers using managed processes.
---

# Development servers

1. Use start_program for a native executable with literal args, or
   start_background for shell commands/scripts (including .cmd/.bat).
   Servers do not exit normally: run_shell/run_program would time out and kill
   them. Never detach with Start-Process or shell backgrounding.
2. Save the returned id and next_cursor. Check the served URL with check_url;
   a startup banner is not reachability evidence.
3. On failure, check_background with the latest next_cursor as cursor.
   cursor=0 replays retained output; eviction means logs are incomplete.
   Diagnose actual errors such as a busy port or missing dependency before retrying.
4. Fix the cause; stop_background the old owned id before restarting if needed.
5. Stop owned servers when done. Frontend exit cleans up managed processes;
   for persistence beyond GEKTOR, give the user a command for a separate terminal.
