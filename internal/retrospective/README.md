# Working on archived-session review

`retrospective` reviews saved execution archives and publishes explicitly
approved lessons. Review and lesson creation are experimental. A review never
resumes the original task or treats historical instructions as current ones.

## Reading path

- `review.go`: result/configuration types and `Evaluate`, including the locally
  constructed reviewer, observed progress, archive identity checks, and saving
  the completed proposal.
- `archive.go`: run selection, the message overview, and bounded Unicode archive
  reads with evidence/read-coverage tracking.
- `submission.go`: structured submission and validation of cited evidence.
- `failure.go`: retained diagnostics and recovery messages.
- `storage.go`: JSON persistence and temporary-file replacement.
- `lesson.go`: `Approve` publishes a reviewed procedure as a workspace skill;
  `Undo` retires only the matching approved guide.

The Agent TUI owns dialogs, user approval, and cancellation. `/eval` starts an
archive review; `/learn` adds the lesson workflow and can reopen saved review
results. Both commands use this package's review implementation.

## Contracts to preserve

- The reviewer receives only archive-reading and structured-submission tools.
  It does not inherit task identity, project instructions, task tools, inbox,
  delegation, or transcript persistence. Keep construction independent of roles.
- Findings cite messages actually read in the selected scope. A proposed lesson
  additionally needs a literal quote from an explicitly successful tool result.
  Read coverage and evidence checks do not prove a lesson's general usefulness.
- Read offsets count Unicode characters. Partial excerpts must not be reported
  as complete message reads.
- Archive identity is checked before saving a review and before approving its
  lesson. Preserve cancellation and changed-archive rejection.
- Review results are proposals. Approval pins evidence before making a skill
  discoverable; duplicate approval and retirement preserve subsequently edited
  files and previously existing lessons.
- Failures retain diagnostics, not proposed lessons. Progress reports observed
  steps and reads rather than a guessed completion percentage.

## Tests

Use deterministic fake model responses and real temporary archives for ordinary
review, evidence, approval, and cancellation tests. Live learning tests are
separate and require explicit opt-in.

From the repository root, run `go test ./internal/retrospective`; use
`go test -race ./internal/retrospective` for progress or cancellation changes.
Follow the [Go architecture skill](../../.agents/skills/go-architecture/SKILL.md).
