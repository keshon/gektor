---
name: go-architecture
description: Review and refactor Go package boundaries, ownership, and test placement. Use for architecture and directory layout work; ordinary statement-level readability follows the project's coding style.
---

# Go architecture

A maintainer should find a feature, follow its ordinary path, and change it
without understanding the whole application. Stay within the requested scope.

## Trace before splitting

- Read the entry points, direct callers, implementation, and relevant tests.
  Follow one ordinary operation and its failure path.
- Identify who owns decisions, mutable state, resources, and cancellation.
  Before extracting a package, sketch its public operations, dependencies,
  results, and cleanup. Trace the same operation across the proposed boundary.
- Extract only when the boundary hides a coherent responsibility behind a
  smaller API. If callers still need most internals, fix ownership first.
  File counts, repeated prefixes, and large structs are clues, not split rules.

## Keep responsibilities together

- Group by feature or responsibility. Keep related state, behavior, configuration,
  errors, and tests together. Name files by responsibility.
- Prefer a package-named starting file with the package introduction and main API.
  Keep responsibility-specific types and behavior together in other files; do not
  create a central file containing all logic or artificial entrypoint wrappers.
- Use types for owned state, dependencies, or lifetimes; use plain functions for
  stateless operations. Neither nesting nor one entry type per package is a goal.
- Put coordination between peer features in their parent. Symmetry means
  comparable ownership, not matching filenames or forced common interfaces.
- Export operations and facts callers need. Do not expose mutable internals,
  add forwarding types, or build large host interfaces merely to make a move compile.
- Share behavior only when its semantics agree. Accept modest duplication when
  sharing needs mode flags, callbacks, or unrelated configuration. Shared packages
  should not switch on feature names or collect types solely to avoid import cycles.
- Prefer concrete owners and direct calls. Avoid technical layers, universal
  dependency bags, and many smaller bags that still travel together. Keep helpers
  local unless a package boundary actually reduces what callers must understand.

## Preserve lifecycle and behavior

- Understand validation, permissions, ordering, synchronization, persistence,
  cancellation, and resource limits before simplifying their implementation.
  Preserve wire formats and user-visible behavior.
- Trace startup, completion, cancellation, and cleanup together. Give mutable
  state one owner, keep locks with guarded data, and wait for owned work to stop.
  Preserve cleanup on construction failure and unblock receivers on cancellation.
- In interactive code, distinguish background results from input. Hidden features
  may receive completion but must not consume another feature's keys or paste.
  Shared overlays need an explicit input owner; stale results must be rejected.
- Keep comments that explain contracts or unavoidable constraints. Update them
  when behavior changes. Avoid compatibility wrappers for internal renames unless
  real callers or an explicit compatibility promise require them.

## Keep tests with their subject

- Feature tests belong with the feature; coordination and shared lifecycle tests
  belong with the parent. A child feature's name in a test filename does not
  establish ownership.
- Exercise real constructors and message paths. Use fakes at dependency boundaries;
  do not recreate production behavior in fixtures or export internals for tests.
- Preserve assertions when moving tests. Cover boundaries likely to regress,
  including retained state, hidden work, and completion after cancellation.
  Wait for asynchronous work before releasing test resources.

## Prove the change helped

- Inspect renames, literals, comments, imports, and assertions; compilation cannot
  detect changed labels or weakened tests. Avoid unrelated formatting churn.
- Format changed Go files and run affected tests and repository-required checks.
  Add build, vet, and race checks as warranted by changed contracts and callers.
- Explain what the reader now needs to understand and where they can stop.
  Fewer simultaneous concepts matter more than fewer lines or files. If the
  change only relocates complexity, revise it or report that limitation.
