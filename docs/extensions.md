# Extensions and skills

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
ID `browser` is reserved while `-browser` is enabled.

Activated catalogs persist through compaction. Resume restores schemas without
connecting. The first tool call reconnects and compares the current configuration
and catalog with the saved fingerprint. A changed catalog requires a new session.
A missing adapter fails its tool call; other work can continue. Older transcripts
without saved catalogs migrate from successful activation results on resume.
Missing operational instruction blocks are appended again after compaction;
the system prompt remains stable.

`read_only` and `concurrent` default to false. These are declarations made by
local configuration, not claims accepted from a remote server. A restored
read-only pack can run in Plan; Plan does not activate new packs. Discovery
and skill loading work in Plan. Concurrent execution requires `read_only`.
These declarations do not sandbox processes; use them for trusted adapters.

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
same ID; duplicate IDs inside one root are errors.

Use YAML frontmatter with `name` and `description`. Names use lowercase letters,
digits and single hyphens, with a maximum of 64 characters. Descriptions retain
up to 1024 characters, including YAML quoted and multiline values. Legacy guides
without a name use their package path as an ID. Frontmatter is limited to 16 KiB.
Only metadata enters the bounded initial catalog.

`system_info` with `group=skills` searches live metadata. `load_skill` with `id`
returns the full guide, source, base directory and SHA-256 digest. Supporting
text files use the same ID plus `resource`, relative to that package. Reads stay
inside that package through `os.Root`, including symlink checks; each file is
limited to 256 KiB. Workspace guides remain readable through `read_file`.
Skills provide instructions and never install tools or grant permissions.
