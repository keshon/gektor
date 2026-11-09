import fs from 'node:fs';
import path from 'node:path';
import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { Marked, Renderer } from 'marked';
import GithubSlugger from 'github-slugger';
import navigation from '../docs-nav.json' with { type: 'json' };
import { escape, homepage, injectHomepageMetadata, renderMetadata, renderRobots, renderSitemap, site } from './site-metadata.mjs';

export const root = fileURLToPath(new URL('../../', import.meta.url));
export const pages = navigation;
const bySource = new Map(pages.map(page => [page.source, page]));
if (bySource.size !== pages.length || new Set(pages.map(p => p.route)).size !== pages.length) {
  throw new Error('Duplicate documentation source or route');
}
for (const page of pages) {
  if (!/^\/(docs|examples)\//.test(page.route) || !page.route.endsWith('/')) throw new Error(`Invalid route: ${page.route}`);
  if (page.source.includes('..') || !/^(docs\/(README|start|guide\/[^/]+|reference\/[^/]+|development\/[^/]+)\.md|examples\/workflows\/(?:[^/]+\/)?README\.md|ARCHITECTURE\.md|SECURITY\.md|eval\/README\.md)$/.test(page.source)) {
    throw new Error(`Source outside publication boundary: ${page.source}`);
  }
}
export const revision = execFileSync('git', ['rev-parse', 'HEAD'], { cwd: root, encoding: 'utf8' }).trim();
const localChanges = execFileSync('git', ['status', '--porcelain', '--', 'docs', 'examples/workflows', 'ARCHITECTURE.md', 'SECURITY.md', 'eval/README.md'], { cwd: root, encoding: 'utf8' }).trim() !== '';
const repository = 'https://github.com/keshon/gektor';
export { escape };
export const markdownRoute = page => page.route + 'index.md';

export function renderLLMs() {
  const groups = [...new Set(pages.map(page => page.section))];
  return `# GEKTOR\n\n> ${homepage.description}\n\nThese pages describe the current development source. Release archives include documentation from their own revision. Follow individual links as needed.\n\n${groups.map(group => `## ${group}\n\n${pages.filter(page => page.section === group).map(page => `- [${page.title}](${site.origin}${markdownRoute(page)}): ${page.description}`).join('\n')}`).join('\n\n')}\n`;
}

// Preserve the source prose and code; only link destinations change for publication.
export function renderMarkdown(page) {
  const source = fs.readFileSync(path.join(root, page.source), 'utf8');
  return publishMarkdown(page.source, source);
}

export function publishMarkdown(sourcePath, source) {
  source = source.replaceAll('\r\n', '\n');
  const marked = new Marked();
  const edits = [];
  let cursor = 0;
  marked.walkTokens(marked.lexer(source), token => {
    if (!['link', 'image', 'code', 'codespan', 'html'].includes(token.type)) return;
    const start = source.indexOf(token.raw, cursor);
    if (start < 0) throw new Error(`Cannot locate Markdown token in ${sourcePath}`);
    cursor = start + token.raw.length;
    if (token.type !== 'link' && token.type !== 'image') return;
    const target = localTarget(sourcePath, token.href);
    if (!target) return;
    const published = bySource.get(target.file);
    const href = published
      ? site.origin + markdownRoute(published) + (target.fragment ? `#${encodeURIComponent(target.fragment)}` : '')
      : new URL(siteLink(sourcePath, token.href), site.origin).href;
    // Reference-style links are expanded, so their definitions need no rewriting.
    const inlineEnd = token.raw.indexOf('](');
    const referenceEnd = token.raw.lastIndexOf('][');
    const labelEnd = inlineEnd >= 0 ? inlineEnd : referenceEnd >= 0 ? referenceEnd : token.raw.lastIndexOf(']');
    const title = token.title ? ` "${token.title.replaceAll('"', '&quot;')}"` : '';
    edits.push({ start, end: cursor, text: `${token.raw.slice(0, labelEnd + 1)}(<${href}>${title})` });
  });
  for (const edit of edits.reverse()) source = source.slice(0, edit.start) + edit.text + source.slice(edit.end);
  return source;
}
export function basePath(value = '/') {
  return `/${value.split('/').filter(Boolean).join('/')}${value.split('/').filter(Boolean).length ? '/' : ''}`;
}
export function localTarget(source, href) {
  if (/^[a-z][a-z\d+.-]*:|^\/\//i.test(href)) return null;
  const [location, fragment = ''] = href.split('#');
  const file = path.posix.normalize(path.posix.join(path.posix.dirname(source), decodeURIComponent(location || path.posix.basename(source))));
  if (file === '..' || file.startsWith('../') || path.posix.isAbsolute(file)) throw new Error(`Link escapes repository: ${source}: ${href}`);
  return { file, fragment: decodeURIComponent(fragment) };
}
export function siteLink(source, href, base = '/') {
  const target = localTarget(source, href);
  if (!target) return href;
  const fragment = target.fragment ? `#${encodeURIComponent(target.fragment)}` : '';
  const page = bySource.get(target.file);
  if (page) return base + page.route.slice(1) + fragment;
  if (target.file.startsWith('docs/images/')) return base + 'doc-assets/' + target.file + fragment;
  const kind = fs.existsSync(path.join(root, target.file)) && fs.statSync(path.join(root, target.file)).isDirectory() ? 'tree' : 'blob';
  return `${repository}/${kind}/${revision}/${target.file.split('/').map(encodeURIComponent).join('/')}${fragment}`;
}

// The same tokenization supplies site anchors and the source-link check.
export function article(source, base = '/') {
  const headings = [], links = [], ids = new Set(), slugger = new GithubSlugger();
  const marked = new Marked({
    renderer: {
      heading({ tokens, depth }) {
        const html = this.parser.parseInline(tokens);
        const text = html.replace(/<[^>]*>/g, '').replace(/&amp;/g, '&').replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&quot;/g, '"').replace(/&#39;/g, "'");
        const id = slugger.slug(text);
        ids.add(id);
        headings.push({ id, depth, text });
        return `<h${depth} id="${escape(id)}">${html}</h${depth}>\n`;
      },
      link(token) {
        return `<a href="${escape(siteLink(source, token.href, base))}"${token.title ? ` title="${escape(token.title)}"` : ''}>${this.parser.parseInline(token.tokens)}</a>`;
      },
      image(token) {
        return `<img src="${escape(siteLink(source, token.href, base))}" alt="${escape(token.text)}" loading="lazy">`;
      },
      table(token) { return `<div class="table-scroll" tabindex="0" role="region" aria-label="Reference table">${Renderer.prototype.table.call(this, token)}</div>`; },
    },
    walkTokens(token) {
      if (token.type === 'link' || token.type === 'image') links.push(token.href);
      if (token.type === 'html') {
        for (const match of token.text.matchAll(/(?:id|name)=["']([^"']+)["']/g)) ids.add(match[1]);
      }
    },
  });
  const html = marked.parse(fs.readFileSync(path.join(root, source), 'utf8'));
  return { html, headings, ids, links };
}

export function renderPage(page, base = '/') {
  const { html, headings } = article(page.source, base);
  const groups = [...new Set(pages.map(p => p.section))];
  const navigation = groups.map(group => `<section><h2>${escape(group)}</h2><ul>${pages.filter(p => p.section === group).map(p => `<li><a href="${base + p.route.slice(1)}"${p === page ? ' aria-current="page"' : ''}>${escape(p.title)}</a></li>`).join('')}</ul></section>`).join('');
  const index = pages.indexOf(page), previous = pages[index - 1], next = pages[index + 1];
  const adjacent = (p, label) => p && p.section === page.section ? `<a href="${base + p.route.slice(1)}">${label}: ${escape(p.title)}</a>` : '<span></span>';
  return `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><meta name="color-scheme" content="light dark">${renderMetadata(page)}<link rel="icon" href="${base}favicon.svg"><link rel="stylesheet" href="${base}docs.css"><script src="${base}docs.js" defer></script></head><body>
<a class="skip-link" href="#content">Skip to content</a>
<header class="docs-header"><a class="wordmark" href="${base}">GEKTOR</a><nav aria-label="Main navigation"><a href="${base}docs/start/">Get started</a><a href="${base}docs/">Docs</a><a href="${base}examples/">Examples</a><a href="${repository}">GitHub</a></nav></header>
<div class="docs-layout"><details class="docs-navigation" open><summary>Documentation menu</summary><nav aria-label="Documentation">${navigation}</nav></details>
<main id="content" tabindex="-1"><p class="version">Development documentation${localChanges ? ' · local changes' : ''} · <a href="${repository}/tree/${revision}">${revision.slice(0, 7)}</a></p><article>${html}</article><nav class="page-turn" aria-label="Adjacent pages">${adjacent(previous, 'Previous')}${adjacent(next, 'Next')}</nav><footer><a href="${repository}/blob/${revision}/${page.source}">Source at this revision</a> · <a href="${repository}/edit/main/${page.source}">Edit on GitHub</a><p>These pages describe the current development source. Release archives include documentation from their own revision.</p></footer></main>
<aside class="page-outline"><nav aria-label="On this page"><h2>On this page</h2><ul>${headings.filter(h => h.depth === 2).map(h => `<li><a href="#${escape(h.id)}">${escape(h.text)}</a></li>`).join('')}</ul></nav></aside></div><p id="copy-status" class="sr-only" role="status" aria-live="polite"></p></body></html>`;
}

export function documentationPlugin() {
  let base = '/';
  return {
    name: 'gektor-documentation',
    configResolved(config) { base = basePath(config.base); },
    transformIndexHtml: {
      order: 'pre',
      handler(html, context) { return context.path === '/index.html' ? injectHomepageMetadata(html) : html; },
    },
    generateBundle() {
      for (const page of pages) {
        this.emitFile({ type: 'asset', fileName: page.route.slice(1) + 'index.html', source: renderPage(page, base) });
        this.emitFile({ type: 'asset', fileName: markdownRoute(page).slice(1), source: renderMarkdown(page) });
      }
      this.emitFile({ type: 'asset', fileName: 'llms.txt', source: renderLLMs() });
      this.emitFile({ type: 'asset', fileName: 'sitemap.xml', source: renderSitemap(pages) });
      this.emitFile({ type: 'asset', fileName: 'robots.txt', source: renderRobots() });
      for (const name of ['docs.css', 'docs.js']) this.emitFile({ type: 'asset', fileName: name, source: fs.readFileSync(path.join(root, 'website/src', name)) });
      for (const name of fs.readdirSync(path.join(root, 'docs/images'))) {
        const source = `docs/images/${name}`;
        if (fs.statSync(path.join(root, source)).isFile()) this.emitFile({ type: 'asset', fileName: `doc-assets/${source}`, source: fs.readFileSync(path.join(root, source)) });
      }
    },
    configureServer(server) {
      server.middlewares.use((req, res, next) => {
        const url = new URL(req.url, 'http://localhost').pathname;
        const page = pages.find(p => base + p.route.slice(1) === url);
        if (page) { res.setHeader('Content-Type', 'text/html; charset=utf-8'); res.end(renderPage(page, base)); return; }
        const markdownPage = pages.find(p => base + markdownRoute(p).slice(1) === url);
        if (markdownPage || url === base + 'llms.txt') {
          res.setHeader('Content-Type', markdownPage ? 'text/markdown; charset=utf-8' : 'text/plain; charset=utf-8');
          res.end(markdownPage ? renderMarkdown(markdownPage) : renderLLMs());
          return;
        }
        const name = url.slice(base.length);
        if (name === 'sitemap.xml' || name === 'robots.txt') {
          res.setHeader('Content-Type', name === 'sitemap.xml' ? 'application/xml; charset=utf-8' : 'text/plain; charset=utf-8');
          res.end(name === 'sitemap.xml' ? renderSitemap(pages) : renderRobots());
          return;
        }
        let file;
        if (['docs.css', 'docs.js'].includes(name)) file = path.join(root, 'website/src', name);
        if (name.startsWith('doc-assets/docs/images/') && !name.includes('..')) file = path.join(root, name.slice('doc-assets/'.length));
        if (file && fs.existsSync(file)) { res.setHeader('Content-Type', name.endsWith('.css') ? 'text/css' : name.endsWith('.js') ? 'text/javascript' : 'application/octet-stream'); res.end(fs.readFileSync(file)); return; }
        next();
      });
    },
  };
}
