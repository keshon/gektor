import fs from 'node:fs';
import path from 'node:path';
import { execFileSync } from 'node:child_process';
import { article, localTarget, pages, root } from './documentation.mjs';

const tracked = execFileSync('git', ['ls-files', '--cached', '--others', '--exclude-standard'], { cwd: root, encoding: 'utf8' }).trim().split('\n');
const sources = tracked.filter(p => p.endsWith('.md') && !p.startsWith('docs/internal/') && (
  !p.includes('/') || p.startsWith('docs/') || p.startsWith('skills/') ||
  p.startsWith('examples/workflows/') || ['demos/README.md', 'eval/README.md', 'scripts/README.md', 'website/README.md'].includes(p)
));
const parsed = new Map();
const read = source => {
  if (!parsed.has(source)) parsed.set(source, article(source));
  return parsed.get(source);
};
const failures = [];
for (const source of sources) {
  for (const href of read(source).links) {
    const target = localTarget(source, href);
    if (!target) continue;
    const fullPath = path.join(root, target.file);
    if (!fs.existsSync(fullPath)) failures.push(`${source}: missing path ${href}`);
    else if (target.fragment && target.file.endsWith('.md') && !read(target.file).ids.has(target.fragment)) {
      failures.push(`${source}: missing anchor ${href}`);
    }
  }
}
for (const page of pages) if (!sources.includes(page.source)) failures.push(`Unreviewed publication source: ${page.source}`);
if (failures.length) throw new Error(failures.join('\n'));
console.log(`Checked local paths and Markdown anchors in ${sources.length} files; ${pages.length} published articles.`);
