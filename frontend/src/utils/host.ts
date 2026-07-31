// Bridge between the UI and whatever is hosting it.
//
// Served by the swarmviz binary, the host is a plain browser tab: the backend
// lives at window.location and there is no editor to talk to. Inside the VS
// Code extension the same bundle runs in a webview document whose origin is an
// opaque vscode-webview://<uuid>, so neither the API nor the WebSocket can be
// derived from window.location — the extension injects the real backend origin
// into the page before the bundle boots, and exposes a message channel back to
// the editor.
//
// Both paths go through the helpers here so nothing else in the UI has to know
// which one it is running under.

interface VsCodeApi {
  postMessage(message: unknown): void;
}

declare global {
  interface Window {
    // Injected by the VS Code extension into the webview HTML.
    __SWARMVIZ_ORIGIN__?: string;
    acquireVsCodeApi?: () => VsCodeApi;
  }
}

// Trailing slashes would double up when concatenated with a path.
function trimTrailingSlash(value: string): string {
  return value.replace(/\/+$/, '');
}

// The origin the swarmviz backend is reachable at, without a trailing slash.
export function backendOrigin(): string {
  const injected = window.__SWARMVIZ_ORIGIN__;
  if (injected) return trimTrailingSlash(injected);
  return trimTrailingSlash(window.location.origin);
}

// Absolute URL for an API path such as "/api/projects".
export function apiUrl(path: string): string {
  return `${backendOrigin()}${path}`;
}

// Absolute ws:// URL for a project's socket. basePath is "" for the root
// project and "/p/{id}" for a discovered one.
export function socketUrl(basePath: string, query = ''): string {
  const origin = backendOrigin();
  const scheme = origin.startsWith('https:') ? 'wss:' : 'ws:';
  return `${scheme}//${origin.replace(/^https?:\/\//, '')}${basePath}/ws${query}`;
}

// acquireVsCodeApi may only be called once per webview, so the handle is
// resolved lazily and kept.
let vscodeApi: VsCodeApi | null | undefined;

function host(): VsCodeApi | null {
  if (vscodeApi === undefined) {
    vscodeApi = typeof window.acquireVsCodeApi === 'function' ? window.acquireVsCodeApi() : null;
  }
  return vscodeApi;
}

// True when the UI is embedded in an editor that can act on our messages.
// Controls affordances that would be dead ends in a plain browser tab.
export function isEmbedded(): boolean {
  return host() !== null;
}

export interface RevealRequest {
  // Repository-relative path, exactly as it appears in the event stream.
  file: string;
  // Absolute path of the repository the file belongs to. Empty when the
  // backend does not report one, in which case the host falls back to the
  // workspace folder it started the server for.
  projectPath?: string;
  // 1-based line to put the cursor on.
  line?: number;
}

// Asks the host editor to open a file. A no-op in a plain browser tab.
export function revealInEditor(request: RevealRequest): void {
  const api = host();
  if (!api) return;
  api.postMessage({ type: 'revealInEditor', ...request });
}

// First line touched by a unified-diff hunk, so a click lands on the change
// rather than the top of the file. Hunk headers look like
// "@@ -12,7 +14,9 @@ func Foo()"; the "+14" is the new-file start.
export function hunkStartLine(hunk?: string): number | undefined {
  if (!hunk) return undefined;
  const match = hunk.match(/^@@ -\d+(?:,\d+)? \+(\d+)/m);
  if (!match) return undefined;
  const line = Number.parseInt(match[1], 10);
  return Number.isFinite(line) && line > 0 ? line : undefined;
}
