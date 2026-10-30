# Browser checks

GEKTOR uses its MCP connection for browser automation. The optional
[Playwright MCP server](https://github.com/microsoft/playwright-mcp) controls
installed Chrome or Edge. DOM snapshots, clicks, forms, JavaScript, console
and network checks work with text-only models. Visual judgment requires a
vision-capable backend. Built-in browser screenshots attach automatically;
custom MCP integrations may require reading the saved image with `read_file`.

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

GEKTOR opens a visible Chrome or Edge window with a dedicated persistent profile.
It does not use your personal browser profile. Successful activation returns browser
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

## Page observations

After activation, `browser_observe` provides three modes without requiring the
model to parse echoed automation code or open browser-generated snapshot files:

| Mode | Returned observation |
| --- | --- |
| `text` (default) | Rendered text from the first visible article, main, or body, with URL, title and scope. Up to 3,000 Unicode characters per page; follow `next_offset` with `offset` |
| `controls` | Current accessibility snapshot with element references for interaction |
| `visual` | Current screenshot attached as image input. Fails explicitly when vision is unavailable or pixels cannot be delivered |

Built-in MCP browser results also inline linked generated `page-*.yml` snapshots
and attach returned screenshots under `.gektor/browser/`. Reads stay confined to
that output directory, including symlink checks; unrelated and older snapshots
are not scanned. Observations are capped at 16 KiB with explicit truncation
markers. Linked originals remain available through `read_file` when more controls
are needed. Successful calls omit redundant echoed Playwright code; failed calls
retain their diagnostics. Page data remains untrusted input.

Optional `expect_text` checks case-sensitive literal text in the rendered body,
independently of the article excerpt. A mismatch is a failed verification tool
result. This proves only that text was present at observation time. To check a
saved change, reopen/reload the page and inspect the actual saved value. A click,
HTTP success or success toast alone does not establish persistence. Visual
judgment and broader task completion still require the relevant evidence.

Browser schemas and instructions remain deferred until activation. Core/mode
prompts live under `internal/prompts/text/`; tool descriptions stay short, and
the browser instructions have a regression budget. Historical instruction
literals used to migrate catalog fingerprints are not added to active context.

## Profiles and manual login

Profiles live under `~/.gektor/browser-profiles/<channel>/<name>/`, outside the
workspace. The default name is `default`. Chrome and Edge have separate profile
directories. Cookies and browser storage stay in these directories; conversation
`state.json` files contain only a reference:

```json
{"browser": {"channel": "chrome", "profile": "work"}}
```

Select an account/environment with `-browser-profile work`. Names are
case-insensitive and use 1–48 letters, digits, underscores or hyphens, starting
with a letter or digit. Opening a saved conversation restores its profile name
unless `-browser-profile` was explicitly supplied. Resume with the saved browser
channel; start a new conversation to change Chrome to Edge or vice versa.
Older conversations without a reference use the launch selection.

Profile files are created only when the browser is activated. Conversations can
reuse a profile, but only one Gektor process can hold it at a time. A busy profile
returns an error; close the other Gektor or select another profile. The operating
system releases the lock if Gektor exits unexpectedly. Deleting a conversation
does not remove its browser profile or sign you out.

For login or MFA, the agent uses `ask_user` to pause. Switch to the visible browser,
finish the manual step, and confirm completion in the CLI/TUI. Enter credentials
in the browser, not the conversation. The browser instructions require a fresh
DOM snapshot after handoff before further interaction. This uses the existing
operator prompt; no separate desktop application is required.

For unattended checks, add `-browser-headless`. Manual login requires restarting
without that flag. Both modes keep the same persistent profile. To sign out,
use the website's normal logout flow in the Gektor browser.

## Custom MCP

Advanced configuration through the existing MCP option is still available.
Explicit custom MCP servers connect at startup and advertise their tools immediately:

```sh
./gektor -tui -workspace /path/to/project -mcp 'browser=npx -y @playwright/mcp@0.0.83 --browser chrome --headless --isolated --image-responses omit --output-dir .gektor/browser'
```

For a custom MCP command, use `--browser msedge` for installed Edge. Omit `--headless` to see the
separate browser window. `--isolated` avoids using your personal profile and
discards its state on close. Custom MCP commands manage their own profiles;
Gektor's named-profile selection and lock apply to the built-in `-browser` path.
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

`GEKTOR_BROWSER_SMOKE=1 go test ./internal/tools -run
TestBrowserTaskObservationsLive -v` exercises article paging, live form controls,
successful and deliberately dropped saves, simulated operator login handoff,
and screenshot delivery through the agent loop. These use real Chrome with a
deterministic model driver; they test the harness, not an LLM success rate.
