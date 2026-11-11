# Working on user paths

`Sub` resolves paths beneath the user's `.gektor` directory. `paths.go` owns that
single policy; callers create directories and own anything they store there.
There is no initialization, cache, or filesystem mutation during lookup. This is
a location helper, not an authorization boundary.

If home lookup fails, `Sub` preserves the relative `.gektor` fallback. A caller
that requires storage under an absolute home path must validate the result.

Tests redirect the process home in `TestMain` and remove their temporary home
before exiting. Run `go test ./internal/userdirs` from the repository root.

Follow the [Readable Go skill](../../.agents/skills/go-readable-keshon/SKILL.md).
