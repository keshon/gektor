# Browser checks

GEKTOR uses its MCP connection for browser automation. The optional
[Playwright MCP server](https://github.com/microsoft/playwright-mcp) controls
installed Chrome or Edge. DOM snapshots, clicks, forms, JavaScript, console
and network checks work with text-only models. Visual judgment requires a
vision-capable backend and a saved screenshot read through `read_file`.

Install Node.js 20+ with npm and Chrome or Edge. On activation, GEKTOR checks these prerequisites
and reports a specific fix if they are missing. Windows, Linux and macOS use
the same option:

On Windows, `run-tui.cmd` already enables `-browser chrome`. To use Edge,
run `run-tui.cmd -browser msedge`; to disable browser automation, use
`run-tui.cmd -browser=`. Explicit arguments override the launcher's default.
The flag makes browser automation available on request; it does not start MCP
or put all its schemas into the opening model request.

```text
gektor -tui -workspace /path/to/project -browser chrome
```

For installed Edge, use `-browser msedge`. The agent first calls `enable_browser`
when its task needs browser interaction. Activation checks the pinned Playwright
MCP package in npm's cache. If unavailable, GEKTOR announces preparation and
downloads `@playwright/mcp@0.0.83` into that cache with a three-minute limit,
separate from the MCP handshake timeout. This needs internet on first use;
later activations reuse the cache. Node.js and browsers are not installed by GEKTOR.
Setup errors include the npm diagnostic and `-browser=` escape hatch.

GEKTOR uses an isolated headless profile. Successful activation returns browser
instructions and exposes the MCP tools for the rest of that session. A connection
failure is a failed tool result with the server's diagnostic; ordinary coding
tasks can still run without a working browser installation.
The session snapshot preserves activated schemas through compaction. Resume
restores the tool set without connecting; the first browser call reconnects and
validates the catalog. Missing browser dependencies fail that tool call while
other session work can continue. Older archives migrate from activation results.
New sessions start without the browser schemas. Activation can invalidate part of the backend's prompt cache once;
tools are not unloaded between turns or Plan/Act switches.
First navigation checks that the browser itself can launch. The selected
browser must already be installed. Without `-browser` or a custom browser MCP,
the model is explicitly told browser automation is unavailable.

Advanced configuration through the existing MCP option is still available.
Explicit custom MCP servers connect at startup and advertise their tools immediately:

```sh
./gektor -tui -workspace /path/to/project -mcp 'browser=npx -y @playwright/mcp@0.0.83 --browser chrome --headless --isolated --image-responses omit --output-dir .gektor/browser'
```

For a custom MCP command, use `--browser msedge` for installed Edge. Omit `--headless` to see the
separate browser window. `--isolated` avoids using your personal profile.
The server runs in the task workspace. The example omits inline images to keep
DOM-based work cheap. With a vision-capable backend, `--image-responses allow`
also permits validated MCP image attachments. Saved screenshot links remain
references; read their workspace files with `read_file` to load the pixels.
For predictable screenshot paths, supply a workspace-relative filename.

Copy `skills/browser/` into the task workspace's `skills/` directory before
starting GEKTOR. Its catalog will advertise the guide; actual skill reads and
browser calls appear in history. No browser tools are loaded unless configured.
Browser checks must use these tools; shell launches and mass termination of
Chrome/Edge are not recovery steps.

Try: “Open the local app, exercise its main form, inspect console errors,
and save a screenshot. Verify the actual page; report any unverified visual
details.” The model still needs reliable tool calling; a browser does not
turn a text-only model into a vision model.

For maintainers, `GEKTOR_BROWSER_SMOKE=1 go test ./internal/tools -run
TestBrowserMCPIntegration -v` runs an opt-in test against a local fixture:
navigation, button click, result assertion, console, screenshot and cleanup.
The test also measures mobile overflow, deliberately introduces overflow to
prove the check detects it, and exposes an import-map error on an HTTP 200 page.
Set that environment variable using your shell's syntax.
Add `GEKTOR_BROWSER_COLD=1` to use a fresh temporary npm cache and verify
first-use download and browser checks together; this requires internet.
