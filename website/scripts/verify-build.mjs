import fs from 'node:fs';
import { Marked } from 'marked';
import { article, basePath, markdownRoute, pages } from './documentation.mjs';
import { escape, homepage, pageMetadata, site } from './site-metadata.mjs';

const base = basePath(process.env.SITE_BASE_PATH || '/');
const dist = new URL('../dist/', import.meta.url);
const files = fs.readdirSync(dist, { recursive: true }).map(p => p.replaceAll('\\', '/')).filter(p => p.endsWith('.html'));
const failures = [];
const publicPages = [homepage, ...pages];
const byFile = new Map(publicPages.map(page => [page.route.slice(1) + 'index.html', pageMetadata(page)]));
const attributes = tag => Object.fromEntries([...tag.matchAll(/([\w:-]+)\s*=\s*"([^"]*)"/g)].map(m => [m[1], m[2]]));
for (const key of ['title', 'description', 'canonical']) {
  if (new Set([...byFile.values()].map(m => m[key])).size !== publicPages.length) failures.push(`Duplicate public ${key}`);
}
for (const page of publicPages) if (!files.includes(page.route.slice(1) + 'index.html')) failures.push(`Missing route: ${page.route}`);
for (const file of files) {
  const html = fs.readFileSync(new URL(file.replaceAll('\\', '/'), dist), 'utf8');
  const expected = byFile.get(file);
  if (!expected) { failures.push(`HTML page outside publication list: ${file}`); continue; }
  const head = html.match(/<head\b[^>]*>([\s\S]*?)<\/head>/i)?.[1] || '';
  const titles = [...head.matchAll(/<title>([\s\S]*?)<\/title>/gi)];
  if (titles.length !== 1 || titles[0][1] !== escape(expected.title)) failures.push(`${file}: incorrect or duplicate title`);
  const meta = [...head.matchAll(/<meta\b[^>]*>/gi)].map(m => attributes(m[0]));
  const required = {
    description: expected.description,
    'og:site_name': site.name,
    'og:title': expected.title, 'og:description': expected.description,
    'og:type': 'website', 'og:url': expected.canonical,
    'og:image': expected.image, 'og:image:alt': site.imageAlt,
    'twitter:card': 'summary_large_image', 'twitter:title': expected.title,
    'twitter:description': expected.description, 'twitter:image': expected.image,
    'twitter:image:alt': site.imageAlt,
  };
  for (const [key, value] of Object.entries(required)) {
    const matches = meta.filter(m => (key.startsWith('og:') ? m.property : m.name) === key);
    if (matches.length !== 1 || matches[0].content !== escape(value)) failures.push(`${file}: incorrect or duplicate ${key}`);
  }
  const canonicals = [...head.matchAll(/<link\b[^>]*>/gi)].map(m => attributes(m[0])).filter(m => m.rel === 'canonical');
  if (canonicals.length !== 1 || canonicals[0].href !== escape(expected.canonical)) failures.push(`${file}: incorrect or duplicate canonical`);
  const discovery = [...head.matchAll(/<link\b[^>]*>/gi)].map(m => attributes(m[0]));
  if (discovery.filter(m => m.rel === 'describedby' && m.href === `${site.origin}/llms.txt`).length !== 1) failures.push(`${file}: missing agent index discovery`);
  if (file !== 'index.html' && discovery.filter(m => m.rel === 'alternate' && m.type === 'text/markdown' && m.href === expected.canonical + 'index.md').length !== 1) failures.push(`${file}: missing Markdown discovery`);
  const route = base + file.replaceAll('\\', '/').replace(/index\.html$/, '');
  for (const match of html.matchAll(/(?:href|src)="([^"#][^"]*)"|href="(#[^"]+)"/g)) {
    const href = match[1] || match[2];
    if (/^[a-z][\w+.-]*:|^\/\//i.test(href)) continue;
    const url = new URL(href, 'https://local.test' + route);
    if (!url.pathname.startsWith(base)) { failures.push(`${file}: link outside base ${href}`); continue; }
    let target = decodeURIComponent(url.pathname.slice(base.length));
    if (target.endsWith('/') || !target) target += 'index.html';
    const full = new URL(target, dist);
    if (!fs.existsSync(full)) { failures.push(`${file}: missing ${href}`); continue; }
    if (url.hash && target.endsWith('.html')) {
      const source = fs.readFileSync(full, 'utf8');
      if (!source.includes(`id="${decodeURIComponent(url.hash.slice(1))}"`)) failures.push(`${file}: missing anchor ${href}`);
    }
  }
}
const sitemapPath = new URL('sitemap.xml', dist);
if (!fs.existsSync(sitemapPath)) failures.push('Missing sitemap.xml');
else {
  const xml = fs.readFileSync(sitemapPath, 'utf8');
  const urls = [...xml.matchAll(/<loc>([^<]+)<\/loc>/g)].map(m => m[1]);
  const expected = [...byFile.values()].map(m => m.canonical).sort();
  if (JSON.stringify(urls.sort()) !== JSON.stringify(expected)) failures.push('Sitemap URLs do not match the public HTML pages');
  if (!xml.includes('xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"') || /<(?:lastmod|priority|changefreq)\b/.test(xml)) failures.push('Unexpected sitemap format or freshness fields');
}
const robotsPath = new URL('robots.txt', dist);
if (!fs.existsSync(robotsPath) || fs.readFileSync(robotsPath, 'utf8') !== `User-agent: *\nAllow: /\nSitemap: ${site.origin}/sitemap.xml\n`) failures.push('Robots policy differs from approved policy A');
const imagePath = new URL(new URL(site.image, site.origin).pathname.slice(1), dist);
if (!fs.existsSync(imagePath)) failures.push('Missing shared social preview image');
const marked = new Marked();
const markdownPages = new Map(pages.map(page => [markdownRoute(page), page]));
const agentFiles = ['llms.txt', ...pages.map(page => markdownRoute(page).slice(1))];
const indexLinks = [];
for (const file of agentFiles) {
  const full = new URL(file, dist);
  if (!fs.existsSync(full)) { failures.push(`Missing agent file: ${file}`); continue; }
  const source = fs.readFileSync(full, 'utf8');
  marked.walkTokens(marked.lexer(source), token => {
    if (token.type !== 'link' && token.type !== 'image') return;
    if (file === 'llms.txt') indexLinks.push(token.href);
    if (!/^[a-z][a-z\d+.-]*:|^\/\//i.test(token.href)) { failures.push(`${file}: relative Markdown destination ${token.href}`); return; }
    const url = new URL(token.href, site.origin);
    if (url.origin !== site.origin) return;
    if (!fs.existsSync(new URL(url.pathname.slice(1), dist))) failures.push(`${file}: missing Markdown destination ${token.href}`);
    const page = markdownPages.get(url.pathname);
    if (url.hash && page && !article(page.source).ids.has(decodeURIComponent(url.hash.slice(1)))) failures.push(`${file}: missing Markdown anchor ${token.href}`);
  });
}
if (JSON.stringify(indexLinks.sort()) !== JSON.stringify(pages.map(page => site.origin + markdownRoute(page)).sort())) failures.push('Agent index does not match the publication list');
if (failures.length) throw new Error(failures.join('\n'));
console.log(`Verified ${files.length} HTML pages and ${agentFiles.length} agent files: metadata, discovery, assets, links and anchors at ${base}`);
