import assert from 'node:assert/strict';
import { test } from 'node:test';
// Explicit extension: these tests run straight off the TypeScript sources
// under `node --test`, whose resolver does not guess one.
import { buildWebviewHtml, makeNonce } from './html.ts';

const ORIGIN = 'http://localhost:41234';
const NONCE = 'testnonce';

// The shape Vite emits, which is what the binary serves.
const INDEX_HTML = `<!DOCTYPE html>
<html lang="en" class="dark">
  <head>
    <meta charset="UTF-8" />
    <link rel="icon" type="image/svg+xml" href="/favicon.svg" />
    <script type="module" crossorigin src="/assets/index-CEJthQ_P.js"></script>
    <link rel="stylesheet" crossorigin href="/assets/index-BFh7aWqf.css">
  </head>
  <body>
    <div id="root"></div>
  </body>
</html>`;

test('asset URLs are absolutised against the backend origin', () => {
  const html = buildWebviewHtml(INDEX_HTML, ORIGIN, NONCE);

  assert.match(html, new RegExp(`src="${ORIGIN}/assets/index-CEJthQ_P\\.js"`));
  assert.match(html, new RegExp(`href="${ORIGIN}/assets/index-BFh7aWqf\\.css"`));
  assert.match(html, new RegExp(`href="${ORIGIN}/favicon\\.svg"`));
  assert.ok(!/(src|href)="\/(?!\/)/.test(html), 'no root-relative URL may survive');
});

test('crossorigin is stripped so assets load without CORS headers', () => {
  const html = buildWebviewHtml(INDEX_HTML, ORIGIN, NONCE);
  assert.ok(!html.includes('crossorigin'), `crossorigin survived:\n${html}`);
});

test('protocol-relative URLs are left alone', () => {
  const html = buildWebviewHtml('<head></head><body><img src="//cdn.example/x.png"></body>', ORIGIN, NONCE);
  assert.ok(html.includes('src="//cdn.example/x.png"'));
});

test('every script tag carries the nonce exactly once', () => {
  const html = buildWebviewHtml(INDEX_HTML, ORIGIN, NONCE);

  const scriptTags = html.match(/<script\b[^>]*>/g) ?? [];
  assert.ok(scriptTags.length >= 2, 'expected the bundle script plus the injected bootstrap');
  for (const tag of scriptTags) {
    const nonces = tag.match(/nonce=/g) ?? [];
    assert.equal(nonces.length, 1, `expected one nonce in ${tag}`);
    assert.ok(tag.includes(`nonce="${NONCE}"`), `wrong nonce in ${tag}`);
  }
});

test('the backend origin is injected before the bundle runs', () => {
  const html = buildWebviewHtml(INDEX_HTML, ORIGIN, NONCE);

  const bootstrapAt = html.indexOf('__SWARMVIZ_ORIGIN__');
  const bundleAt = html.indexOf('/assets/index-CEJthQ_P.js');
  assert.ok(bootstrapAt > -1, 'origin bootstrap is missing');
  assert.ok(bootstrapAt < bundleAt, 'the bootstrap must come before the bundle');
  assert.ok(html.includes(`window.__SWARMVIZ_ORIGIN__ = "${ORIGIN}"`));
});

test('the CSP allows the backend over http and ws, and nothing else by default', () => {
  const html = buildWebviewHtml(INDEX_HTML, ORIGIN, NONCE);

  const csp = html.match(/<meta http-equiv="Content-Security-Policy" content="([^"]+)">/)?.[1];
  assert.ok(csp, 'CSP meta tag is missing');
  assert.ok(csp.includes(`default-src 'none'`));
  assert.ok(csp.includes(`connect-src ${ORIGIN} ws://localhost:41234`));
  assert.ok(csp.includes(`script-src ${ORIGIN} 'nonce-${NONCE}'`));
});

test('nonces are random and long enough to be worth having', () => {
  const first = makeNonce();
  const second = makeNonce();

  assert.equal(first.length, 32);
  assert.notEqual(first, second);
  assert.match(first, /^[A-Za-z0-9]+$/);
});
