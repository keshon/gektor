# Model contracts

`llm` defines the shared contract between the agent and an external model backend.
Start with `llm.go`: `Client`, optional streaming, requests, responses, progress,
and usage. Concrete backends live in `chatcompletions` and `responses`.

- `message.go`: message roles, tool definitions/calls, and model-facing history.
- `context.go`: prompt sections, context reports, and fallback token estimation.
- `state.go`: provider-specific replay state and its matching rules.
- `capabilities.go`: measured model facts shared with consumers.
- Attachment, argument, retry, and reasoning files own their respective operations.

Keep provider encoding and transport quirks in concrete backends. Message values
contain slices and pointers; copying a value does not make it independently mutable.
Token estimates are approximations; backend usage reports remain authoritative.
This package defines no universal client cleanup or concurrency guarantee;
concrete implementations own those contracts.

Follow [Go style](../../.agents/skills/go-style/SKILL.md) and
[Go architecture](../../.agents/skills/go-architecture/SKILL.md).
