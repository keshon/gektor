# Documentation migration — 2026-11-09

Implemented the approved [documentation plan](documentation-plan.md). The
[inventory](documentation-inventory.json) retains the original baseline and adds
the canonical sources and implementation constraints.

## Delivered

- A shorter root README, documentation home and two complete first-run paths.
- Ten task guides, seven references and five contributor pages.
- Compatibility pages at all ten previous public documentation paths, retaining
  their original section headings. Frozen skills can keep their existing links.
- Shared Markdown publication through the existing Vite site: 35 articles,
  explicit navigation, page outlines, copyable code, source/revision links and
  responsive layouts. Each article has a static HTML route.
- Landing-page links to those routes and a model-neutral first Agent example.
- Canonical path updates in help strings and conventions checks, preserving
  CLI flag and rule-tag coverage.
- Explicit release documentation selection. Internal work records and native
  design assets are excluded; links to omitted contributor sources point to
  the packaged Git revision. The frozen skill catalog links to the embedded
  workflow guide's source, so that guide is also included unchanged in archives.

Repository skills, every SKILL.md, runtime prompts, embedded guides and eval
prompts remain byte-for-byte unchanged: 67 baseline files checked by SHA-256.
The reusable personal skill is outside this repository at
`C:/Users/keshon/.codex/skills/docs-audit-plan/`.

## Verification

- `go test ./...` and `go vet ./...` passed.
- `go run ./cmd/eval -dry` passed all 35 selected probes. No model inference ran.
- Canonical conventions tests passed after the final documentation corrections.
- Website checks passed: local paths and Markdown anchors across 61 maintained
  files, plus renderer tests. Builds at `/` and `/gektor/` passed checks of all
  36 generated HTML pages, their links, anchors and assets.
- Packaging tests passed: selection boundaries, offline links, frozen skill
  bytes and current article link closure. Full binary release archives were
  not built or published.
- Transform-and-review and log-report completed in fresh workspaces using an
  isolated GEKTOR user directory. Review continued the paused runs; arithmetic
  output was `4, 6, 10`. The exported log report contained the expected two errors.
- Browser inspection covered 1440px and 390px layouts, mobile navigation with
  the keyboard, code copying and a production direct-route reload under
  `/gektor/`. No horizontal page overflow was observed on the checked pages.
- The Impeccable mechanical detector reported no findings on the changed UI.

The browser pass found and fixed a sidebar resize issue. The generated-link
check found stale landing anchors and root-relative image links under a project
base path; both were corrected. Current TUI source confirmed Recipe and Run menu
labels and Permissions under Settings; the instructions now use those labels.

## Limits

No new-reader study or live Agent task was run. Native terminal instructions
were checked against current code and existing tests; the examples above were
executed through the CLI. External URLs and the deployed website were not
revalidated. The site still has no search service or multi-version selector.

These checks were performed locally before commit and publication.
The final image cleanup uses the existing WebP in the root README and removes
the redundant PNG; website and packaging checks passed after that change.
