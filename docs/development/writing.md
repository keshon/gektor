# Write and maintain documentation

Write for the person trying to complete a task. Start with what they can do,
what they need, and where to begin. Keep implementation explanations in the
contributor section unless they change a user's decision.

## Content ownership

- The root README introduces the product and links to Get started.
- Get started contains two complete first runs: Agent and a model-free workflow.
- Guides explain tasks and recovery. References own complete syntax and limits.
- SECURITY.md owns security policy; ARCHITECTURE.md owns implementation boundaries.
- Examples keep instructions beside runnable files. The website renders those sources.
- Internal plans are dated records outside public navigation.
- Skills, prompts, agent instructions, eval specifications and fixtures have
  operational meaning. They are outside this editorial migration and remain frozen.

Link to the canonical rule instead of copying it into several articles. A small
reminder beside an affected step is useful; competing full explanations are not.
Keep historical claims dated and distinguish them from current contracts.

## Editorial pass

Use the plain technical approach in [Humanizer](https://github.com/blader/humanizer/blob/main/SKILL.md).
Remove inflated claims, staged introductions, repeated conclusions, chat residue
and emphasis that serves no purpose. Use current menu names and concrete actions.
Neutral technical prose needs neither invented personality nor arbitrary variation.

Preserve useful limits, failure behavior and permission effects. Keep commands,
code, identifiers, metadata and data unchanged during prose editing. Relocation
and link updates are separate work. Do not use banned-word counts as a quality score.

For a tutorial, check prerequisites, working directory, steps, expected output
and repeat-run effects. Say when a command needs a source checkout, an extracted
release, a model, a network connection or a separate runtime.

## Links, publication and checks

Use relative Markdown links so articles work on GitHub and in extracted releases.
Old public paths retain short compatibility pages and original heading anchors,
including paths referenced by frozen skills. New links should use canonical pages.

The website's publication list is `website/docs-nav.json`. Add an article there
only when it belongs in public navigation. Its renderer rewrites links at build
time, identifies development docs by revision and leaves source articles canonical.
Internal plans, operational prompts, fixtures and native design projects are not
public articles. Release packaging has a separate explicit selection.

Install website dependencies with `npm --prefix website ci` first.
From the repository root:

```sh
node website/scripts/check-docs.mjs
go test ./internal/conventions
npm --prefix website run check
npm --prefix website run build
```

The conventions test checks CLI flag coverage and project rule tags in their
canonical references. A local link check does not verify external URLs or prove
that a new reader can follow a tutorial. Test runnable examples and inspect the
built site, including direct reloads and a project base path, when changing publication.
