// Production identity is independent of Vite's local/project preview base path.
export const site = {
  origin: 'https://gektor.keshon.ru',
  name: 'GEKTOR',
  image: '/images/gektor-terminal.webp',
  imageAlt: 'GEKTOR terminal showing an Agent conversation',
};

export const homepage = {
  route: '/',
  seoTitle: 'GEKTOR — A coding agent and workflow runner',
  description: 'GEKTOR is a terminal app for working on code with an AI agent and running saved workflows. For Windows and Linux.',
};

export const escape = text => String(text).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]);

export function canonicalURL(route) {
  if (!/^\/(?:[a-z0-9-]+\/)*$/.test(route)) throw new Error(`Invalid canonical route: ${route}`);
  return new URL(route, site.origin).href;
}

export function pageMetadata(page) {
  const title = page.seoTitle ?? (typeof page.title === 'string' && page.title.trim() ? `${page.title.trim()} · ${site.name}` : '');
  if (typeof title !== 'string' || !title.trim()) throw new Error(`Missing title: ${page.route}`);
  if (typeof page.description !== 'string' || !page.description.trim()) throw new Error(`Missing description: ${page.route}`);
  return {
    title: title.trim(), description: page.description.trim(),
    canonical: canonicalURL(page.route),
    image: new URL(site.image, site.origin).href,
  };
}

export function renderMetadata(page) {
  const { title, description, canonical, image } = pageMetadata(page);
  return `<title>${escape(title)}</title>
<meta name="description" content="${escape(description)}">
<link rel="canonical" href="${escape(canonical)}">
<link rel="describedby" href="${site.origin}/llms.txt">${page.route === '/' ? '' : `\n<link rel="alternate" type="text/markdown" href="${canonical}index.md">`}
<meta property="og:site_name" content="${escape(site.name)}">
<meta property="og:title" content="${escape(title)}">
<meta property="og:description" content="${escape(description)}">
<meta property="og:type" content="website">
<meta property="og:url" content="${escape(canonical)}">
<meta property="og:image" content="${escape(image)}">
<meta property="og:image:alt" content="${escape(site.imageAlt)}">
<meta name="twitter:card" content="summary_large_image">
<meta name="twitter:title" content="${escape(title)}">
<meta name="twitter:description" content="${escape(description)}">
<meta name="twitter:image" content="${escape(image)}">
<meta name="twitter:image:alt" content="${escape(site.imageAlt)}">`;
}

export function renderSitemap(pages) {
  const urls = [homepage, ...pages].map(page => canonicalURL(page.route));
  if (new Set(urls).size !== urls.length) throw new Error('Duplicate sitemap URL');
  return `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
${urls.map(url => `  <url><loc>${escape(url)}</loc></url>`).join('\n')}
</urlset>\n`;
}

// Policy A: retain public crawling, with no new training-specific restrictions.
export function renderRobots() {
  return `User-agent: *\nAllow: /\nSitemap: ${site.origin}/sitemap.xml\n`;
}

export function injectHomepageMetadata(html) {
  const marker = '<!-- site-metadata -->';
  if (html.split(marker).length !== 2) throw new Error('Homepage must contain exactly one site-metadata marker');
  return html.replace(marker, () => renderMetadata(homepage));
}
