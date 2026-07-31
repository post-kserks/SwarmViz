/**
 * Rewrites the index.html served by the swarmviz binary so it can run inside a
 * VS Code webview.
 *
 * The page is taken from the running server rather than bundled with the
 * extension, so the UI can never drift out of step with the backend it talks
 * to. Three things stand in the way of showing it as-is:
 *
 *   - asset URLs are root-relative and would resolve against the
 *     vscode-webview:// document instead of the server;
 *   - Vite marks the bundle `crossorigin`, which turns the load into a CORS
 *     request and would demand Access-Control-Allow-Origin on every asset;
 *   - a webview enforces a CSP, and inline script needs a nonce.
 *
 * This module deliberately imports nothing from vscode, so it stays testable
 * outside the editor.
 */
export function buildWebviewHtml(indexHtml: string, origin: string, nonce: string): string {
  const csp = [
    `default-src 'none'`,
    `img-src ${origin} data: blob:`,
    `font-src ${origin} data:`,
    `style-src ${origin} 'unsafe-inline'`,
    `script-src ${origin} 'nonce-${nonce}'`,
    `connect-src ${origin} ${origin.replace(/^http:/, 'ws:')}`,
  ].join('; ');

  const bootstrap = [
    `<meta http-equiv="Content-Security-Policy" content="${csp}">`,
    `<script nonce="${nonce}">window.__SWARMVIZ_ORIGIN__ = ${JSON.stringify(origin)};</script>`,
  ].join('\n    ');

  return (
    indexHtml
      .replace(/\s+crossorigin(=(?:"[^"]*"|'[^']*'|[^\s>]+))?/g, '')
      .replace(/\b(src|href)="\/(?!\/)/g, `$1="${origin}/`)
      .replace(/<script\b(?![^>]*\bnonce=)/g, `<script nonce="${nonce}"`)
      // Injected last: the bootstrap already carries its own nonce and must
      // not be touched by the pass above.
      .replace(/<head>/i, `<head>\n    ${bootstrap}`)
  );
}

export function makeNonce(): string {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
  let nonce = '';
  for (let i = 0; i < 32; i++) {
    nonce += alphabet.charAt(Math.floor(Math.random() * alphabet.length));
  }
  return nonce;
}
