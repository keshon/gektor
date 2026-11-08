import { defineConfig } from 'vite';

export default defineConfig({
  // The Pages workflow supplies its actual base path, including custom domains.
  base: process.env.SITE_BASE_PATH || '/',
});
