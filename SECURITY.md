# Security Policy

GEKTOR runs locally within the security boundary of the user running it.
It is the operator's responsibility to monitor it or contain it in a
container or VM.

GEKTOR treats the local user account and files writable by that account as
inside the same trust boundary as the agent process. Deny rules are terminal;
Ask rules fail closed when no approval handler exists. Approvals marked
Always last only for the current run. `internal/workspace`
confines built-in file reads, writes, patches, moves, search and previews
through `os.Root`, including symlink/junction traversal checks at the operation.
Relative and in-workspace absolute paths share normalized relative permission resources;
moves apply the configured permission rules to both endpoints. The process
tools set a working directory and nothing else, and child processes inherit a
scrubbed environment (no `*_API_KEY`/`*_SECRET`).

The root handle is owned by the launch and closed at shutdown/restart.
Ancestor `AGENTS.md` files are explicit launch configuration; sessions and
immutable image snapshots live in the application data directory. Those
application-owned paths are separate from workspace file-tool access.

Skill libraries are additional read roots. `load_skill` reads discovered guides
and supporting files inside the selected package through its own `os.Root`;
it does not widen ordinary workspace file tools. The CLI includes user skill
directories and project directories; extra roots come from explicit capability
configuration. See [Extensions](docs/extensions.md) for precedence.

The default permission tripwires ask before process-kill commands such as
`taskkill`, `Stop-Process`, `pkill` and `killall`, including direct program/args
execution. These are operator checks, not a shell sandbox or an exhaustive
command parser. Agent-owned background jobs are stopped by their tracked id;
the optional browser MCP uses an isolated profile instead of the user's profile.

With `-browser`, activation through `enable_browser` may fetch the pinned
Playwright MCP package into npm's cache if missing. Saved catalogs restore
without connecting; preparation can occur on the first actual browser call.
Older transcripts without saved catalogs can prepare during migration.
Preparation is announced during activation
and time-bounded.
GEKTOR checks for Node/npm and the requested browser, but does not install them.

Executable and MCP packs are selected explicitly with `-capabilities`. Loading
a skill does not activate a pack. Each actual tool call is authorized under the
current policy; saved activation never saves an approval. Locally configured
`read_only` and `concurrent` declarations are trusted adapter metadata, not
process containment. Executable adapters share native process environment
scrubbing, output limits and process-tree cleanup.

GEKTOR relies on trustworthy skills, extensions and repositories. Files like
`SKILL.md`, `AGENTS.md` or comment instructions can prompt-inject the agent
and this cannot be protected against — only contained with approvals and
isolation.

## User attachments and approvals

Model tool calls go through policy; explicit user attachments are a separate
path. TUI `@path` references do not invoke read_file approval or sensitive-path
filtering. They intentionally send the selected file to the configured model
backend, which may be hosted. TUI references must remain inside the workspace
after symlink resolution and satisfy the documented text/image limits. CLI/RPC
image paths use the workspace's textual guard, like the file tools.

The default policy asks for sensitive reads and known destructive shell or
background command shapes. Explicit Deny cannot be approved away. CLI/RPC
`-yes` denies permission Ask requests;
it does not authorize every tool. TUI permission requests remain interactive.
All frontends remember an Always approval for the exact tool/resource pair
for the current run only. These grants are not saved with the session.

## Network probes

`check_url`, `webfetch`, `fetch_raw`, and `search_web` use GET requests and send no credentials: no
`Authorization`, `Cookie`, or `Proxy-Authorization` header leaves the
process (asserted by `TestHealthProbes_SendNoCredentials`), and the child
environment is scrubbed before any shell runs. `webfetch` and `fetch_raw` refuse
non-public URLs before dialing (loopback, intranet names, non-global IPs
including legacy `inet_aton` spellings). It validates redirects and every
resolved address, then connects to a validated literal IP to prevent DNS
rebinding. Web search uses the same public-destination transport.
Environment proxies are disabled. `check_url` permits loopback
because probing a just-started dev server is its job, and returns only a
status plus a 512-byte prefix. A health probe must never become a
credential or intranet oracle: any new network tool keeps these rules.

Repository search follows Git ignore rules (tracked files remain searchable)
and excludes sensitive paths; explicit reads use the permission gate. Shell
commands can still read those files under the local account. Shell tools and final verification share environment scrubbing, bounded output,
cancellation and process-tree cleanup. MCP servers are trusted local extensions.

State files and debug traces can contain conversation content, tool arguments
and tool output, including file contents. Image attachments are stored as
session-owned snapshots. Protect these local artifacts according to the
sensitivity of the workspace. There is no separate workspace backup or rollback.

## Out of scope

- Local code execution or sandboxing (intentionally no sandbox).
- Prompt injection via repo files, skills, tool output, web content.
- Untrusted skills, extensions, MCP servers, repositories.
- User-approved or user-initiated local actions.
- Exposed third-party/user-controlled credentials.
- Reports requiring prior local write access (home dir, workspace, env,
  shell startup, GEKTOR config) unless they show how GEKTOR granted it.
- Resource exhaustion requiring trusted local input.

Report genuine boundary bypasses (privilege escalation, escaping the
workspace guard via the harness itself) with repro, commit and impact.
