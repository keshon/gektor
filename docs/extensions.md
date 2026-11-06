# Extensions and skills

## Saved MCP servers

Open **MCP servers** in Settings, or use `/mcp`, to list, add, edit, delete, import,
enable, and connect servers. The manager uses the same capability catalog as
file-based extensions. Changes are allowed between agent turns.

Servers are saved under `~/.gektor/mcp/`. Enabled servers are available to the
agent on demand. **Connect** discovers tools and makes them available on the
next turn; it does not call those tools. **At startup** connects at launch.
**Disconnect** closes the connection; an enabled server can reconnect on demand.
Disable it to remove it from discovery. Existing session catalogs retain their
original definitions; changed definitions may require a fresh session.

The importer accepts `mcpServers` objects with `url` and `headers`, or `command`
and `args`. Import rejects existing IDs unless replacement is explicitly selected.
In the MCP servers form, choose **Paste JSON** to paste the configuration into
a multiline field, or **Import file** to read it from a file. Choose **Import**
to save the servers. This does not connect them or call their tools.

For example:

```json
{
  "mcpServers": {
    "reference": {
      "url": "https://example.com/mcp"
    }
  }
}
```

Replace the example address with your server's endpoint.

Use replacement import to rotate HTTP credentials. Subprocess environment
credentials use `env_refs`, mapping destination names to existing environment
variable names; inline `env` import is rejected.

HTTP header values are stored separately under `~/.gektor/credentials/`; server
files contain opaque references. Windows encrypts them with DPAPI for the current
user. Other platforms use owner-only files in a private directory. References
are bound to an HTTPS origin and header name. Authenticated connections refuse
redirects. OAuth login is not implemented.

CLI management works without an LLM backend:

```powershell
.\gektor.exe mcp list
.\gektor.exe mcp import C:\path\to\mcp.json
.\gektor.exe mcp connect yougile
.\gektor.exe mcp disable yougile
.\gektor.exe mcp enable yougile
.\gektor.exe mcp delete yougile
```

`connect` checks the handshake and tool catalog, then closes its connection when
the CLI command exits. `save FILE` adds or updates a server definition;
`import-replace FILE` explicitly replaces matching imported definitions.
`import -` reads JSON from stdin. A saved definition uses `id`, `description`,
`url` or a `command` array, `enabled`, optional `startup`, `header_refs`, and
`env_refs`. Unknown or conflicting adapter IDs are rejected at launch.

Server failures are visible in Details, including failures from agent-triggered
activation. Tool calls retain normal permission checks. Startup connection
failures are reported and leave the server available for a later retry.


Pass `-capabilities path/to/extensions.json` to configure optional tool packs
and extra skill libraries. CLI, TUI and RPC use the same configuration. The
file is selected explicitly; opening a workspace does not load executable
extensions from files found there.

```json
{
  "version": 1,
  "skill_roots": ["./skills-library"],
  "packs": [
    {
      "id": "widgets",
      "version": "1",
      "description": "Look up widget metadata",
      "instructions": "Use lookup for current widget details.",
      "type": "executable",
      "command": ["python", "C:/tools/widget_adapter.py"],
      "read_only": true,
      "concurrent": true,
      "resource_field": "key",
      "timeout_seconds": 30,
      "tools": [
        {
          "name": "lookup",
          "description": "Look up one widget by key",
          "input_schema": {
            "type": "object",
            "properties": {"key": {"type": "string"}},
            "required": ["key"],
            "additionalProperties": false
          }
        }
      ]
    },
    {
      "id": "reference",
      "version": "1",
      "description": "Search the configured reference service",
      "type": "mcp",
      "url": "http://127.0.0.1:9000/mcp"
    }
  ]
}
```

Unknown fields, duplicate IDs, malformed schemas and invalid permission
patterns fail with an error. Pack and MCP server IDs use 1–32 ASCII letters,
digits, underscores or hyphens. Tool names are bounded to 64 ASCII characters;
long or sanitized names receive a stable hash suffix. Registration rejects
duplicate names as a complete batch.

## Loading tools

The agent searches `discover_capabilities` by ID or description, then calls
`enable_capability` with the selected ID. Discovery returns metadata only.
Activation registers tool definitions and returns the pack's instructions.
It does not execute a requested tool action. Executable activation starts no
process; MCP activation connects to discover the remote catalog.

Executable tools are named `cap__<pack>__<tool>`. MCP tools use
`mcp__<server>__<tool>`. The existing `-mcp` flag still connects eagerly.
`-browser` uses the shared capability lifecycle and retains `enable_browser`.
ID `browser` is reserved while `-browser` is enabled. IDs starting with
`native_` are reserved for native tool packages. Rename any configured pack
using that prefix before launching this version.

Built-in web tools remain available from the first model request. They now use
the same activation manager as optional packs and persist as `native_web`.
Native tools keep their public names and execution permissions. Their saved
definitions are checked against the current build before a resumed run; a
mismatch requires compatible code or a fresh session. Other always-present
native tools are reconstructed normally and are not saved as capability packs.

Built-in `native_images` and `native_workflows` load on demand.
See [image workflows](workflows.md) for tools, limits and the recipe format.

The prompt includes a bounded map of optional pack IDs and descriptions from
the discovery catalog. A known ID can be enabled without a preceding search.

Activated catalogs persist through compaction. For adapter packs, resume restores schemas without
connecting. The first tool call reconnects and compares the current configuration
and catalog with the saved fingerprint. A changed catalog requires a new session.
A missing adapter fails its tool call; other work can continue. Older transcripts
without saved catalogs migrate from successful activation results on resume.
Missing operational instruction blocks are appended again after compaction;
the system prompt remains stable.

`read_only` and `concurrent` default to false. These are declarations made by
local configuration, not claims accepted from a remote server. A restored
read-only pack can run in Plan. Plan can activate native packages without IO;
external-pack activation remains blocked, including packs declared read-only.
Activation and subsequent tool calls still pass through permission checks. Discovery
and skill loading work in Plan. Concurrent execution requires `read_only`.
These declarations do not sandbox processes; use them for trusted adapters.

Mixed packs can override policy per exposed tool name:

```json
"tool_policies": {
  "mcp__social__read_post": {
    "read_only": true,
    "concurrent": true,
    "resource_fields": ["url"]
  },
  "mcp__social__copy_post": {
    "resource_fields": ["source", "destination"]
  }
}
```

Unspecified values inherit the pack defaults. `resource_fields` names top-level
string arguments; every selected resource is authorized before execution.
An empty array clears the inherited resource selection. Unknown tool names
fail activation. Policy changes require a new session, like other catalog changes.

For subprocess credentials, use references rather than values:

```json
"env_refs": {"READER_TOKEN": "MY_READER_TOKEN"}
```

The destination variable is provided only to that adapter. References resolve
when an executable runs or an MCP process connects; missing values fail that
operation. MCP restarts reuse its connection's resolved environment. Restart
the host to rotate credentials. HTTP MCP packs do not accept `env_refs`; use `header_refs` created by the
MCP manager for HTTP credentials. Executable and stdio MCP processes both use
the native scrubbed environment plus explicit references. Known credential
values are redacted from returned text and errors; adapters must still avoid
logging secrets or returning encoded/transformed credentials. This is not process isolation.

MCP calls default to 60 seconds; `timeout_seconds` overrides that limit up to
600 seconds and parent cancellation always wins. Initial MCP discovery has a
separate 20-second limit.

`resource_field` selects a nonempty string argument for permission matching.
For example, `-deny "cap__widgets__*=private-*"` blocks matching widget keys.
`-deny "enable_capability=widgets"` blocks activation. Tool patterns accept
case-insensitive globs, including `mcp__reference__*`; later rules win.
Every actual call is authorized again under the current policy.

## Executable protocol

Gektor starts `command[0]` with the remaining elements as literal arguments,
using the task workspace as its working directory. Relative script arguments
are relative to that workspace; use absolute paths for shared adapters.
Arguments are never interpolated into shell source.

One JSON request is sent on stdin, followed by a newline:

```json
{"tool":"lookup","arguments":{"key":"example"}}
```

Write exactly one JSON response to stdout; use stderr for diagnostics:

```json
{"output":{"key":"example","status":"available"}}
```

`output` may be a string or any JSON value. To report failure with partial
evidence, return `{"output":"partial result","error":"reason"}`. A nonzero
exit, invalid response, multiple responses, timeout, or oversized stdout fails
the tool. The default timeout is 60 seconds; `timeout_seconds` accepts 1–600.
Stdout is limited to 1 MiB, stderr to 8 KiB, and model output to 48 KiB with
the normal output store. Cancellation stops the process tree. Executables
inherit the same credential-scrubbed environment as native process tools.

## Built-in social reader

The `social` capability is compiled into Gektor. It needs no extra executable
or configuration file. Discover it, enable `social`, then call
`cap__social__read_post` with an HTTPS X/Twitter post URL. Discovery and
activation make no network requests. Until activation, its tool schema and
instructions do not enter the prompt. A configured pack with ID `social`
replaces the built-in provider.

The reader uses the [FxTwitter Status Fetch API](https://github.com/FxEmbed/FxEmbed/wiki/Status-Fetch-API).
The requested post ID is sent to that third-party service. Results contain
canonical URL, author, text, dates, media references and retrieval status.
Media is not downloaded. Requests have a 20-second deadline and a 2 MiB
response limit; normalized output is bounded to 40 KiB. Restricted, missing,
transient, unsupported, malformed and truncated results fail the tool while
retaining available structured evidence. Cancellation remains cancellation.

This provider supports public single posts, not Instagram, private posts,
full threads or replies. It requires no API key. Upstream availability is not
guaranteed. Older sessions using the standalone example must start a new
session to adopt the native provider's catalog.

Compiled Go integrations supply a capability loader returning tools through
the existing agent tool interface. They share activation, persistence,
permissions and catalog validation with executable and MCP adapters.

## Skill libraries

Roots have the following precedence, from lowest to highest:

1. `~/.agents/skills`
2. `~/.gektor/skills`
3. `skill_roots` in declaration order
4. Workspace `skills/`
5. Workspace `.agents/skills/`
6. Workspace `.gektor/skills/`

Relative configured roots resolve against the configuration file's directory.
Discovery is recursive and skips directory symlinks. Later roots override the
same ID; duplicate IDs inside one root are skipped with a warning.
Malformed skills do not prevent valid skills from loading. Discovery warnings
appear in the initial catalog and `system_info group=skills`.

Use YAML frontmatter with `name` and `description`. Names use lowercase letters,
digits and single hyphens, with a maximum of 64 characters. Descriptions retain
up to 1024 Unicode characters by default, including YAML quoted and multiline
values. Set `GEKTOR_SKILL_DESCRIPTION_LIMIT` to a positive integer before launch
to change this limit. Legacy guides
without a name use their package path as an ID. Frontmatter is limited to 16 KiB.
Only metadata enters the bounded initial catalog.

The initial catalog uses IDs and descriptions shortened to 120 characters.
If the list exceeds 2 KiB, only discovery instructions and the count enter the
prompt. Full metadata remains searchable through `system_info group=skills`.

`system_info` with `group=skills` searches live metadata. `load_skill` with `id`
returns the full guide, source, base directory and SHA-256 digest. Supporting
text files use the same ID plus `resource`, relative to that package. Reads stay
inside that package through `os.Root`, including symlink checks; each file is
limited to 256 KiB. Workspace guides remain readable through `read_file`.
Skills provide instructions and never install tools or grant permissions.

Repeated guide loads use a short reminder in the outgoing model view only
when an identical complete guide is already present there. Matching includes
source, SHA-256 and body after output limiting. Truncated, offloaded or failed
loads do not count; supporting resources are read normally. The view is rebuilt
after compaction and on resume, so a retained load regains its full body when
the earlier copy is removed. The saved archive keeps the original results.

### Role availability

Set `main_only: true` on a configured pack to keep it out of subagent discovery,
activation and restoration. The default is false, preserving existing pack
availability. This restriction is independent of `read_only` and permissions.
Role constructors also reject `ask_user` and `delegate_task` in child registries,
including direct programmatic extras. A main agent without an ask callback cannot
acquire `ask_user` through an extra or pack.

Dynamically loaded packs still undergo a tool-name check before publication.
A loader that fails to declare its main-only requirement may run discovery
before that check; its forbidden tools are never published. Declare `main_only`
when the entire pack is unsuitable for children.
