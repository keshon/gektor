---
name: go-readable
description: Write, review, and refactor Go for clear feature ownership and straightforward reading. Use for Go implementation, package organization, API design, and readability work, especially when flat packages, large state objects, or callback plumbing obscure a feature.
---

# Readable Go

Optimize for the maintainer returning after a month: they should find a feature,
follow its ordinary path, and change it without first understanding the whole app.
This is a preference for explicit, concrete Go, not a claim that one layout is
universally idiomatic. Honor the requested scope and existing behavior contracts.

## Start with the actual feature

- Read its entry points, implementation, direct callers, and relevant tests.
  Follow one ordinary operation and its failure path before proposing a boundary.
- Identify who owns its state, decisions, resources, and cancellation. Distinguish
  behavior from wiring, rendering, persistence, and transport where that helps
  explain the feature; do not automatically turn those words into packages.
- Match established good patterns. Existing complexity is evidence to examine,
  not a convention to reproduce. A local task does not authorize a general rewrite.

## Give related work a home

- Group by a recognizable feature or responsibility. A directory hierarchy can
  provide useful context; neither flatness nor nesting is a goal by itself.
  In Go, a new directory introduces a package boundary, so assess its API too.
- Move related state and behavior together. Keep locks with the data they guard
  and cleanup with the lifetime it controls. A root UI model coordinates screens;
  each screen or component should own its local state and interaction.
- Extract a package when it hides a coherent implementation behind a useful,
  smaller surface. Keep helpers local when a separate package would expose their
  internals, require callbacks in both directions, or force callers to understand
  both halves. One public entry type can legitimately hide a substantial feature.
- Repeated filename prefixes, unrelated clusters in a large struct, and tests
  that must construct the entire application are clues to inspect, not automatic
  split rules. Do not create one package per type or arbitrary architecture layers.
- Evaluate the reader's route through the code. A move that adds exported fields,
  forwarding types, or a large host interface without hiding decisions is unfinished.

## Write steps a person can follow

- Prefer ordinary loops, switches, early error returns, and named intermediate
  values. Keep an operation's meaningful steps in reading order. Dense nested
  literals and chained anonymous functions deserve expansion when they hide work.
- Extract a function when its name explains a useful operation or hides detail
  its caller need not know. Keep consecutive steps together when extracting them
  would merely replace readable code with jumps between tiny helpers.
- Name things using ordinary domain words and their package/receiver context.
  Preserve distinctions that matter. Short conventional names work in short
  scopes; wide scopes need enough detail to avoid repeated definition lookups.
- Use explicit fields for substantial configuration and related parameters when
  they make call sites clearer. Several positional strings or booleans are a
  warning sign. Avoid collecting unrelated dependencies into a universal Env.
  Do not replace such a bag with many tiny bags that are still passed together.
- Prefer concrete structs and direct calls. Introduce a small consumer interface
  for an actual substitution or dependency boundary. A callback is appropriate
  for a real event or supplied operation; a bundle mirroring another object's
  methods deserves a boundary review, not automatic conversion to an interface.
- Prefer a named config struct over functional options for ordinary internal
  construction. Use options when the API's actual consumers benefit from them.
- Accept modest duplication when sharing would require mode flags, callbacks,
  type assertions, or configuration unrelated to a caller. Share behavior whose
  semantics really agree. Generics and dependencies must earn their complexity
  through a present need; new language features are tools, not modernization goals.

## Write comments that carry useful knowledge

- Use plain `//` comments and short paragraphs above the declaration or decision
  they explain. Reserve trailing comments for brief field facts such as units or
  sentinel meanings. Avoid decorative banners and numbered narration of code steps.
- Document exported APIs with Go doc comments beginning with the declared name.
  State their purpose and caller-visible contract, not their implementation recipe.
  Add package documentation when it helps explain the package's responsibility
  and main entry point. Unexported helpers need comments when their contract or
  reasoning is not apparent from their name and body, not merely because they exist.
- Explain what a caller cannot safely infer: ownership and mutation, concurrency
  and ordering guarantees, cancellation, meaningful zero/nil values, units, limits,
  side effects, and significant failure behavior. Include only facts relevant to
  using or changing this code; avoid repeating types and obvious operations.
- Explain non-obvious choices near the code they constrain. For example, say why
  a cache snapshot must remain immutable or why an old result must be rejected.
  "Why, not what" is a useful bias, not a ban on documenting behavior or contracts.
- Keep comments factual and current. Omit author voice, self-praise, change diaries,
  and obsolete implementation history. If a workaround or TODO matters, describe
  the present constraint and the condition for removing it; link an issue when useful.
- Update comments with behavior changes and retain still-valid correctness
  explanations during cleanup. If prose must untangle ordinary control flow,
  simplify that flow first; preserve comments explaining unavoidable complexity.

## Preserve the reasons the code is complex

- Simplicity must retain validation, permissions, ordering, cancellation,
  synchronization, persistence compatibility, and resource limits. Understand
  these contracts before removing branches, caches, hooks, or error handling.
- Handle errors intentionally. Add useful operation context and use %w when
  preserving the error chain matters; do not wrap every forwarding return just
  to satisfy a slogan. Best-effort failure handling should be apparent.
- Keep concurrency only where the feature needs it. Preserve ownership of mutable
  snapshots and admission of asynchronous results; shorter code that admits stale
  work or blocks the UI is worse. Do not replace a mutex with channels by doctrine.
- Preserve existing wire formats and user-visible behavior during a refactor.
  Do not add compatibility wrappers for internal renames unless actual callers
  or a compatibility promise require them.

## Finish with evidence

- Format changed Go files and inspect the diff for unrelated churn. Use existing
  behavioral tests; add targeted coverage for a changed or insufficiently covered
  contract, not tests asserting the chosen filenames or helper structure.
- Run affected tests and repository-required checks. Add build, vet, and race
  checks where the change warrants them; broaden scope when package/API changes
  affect callers. Report actual commands and limitations. Passing tests do not
  establish readability.
- In a readability review, name what the reader now needs to understand and where
  they can stop. Check that the change reduced simultaneous concepts rather than
  merely line counts, file sizes, or constructor argument counts.
- Report remaining costs honestly. If the new API exposes much of the old
  implementation or the refactor only relocates complexity, say so and narrow or
  revise the change. Do not declare success because a directory was created.

Background: the clarity and simplicity priorities in
[Google Go style](https://google.github.io/styleguide/go/guide), and the focused
[style](https://github.com/cxuu/golang-skills/blob/main/skills/go-style-core/SKILL.md)
and [package guidance](https://github.com/cxuu/golang-skills/blob/main/skills/go-packages/SKILL.md).
The rules above are the operative project preferences; those sources are context,
not additional instructions to load automatically.
