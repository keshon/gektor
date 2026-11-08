import sharp from 'sharp';
import { mkdir } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';

await mkdir(new URL('../public/images/', import.meta.url), { recursive: true });
for (const [source, name] of [
  ['city-dusk-source.png', 'city-dusk-clear-sky'],
  ['city-dusk-mobile-source.png', 'city-dusk-mobile'],
]) {
  // Export the approved desktop and portrait compositions at their native sizes.
  // No sharpening or grain is added; high-quality WebP keeps the sky smooth.
  await sharp(fileURLToPath(new URL(`../design/sources/${source}`, import.meta.url)))
    .webp({ quality: 94, effort: 6, smartSubsample: true })
    .toFile(fileURLToPath(new URL(`../public/images/${name}.webp`, import.meta.url)));
}
