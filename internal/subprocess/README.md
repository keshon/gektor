# Working on JSON workers

`Run` starts the current executable as `workflow <mode>`, writes one stdin payload,
and accepts one bounded JSON value on stdout. The executable must dispatch worker
modes before entering normal application startup. `Run` owns cancellation, waiting,
and process resources; callers own the input bytes and receive the result value.

- `run.go`: command setup, deadlines, bounded stdout/stderr, and JSON validation.
- `limits_windows.go`: hidden worker windows and a 256 MiB process-memory job.
  Successful setup keeps the kernel handle open for the worker's lifetime; Windows
  releases it at process exit. Failed setup closes it immediately.
- `limits_unix.go`: address-space limits. Linux allows existing runtime mappings
  plus 512 MiB so race-runtime reservations fit; other Unix targets use 2 GiB.

Workers call `Limit` once before processing input. The parent supplies only
`GOMEMLIMIT=128MiB`, independently of the platform limit. These are resource bounds,
not an operating-system sandbox. Cancellation terminates the worker and waits for
its exit; it does not promise to contain arbitrary descendant processes.

Run `go test ./internal/subprocess` and `go test -race ./internal/subprocess`.
Platform limit tests require that platform; cross-compilation only checks code.
Follow the [Go style skill](../../.agents/skills/go-style/SKILL.md).
