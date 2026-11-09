import assert from 'node:assert/strict';
import test from 'node:test';
import { article, basePath, localTarget, pages, renderPage, revision, siteLink } from './documentation.mjs';

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
