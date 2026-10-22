# Browser checks

GEKTOR uses its MCP connection for browser automation. The optional
[Playwright MCP server](https://github.com/microsoft/playwright-mcp) controls
installed Chrome or Edge. DOM snapshots, clicks, forms, JavaScript, console
and network checks work with text-only models. Visual judgment requires a
vision-capable backend and a saved screenshot read through `read_file`.

Install Node.js/npm and Chrome first. Warm the pinned package once so its
download does not consume GEKTOR's MCP startup timeout:

```text
npx -y @playwright/mcp@0.0.83 --help
```

After that one-time setup, Windows, Linux and macOS use the same option:

On Windows, `run-tui.cmd` already enables `-browser chrome`. To use Edge,
run `run-tui.cmd -browser msedge`; to disable browser automation, use
`run-tui.cmd -browser=`. Explicit arguments override the launcher's default.

```text
gektor -tui -workspace /path/to/project -browser chrome
```

For installed Edge, use `-browser msedge`. GEKTOR starts the cached, pinned
MCP package without downloading it, uses an isolated headless profile, and
reports the discovered browser tools. A connection failure stops launch with
the server's diagnostic instead of continuing without the requested browser.
First navigation checks that the browser itself can launch. The selected
browser must already be installed. Without `-browser` or a custom browser MCP,
the model is explicitly told browser automation is unavailable.

Advanced configuration through the existing MCP option is still available:

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
Set that environment variable using your shell's syntax.
