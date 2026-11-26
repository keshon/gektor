# Website and documentation audit

Baseline a998938 plus local changes, 26 November 2026.

## Scope

I traced the homepage, README, documentation index, first Agent run, model
connection, first model-free workflow, failure diagnosis, and contributor entry.
I reviewed the publication list and heading structure across the public docs.
I read the entry pages and linked procedures on those paths. The long reference
pages received a navigation review rather than a line-by-line copy edit.

The website check covers local paths and Markdown anchors in 75 files.
The build checks 37 HTML pages, including the homepage. Internal records,
operational prompts, fixtures and release history were outside the editorial pass.

## Findings

1. **The homepage repeated the headline and sent readers to an old setup page.**
   The GitHub link led to a compatibility page whose first link was about stored
   data. The homepage now names the connection options once and links directly
   to Connect a model. Keep the compatibility page for existing inbound links.
2. **The setup instructions lagged behind the UI.** Get started said “protocol”
   where Settings says “Provider.” Connect a model told LM Studio users to choose
   the generic adapter. Both pages now match Settings. The model guide identifies
   the named presets and gives the key field and shortcut for a custom endpoint.
3. **The model guide changes audience halfway through account setup.** Its
   ChatGPT section goes from sign-in and model selection into DPAPI, PKCE and
   opaque replay state. Keep the sign-in commands, account switching and
   model-access checks in the guide. Put storage facts in Configuration and
   replay implementation in Architecture. Preserve the ChatGPT section anchor.
4. **The two first-run paths are clearer than the homepage setup block.**
   Get started gives Agent and model-free Workflows a complete route and an
   expected result. The homepage's longer setup block is Agent-only and
   repeats part of that route. In the next copy pass, use the homepage to
   choose between the two first runs and leave setup steps in Get started.
5. **Reference pages bury frequent lookups.** Recipe format puts a full image
   example and fixture tests before Inputs and data flow. Operations places
   more than 200 lines of SSH material before file editing. Move common
   recipe syntax up, link to the runnable image example, and give SSH its
   own deep-link destination when the SSH content is next revised. Keep older
   headings as compatibility anchors.
6. **Contributor navigation mixes entry points with specialist pages.** Build
   and contribute is the right entry point. Keep the gallery next to Terminal
   architecture and the native design archive beside its sources. Put
   Architecture and Testing ahead of specialist pages in the Development
   sidebar. Capabilities is linked from Build and contribute but is outside the
   publication list, which takes readers to the pinned GitHub source. Decide
   whether to publish it when revising the contributor sequence.
7. **The docs index and sidebar order Guides differently.** The index lists
   scheduling and browser tasks before model connection and puts Workflows
   last. The sidebar starts with Agent, Workflows and Models. Put model
   connection near the start of both lists and keep the two routine tasks
   easy to spot. Give task guides explicit next links only where the next
   step follows from the current article.

## Next pass

- Keep Get started as the owner of the two first runs. Link to it from the
  homepage and root README. Keep model setup, troubleshooting, and workflow
  examples in their existing source files.
- Revise the Guides navigation and account setup first. Read the revised
  paths in the website preview as a first-time reader. Check each control
  label, command, prerequisite and expected result.
- Revise Recipe format and Operations one section at a time. Preserve routes,
  anchors, identifiers and release links. Run the site checks after each move.
- Remove repeated framing when the same fact is already in the heading,
  prior sentence or linked procedure. Keep details that affect a decision
  or explain recovery. Treat one complete reader path as the unit of review.

## Verification

The root and project-path builds passed. Direct local requests reached the
homepage, first-run guide, model guide, Development, Terminal architecture,
gallery and both gallery stylesheets. The gallery rendered in Chrome at desktop
and narrow widths. The deployed domain could not be verified from this
environment. No deployed changes, reader study, or live model setup were tested.


## Approved edits

The homepage now links to both first runs. The sidebar and docs index use the
same guide order, and Development puts Architecture and Testing before
specialist pages. Capabilities remains linked from Build and contribute as a
source document.

The model guide keeps sign-in, model selection and account checks together.
Configuration owns credential storage, and Architecture explains callback
validation and replay. Recipe syntax and execution rules come before the
image example and fixture tests. SSH has its own reference page. The Operations catalog links directly to it.

After these edits the site checks covered 76 Markdown files and the build
verified 38 HTML pages at both hosting bases.


## Reference follow-up

The Operations table now links to the corresponding entries and gives Agent
tools a direct path. Downloads and archive operations have separate headings.
Recipe execution has headings for ordering, collections, review and retries.
The run-record section links directly to interrupted-run recovery and keeps
storage and inspection in one place.


## First-run smoke test

Built GEKTOR on Windows and ran the documented CLI example in a fresh
workspace with isolated user data. Validation passed, the run paused before
double, and inspection after approval showed outputs 4, 6 and 10. Preparation
had one attempt. The example now describes the item objects that inspection
actually returns. This check exercised the CLI, not the interactive TUI steps.
