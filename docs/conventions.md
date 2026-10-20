# Conventions

> How the system works is in [ARCHITECTURE.md](../ARCHITECTURE.md). Usage is in
> [README.md](../README.md). This document is the house rules.

## How to read this

Every rule carries a tier, because a rule nobody can verify is worse than no
rule: it creates the belief of compliance without the fact.

- **[enforced: &lt;name&gt;]** — a check fails, and the name says which one.
  `internal/conventions` is an ordinary Go test, so `go test ./...` runs it
  with everything else. `TestDocumentAndChecksAgree` keeps the tags here and
  the checks there in step, so this file cannot advertise enforcement that does
  not run, and a check cannot enforce something this file never states.
- **[invariant]** — nothing checks it, and violating it breaks something
  nameable. Each states the failure it prevents.
- **[practice]** — how things are done here. Nothing breaks if you deviate.

A rule earns a place only if it can claim one of those three. A preference
without a test method is a preference, and preferences get deleted rather than
documented.

**This file is executable.** `internal/conventions` reads it while the tests
run. Reword a paragraph tagged `[enforced: x]` and you have reworded a build
failure message. Remove or rename a tag and the build goes red until the checks
match — deliberately, because a rule that quietly stops being enforced is the
failure this arrangement exists to prevent.

**Adding a rule.** Give it a tier. If it would be [enforced], write the check
first and let it fail, then record the baseline.

## Package layers

**[enforced: package-layers]** `internal/llm`, `internal/prompts`,
`internal/workspace`, `internal/permission` and `internal/events` import no other
internal package.
`internal/session` imports only `internal/llm`.
`internal/harness` imports nothing internal: it holds the vocabulary shared
by the loop and the preferences file, so each can reach it without either
reaching for the other.
`internal/agent` imports only `internal/llm`, `internal/prompts`,
`internal/permission`, `internal/session` and `internal/harness`. This is what
lets `internal/tools` implement `agent.Tool` without an import cycle, and what
keeps the loop testable against a stub client with no filesystem in sight.

**[invariant]** `internal/agent` never learns which backend it is talking to.
A backend quirk handled inside the loop is a quirk that will be handled twice
the next time a backend is added, and inconsistently the second time.

**[enforced: structured-output]** A constrained `llm.ChatRequest` sets both
`Grammar` and `JSONSchema`. The two are one contract in two encodings, and each
backend reads only one of them: koboldcpp takes GBNF on the chat endpoint,
llama-server takes `response_format` and ignores a grammar field there without
complaint. Sending one encoding leaves the other backend generating free text
that happens to be asked for JSON, which fails later and somewhere else.

## Dependencies

**[enforced: cgo-free]** No package imports `C`. cgo turns a single `go build`
into a toolchain problem, breaks cross-compilation, and is not worth it for a
tool whose whole job is to run somewhere unattended.

**[practice]** The standard library first. A third-party package is considered
only if it is cgo-free, widely used, and earns its weight — `golang.org/x/sys`
qualifies because Windows job objects have no standard-library equivalent and
without them a stopped dev server can survive, still holding its port. The
Charm terminal stack (Bubble Tea v2, Lipgloss v2, Bubbles v2,
and their input/ANSI helpers) qualifies for `-tui`
only: writing a fullscreen renderer on raw ANSI escapes is a second project,
and the agent loop and model transports do not import the terminal stack.
Process cleanup in the tools package uses the Windows system dependency.

## Building agents

**[enforced: agent-construction]** Only `internal/roles` calls `agent.New`.
Agent roles were once configured at their call sites, and they
drifted — one harness ended up without `ask_user` and `delegate_task` while the
shipping CLI had both, so it was scoring a different agent than the one under
test. One answer per role, in one place.

**[invariant]** A capability is removed by the harness, never by asking the
model not to use it. The plan role keeps the acting role's system prompt and
tool list — a narrower one would change the prompt prefix, so a backend that
caches prompts would re-read the whole conversation on every mode switch — and
refuses every call outside the read-only set before it runs. A subagent gets a
registry without delegation, so delegation cannot recurse.

## Verification

**[invariant]** File effects and verification results are measured, never inferred
from answer wording. An explicit verify command supplies mechanical evidence.
Verification records a real command verdict without requesting a model self-check.

**[invariant]** Repeated tool calls remain legal whenever something may have
changed between them; the step limit bounds execution, and checklist changes
cannot extend it. Two deterministic guards cover the case where nothing did
change. An exact repeat of an idempotent call (a pure read, a directory listing,
a search) is refused instead of re-executed, because its result is still in
history. Three consecutive steps with the same call signature end the run with
`ErrRepeatingSteps` after one turn with no tools offered. Both guards reset on
any mutation, so rewrites and shell commands that change the workspace are never
counted as repetition. Recovery addresses concrete protocol failures.

## Prompts

**[enforced: prompt-registration]** Every `internal/prompts/text/*.txt` is
referenced by `internal/prompts/prompts.go`. An orphaned prompt file is either
dead weight or a prompt someone forgot to wire up, and both look the same from
outside.

**[enforced: skill-paths]** Every `skills/.../SKILL.md` a prompt names exists in the
repository, and every prompt that names one says "if it exists". A model told
to read a file that is not there spends a turn reasoning about the
contradiction — live, one concluded the instruction itself was an error — and
the agent's workspace is often not this repository.

**[practice]** Prompts live as `.txt`, not Go string constants. A one-sentence
wording change should be a one-line diff, not a change buried in a
multi-line concatenation.

## Evals

**[enforced: probe-prompts]** Every probe's `prompt` field names a file that
exists in `eval/prompts/`. A probe whose write-up has been renamed or deleted
is a criterion nobody can check the intent of.

**[enforced: cli-flags]** Every flag `cmd/gektor` registers is named in
`docs/cli.md` (as `` `-name` ``). A reference that rots one flag at a time
is how five flags went undocumented unnoticed.

**[invariant]** A probe's pass criteria come from its write-up in
`eval/prompts/`, not from what seems reasonable while writing the JSON. A
criterion invented at the probe is a number the harness will one day fail on
for no stated reason.

**[invariant]** A run that never reached the model is not scored. A dead
backend produces failed workspace checks that look exactly like a model that
got worse.

## Documentation

**[enforced: doc-identifiers]** Every Go identifier written in backticks in
`ARCHITECTURE.md` and `README.md` exists in the code. Documentation that names
something removed three refactors ago is worse than documentation that omits
it.

**[enforced: comment-dates]** Comments in non-test Go files do not cite dates.
A comment saying a rule came from an incident on a particular day is
unverifiable to every reader who was not there. Keep the rule; the incident
belongs in a test name. Existing occurrences are recorded in `baseline.json`
and may only decrease.

**[practice]** Comments explain why, not what. The code already says what.

## Testing

**[enforced: doc-and-checks-agree]** Every `[enforced: x]` tag here has a check
in `internal/conventions`, and every check has a tag here.

**[practice]** A bug found in a live run gets a test named after its shape
before it gets a fix. The test name is where that history belongs.
