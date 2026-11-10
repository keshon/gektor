# Configuration and stored data

Look up configuration precedence, saved state, capability files and adapter protocols here. For connection steps, see [Connect a model](../guide/models.md).

## State

CLI task history is stored under `~/.gektor/tasks/<id>/`; TUI Agent sessions
use `~/.gektor/sessions/<id>/`. Each has a `state.json` snapshot, rewritten
atomically as the agent progresses. Display titles, mode metadata, child
archives and retained tool output can be stored alongside it. See
[saved data](configuration.md#saved-data) for the storage locations.
Resume accepts a task directory or state file.
Fork loads a transcript into a fresh task; it does not create a Git branch or
an isolated checkout. Ancestor/workspace AGENTS.md instructions are refreshed.

GEKTOR does not archive the workspace, so it does not revert it. Commit or stash
before a run on a tree you care about.

## Connection ownership

Each launch binds its ChatGPT account registration. Selecting another registration
with `gektor auth select` affects new launches; an existing launch continues using
its bound account. Signing that account out makes further requests fail until it
is connected again. Local model facts are refreshed before each run, while one
run and its delegated agents share a consistent model/capability snapshot.

Saved ChatGPT native state is reused only for a matching integration, model,
account, and unchanged message. Otherwise GEKTOR sends the normal transcript.
Only current versioned conversation snapshots are supported. Retired array
snapshots and catalog migrations are not read or rewritten. Incompatible native
replay state is discarded in favor of the normalized transcript. Interrupted
output is retained as incomplete history.

Profiles use the same strict decoder in CLI and workflow model operations:
unknown keys and additional JSON documents are errors. Retired preference keys
(`nudges`, `think_budget`, and `density`) are rejected rather than migrated.



## Configuration

The TUI saves connection and generation settings in the user's GEKTOR directory.
Explicit CLI flags override the selected configuration. API keys are read from
the process environment; GEKTOR does not automatically load `.env`.
See [authentication](cli.md#authentication-remote-providers).

`.env.example` documents the API key variable and optional external benchmark
settings. Only `scripts/bench-llama.cmd` reads its benchmark settings from `.env`.
The file is gitignored; do not commit credentials or machine-specific paths.

## Saved data

Preferences, sessions and retained tool output live under `~/.gektor`.
On Windows, `~` means your user folder, usually `C:\Users\<name>`.
`GEKTOR_API_KEY` is the generic API key variable.

| Data | Location | What it is for |
|---|---|---|
| TUI preferences | `~/.gektor/application.json` | Workspace and display preferences |
| Custom profiles | `~/.gektor/profiles/custom/` | Saved model and identity settings |
| Agent sessions | `~/.gektor/sessions/` | TUI conversation history and saved context |
| CLI tasks | `~/.gektor/tasks/` | Standalone CLI session history |
| Workflow recipes | Files you choose inside the workspace | Editable instructions; can be tracked in Git |
| Workflow run records | `~/.gektor/workflows/<workspace-hash>/runs/<run-id>/run.json` | Captured recipe, inputs, results and status |
| Workflow records | `~/.gektor/workflows/<workspace-hash>/records/` | Structured data used by record operations |
| Canvas layouts | `~/.gektor/workflow-layouts/` | Card positions and position locks, separate from recipes |

Workflow history belongs to a workspace path. Moving the workspace gives it a
different history location. Existing records remain under the old workspace key.
There is no shared recipe/template library in the user directory yet.
To reuse a recipe, copy it into the target workspace and check its paths.

Schedules and queued jobs also use the workspace's workflow store. They need
the [workflow service](cli.md#workflow-queue-and-service) to process them.
The workflow editor manages a recipe's Schedule trigger. Global job lists and
schedule management across recipes use the CLI; there is no global TUI manager.

A session stores the full conversation archive and its compacted model context.
The archive allows resume, fork and historical reads. It is not a backup of
workspace files. Output retention and session controls are described in the
[terminal guide](terminal.md).

## Distribution

Release archives contain a Windows or Linux amd64 binary, profiles, skills and
documentation and workflow examples. Windows archives also include launchers,
CLI helpers and demos.
Models, server runtimes, Node.js and browsers are external dependencies.
Browser requirements and first-use preparation are documented in
[browser setup](../guide/browser.md).

## Capability files

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

## Executable protocol

GEKTOR starts `command[0]` with the remaining elements as literal arguments,
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

## Browser catalog lifecycle

The session snapshot preserves activated schemas through compaction. Resume
restores the tool set without connecting; the first browser call reconnects and
validates the catalog. Missing browser dependencies fail that tool call while
other session work can continue. Older archives migrate from activation results.
New sessions start without the browser schemas. Activation can invalidate part of the backend's prompt cache once;
tools are not unloaded between turns or Plan/Act switches.

Browser schemas and instructions remain deferred until activation. Core/mode
prompts live under `internal/prompts/text/`; tool descriptions stay short, and
the browser instructions have a regression budget. Historical instruction
literals used to migrate catalog fingerprints are not added to active context.



## Profile files

The repository includes two profiles under `profiles/`:

- `llama-external.json` connects to a server at `http://127.0.0.1:9931`.
  Adjust its endpoint and context window to match your server.
- `chatgpt.json` uses a ChatGPT account connection. Run `gektor auth login` first.

You can also set `-backend` and `-model` without loading a profile.

A profile carries three blocks. `provider` names the protocol, endpoint and
model. `generation` sets the response budget, context window and
temperature. `identity` sets the persona:
`user_name`, `system_prompt`, `post_history`, `name`, `user_persona` and
`prompt_preset`.

Decoding is strict. An unrecognized key is an error, so a misspelled setting fails at
load instead of being silently ignored, and a profile written for a removed schema is
rejected with the offending key named rather than being rewritten.


## Skill metadata

`GEKTOR_SKILL_DESCRIPTION_LIMIT` sets the maximum skill description length in
Unicode characters (default: 1024). Set it before launching GEKTOR; it must be
a positive integer. The separate 16 KiB frontmatter limit still applies.
Malformed skills are skipped without hiding valid skills. Discovery warnings
appear in the skill catalog and `system_info` with `group=skills`.
