# SEO and AI-search visibility proposal

Proposed on 2026-11-09 against `348b0bb`. Status: Phase 1 approved on 2026-11-09, with recommended crawler policy A.
Account setup and Phases 2-3 remain separate follow-ups; the source proposal below
records the agreed scope. Approval covers implementation, not publication.

## Recommendation

Start with one small website change: page metadata, a sitemap, an explicit
crawler policy and build checks. Then measure discovery before deciding which
tutorials to expand. Keep the existing Vite site and canonical Markdown sources.

Treat GEO as helping search and answer systems discover accurate, useful
information about GEKTOR. Human-readable documentation remains the source.
Do not add an AI-only version of the product story.

## Current evidence

The live homepage and two articles were inspected on 2026-11-09:

- The homepage returns HTTP 200 and has a description, canonical URL and Open
  Graph metadata.
- Get started and the RPC reference return HTTP 200. They have document titles,
  but no descriptions, canonical tags, Open Graph metadata or JSON-LD.
- `/robots.txt` and `/sitemap.xml` return HTTP 404.
- The source renderer confirms that all 35 published articles use the same
  minimal head template. The landing page is a separate HTML entry.
- The site already emits full static HTML, has crawlable navigation, and uses
  an explicit publication list. No framework migration is needed.

A missing robots file is not evidence that crawling is blocked. Search Console,
Bing Webmaster Tools, actual index coverage, search demand and crawler access
through hosting infrastructure have not been inspected. Do not describe this
as a ranking or indexing failure without that evidence.

## Phase 1 — technical foundation

Recommended first implementation scope, subject to approval.

### Page metadata

Add a required, human-written `description` to every article record in
`website/docs-nav.json`. Add an optional `seoTitle` only where a more specific
search title helps. Keep `title` as the navigation label. Do not add keyword
arrays, scoring fields or a general content-management schema.

Example for the model connection guide:

```json
{
  "title": "Connect a model",
  "seoTitle": "Connect llama.cpp, LM Studio or ChatGPT to GEKTOR",
  "description": "Configure a local model server, an OpenAI-compatible API or a ChatGPT account for GEKTOR. Check connection settings and context budgets."
}
```

This illustrates the new fields; existing source, section and route fields remain.
Check every description against its article. Avoid unsupported claims such as
complete privacy, sandbox isolation or universal model compatibility.

Generate the following in the initial HTML response for each article and the
homepage:

- A unique, descriptive title and meta description.
- One absolute, self-referencing canonical URL on `https://gektor.keshon.ru`.
- Open Graph title, description, URL, type and image metadata.
- A Twitter summary-card declaration using the same factual metadata and image.

Use the existing public WebP screenshot initially. No new artwork or per-page
social-image generator is needed. Social metadata improves shared-link previews;
it is not presented as a direct search-ranking improvement.

Create one small shared metadata helper with the production origin, site
defaults, URL construction and escaping. Use the existing Vite plugin to render
article metadata and inject homepage metadata at a clear marker in index.html.
This gives the homepage and articles one metadata owner without a template
framework or another build system.

### Canonical and preview URLs

Keep production identity separate from `SITE_BASE_PATH`, which controls served
asset/navigation paths. A local `/gektor/` preview must not produce localhost,
`/index.html` or preview-prefixed production canonicals.

The current public deployment is the custom domain at its root. Production
canonical URLs use its existing trailing-slash routes. Preserve all article
paths and anchors; no redirect migration is part of this work.

PR builds are not currently public preview deployments. Do not introduce a
preview-indexing system for a hosting feature we do not have. If public previews
are added later, specify their indexing policy at that time.

### Sitemap

Generate `sitemap.xml` from the publication list plus the homepage: currently
36 canonical URLs. Include only real, published HTML routes.

Exclude internal plans, repository-only files, prompts, skills, fixtures, assets,
anchors, `/index.html` aliases and preview origins. Keep the publication
allow-list authoritative; do not discover sitemap entries by scanning all files.

Omit `lastmod` initially. Build time and the latest repository commit are not
reliable per-article modification dates. Do not invent update timestamps,
priorities or change frequencies.

### Crawler policy

Recommended baseline: permit public search crawling and advertise the production
sitemap in `robots.txt`. Do not add disallow rules for internal source paths that
the website never publishes, and do not use robots rules as an access-control
mechanism.

Training policy is a separate review choice:

- **A — preserve current openness (recommended for the smallest change):** no
  new training-specific restrictions; allow public crawling and list the sitemap.
- **B — opt out of OpenAI training crawling:** add a specific GPTBot disallow
  rule while keeping OAI-SearchBot allowed for search discovery.

Option B is an OpenAI-specific robots policy, not a promise to block every
training crawler or every use of public content. Other providers would need
their own verified controls. Do not silently choose a broader policy.

Generate robots.txt through the same publication code that knows the production
origin. Its sitemap URL must agree with the sitemap and canonical tags. Because
production uses a root custom domain, it will be served at `/robots.txt`.
A file served under `/gektor/robots.txt` during a project-path preview does not
control crawling for that host's root.

### Files and entities affected

- **`website/docs-nav.json`:** add article descriptions and optional search-title
  overrides; retain publication membership, routes and navigation labels.
- **New `website/scripts/site-metadata.mjs`:** small shared metadata/URL helper
  and homepage defaults; no new dependency expected.
- **`website/scripts/documentation.mjs`:** use the helper for article heads;
  emit sitemap.xml and robots.txt; inject homepage metadata through Vite's HTML
  transform hook. Keep existing Markdown rendering and link behavior.
- **`website/index.html`:** replace independently maintained SEO head tags with
  the injection marker. Keep visible landing-page copy and layout unchanged.
- **`website/scripts/documentation.test.mjs`:** verify metadata selection,
  escaping, URL construction and crawler/sitemap output.
- **`website/scripts/verify-build.mjs`:** check the generated HTML and discovery
  files, alongside the existing path/anchor/asset checks.
- **`website/README.md`:** document metadata ownership, crawler policy and
  production-versus-preview URL behavior.
- **`.github/workflows/website.yml`:** no change expected; it already watches
  website sources and runs the relevant check/build commands.

No app code, model providers, workflow engine, release packaging, prompts or
skills are involved. Repository skills, every SKILL.md, embedded guides, runtime
prompts and eval specifications remain frozen.

### Acceptance

- Every one of the 36 generated HTML pages has exactly one title, description
  and canonical, plus complete social metadata in its source HTML.
- Article descriptions and final titles are nonempty and distinct. Review their
  accuracy and usefulness; do not enforce an invented ideal character count.
- Metadata safely escapes quotation marks and HTML-sensitive characters.
- Canonical and Open Graph URLs agree with the production route.
- Sitemap entries are unique, match the publication list and point to generated
  pages. No private/unpublished paths, fragments or invented dates appear.
- Robots policy matches the approved choice and references the correct sitemap.
- Existing route, anchor, asset and Markdown-link checks continue to pass.
- Check and build at both `/` and `/gektor/`; served links follow the base path,
  while production identity stays consistent.
- Inspect the homepage, Get started, a reference and an example in generated
  HTML. Confirm shared-link image URLs resolve. After an authorized deployment,
  verify live HTTP status and metadata; a local build does not prove deployment.

Implementation approval would authorize this bounded scope. Commit, push and
publication should follow the user's instructions at that point; they are not
part of approving this proposal by implication.

## Phase 2 — discovery measurements

Set up or inspect owner access to Google Search Console and Bing Webmaster Tools.
This needs a separate account/verification step; no account connection, DNS edit,
verification token or analytics service is added during Phase 1.

Submit the sitemap, inspect the homepage and a few representative article URLs,
and establish a baseline for index coverage, queries, impressions, clicks and
landing pages. Record the date of deployment so later results can be compared.

Review results after enough data accumulates. Distinguish discovery, indexing,
traffic and actual product interest; do not treat a metadata change as proof of
ranking gains. No recurring monitoring automation is proposed.

## Phase 3 — useful content and factual identity

Choose content work from observed queries and real user questions. Initial
candidates, not commitments to create duplicate pages:

- Expand the model guide with a tested first Agent task using llama.cpp.
- Improve the existing model-free workflow example with its actual output and
  review steps, rather than creating another overview article.
- Add a complete local-browser verification walkthrough with prerequisites,
  screenshots and limits on what was verified.

For each, decide whether an existing page should expand or a separate tutorial
answers a genuinely different task. Keep instructions canonical in the repository
and render them on the site. Record the model/version and relevant conditions
when presenting measurements. Do not manufacture benchmarks or competitor rankings.

Keep the product name, purpose, supported platforms, repository, license and
model requirements consistent across the homepage, GitHub and documentation.
Consider factual WebSite/software structured data in a separate small follow-up.
Breadcrumb markup should accompany visible, working breadcrumbs. No invented
ratings, reviews, offers or unsupported rich-result promises.

## Deferred

No CMS, SEO plugin, external search service, keyword dashboard, paid SEO tooling,
automatic blog factory, hidden AI-targeted prose or speculative localization.
The initial plan deferred llms.txt and Markdown exports. On 2026-11-09 the user
approved adding them after the technical foundation, as an optional entry point
for agents. Both are generated from the existing publication list and canonical
sources; they are not a Google Search requirement or a ranking promise.

## Basis for the recommendations

- [Google: AI-search optimization guidance](https://developers.google.com/search/docs/fundamentals/ai-optimization-guide):
  retain people-first content and normal search fundamentals; special AI wording
  or llms.txt is not required for Google's AI search features.
- [Google: canonical URLs](https://developers.google.com/search/docs/crawling-indexing/consolidate-duplicate-urls)
  and [sitemap construction](https://developers.google.com/search/docs/crawling-indexing/sitemaps/build-sitemap):
  communicate preferred, discoverable URLs consistently.
- [OpenAI: crawler controls](https://developers.openai.com/api/docs/bots):
  OAI-SearchBot search access and GPTBot training crawling have separate controls.

These controls support discovery and interpretation. They do not guarantee
indexing, rankings or citation by an answer system.

## Review decisions

1. Approve Phase 1 as the first implementation, or revise its scope.
2. Choose crawler policy A or B; A is the recommended minimal baseline.
3. Keep structured data, account verification and tutorial work as separate
   follow-ups, or explicitly bring a named item into the first scope.

Phase 1 received the user's greenlight. Implementation and validation are recorded below.

## Phase 1 implementation record — 2026-11-09

Implemented locally with crawler policy A:

- Shared production metadata helper and homepage injection marker.
- Human-written descriptions for all 35 published articles; four search-title
  overrides, with navigation labels, source files and routes unchanged.
- Static titles, descriptions, canonicals, Open Graph and Twitter card metadata
  for all 36 public pages, using the existing WebP image.
- Generated sitemap.xml and robots.txt for builds and local development.
- Metadata, escaping, sitemap, crawler-policy and build-output checks.
- Website maintenance instructions; no dependencies or CI configuration added.

Validation completed:

- Website check passed: 61 Markdown files and six renderer/metadata tests.
- Builds at `/` and `/gektor/` passed checks of all 36 generated HTML pages,
  metadata, sitemap membership, robots policy, assets, links and anchors.
- A deliberately incorrect canonical in a generated article was rejected by
  the build verifier. The temporary corruption was restored immediately.
- HTTP checks against production previews at both base paths passed for the
  homepage, Get started, RPC and log-report example. Discovery files returned
  HTTP 200 with valid XML/plain text and exactly 36 sitemap URLs. Canonicals
  stayed on the production custom domain without the preview prefix.
- The shared image returned HTTP 200 with image/webp from local previews and
  its existing public URL. Development homepage/RPC metadata also passed HTTP checks.
- The visible landing body and publication membership were unchanged. All
  67 frozen source files remained byte-for-byte unchanged.

These checks validate local output, not deployment, search indexing or social
platform cache refreshes. No commit, push, deployment, account verification,
structured data, analytics or new tutorials were performed.

## Agent documentation access — 2026-11-09

Approved as an addition to Phase 1, with commit and push requested:

- Generate `/llms.txt` from the existing public documentation manifest.
- Publish each article at its canonical route plus `index.md`, with absolute
  destinations to Markdown articles, public images or revision-pinned sources.
- Advertise the index and Markdown alternatives in initial HTML head links.
- Serve identical generated content in development and production builds.
- Verify all 35 exported articles retain prose and code, and check publication
  membership, links and anchors. Do not publish internal plans, prompts or skills.

No extra dependencies, manually maintained article copies or full-site dump.

Validation: eight website tests passed. Builds at `/` and `/gektor/` verified
36 HTML pages and 36 agent files, including metadata, discovery, destinations,
anchors and publication membership. All agent files were also checked over HTTP
in development and the root production preview. All 67 frozen files are unchanged.
