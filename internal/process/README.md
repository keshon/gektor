# Working on native processes

`process` runs finite native programs and provides process-tree lifecycle helpers
for background commands. Callers own authorization, program selection, and the
working directory. GEKTOR's same-executable JSON worker protocol belongs to
`internal/subprocess`.

## Reading path

- `run.go`: `Request`, bounded `Result`, and `Run` from startup through exit,
  cancellation, output capture, and descendant cleanup.
- `environment.go`: `ScrubEnv` removes credential-like environment variables.
- `group_windows.go` and `group_unix.go`: process-tree ownership and termination.
- `window_windows.go` and `window_unix.go`: platform window configuration.

## Contracts to preserve

- Arguments are literal; `Run` does not construct a shell command. Scripts need
  an explicit interpreter. Timeout must be positive and at most ten minutes.
- Each output stream retains at most 1 MiB. Overflow stops execution and marks
  the result truncated. Failures and cancellation retain captured output.
- Environment filtering matches variable names heuristically; it does not
  inspect values or establish a security boundary.
- Trace startup, group adoption, completion, cancellation, and release together.
  Descendants may outlive the direct child and keep output pipes open.
- Windows uses job ownership plus a best-effort PID tree sweep; Unix uses process
  groups. Preserve the platform-specific failure and fallback behavior.
- Background callers using `Group` own startup ordering and handle release.
  Native execution does not provide filesystem or network isolation.

## Tests

`run_test.go` launches real helper processes to check literal arguments, input,
environment filtering, exit failures, timeouts, output bounds, and cancellation.
Retain readiness synchronization before testing cancellation.

From the repository root, run `go test ./internal/process`; use
`go test -race ./internal/process` when changing output or lifecycle handling.
Follow the [Go architecture skill](../../.agents/skills/go-architecture/SKILL.md).
