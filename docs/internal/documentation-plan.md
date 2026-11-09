# Documentation proposal

Proposed on 2026-11-09 against `17d7d4d`. This is a content and navigation plan;
approved for implementation on 2026-11-09. The audit below describes the original baseline.

Implementation constraint: repository skills and prompts are frozen, including
`skills/README.md`, every `SKILL.md`, `internal/prompts/`,
`internal/workspace/builtin/` and `eval/prompts/`. Compatibility pages preserve
their existing links. The reusable audit skill lives outside this repository.

## Recommendation

Give GEKTOR one documentation collection, organized around what a person wants
to do. Publish the same Markdown sources on GitHub, in release archives, and
eventually at `gektor.keshon.ru/docs/`.

Use four sections: Get started, Guides, Reference, and Development. Keep working
examples beside their recipes and link them into this collection. The landing
page introduces the product and sends people to a specific next step.

Apply [Humanizer](https://github.com/blader/humanizer/blob/main/SKILL.md) during
the rewrite. Its plain technical voice fits this project. Better wording needs
to accompany a change in reading order and content ownership.

## What was reviewed

The [inventory](documentation-inventory.json) records every tracked Markdown
and text file at the baseline, with headings, links, size and intended treatment.
It contains 100 files, including 73 Markdown files:

- 28 maintained guides and indexes, about 39,900 words.
- 27 eval specifications for contributors.
- 10 project agent guides and supporting references.
- 2 embedded runtime skills and 23 runtime prompt files.
- 2 development history documents.
- 7 eval fixture files and 1 third-party license text.

The human guides and eval write-ups were read for structure and wording. Runtime
instructions and fixtures were classified separately because their text has
operational or test meaning. The landing HTML, its build/deployment setup,
in-product help, release packaging, and documentation checks were also inspected.
The root license and environment-variable example are additional surfaces.
Generated eval results, ignored prototypes and build output are outside the
maintained documentation collection.

A local Markdown link pass found no missing files or anchor candidates among
the scanned non-fixture Markdown files. This was a local path/heading check;
external URLs and the deployed website were not tested. Navigation can be hard
to use even when its links resolve.

## Findings

### The strongest bias is toward maintainers

Most prose is already reasonably factual. The bigger problem is the amount of
implementation history, validation machinery and defensive explanation a user
must cross to reach an action.

The [CLI reference](../cli.md) opens with capability configuration and explains
the test that checks flag coverage before showing invocation. That belongs in
contributor documentation. The user needs a command and its options.

The [browser guide](../browser.md) starts with useful prerequisites, then moves
through catalog restoration, prompt-cache behavior, fingerprint migration and
internal prompt locations. Those details interrupt the setup and login task.
Its live-test instructions belong with testing.

Keep the technical material. Give it the right reader and location. The current
reference work contains useful limits and failure semantics that a stylistic
rewrite must preserve.

### Getting to the first result takes too long

In the [terminal guide](../tui.md), the first Agent task starts at line 113.
Picker mechanics, menu tables, appearance, profile behavior and MCP management
come before it. A first-time user should reach a small task before learning
every control.

In the [workflow reference](../workflows.md), First recipe starts at line 264,
after catalogs, image restrictions, archives and document parsing. It also begins
by asking a model to prepare an image recipe. A reader can try a saved workflow
without a model or an image file; that is the better first workflow.

### The front doors describe different starting points

The [README](../../README.md) starts with a terminal app for models and workflows;
the [landing page](../../website/index.html) calls it a coding agent and workflow
runner. Both describe real behavior, but they should share a concrete opening.

The landing page's Documentation link goes to a GitHub file index. Its Agent
example requires a signed-in ChatGPT account, although the product also works
with local models. This biases the demonstration toward one connection path.
The download path and the source-build path also need clearer separation.

### Some statements need reconciliation

- [Setup](../bootstrap.md), line 65, says there is no schedule-management screen.
  The terminal guide explains editing Schedule triggers and refreshing their
  status. Distinguish recipe-level schedule editing from a global queue or
  schedule manager; the current sentence can suggest that scheduling is CLI-only.
- [Extensions](../extensions.md), line 300, says top-level project skills are not
  bundled. [The release script](../../scripts/release.py), lines 22-26, includes
  tracked `skills/` files. Explain the difference between files included in an
  archive, embedded built-in skills, and guides discovered in a task workspace.
  Resolve the wording against packaging rather than changing packaging to make
  the sentence true.
- The terminal guide sometimes names an Agent menu and sometimes Run for the
  same action. Verify current menu labels against the application, then use
  those labels consistently. Avoid making readers translate legacy UI names.

These are concrete issues, not evidence that every technical claim is wrong.

### Ownership is scattered

Connection setup, profiles, saved state, schedules and permissions appear across
the README, setup, terminal, CLI, browser, extensions and workflow guides.
Readers encounter repeated constraints without a clear place to look up the
complete rule. The CLI reference also owns detailed tool behavior unrelated
to CLI syntax.

The [docs index](../README.md) labels material by audience, but the distinction
disappears inside the linked pages. Large references combine procedures,
lookup tables, explanations and implementation notes.

### The release and site boundaries need explicit rules

The release script currently collects all tracked `docs/`, including internal
work records and design assets. The website publishes only its own build output;
its workflow currently triggers on website changes, not documentation changes.

A future docs website needs a publication allow-list, matching build triggers,
and working relative links. It should not publish every Markdown file merely
because the file is in the repository.

## Reader paths

### Someone deciding whether to try GEKTOR

Landing page: what it does, one real Agent task, one saved workflow, requirements,
download, and a route to Get started. Keep product claims tied to current behavior.
Use the same product description in the README and site metadata.

### Someone who has downloaded it

Get started: choose Windows or Linux, extract the release, open GEKTOR, choose a
workspace, and choose an Agent task or a model-free workflow. Put source builds
in Development. Explain the local-server prerequisite before a local Agent task.
Link the connection guide for server/API/account-specific instructions.

### Someone doing everyday work

Guides: change code, inspect a tool result, stop or retry, reopen a conversation,
attach a file, connect a browser, add an MCP server, or schedule a recipe.
Each guide starts with the outcome, prerequisites and steps, followed by the
expected result and common failures.

### Someone looking up a detail

Reference: flags, configuration precedence, keyboard bindings, recipe fields,
operation arguments/results, output limits, HTTP endpoints and RPC events.
Examples support the definition; a reference does not make the reader complete
a tutorial first.

### Someone contributing

Development: build, test, understand ownership, change the TUI, add an eval,
maintain the website, or update documentation. Keep historical measurements and
design-review notes identifiable as historical records.

## Proposed source layout

Existing adjacent READMEs remain where their runnable files live. The tree below
shows the maintained collection after migration; short compatibility pages at
old documentation paths are omitted from the tree.

```text
README.md                         project overview and short entry point
SECURITY.md                       canonical security policy and trust boundary
ARCHITECTURE.md                   canonical contributor architecture reference
LICENSE

docs/
  README.md                       documentation home and reading paths
  start.md                        first Agent task or model-free workflow

  guide/
    agent.md                      everyday Agent work, Plan/Act, review and retry
    workflows.md                  open, edit, run and inspect a saved recipe
    models.md                     local servers, APIs, ChatGPT and connection checks
    sessions.md                   save, search, reopen, fork and context compaction
    files-and-images.md           attachments, native readers and image requirements
    browser.md                    enable a browser, sign in and verify a task
    extensions.md                 connect MCP servers and use skill libraries
    automation.md                 schedules, service, queued jobs and reviews
    permissions.md                permission requests and practical trust boundaries
    troubleshooting.md            symptoms, checks and useful diagnostics

  reference/
    cli.md                        invocation, flags and command syntax
    configuration.md              profiles, environment, precedence and stored data
    terminal.md                   current menus, shortcuts and slash commands
    recipes.md                    recipe format, references and execution semantics
    operations.md                 grouped operation contracts and limits
    http-api.md                   workflow ingress endpoints and authentication
    rpc.md                        stdio protocol and events

  development/
    README.md                     build and contributor entry point
    conventions.md                enforced rules and practices
    testing.md                    test map and terminal smoke procedure
    tui.md                        component and interaction contracts
    writing.md                    editorial rules and content ownership

  design/tui/                     existing editable assets and their README
  images/                         documentation images
  internal/                       dated plans and work records; outside public nav

examples/workflows/                runnable recipes and their canonical READMEs
demos/README.md                    launcher instructions beside demo launchers
scripts/README.md                  development/release helpers beside scripts
eval/README.md                     canonical eval runner and scoring guide
eval/prompts/                      frozen scenario specifications
skills/                           project agent guides; own operational format
internal/workspace/builtin/         embedded operational skills
internal/prompts/text/              runtime prompts
website/                          landing page and publication code
```

The new pages each answer a different reader question. Keep related reference
sections together initially; split an operation family only if navigation or
maintenance makes the split useful. A page-count or line-count target should
not dictate the structure.

The developer section can link to ARCHITECTURE.md, eval/README.md and the design
assets. Their sources stay canonical in those locations. An additional
CONTRIBUTING.md is unnecessary for this first migration: the README can link
directly to the contributor entry point.

## Content ownership and migration

Every general human-facing source has a treatment in the inventory. The key
consolidations are:

- `README.md`: keep at the root. Short overview, download, first task, workflow
  alternative, practical permission note and contributor link. Move the long
  launcher table and development command list to their destinations.
- `docs/README.md`: replace the file catalog with reading paths and short
  descriptions of the four sections.
- `docs/bootstrap.md`: distribute setup into start/models; move profile and
  storage details into reference/configuration. Reconcile schedule wording.
- `docs/tui.md`: distribute task instructions among Agent, Workflows, sessions,
  files/images and automation. Move complete menu/key tables to reference/terminal.
- `docs/cli.md`: retain all flag and subcommand coverage in reference/cli.
  Move connection procedures to models, configuration to its reference, session
  recovery to sessions, and tool contracts to operations.
- `docs/workflows.md`: move the manual first run to the workflow guide, recipe
  semantics to reference/recipes, tool contracts to reference/operations, worker
  procedures to automation, and ingress protocol to reference/http-api.
- `docs/browser.md`: keep operator setup/login/checks in guide/browser. Move
  catalog and adapter mechanics to configuration/architecture as appropriate;
  put live-test commands in the testing/eval documentation.
- `docs/extensions.md`: keep connecting servers and using guides in
  guide/extensions. Put capability-file, executable-protocol and credential
  reference details in reference/configuration. Resolve archive/skill wording.
- `SECURITY.md`: keep its root policy role. Guide/permissions explains user
  actions and links to it; it does not create another competing policy.
- `ARCHITECTURE.md`: keep its root canonical role. Reorganize around common
  contributor changes and ownership, preserving implementation facts.
- `docs/conventions.md`: move to development/conventions, preserving enforced
  tags and updating their code consumers in the same change.
- `docs/tui-design.md`: move to development/tui. Keep current contracts;
  separate historical migration status and review tasks from permanent rules.
- `docs/tui-smoke.md`: consolidate its checklist into development/testing.
- `docs/rpc.md`: move to reference/rpc. Keep protocol examples and guarantees.
- `docs/design/tui/README.md`: keep beside the native design project. Link it
  from contributor material rather than exposing it as an end-user tutorial.
- `eval/README.md`: keep beside the runner. Organize the normal run path before
  metadata interpretation and optional experiments. The development test map
  links to it instead of copying its commands and scoring contract.
- `scripts/README.md`: keep development helpers. Put the authoritative end-user
  launcher syntax in reference/cli and link to it.
- `skills/README.md`: keep the agent-guide catalog; clarify who loads each guide
  and link human skill setup to guide/extensions. Preserve its operational format.
- `demos/README.md`: keep launcher-specific instructions. Label OS and model
  requirements; provide a model-neutral first Agent tutorial in the main docs.
- `examples/workflows/README.md`: keep as the example catalog. Select examples
  by intended task and requirements; link into the website's Examples section.
- All seven example READMEs stay beside their recipes. Normalize prerequisites,
  working directory, run steps, expected output and repeat-run behavior. Keep
  actual commands and recipe filenames intact until separately tested changes.
- `website/README.md`: keep website development/deployment instructions. Add
  publication source rules when documentation rendering is implemented.

Agent checklists, runtime prompts, fixtures and third-party licenses retain their
current roles and locations. Humanizer is not a reason to edit prompt behavior
or alter frozen eval criteria. Eval write-ups can receive prose cleanup while
keeping their tasks, facts, budgets and scoring meaning unchanged. Keep the
Ornith measurements and document-reader failures as dated evidence, outside the
first-run path. Development history stays available through contributor links.

## Website publication

### One source for each article

The future site reads the Markdown files above at build time. Example articles
come from their adjacent READMEs. Do not maintain hand-copied website versions.
The existing Vite site can remain the landing-page host. Select a maintained
Markdown renderer during implementation; this proposal does not require a new
site framework, CMS or application runtime.

Use an explicit publication/navigation file, such as `website/docs-nav.json`,
to map a source path, title, section and public route. Keep it a small list of
articles. It is not an execution language or a second store for article prose.
Use this mapping for internal link rewriting and an Edit on GitHub target.

Suggested public routes:

```text
/                                  existing landing page
/docs/                             documentation home
/docs/start/                       first task
/docs/guides/agent/                 task guide
/docs/reference/cli/                flag lookup
/docs/development/                  contributor entry point
/examples/                         task-oriented example catalog
/examples/log-report/               rendered adjacent recipe README
```

Render an actual HTML entry for each route. Direct links and reloads must work
on GitHub Pages, which does not provide a configurable application server.
Honor the existing SITE_BASE_PATH setting for project-path previews.

Publish standard reading controls: section navigation, a page table of contents,
clear code blocks, copyable commands, previous/next links where the sequence
matters, and a visible source/version label. Add search when it helps readers
find these pages; it is not a prerequisite for correcting their structure.

The landing page keeps short examples and links to the corresponding tutorial,
not to an 800-line reference. Future product pages can use the same routes.
The initial site navigation can expose Get started, Docs, Examples and GitHub;
Download remains the main landing-page action.

### Version and offline behavior

A main-branch build must identify itself as development documentation. A release
archive must contain documentation from its own tag. Do not silently promise
that a newly documented feature exists in an older installed binary.
Full multi-version hosting can wait until releases justify maintaining it.

Relative Markdown links must still work in GitHub and extracted archives.
Rewrite them only in the website build, using the publication map. Links to
unpublished source files go to a commit-pinned GitHub target, not a guessed URL.
The website renderer must not include docs/internal, runtime prompts, eval
fixtures, native design projects or license files as ordinary articles.

### Build and packaging changes to account for

- `.github/workflows/website.yml`: trigger on every published content source,
  including relevant docs, examples and root guides. Build the docs and landing
  page from the same revision.
- `website/package.json`, `website/vite.config.js` and a small build-time renderer:
  produce static article routes and reuse the current asset/base-path handling.
- `website/index.html`: update documentation and example links after those routes
  exist. Do not ship links to planned pages.
- `scripts/release.py`: make release documentation selection explicit. Include
  user guides, reference, required images and runnable example instructions;
  exclude internal work records and native design assets from the end-user kit.
  Preserve the chosen project-skill packaging behavior and document it accurately.

## Editorial rules

Use Humanizer as an editing pass, then check the rewrite against the source.
Keep factual limits, permission effects, defaults and failure behavior. Avoid
inventing specificity or claims of reliability to make the writing sound human.

For user guides, start with the task and explain only the concepts needed for
that step. Prefer normal verbs, concrete nouns and current interface labels.
Remove staged introductions, promotional claims, repeated conclusions and chat
residue. Give emphasis and headings a purpose. Keep short numbered procedures
and lookup tables when they help a reader follow or compare facts.

Technical prose can stay neutral. It does not need jokes, an invented personal
voice, or arbitrary stylistic variation. A useful warning belongs next to the
action it changes. A full trust-boundary explanation belongs in its canonical
reference. Keep technical contrasts that distinguish real behaviors, such as
saved sessions versus workspace backups.

During the prose pass, preserve commands, code, identifiers, data, metadata and
link targets. Relocation and link updates are explicit migration work checked
separately. Do not use a banned-word score or a regex as the definition of good
writing. Put the small editorial policy in development/writing; avoid copying
the entire third-party skill into runtime prompts or bundled GEKTOR skills.

## Examples of the intended voice

These are proposed samples, not edits to the current pages.

### Product introduction

GEKTOR is a terminal app for working on code with an AI agent and running saved
workflows. The Agent can read and edit project files, run commands, and use a
browser when configured. Workflows run steps you define and keep a record of
their results. You can connect a local model server, an OpenAI-compatible API,
or an eligible ChatGPT account. Workflows without model steps need no connection.

### CLI reference opening

From the folder where you extracted GEKTOR, run a task in your project:

```powershell
.\gektor.exe -workspace "D:\path\to\project" -verify-cmd "go test ./..." "Fix the failing test"
```

Put flags before the task text. `-workspace` selects the project folder.
`-verify-cmd` runs once after the Agent finishes and fails the run if the command
fails. For the interactive terminal, run `.\gektor.exe -tui`.

The flag-coverage test explanation moves to the writing/testing guide.

### A limit that affects the task

Document readers return part of a large item at a time. If an item has
`next_text_offset`, use that value to continue reading it before deciding that
a field is missing. Top-level `more: false` means there are no later items; the
last item may still have unread text.

## Migration sequence

1. Write the new start page and its two complete paths. Establish the shared
   product description and page ownership. Keep existing routes working.
2. Restructure the large guides into the agreed guide/reference pages. Humanize
   each resulting article after its facts and reader are clear. Reconcile the
   schedule, skill-distribution and menu wording against code.
3. Update consumers in the same migration: documentation checks, source help
   references, example/skill links and release selection. Add a local link/anchor
   check. Run the documented examples in fresh workspaces as appropriate.
4. Publish the collection through the existing site. Update site build triggers,
   implement source-to-route links, then change the landing-page navigation.

Keep short compatibility pages for previously public Markdown paths during the
migration. Preserve old section anchors with links to their new destinations;
GitHub Markdown stubs do not provide HTTP redirects. Website redirects, where
needed later, must match the static hosting mechanism. Retire compatibility
pages only after repository links and known external entry points are addressed.

## Files outside prose that the migration will touch

Moving documents affects working contracts:

- `internal/conventions/conventions_test.go` reads docs/conventions.md and
  docs/cli.md directly. Update those canonical paths while retaining rule and
  flag coverage.
- `internal/conventions/documentation_test.go` has scope tests and an exclusion
  for docs/internal. Preserve its intent under the new layout.
- `internal/conventions/doc.go` describes the conventions source.
- `cmd/gektor/options.go` has help strings linking to browser, extensions and RPC
  documentation. `cmd/gektor/serve.go` and `cmd/gektor/options_test.go` also name
  old documentation paths. Update those references without changing behavior.
- `internal/tui/dialog.go` owns in-product help. Check its labels against the
  terminal reference; change copy only where the audit finds a discrepancy.
- `.env.example`, scripts/README.md, skills/README.md, skills/browser/SKILL.md,
  root guides, design notes and example READMEs contain cross-links to update.
- The website and release files listed above determine which content actually
  ships. A directory move alone does not complete the migration.

No agent-loop, provider, permissions or workflow-engine redesign is needed.

## Acceptance

Ask someone unfamiliar with GEKTOR to follow these paths without repository
history: install a release and run one Agent task; run a model-free workflow;
reopen a session; connect a browser or MCP server; find a failed workflow result.
Observe where the text leaves them without a next step. These are proposed
checks; this audit did not run a reader study.

For each tutorial, check prerequisites, working directory, actual command syntax,
expected output and repeat-run effects. Test the published page as well as the
source: direct route reload, relative links, images, anchors, narrow screens,
keyboard navigation and command copying. Test both custom-domain and project
base paths. Verify extracted-release links and file selection.

Preserve flag coverage, enforced convention tags, operation names and frozen
eval criteria. Run the existing conventions tests, the website check/build, and
relevant example and packaging checks. Prose-only work does not need repeated
model evals; changes to runtime prompts or skills would be a separate tested scope.

The current deliverables are this proposal, the complete source inventory, the
migration map and writing samples. The first implementation increment is the new
start page and its two working paths.
