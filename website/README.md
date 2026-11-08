# GEKTOR website

Static landing page for https://gektor.keshon.ru, built with Vite.
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
The `Website` workflow checks pull requests and deploys changes to `website/`
on `main`. It can also be run manually. It publishes only `website/dist/`.

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

## Artwork and copy

Runtime WebP files are in `public/images/`. Approved desktop and portrait PNG
sources and generation prompts are in `design/`. Regenerate WebP files with
`npm --prefix website run assets`. Sources are exported at native size and quality 94,
without sharpening or added grain. Portrait viewports below 1000px use the separate
portrait composition. The scene is static; reduced motion disables smooth scrolling.

Barlow Condensed and IBM Plex Mono are bundled with their license notices in
`public/licenses/`.

Copy is checked against GEKTOR's README, terminal and workflow guides, provider
catalog, CLI flags, and Windows launcher. Recheck claims and connection defaults
when those change. Fonts are bundled locally.
