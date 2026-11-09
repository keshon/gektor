import fs from 'node:fs';
import { basePath, pages } from './documentation.mjs';

const base = basePath(process.env.SITE_BASE_PATH || '/');
const dist = new URL('../dist/', import.meta.url);
const files = fs.readdirSync(dist, { recursive: true }).map(p => p.replaceAll('\\', '/')).filter(p => p.endsWith('.html'));
const failures = [];
for (const page of pages) if (!files.includes(page.route.slice(1) + 'index.html')) failures.push(`Missing route: ${page.route}`);
for (const file of files) {
  const html = fs.readFileSync(new URL(file.replaceAll('\\', '/'), dist), 'utf8');
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
if (failures.length) throw new Error(failures.join('\n'));
console.log(`Verified ${files.length} built HTML routes, assets, links and anchors at ${base}`);
