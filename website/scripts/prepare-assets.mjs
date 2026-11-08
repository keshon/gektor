import sharp from 'sharp';
import { mkdir } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';

await mkdir(new URL('../public/images/', import.meta.url), { recursive: true });
for (const [source, name, quality] of [
  ['city-dusk-source.png', 'city-dusk', 84],
  ['city-dusk-mobile-source.png', 'city-dusk-mobile', 94],
]) {
  // Export the approved desktop and portrait compositions at their native sizes.
  // No sharpening or grain is added. Quality is tuned for each composition.
  await sharp(fileURLToPath(new URL(`../design/sources/${source}`, import.meta.url)))
    .webp({ quality, effort: 6, smartSubsample: true })
    .toFile(fileURLToPath(new URL(`../public/images/${name}.webp`, import.meta.url)));
}
