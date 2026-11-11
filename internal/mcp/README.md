# MCP protocol sessions

This package owns JSON-RPC dispatch, bounded catalog discovery, stdio server
lifetime and restart, HTTP session renewal, and SSE response matching. Start with
[mcp.go](mcp.go), then [StartStdio](process.go) or [NewHTTP](http.go).

Callers authorize commands and endpoints, resolve credentials, and supply the
HTTP network policy. The adapters in `internal/tools` redact credentials,
assign Agent tool names, apply tool timeouts, and adapt text and image results.
This package does not import Agent or its tool registry.

`StartStdio` copies command arguments and environment values. Its startup context
bounds discovery; the returned client owns the process tree until `Close`.
`Close` stops and waits for processes and response readers. A transport failure
can restart the server for subsequent requests, but a disconnected `tools/call`
is never replayed because its effects may already have happened.

`NewHTTP` retains the supplied HTTP client and copies headers. Each call uses its
caller's deadline. Session expiration renews once; tool calls require an explicit
retry after renewal. Finish active calls before closing the HTTP session.

Protocol and lifecycle tests live here. Agent adaptation, credential redaction,
network authorization, and browser integration tests remain in `internal/tools`.
