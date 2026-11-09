import { copyFile, mkdir, readFile, writeFile } from 'node:fs/promises';
import { createHash } from 'node:crypto';

// The source is the actual terminal screenshot, already exported as WebP.
// Copy its bytes unchanged so asset preparation cannot restore an older image.
const source = new URL('../../docs/images/gektor-demo.webp', import.meta.url);
const target = new URL('../public/images/gektor-terminal.webp', import.meta.url);
const bytes = await readFile(source);
if (bytes.toString('ascii', 0, 4) !== 'RIFF' || bytes.toString('ascii', 8, 12) !== 'WEBP') {
  throw new Error('docs/images/gektor-demo.webp must be a WebP image');
}
await mkdir(new URL('../public/images/', import.meta.url), { recursive: true });
await copyFile(source, target);

const version = createHash('sha256').update(bytes).digest('hex').slice(0, 12);
const index = new URL('../index.html', import.meta.url);
const html = await readFile(index, 'utf8');
const updated = html.replace(/images\/gektor-terminal\.webp(?:\?v=[a-f0-9]+)?/g,
  `images/gektor-terminal.webp?v=${version}`);
if (updated !== html) await writeFile(index, updated);
console.log(`Terminal screenshot copied unchanged (${bytes.length} bytes).`);
