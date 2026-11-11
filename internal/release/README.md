# Working on distribution packaging

`Build(ctx, Options)` stages development distributions or versioned Windows/Linux
releases using the installed Go and Git commands. It owns temporary staging files
and subprocesses. The caller owns the progress writer and published output paths.

## Reading path

- `release.go`: public options and build orchestration; start here.
- `build.go`: target selection, executable naming, and checksum generation.
- `source.go`: inspect repository identity, cleanliness, revision, timestamp, and tracked files.
- `manifest.go`: select runtime payloads and rewrite links to omitted source files.
  Skill bytes remain frozen. Manifest lists and compiled patterns are local to each call.
- `staging.go`: copy selected payloads, build the target executable, verify the source
  revision, then replace that target directory.
- `archive.go`: write ZIP or TAR.GZ directly, finish writers, and publish verified archives.
- `verify.go`: check member names, duplicates, modes, contents, and compressed-stream completion.

Versioned builds require a clean checkout and read committed Git blobs so checkout
line endings do not change payload bytes. Development builds may include tracked
working-tree changes. Payload selection follows tracked paths and required Windows
launchers; arbitrary working-tree files are not added. Root `.env`, internal plans,
and design documents are excluded.

Publication is per target, not a transaction over the entire release. A later failure
may leave an earlier target published. Failed archive creation retains an existing
archive; temporary cleanup is best effort. Preserve deterministic ordering, commit
timestamps, file modes, byte verification, and checksum names during refactors.

Run `go test ./internal/release`, adding `-race` for ownership changes. Tests build
small fixture repositories for both targets; they do not publish a GitHub release.
Follow the [Go style skill](../../.agents/skills/go-style/SKILL.md).
