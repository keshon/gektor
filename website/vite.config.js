import { defineConfig } from 'vite';
import { documentationPlugin } from './scripts/documentation.mjs';

export default defineConfig({
  plugins: [documentationPlugin()],
  // The Pages workflow supplies its actual base path, including custom domains.
  base: process.env.SITE_BASE_PATH || '/',
});
