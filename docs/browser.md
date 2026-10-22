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

Windows (PowerShell or CMD):

```text
gektor.exe -tui -workspace D:\path\to\project -mcp "browser=cmd.exe /d /c npx -y @playwright/mcp@0.0.83 --browser chrome --headless --isolated --image-responses omit --output-dir .gektor/browser"
```

Linux/macOS:

```sh
./gektor -tui -workspace /path/to/project -mcp 'browser=npx -y @playwright/mcp@0.0.83 --browser chrome --headless --isolated --image-responses omit --output-dir .gektor/browser'
```

Use `--browser msedge` for installed Edge. Omit `--headless` to see the
separate browser window. `--isolated` avoids using your personal profile.
The server runs in the task workspace. Images are saved to files; inline MCP
image payloads are omitted because GEKTOR currently reads images via `read_file`.
For predictable screenshot paths, supply a workspace-relative filename.

Copy `skills/browser/` into the task workspace's `skills/` directory before
starting GEKTOR. Its catalog will advertise the guide; actual skill reads and
browser calls appear in history. No browser tools are loaded unless configured.

Try: “Open the local app, exercise its main form, inspect console errors,
and save a screenshot. Verify the actual page; report any unverified visual
details.” The model still needs reliable tool calling; a browser does not
turn a text-only model into a vision model.

For maintainers, `GEKTOR_BROWSER_SMOKE=1 go test ./internal/tools -run
TestBrowserMCPIntegration -v` runs an opt-in test against a local fixture:
navigation, button click, result assertion, console, screenshot and cleanup.
Set that environment variable using your shell's syntax.
