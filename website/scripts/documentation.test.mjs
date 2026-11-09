import assert from 'node:assert/strict';
import test from 'node:test';
import fs from 'node:fs';
import path from 'node:path';
import { Marked } from 'marked';
import { article, basePath, localTarget, markdownRoute, pages, publishMarkdown, renderLLMs, renderMarkdown, renderPage, revision, root, siteLink } from './documentation.mjs';
import { canonicalURL, homepage, injectHomepageMetadata, pageMetadata, renderMetadata, renderRobots, renderSitemap, site } from './site-metadata.mjs';

test('published links honor the hosting base; unpublished sources stay pinned', () => {
  assert.equal(basePath('/gektor'), '/gektor/');
  assert.equal(siteLink('docs/start.md', 'guide/agent.md#stop-retry-and-continue', '/gektor/'), '/gektor/docs/guides/agent/#stop-retry-and-continue');
  assert.equal(siteLink('docs/start.md', '../cmd/gektor/options.go'), `https://github.com/keshon/gektor/blob/${revision}/cmd/gektor/options.go`);
  assert.equal(siteLink('docs/README.md', 'images/gektor-demo.webp', '/gektor/'), '/gektor/doc-assets/docs/images/gektor-demo.webp');
  assert.equal(siteLink('docs/start.md', 'https://example.com/#hello'), 'https://example.com/#hello');
  assert.throws(() => localTarget('README.md', '../outside.md'), /escapes repository/);
});

test('every published article has a full document, source link and working outline anchors', () => {
  for (const page of pages) {
    const html = renderPage(page, '/gektor/');
    assert.match(html, /^<!doctype html>/);
    assert.ok(html.includes(`/blob/${revision}/${page.source}`));
    assert.ok(html.includes('href="/gektor/docs.css"'));
    const { ids, headings } = article(page.source);
    for (const heading of headings) assert.ok(ids.has(heading.id));
  }
});

test('production metadata stays independent of the preview base and navigation label', () => {
  const page = pages.find(p => p.source === 'docs/guide/models.md');
  const metadata = pageMetadata(page);
  assert.equal(metadata.title, 'Connect llama.cpp, LM Studio or ChatGPT to GEKTOR');
  assert.equal(page.title, 'Connect a model');
  assert.equal(metadata.canonical, `${site.origin}/docs/guides/models/`);
  const html = renderPage(page, '/gektor/');
  assert.ok(html.includes(`rel="canonical" href="${metadata.canonical}"`));
  assert.ok(html.includes('href="/gektor/docs.css"'));
  assert.ok(html.includes('href="/gektor/docs/guides/models/"'));
  assert.ok(!metadata.canonical.includes('/gektor/'));
});

test('metadata requires real titles and descriptions and escapes text', () => {
  const page = { route: '/docs/test/', title: 'A & B <guide>', description: 'Use "quotes", <tags> & apostrophes\'.' };
  const html = renderMetadata(page);
  assert.ok(html.includes('<title>A &amp; B &lt;guide&gt; · GEKTOR</title>'));
  assert.ok(html.includes('content="Use &quot;quotes&quot;, &lt;tags&gt; &amp; apostrophes&#39;."'));
  for (const description of ['', '  ', undefined, 123]) {
    assert.throws(() => pageMetadata({ ...page, description }), /Missing description/);
  }
  assert.throws(() => pageMetadata({ ...page, seoTitle: ' ' }), /Missing title/);
  assert.throws(() => pageMetadata({ ...page, title: undefined }), /Missing title/);
  assert.throws(() => pageMetadata({ ...page, title: ' ' }), /Missing title/);
  for (const route of ['https://other.test/', '//other.test/', '/docs/../', '/index.html', '/docs/?query=1', '/docs/#anchor']) {
    assert.throws(() => canonicalURL(route), /Invalid canonical route/);
  }
});

test('homepage metadata has one injection point and all public descriptions are distinct', () => {
  const html = injectHomepageMetadata('<head><!-- site-metadata --></head>');
  assert.equal([...html.matchAll(/<title>/g)].length, 1);
  assert.ok(html.includes(`rel="canonical" href="${site.origin}/"`));
  assert.ok(!html.includes('site-metadata'));
  assert.throws(() => injectHomepageMetadata('<head></head>'), /exactly one/);
  assert.throws(() => injectHomepageMetadata('<!-- site-metadata --><!-- site-metadata -->'), /exactly one/);
  const metadata = [homepage, ...pages].map(pageMetadata);
  for (const key of ['title', 'description', 'canonical']) {
    assert.equal(new Set(metadata.map(m => m[key])).size, metadata.length, `Duplicate ${key}`);
  }
});

test('sitemap follows only the publication list; crawler policy retains public access', () => {
  const xml = renderSitemap(pages);
  assert.deepEqual([...xml.matchAll(/<loc>([^<]+)<\/loc>/g)].map(m => m[1]), [homepage, ...pages].map(p => canonicalURL(p.route)));
  assert.ok(xml.includes('xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"'));
  assert.doesNotMatch(xml, /lastmod|changefreq|priority|localhost|\/internal\/|\/skills\//);
  assert.throws(() => renderSitemap([...pages, pages[0]]), /Duplicate sitemap/);
  assert.equal(renderRobots(), `User-agent: *\nAllow: /\nSitemap: ${site.origin}/sitemap.xml\n`);
});

test('Markdown publication preserves article content and code while resolving destinations', () => {
  const marked = new Marked();
  const content = source => marked.parse(source).replace(/(?:href|src)="[^"]*"/g, 'destination=""');
  for (const page of pages) {
    const source = fs.readFileSync(path.join(root, page.source), 'utf8');
    const markdown = renderMarkdown(page);
    assert.equal(content(markdown), content(source), page.source);
    marked.walkTokens(marked.lexer(markdown), token => {
      if (token.type === 'link' || token.type === 'image') {
        assert.match(token.href, /^[a-z][a-z\d+.-]*:|^\/\//i, `${page.source}: ${token.href}`);
      }
    });
  }
  const source = '[Agent](guide/agent.md)\n\n`[Agent](guide/agent.md)`\n\n```md\n[Agent](guide/agent.md)\n```\n\n[Agent][ref]\n\n[ref]: guide/agent.md\n';
  const markdown = publishMarkdown('docs/start.md', source);
  assert.equal(content(markdown), content(source));
  assert.ok(markdown.includes('`[Agent](guide/agent.md)`'));
  assert.ok(markdown.includes('```md\n[Agent](guide/agent.md)\n```'));
  assert.equal([...markdown.matchAll(/https:\/\/gektor.keshon.ru\/docs\/guides\/agent\/index.md/g)].length, 2);
});

test('agent index and HTML discovery use the existing publication boundary', () => {
  const index = renderLLMs();
  const links = [];
  const marked = new Marked();
  marked.walkTokens(marked.lexer(index), token => { if (token.type === 'link') links.push(token.href); });
  assert.deepEqual(links, pages.map(page => site.origin + markdownRoute(page)));
  assert.doesNotMatch(index, /\/internal\/|\/skills\/|localhost/);
  for (const page of pages) {
    const html = renderPage(page);
    assert.ok(html.includes(`rel="alternate" type="text/markdown" href="${site.origin}${markdownRoute(page)}"`));
    assert.ok(html.includes(`rel="describedby" href="${site.origin}/llms.txt"`));
  }
});
