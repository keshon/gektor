# Test and verify a change

Run focused tests for the change, then the required project checks. Model evals and real browser checks answer different questions from unit tests.

```sh
go test ./...
go vet ./...
go test -race ./...
go run ./cmd/eval -dry
```

On Windows, `build.cmd check` runs the local suite. See the [eval runner](../../eval/README.md) for model-backed scenarios and scoring. The CLI flag-coverage check reads [the canonical reference](../reference/cli.md); update it when adding a flag.

## Browser integration checks

`GEKTOR_BROWSER_SMOKE=1 go test ./internal/tools -run
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

## Media compatibility checks

Media unit tests are offline. Optional checks exercise installed applications
and current YouTube behavior. Set these variables with your shell's syntax:

```text
GEKTOR_MEDIA_LIVE=1 go test ./internal/capabilities/media -run TestLiveMedia -v
```

This checks application discovery, ffprobe inspection and FFmpeg conversion,
clipping, frame extraction and merging using temporary generated fixtures.
Add `GEKTOR_MEDIA_LIVE_NETWORK=1` to check metadata and captions through native
YouTube and yt-dlp. Set `GEKTOR_MEDIA_LIVE_DOWNLOAD=1` and run
`TestLiveDownload` to download bounded audio through both backends. These
network checks can fail when upstream access or formats change. They do not
measure an LLM's ability to select tools.

Use `go run ./cmd/eval -suite media -runs 3 -jobs 1 -backend-kind llama -backend http://127.0.0.1:9931` for model-backed checks. See [media eval criteria](../../eval/prompts/media.md) for network and application requirements.

Session diagnostics retain per-request prompt/cache/processed counts and final usage, with `estimated` and `cache_known` flags. Prefill samples are grouped by 1024 tokens; generation logs phase boundaries rather than every token. A missing cache count is not evidence of a cache miss.

## Session recovery smoke check

Use a real terminal. The helper starts a local fake backend and gives the child
process a temporary home directory. It creates two workspaces and four saved
sessions: A, B, an unassigned session, and a session whose folder is missing.
No personal sessions or real model service are used.

```text
go build -o bin/gektor.exe ./cmd/gektor
go run scripts/tui-smoke.go -binary bin/gektor.exe
```

On Linux or macOS, build `bin/gektor` and pass that path instead. Add `-keep`
to retain the printed temporary directory for inspection. Otherwise the helper
removes its fixtures after closing GEKTOR.

1. Open `/sessions`. Open B and type an unsent draft.
2. Open A. Confirm the header changes to workspace A and B's draft is absent.
3. Reopen B. Confirm its draft returns. Clear it before sending anything.
4. Reopen A, start `/new`, and send a short message. The fake backend answers
   `Local smoke reply.`. Reopen that new session from B and check the A header.
5. In the session list, use CTRL+F to switch between all and current workspaces.
6. Open the missing and unassigned sessions. Both must stay in the current
   workspace and offer CTRL+W to assign one.
7. Press CTRL+W. Check the destination, cancel with N, and confirm nothing
   changed. Repeat and confirm with Y. The selected session must now open.
8. Repeat the session-list checks at 40 columns. OPEN, CLOSE and navigation
   must remain visible. The assignment must show the end of the folder path.

Quit with CTRL+Q. The helper prints the number of mock model requests. Opening,
filtering and reassigning saved sessions should make none; sending a message
should increase it. This counter does not include model-list discovery.

Automated coverage in `internal/tui/session_workspace_test.go` checks 40- and
80-column rendering, scope filters, missing and legacy roots, draft transfer,
confirmation cancellation, and a refresh that removes or reorders a session.
These tests exercise state and layout, not native terminal input.
