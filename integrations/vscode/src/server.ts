import { ChildProcess, spawn } from 'node:child_process';
import * as http from 'node:http';
import * as fs from 'node:fs';
import * as path from 'node:path';
import * as vscode from 'vscode';

// The line cli.go prints once the listener is bound. Parsed rather than
// assumed because --port 0 lets the kernel choose, which is the default here:
// several VS Code windows each want their own server and a fixed port would
// have them fight over one.
const LISTENING_LINE = /SwarmViz listening on (http:\/\/\S+)/;

const STARTUP_TIMEOUT_MS = 20_000;

export interface RunningServer {
  // Origin the extension host can reach the server at, no trailing slash.
  origin: string;
  port: number;
  // True when this process is ours to stop; false when we attached to a
  // server the user started themselves.
  spawned: boolean;
}

export class ServerStartError extends Error {}

/**
 * Owns the swarmviz child process for one workspace folder.
 *
 * The extension deliberately does not reimplement any of the watching, diffing
 * or attribution: it runs the same binary the CLI does and renders the UI that
 * binary already serves.
 */
export class SwarmVizServer implements vscode.Disposable {
  private process: ChildProcess | undefined;
  private current: RunningServer | undefined;
  private exitListeners: Array<(code: number | null) => void> = [];

  constructor(private readonly output: vscode.OutputChannel) {}

  get running(): RunningServer | undefined {
    return this.current;
  }

  onExit(listener: (code: number | null) => void): void {
    this.exitListeners.push(listener);
  }

  /**
   * Returns a reachable server, starting one if needed. Settings may point at
   * an already-running server, in which case nothing is spawned.
   */
  async ensureStarted(folder: vscode.WorkspaceFolder): Promise<RunningServer> {
    if (this.current) return this.current;

    const config = vscode.workspace.getConfiguration('swarmviz', folder.uri);
    const attachTo = (config.get<string>('serverUrl') ?? '').trim();

    if (attachTo) {
      const origin = attachTo.replace(/\/+$/, '');
      await this.waitForHealth(origin);
      this.current = { origin, port: portOf(origin), spawned: false };
      this.output.appendLine(`Attached to an existing server at ${origin}`);
      return this.current;
    }

    return this.spawnServer(folder, config);
  }

  private async spawnServer(
    folder: vscode.WorkspaceFolder,
    config: vscode.WorkspaceConfiguration
  ): Promise<RunningServer> {
    if (folder.uri.scheme !== 'file') {
      throw new ServerStartError(
        `SwarmViz watches a directory on disk; this workspace folder is "${folder.uri.scheme}:".`
      );
    }

    const cwd = folder.uri.fsPath;
    const binary = resolveBinary(config.get<string>('binaryPath') ?? '', cwd);
    const args = ['--path', cwd, '--port', String(config.get<number>('port') ?? 0)];

    // Without this the webview's fetch of /api/projects is a cross-origin
    // request from an opaque vscode-webview:// document and the browser
    // discards the response.
    args.push('--allow-origin', 'vscode-webview://*');

    const projectsRoot = (config.get<string>('projectsRoot') ?? '').trim();
    if (projectsRoot) args.push('--projects-root', projectsRoot);

    args.push(...(config.get<string[]>('extraArgs') ?? []));

    this.output.appendLine(`$ ${binary} ${args.join(' ')}`);

    const child = spawn(binary, args, { cwd, env: process.env });
    this.process = child;

    const started = new Promise<RunningServer>((resolve, reject) => {
      let settled = false;
      let buffered = '';

      const timer = setTimeout(() => {
        if (settled) return;
        settled = true;
        child.kill();
        reject(
          new ServerStartError(
            `swarmviz did not report a listening address within ${STARTUP_TIMEOUT_MS / 1000}s. See the SwarmViz output channel.`
          )
        );
      }, STARTUP_TIMEOUT_MS);

      const consume = (chunk: Buffer) => {
        const text = chunk.toString();
        this.output.append(text);
        if (settled) return;

        buffered += text;
        const match = buffered.match(LISTENING_LINE);
        if (!match) return;

        settled = true;
        clearTimeout(timer);
        const origin = match[1].replace(/\/+$/, '');
        resolve({ origin, port: portOf(origin), spawned: true });
      };

      child.stdout?.on('data', consume);
      child.stderr?.on('data', consume);

      child.on('error', (err: NodeJS.ErrnoException) => {
        if (settled) return;
        settled = true;
        clearTimeout(timer);
        reject(
          new ServerStartError(
            err.code === 'ENOENT'
              ? `Could not run "${binary}". Build it with \`go build -o swarmviz .\` in the SwarmViz repository, or set swarmviz.binaryPath.`
              : `Failed to start ${binary}: ${err.message}`
          )
        );
      });

      child.on('exit', (code) => {
        if (!settled) {
          settled = true;
          clearTimeout(timer);
          reject(
            new ServerStartError(
              `swarmviz exited with code ${code} during startup. See the SwarmViz output channel.`
            )
          );
        }
        // An exit after startup means the server died under us; the panel
        // needs to know so it can stop pretending to be live.
        this.process = undefined;
        this.current = undefined;
        for (const listener of this.exitListeners) listener(code);
      });
    });

    this.current = await started;

    // The listening line is printed before the first request is served in
    // practice, but health-polling removes the race entirely.
    await this.waitForHealth(this.current.origin);

    this.output.appendLine(`Server ready at ${this.current.origin}`);
    return this.current;
  }

  private async waitForHealth(origin: string): Promise<void> {
    const deadline = Date.now() + STARTUP_TIMEOUT_MS;

    for (;;) {
      try {
        const body = await httpGet(`${origin}/api/health`);
        if (body.includes('"ok"')) return;
      } catch {
        // Not up yet.
      }

      if (Date.now() > deadline) {
        throw new ServerStartError(`No healthy response from ${origin}/api/health.`);
      }
      await delay(150);
    }
  }

  stop(): void {
    const child = this.process;
    this.process = undefined;
    this.current = undefined;
    if (!child) return;

    this.output.appendLine('Stopping server');
    // SIGTERM is what the CLI traps for its graceful shutdown, which is what
    // removes the shadow copy from /tmp.
    child.kill('SIGTERM');

    const forceKill = setTimeout(() => child.kill('SIGKILL'), 5_000);
    child.on('exit', () => clearTimeout(forceKill));
  }

  dispose(): void {
    this.stop();
  }
}

function portOf(origin: string): number {
  const port = Number.parseInt(new URL(origin).port, 10);
  return Number.isFinite(port) ? port : 80;
}

function resolveBinary(configured: string, cwd: string): string {
  if (configured.trim()) {
    return configured.trim().replace(/^~(?=\/|$)/, process.env.HOME ?? '~');
  }

  // A freshly built checkout keeps the binary next to the sources, which is
  // the common case when hacking on SwarmViz itself.
  const local = path.join(cwd, 'swarmviz');
  try {
    fs.accessSync(local, fs.constants.X_OK);
    return local;
  } catch {
    // Fall through to PATH resolution by spawn().
  }

  return 'swarmviz';
}

function httpGet(url: string): Promise<string> {
  return new Promise((resolve, reject) => {
    const request = http.get(url, { timeout: 2_000 }, (response) => {
      const chunks: Buffer[] = [];
      response.on('data', (chunk: Buffer) => chunks.push(chunk));
      response.on('end', () => {
        const body = Buffer.concat(chunks).toString();
        if (response.statusCode && response.statusCode >= 400) {
          reject(new Error(`HTTP ${response.statusCode} for ${url}`));
          return;
        }
        resolve(body);
      });
    });

    request.on('timeout', () => request.destroy(new Error(`Timed out requesting ${url}`)));
    request.on('error', reject);
  });
}

export { httpGet };

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
