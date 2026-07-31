import * as vscode from 'vscode';
import { SwarmVizPanel } from './panel';
import { ServerStartError, SwarmVizServer } from './server';

let output: vscode.OutputChannel;
let server: SwarmVizServer;
let status: vscode.StatusBarItem;

export function activate(context: vscode.ExtensionContext): void {
  output = vscode.window.createOutputChannel('SwarmViz');
  server = new SwarmVizServer(output);

  status = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Right, 100);
  status.command = 'swarmviz.open';
  updateStatus();

  server.onExit((code) => {
    updateStatus();
    if (SwarmVizPanel.isOpen) {
      void vscode.window
        .showErrorMessage(`SwarmViz server exited (code ${code}).`, 'Show Log')
        .then((choice) => {
          if (choice === 'Show Log') output.show();
        });
      SwarmVizPanel.closeIfOpen();
    }
  });

  context.subscriptions.push(
    output,
    server,
    status,
    vscode.commands.registerCommand('swarmviz.open', openView),
    vscode.commands.registerCommand('swarmviz.restart', restartServer),
    vscode.commands.registerCommand('swarmviz.stop', stopServer),
    vscode.commands.registerCommand('swarmviz.showLog', () => output.show())
  );
}

export function deactivate(): void {
  server?.stop();
}

async function openView(): Promise<void> {
  const folder = await pickFolder();
  if (!folder) return;

  try {
    const running = await vscode.window.withProgress(
      { location: vscode.ProgressLocation.Window, title: 'Starting SwarmViz…' },
      () => server.ensureStarted(folder)
    );

    updateStatus();

    await SwarmVizPanel.show(running, folder, output, () => {
      const stopOnClose = vscode.workspace
        .getConfiguration('swarmviz', folder.uri)
        .get<boolean>('stopServerOnClose', true);

      // Only ours to stop — an attached server belongs to whoever started it.
      if (stopOnClose && server.running?.spawned) {
        server.stop();
      }
      updateStatus();
    });
  } catch (err) {
    const message = err instanceof ServerStartError ? err.message : String(err);
    output.appendLine(message);
    const choice = await vscode.window.showErrorMessage(`SwarmViz: ${message}`, 'Show Log');
    if (choice === 'Show Log') output.show();
    updateStatus();
  }
}

async function restartServer(): Promise<void> {
  SwarmVizPanel.closeIfOpen();
  server.stop();
  updateStatus();
  await openView();
}

function stopServer(): void {
  SwarmVizPanel.closeIfOpen();
  server.stop();
  updateStatus();
}

/**
 * Which repository to watch. One folder needs no question; several do, since
 * a server watches exactly one root (plus whatever --projects-root discovers).
 */
async function pickFolder(): Promise<vscode.WorkspaceFolder | undefined> {
  const folders = vscode.workspace.workspaceFolders ?? [];

  if (folders.length === 0) {
    void vscode.window.showErrorMessage('SwarmViz: open a folder first — there is no repository to watch.');
    return undefined;
  }

  if (folders.length === 1) return folders[0];

  // Already running: keep watching whatever was chosen rather than asking again.
  if (server.running) {
    const active = vscode.window.activeTextEditor?.document.uri;
    const current = active ? vscode.workspace.getWorkspaceFolder(active) : undefined;
    if (current) return current;
  }

  return vscode.window.showWorkspaceFolderPick({ placeHolder: 'Which repository should SwarmViz watch?' });
}

function updateStatus(): void {
  const running = server.running;
  status.text = running ? '$(pulse) SwarmViz' : '$(circle-outline) SwarmViz';
  status.tooltip = running
    ? `SwarmViz is watching — ${running.origin}`
    : 'SwarmViz is stopped. Click to open the swarm view.';
  status.show();
}
