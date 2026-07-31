import * as path from 'node:path';
import * as vscode from 'vscode';
import { buildWebviewHtml, makeNonce } from './html';
import { RunningServer, httpGet } from './server';

// Message the UI posts through acquireVsCodeApi() — see
// frontend/src/utils/host.ts, which is the other half of this contract.
interface RevealInEditorMessage {
  type: 'revealInEditor';
  file: string;
  projectPath?: string;
  line?: number;
}

type WebviewMessage = RevealInEditorMessage;

/**
 * The swarm view.
 *
 * The page is not rebuilt here: the extension fetches the very index.html the
 * running binary serves and rewrites its asset URLs to absolute ones, so the
 * UI always matches the binary rather than a copy bundled at package time.
 */
export class SwarmVizPanel {
  private static current: SwarmVizPanel | undefined;

  private readonly disposables: vscode.Disposable[] = [];

  private constructor(
    private readonly panel: vscode.WebviewPanel,
    private readonly server: RunningServer,
    private readonly folder: vscode.WorkspaceFolder,
    private readonly output: vscode.OutputChannel,
    private readonly onDisposed: () => void
  ) {
    this.panel.onDidDispose(() => this.dispose(), null, this.disposables);
    this.panel.webview.onDidReceiveMessage(
      (message: WebviewMessage) => this.handleMessage(message),
      null,
      this.disposables
    );
  }

  static async show(
    server: RunningServer,
    folder: vscode.WorkspaceFolder,
    output: vscode.OutputChannel,
    onDisposed: () => void
  ): Promise<void> {
    if (SwarmVizPanel.current) {
      SwarmVizPanel.current.panel.reveal(vscode.ViewColumn.Active);
      return;
    }

    const panel = vscode.window.createWebviewPanel(
      'swarmviz.view',
      `SwarmViz — ${folder.name}`,
      vscode.ViewColumn.Active,
      {
        enableScripts: true,
        // The graph, stream and chart all hold live state that a reload would
        // throw away; the socket would have to replay from scratch.
        retainContextWhenHidden: true,
        // Nothing is loaded from the extension bundle — every asset comes
        // from the server.
        localResourceRoots: [],
        // How a webview reaches a local server. Direct localhost only works
        // when the editor UI and the server are the same machine; over
        // Remote-SSH the extension host is remote and this mapping is what
        // tunnels the port to the UI side.
        portMapping: [{ webviewPort: server.port, extensionHostPort: server.port }],
      }
    );

    SwarmVizPanel.current = new SwarmVizPanel(panel, server, folder, output, onDisposed);
    await SwarmVizPanel.current.render();
  }

  static get isOpen(): boolean {
    return SwarmVizPanel.current !== undefined;
  }

  static closeIfOpen(): void {
    SwarmVizPanel.current?.panel.dispose();
  }

  private async render(): Promise<void> {
    // Fetched over loopback from the extension host, which is not subject to
    // the webview's CSP or origin rules.
    const indexHtml = await httpGet(`${this.server.origin}/`);

    // The document is served to the webview from a mapped port, so the UI
    // must address the backend as localhost:<port> rather than by whatever
    // host the extension host used.
    const webviewOrigin = `http://localhost:${this.server.port}`;

    this.panel.webview.html = buildWebviewHtml(indexHtml, webviewOrigin, makeNonce());
  }

  private async handleMessage(message: WebviewMessage): Promise<void> {
    if (message?.type !== 'revealInEditor') return;

    const root = message.projectPath?.trim() ? message.projectPath : this.folder.uri.fsPath;
    const target = path.isAbsolute(message.file) ? message.file : path.join(root, message.file);

    try {
      const document = await vscode.workspace.openTextDocument(vscode.Uri.file(target));
      const editor = await vscode.window.showTextDocument(document, {
        preview: true,
        // Beside the panel, so opening a file does not hide the view that
        // pointed at it.
        viewColumn: vscode.ViewColumn.Beside,
      });

      if (message.line && message.line > 0) {
        // The UI counts lines from 1, the API from 0.
        const line = Math.min(message.line - 1, document.lineCount - 1);
        const position = new vscode.Position(line, 0);
        editor.selection = new vscode.Selection(position, position);
        editor.revealRange(
          new vscode.Range(position, position),
          vscode.TextEditorRevealType.InCenterIfOutsideViewport
        );
      }
    } catch (err) {
      // Deleted, ignored, or living in a repository this window does not have
      // open — worth a line in the log, not a modal.
      this.output.appendLine(`Could not open ${target}: ${String(err)}`);
      void vscode.window.showWarningMessage(`SwarmViz: could not open ${message.file}`);
    }
  }

  dispose(): void {
    SwarmVizPanel.current = undefined;
    for (const disposable of this.disposables.splice(0)) disposable.dispose();
    this.onDisposed();
  }
}

