# Configuration

`config` owns the persisted profile and application-preference formats, their
validation, and conversion of editor fields. It does not own a running session,
provider connection, workspace, cache, or background work.

## Reading path

- `config.go`: package introduction, `Profile`, `Preferences`, and ownership contracts.
- `profile.go`: profile-specific types, first-run defaults, cloning, and validation.
- `profile_io.go`: profile loading, decoding, exclusive exports, and managed
  custom-profile saves.
- `preferences.go`: application-choice validation, loading, and atomic saves.
- `appearance.go` and `harness.go`: rendering choices and execution allowances.
- `edit.go`: editor sections and conversion into a staged profile.
- `json.go`: shared strict, bounded JSON decoding.

These responsibilities share the same small value types and callers, so they
remain in one package. There is no configuration service or generic storage layer.
Tests sit beside the responsibility they exercise; `fixture_test.go` supplies one
valid profile for the package tests.

## Contracts

Profiles and preferences are caller-owned values. A copied profile still shares
its optional identity pointer; use `Profile.Clone` before editing. The profile
inside preferences is also a pointer and must be cloned separately when staging
changes. `ApplySection` may partially edit a staged profile on validation failure;
unknown sections leave it unchanged. It does not save or fully validate a profile.

Both readers reject unknown JSON fields, additional JSON documents, and files over
1 MiB. Missing preferences return version-one defaults; missing profiles are errors. Preferences validate application choices; an embedded profile
is validated when selected for execution. Zero temperature means transport default,
not deterministic sampling. Model response limits are ceilings, not reservations.

`SaveCopy` validates and exclusively creates a file; it never replaces an existing
one and removes an incomplete copy on failure. `SaveCustom` stages a complete copy
before renaming it, and reuses only a `custom-*.json` path directly in the managed
directory. This naming policy is not a filesystem security boundary.
`SavePreferences` validates and replaces its target through a synced temporary file.
Temporary-file cleanup is best effort. None of these operations retain open files
on success. New directories and files request owner-only permissions where supported.

Apply [Go style](../../.agents/skills/go-style/SKILL.md) and
[Go architecture](../../.agents/skills/go-architecture/SKILL.md) when changing this package.
