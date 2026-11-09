# GEKTOR website

Landing page and documentation for https://gektor.keshon.ru, built with Vite.
Website dependencies are separate from the Go application.
Node.js 22.12+ is required; CI uses Node.js 24.

## Local development

From the repository root:

```sh
npm --prefix website ci
npm --prefix website run dev
```

Open http://127.0.0.1:5176. On Windows, `website/start.cmd` installs missing
dependencies and opens the preview. No GEKTOR process or model connection is needed.

```sh
npm --prefix website run check
npm --prefix website run build
npm --prefix website run preview
```

The default build is served at http://127.0.0.1:4176/.
Dependencies and `dist/` are ignored by Git.

## GitHub Pages

In the repository's Settings → Pages, select **GitHub Actions** as the source.
The `Website` workflow checks pull requests and deploys website and published documentation changes on `main`. It can also be run manually. It publishes only `website/dist/`.

The custom domain is `gektor.keshon.ru`; its DNS CNAME points to `keshon.github.io`.
The domain must also be configured in the repository's Pages settings. The copy
in `public/CNAME` records it in the build, but does not configure Pages by itself.
The workflow gets the base path from Pages and builds at `/` for this domain.
Enable HTTPS in Pages settings once the certificate is ready.

Local production builds default to `/`. To test a project-path build in PowerShell:

```powershell
$env:SITE_BASE_PATH = '/gektor/'
npm --prefix website run build
npm --prefix website run preview
```

## Screenshot and copy

The page uses a system sans-serif font. IBM Plex Mono is bundled for the logo
and command examples, with its license notice in `public/licenses/`.

The terminal screenshot source is `../docs/images/gektor-demo.webp`.
Run `npm --prefix website run assets` after replacing it. The script copies the
WebP unchanged into `public/images/gektor-terminal.webp` and updates its cache
version in `index.html`. If its dimensions or contents change, update the image's
`width`, `height`, alt text and caption in `index.html` as appropriate.

Local design experiments and archived artwork live in `prototypes/`, which is
ignored by Git and excluded from the production build. Local review screenshots
in `design/previews/` are also ignored.

Copy is checked against GEKTOR's README, terminal and workflow guides, provider
catalog, CLI flags, and Windows launcher. Recheck claims and connection defaults
when those change. Fonts are bundled locally.

## Documentation publication

`docs-nav.json` is the publication allow-list: each article has one source, title,
section and route. `scripts/documentation.mjs` reads the canonical Markdown from
`docs/`, root policy/architecture, the eval runner and adjacent example READMEs.
No second article copies live in this folder. Internal plans, prompts, skills,
fixtures and native design projects are excluded.

The Vite plugin emits an HTML entry for every route, so direct links and reloads
work on GitHub Pages. It also serves those routes during `npm run dev`. Relative
article links become site routes; omitted sources point to the build revision
on GitHub. Documentation images are copied into `doc-assets/`. New Markdown
features with local links or assets must be covered by the renderer and checks.

Pages identify themselves as development documentation and link to their source
revision. A local preview can contain uncommitted edits; the revision link names
the Git baseline. Release archives include their own docs and rewrite links to
omitted contributor sources to GitHub. Full versioned hosting and search are
not implemented.

`npm run check` checks Markdown paths/anchors and renderer behavior. `npm run build`
checks every generated HTML link, anchor and asset after Vite finishes. Test both
`SITE_BASE_PATH=/` and a project path such as `/gektor/` when changing publication.
The Website workflow watches docs and examples as well as website sources.

## Search metadata and crawler files

Each `docs-nav.json` article has a required, human-written `description` and an
optional `seoTitle`. The ordinary `title` remains its navigation label. Descriptions
should explain what that page helps a reader do; keep them accurate and distinct.

`scripts/site-metadata.mjs` owns the production origin, homepage metadata and
shared social-preview image. The documentation renderer uses it for article
heads. The Vite HTML hook replaces the single `site-metadata` marker in
`index.html` with the homepage head tags. Metadata is present in the initial
HTML response; no client-side script inserts it.

Canonical and social URLs identify `https://gektor.keshon.ru` even in a local
`SITE_BASE_PATH=/gektor/` preview. The base path changes served navigation and
assets, not production identity. A different production domain or hosting path
requires an explicit metadata configuration change.

The plugin generates `sitemap.xml` from the publication list plus the homepage.
It includes only canonical page routes, with no fabricated modification times.
`robots.txt` uses policy A: public crawling is allowed and the production sitemap
is advertised. No new training-specific restrictions are added. Search and
training crawler policies can be changed separately after an explicit decision.

Both files are generated for production builds and served in local development.
Production uses the custom domain's root `/robots.txt`; a project-path preview's
`/gektor/robots.txt` does not control crawling for that host's root.

The existing WebP terminal screenshot is the shared social image. Asset
preparation still updates the landing page's image cache version; social tags
use the stable public image URL. No per-page image generator is involved.

`npm run check` verifies metadata inputs, escaping, publication and crawler rules.
`npm run build` also checks titles, descriptions, canonicals and social tags in
every generated HTML head, and verifies sitemap membership and robots policy.
Run builds at `/` and `/gektor/` after changing URL behavior. Account verification,
analytics, structured data and tutorial expansion are separate follow-ups.

## Agent documentation access

The same plugin generates `/llms.txt` from the publication list, using its section
names, titles and descriptions. Each entry links to an article's `index.md`, for
example `/docs/start/index.md`. These are generated from the canonical sources;
there are no separately maintained article copies or combined full-site dump.

Markdown exports retain the source prose and code examples. Published article
links point to their Markdown versions; images use public asset URLs and omitted
sources use revision-pinned GitHub links. Destinations are absolute production
URLs, including in project-path previews. HTML heads advertise the index through
`rel="describedby"` and each article's Markdown through `rel="alternate"`.

Both production and development serve the agent files. The checks verify index
membership, generated destinations and anchors, discovery tags, and preservation
of article content and code examples. New Markdown link forms must pass those
checks before publication. Internal plans, prompts and skills remain excluded.

`llms.txt` is an optional entry point for tools that support it. It is not a
search-crawler permission file and does not promise better search rankings.
