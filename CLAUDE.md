# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

This host has no system Go or Node. Both are installed locally and are **not** on `$PATH` by default:

```bash
export PATH=/home/pin/.local/go/bin:/home/pin/.local/node/bin:$PATH
```

Invoking `/home/pin/.local/node/bin/npm` directly without fixing `PATH` fails — npm is a shell script with `#!/usr/bin/env node`.

```bash
# Build order matters: the UI is embedded, so build the frontend FIRST
npm --prefix frontend install
npm --prefix frontend run build      # tsc && vite build -> frontend/dist
go build -o swarmviz .

go test ./...                        # full suite
go test -race ./...                  # what CI-equivalent verification should use
go test -run 'TestAPIBatch' ./pkg/server/    # single test / package
go vet ./...
(cd frontend && npx tsc --noEmit)    # frontend typecheck without a build

./swarmviz --path ./ --port 8942     # run against a git repo
./dev.sh                             # Go backend :8942 + Vite dev UI :3000
```

`dev.sh` relies on `go` and `npm` being on `$PATH`, so export it first. Vite proxies `/ws` and `/api` from :3000 to :8942, so the dev UI talks to a real backend.

To see all four widgets populated without a real orchestrator:

```bash
./swarmviz --path /tmp/demo-repo --claim-ttl 10s &
python3 examples/demo_swarm.py --repo /tmp/demo-repo
```

## Architecture

A single Go binary watches a git repository, computes per-file diffs, attributes each diff to the agent that claimed the file, and streams everything to an embedded React UI over one WebSocket.

### The pipeline

`pkg/cli/cli.go` is the composition root — read it first; it wires every component in order and is the only place that knows the whole graph.

```
fsnotify watcher ──FSEvent──► diff.Engine ──DiffResult──► server.BroadcastEvent ──► RingBuffer ──► WS clients
   (debounced,                (shadow copy                      │
    .git + gitignore           comparison,                      ├──► stats.Tracker ──► FILE_TREE_UPDATE
    aware)                     Myers diff)                      │                      LOC_DELTA_UPDATE
                                    │                           └──► LIVE_DIFF_STREAM
                              AttributionEngine ◄──claims── hub.EventHub ◄── HTTP /api/ or Go interface
```

- **`pkg/watcher`** debounces events per file (`--interval`), skips `.git`, and consults `git check-ignore`, so ignored files never enter the pipeline.
- **`pkg/core`** does fail-fast startup validation (path is a git repo, `git` on `$PATH`, port free, repo under `--max-repo-size`) and maintains the **shadow copy**: a baseline of `git ls-files` output under `/tmp/swarmviz_run_<pid>/`. Diffs are working-tree vs. shadow, and the shadow is updated after each diff. It lives on disk, not in memory, deliberately — the design target is OOM safety. The disk monitor can flip the process into **degraded mode**, which skips files over 10MB and emits `SERVER_WARNING`.
- **`pkg/diff`** holds a per-file `sync.Mutex` while diffing, so concurrent writes to different files proceed in parallel but the same file never races with itself.
- **`pkg/hub`** is the in-memory agent store plus a pub/sub bus. Every mutation emits a `HubEvent` to subscribers; the server is one such subscriber.
- **`pkg/stats`** aggregates the file tree and cumulative LOC totals. It is seeded from the shadow copy's file list, so the tree shows exactly the tracked, non-ignored files.

### Claim-then-diff: the core contract

Attribution is resolved at diff time, not write time. `AttributionEngine.Attribute` asks the hub which claims are active for the path:

- 0 claims → `external` / agent id `external`
- 1 claim → `claimed` / that agent
- 2+ claims → `conflict` / agent id `multi`

So a claim must be registered **before** the write lands and must still be alive when the debounced diff runs. Claims auto-expire after `--claim-ttl` (default **2s**), which is short enough to silently break tests and demos that sleep between claiming and asserting — raise `--claim-ttl` there rather than assuming attribution is broken.

### Two ways in

Orchestrators feed the hub either through the Go `AgentEventHub` interface (in-process) or the HTTP ingest API in `pkg/server/api.go` (`/api/agents`, `/api/claims`, `/api/edges`, batch `/api/events`). Both converge on the same `hub.EventHub`, so anything added to one path should be reachable from the other. The API validates against the hub's enums and rejects unknown JSON fields; `--api-token` adds bearer auth, which matters when `--host` exposes the port beyond loopback.

### WebSocket protocol and replay

`RingBuffer` assigns every outbound event a monotonic `seq` and retains the last `--buffer-size` envelopes. Clients reconnect with `?since=<seq>`: if the buffer still covers that seq the server replays the gap, otherwise it sends a fresh `INIT_STATE`. The frontend tracks `lastSeq` and reconnects with exponential backoff.

**Consequence:** always emit server→client events via `server.BroadcastEvent` / the hub. Writing to a socket directly bypasses sequencing and silently breaks replay for every reconnecting client.

### The Go↔TypeScript wire contract

Go serialises **snake_case** (`agent_id`, `agent_type`, `from_id`, `edit_count`), while the TS interfaces in `frontend/src/types/swarm.ts` are camelCase; `frontend/src/store/useSwarmStore.ts` normalises at the boundary. This seam is where the two halves have drifted before — when adding an event type, update all of: the emitter (hub or `cli.go`), the `WSEventType` union, the `handleWSEvent` switch, and the store action.

Widgets are numbered by layout position (`Widget1_SubagentMap` … `Widget4_LocChart`) and read only from the zustand store; nothing fetches on its own.

## Build gotchas

- `frontend/dist/` is git-ignored except for a committed `.gitkeep`, which exists solely so `//go:embed all:dist` resolves on a clean clone. A Go build without a frontend build succeeds but embeds an empty UI — `frontend.IsBuilt()` detects this and the binary warns at startup. The API and WebSocket still work.
- `go.sum` was previously committed with hashes that did not match `sum.golang.org`. If verification fails, check the real hash (`curl https://sum.golang.org/lookup/<module>@<version>`) before regenerating — don't assume the checksum database is wrong.
- `go.mod` targets Go 1.22, which constrains dependency upgrades (`golang.org/x/sys` is pinned back to v0.15.0 for this reason). Bumping a dependency that requires a newer toolchain silently breaks the advertised minimum.

## Multi-project mode

`--path`/`--port` alone behave exactly as before: one process, one repo, routes at `/ws` and `/api/...` unprefixed. Adding `--projects-root <dir>` (optionally narrowed with `--projects name1,name2`) additionally discovers every git repo directly under `<dir>` (`pkg/registry.Discover`) and exposes each one, lazily, at `/p/{id}/...` on the same port — the root project keeps its unprefixed routes untouched, which is what keeps `integrations/claude-code/swarmviz_hook.py` (hits bare `/api/...`) working without changes.

- `GET /api/projects` lists the root project plus every discovered one: `{id, name, path, basePath, watching}`. `basePath` is what the frontend prepends to `/ws`/`/api/...` (`""` for root, `"/p/{id}"` otherwise).
- An extra project's full pipeline (shadow copy, hub, stats, disk monitor, diff engine, watcher, its own `server.Server`) only starts on first request to `/p/{id}/...` (`pkg/cli/multi.go`'s `multiRouter.getOrStart`), not at process startup — so pointing `--projects-root` at a large directory of repos doesn't eagerly shadow-copy all of them.
- This wiring is deliberately a separate, near-duplicate copy of the root `RunE` wiring in `pkg/cli/cli.go`, not a shared refactor — the root path (the one actually deployed) stays byte-for-byte unchanged.
- `core.CopierOptions.RunID` disambiguates shadow dirs (`swarmviz_run_<RunID>` vs the default `swarmviz_run_<PID>`) so extra projects in the same process don't collide. `core.Validator.SkipPortCheck` skips the bind-and-close check for extra projects, since they share the root's already-bound port.
