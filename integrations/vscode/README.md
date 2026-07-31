# SwarmViz for VS Code

Runs SwarmViz inside the editor: the agent graph, the attributed diff stream,
the workspace tree and the LOC chart in a panel next to your code, watching the
repository the window has open.

The extension does not reimplement any of SwarmViz. It starts the same
`swarmviz` binary the CLI does and renders the UI that binary serves, so
watching, diffing and attribution behave exactly as they do in a browser tab.

## What the editor adds

- **Click a file, land on the change.** File names in the diff stream open in
  the editor at the first line of the hunk; selecting a file in the workspace
  tree opens it. In a browser tab these are plain text, since there would be
  nothing to open them in.
- **A server per window.** With `swarmviz.port` left at `0` the kernel assigns
  a free port, so several windows can each watch their own repository without
  colliding on 8942.
- **Lifecycle handled.** The server starts when the view opens, stops when it
  closes (see `swarmviz.stopServerOnClose`), and its output goes to the
  *SwarmViz* output channel.

## Requirements

A `swarmviz` binary. The extension looks at `swarmviz.binaryPath`, then
`./swarmviz` in the workspace folder, then `PATH`. To build one from this
repository:

```bash
npm --prefix frontend ci && npm --prefix frontend run build   # the UI must exist before the binary embeds it
go build -o swarmviz .
```

## Commands

| Command | What it does |
| --- | --- |
| `SwarmViz: Open Swarm View` | Starts the server if needed and opens the panel |
| `SwarmViz: Restart Server` | Stops the server, starts a fresh one, reopens the panel |
| `SwarmViz: Stop Server` | Closes the panel and shuts the server down |
| `SwarmViz: Show Server Log` | Reveals the output channel |

## Settings

| Setting | Default | Meaning |
| --- | --- | --- |
| `swarmviz.binaryPath` | `""` | Path to the binary; empty resolves `./swarmviz` then `PATH` |
| `swarmviz.serverUrl` | `""` | Attach to a server you already run instead of spawning one |
| `swarmviz.port` | `0` | Port for the spawned server; `0` picks a free one |
| `swarmviz.projectsRoot` | `""` | Passed as `--projects-root`, making sibling repositories switchable in the view |
| `swarmviz.extraArgs` | `[]` | Extra arguments for the spawned server |
| `swarmviz.stopServerOnClose` | `true` | Whether closing the panel also stops the server |

Attaching to your own server via `swarmviz.serverUrl` only works if that server
was started with `--allow-origin 'vscode-webview://*'` — a webview document
lives on an opaque `vscode-webview://` origin, and without that flag the
browser discards its responses. The extension passes the flag automatically to
servers it starts itself.

## Development

```bash
npm install
npm run build      # bundles src/ into dist/extension.js with esbuild
npm run typecheck
npm test           # covers the webview HTML rewrite
npm run watch      # rebuild on change; then F5 in VS Code to launch a host window
npm run package    # produces a .vsix
```

The extension is not published to the marketplace; install the `.vsix` with
`code --install-extension swarmviz-0.1.0.vsix`.

## How it fits together

```
extension host                          webview (vscode-webview://…)
  spawn swarmviz --port 0                 index.html, rewritten by src/html.ts
  parse "SwarmViz listening on …"           <script>window.__SWARMVIZ_ORIGIN__ = …
  poll /api/health                          bundle loaded from the server
  fetch / and rewrite it        ───────▶    fetch /api/projects   ──▶ swarmviz
  handle "revealInEditor"       ◀───────    WebSocket /ws         ──▶ swarmviz
```

Two edges of that picture live outside this directory:
`frontend/src/utils/host.ts` resolves the backend origin and posts the
`revealInEditor` message, and `pkg/server/cors.go` is what lets the webview's
`fetch` read a response at all.
