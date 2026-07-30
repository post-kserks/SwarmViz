package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/swarmviz/swarmviz/pkg/core"
	"github.com/swarmviz/swarmviz/pkg/diff"
	"github.com/swarmviz/swarmviz/pkg/hub"
	"github.com/swarmviz/swarmviz/pkg/registry"
	"github.com/swarmviz/swarmviz/pkg/server"
	"github.com/swarmviz/swarmviz/pkg/stats"
	"github.com/swarmviz/swarmviz/pkg/watcher"
)

// projectRuntime is one lazily-started extra project's full pipeline: shadow
// copy, hub, stats, disk monitor, diff engine, watcher and its own
// server.Server. It intentionally mirrors the root project's wiring in
// NewRootCmd's RunE (kept separate, not shared, so the already-deployed
// single-project path stays untouched by this addition).
type projectRuntime struct {
	copier      *core.ShadowCopier
	diskMonitor *core.DiskMonitor
	diffEngine  *diff.Engine
	watcher     *watcher.Watcher
	server      *server.Server
	cancel      context.CancelFunc
	stopped     chan struct{}
}

func startProjectRuntime(parent context.Context, base core.Config, printf func(string, ...interface{}), p registry.Project) (*projectRuntime, error) {
	pc := base
	pc.Path = p.Path

	validator := core.NewValidator(&pc)
	validator.SkipPortCheck = true
	if err := validator.ValidateAll(); err != nil {
		return nil, err
	}

	eventHub := hub.NewEventHub(hub.WithClaimTTL(pc.ClaimTTL))

	copier := core.NewShadowCopier(core.CopierOptions{
		RepoPath:    pc.Path,
		MaxRepoSize: pc.MaxRepoSizeBytes,
		RunID:       fmt.Sprintf("%d-%s", os.Getpid(), p.ID),
	})
	copyRes, err := copier.CreateBaseline()
	if err != nil {
		return nil, err
	}

	statsTracker := stats.New(p.Name, copyRes.CopiedFiles)

	srv := server.NewServer(
		eventHub,
		pc.BufferSize,
		statsTracker,
		statsTracker,
		server.WithAPIToken(pc.APIToken),
	)

	var isDegradedAtomic int32
	diskMonitor := core.NewDiskMonitor(core.DiskMonitorOptions{
		ShadowDir:      copyRes.ShadowDir,
		MaxRepoSize:    pc.MaxRepoSizeBytes,
		CheckInterval:  pc.DiskCheckInterval,
		MaxFileLimitMB: 10,
		OnWarning: func(code, message string) {
			atomic.StoreInt32(&isDegradedAtomic, 1)
			printf("⚠ [%s] SERVER_WARNING [%s]: %s\n", p.ID, code, message)
			srv.BroadcastEvent("SERVER_WARNING", map[string]interface{}{
				"code":    code,
				"message": message,
			})
		},
	})

	ctx, cancel := context.WithCancel(parent)
	diskMonitor.Start(ctx)

	diffEngine := diff.NewEngine(pc.Path, copyRes.ShadowDir, eventHub, func() bool {
		return atomic.LoadInt32(&isDegradedAtomic) == 1
	})

	fsWatcher, err := watcher.NewWatcher(watcher.Config{RepoPath: pc.Path, Interval: pc.Interval})
	if err != nil {
		cancel()
		diskMonitor.Stop()
		_ = copier.Cleanup()
		return nil, fmt.Errorf("failed to create watcher for %s: %w", p.ID, err)
	}
	if err := fsWatcher.Start(ctx); err != nil {
		cancel()
		diskMonitor.Stop()
		_ = copier.Cleanup()
		return nil, fmt.Errorf("failed to start watcher for %s: %w", p.ID, err)
	}

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-fsWatcher.Events():
				if !ok {
					return
				}
				res, diffErr := diffEngine.ProcessEvent(ev)
				if diffErr != nil {
					printf("[%s] Error processing diff event for %s: %v\n", p.ID, ev.RelPath, diffErr)
					continue
				}
				if res != nil {
					srv.BroadcastEvent("LIVE_DIFF_STREAM", res)
					editCount, totalAdded, totalRemoved := statsTracker.RecordEdit(res.File, res.Added, res.Removed)
					srv.BroadcastEvent("FILE_TREE_UPDATE", map[string]interface{}{
						"file":       res.File,
						"edit_count": editCount,
						"state":      string(res.Status),
					})
					srv.BroadcastEvent("LOC_DELTA_UPDATE", map[string]interface{}{
						"total_added":   totalAdded,
						"total_removed": totalRemoved,
					})
				}
			case watcherErr, ok := <-fsWatcher.Errors():
				if !ok {
					return
				}
				printf("[%s] Watcher error: %v\n", p.ID, watcherErr)
			}
		}
	}()

	return &projectRuntime{
		copier:      copier,
		diskMonitor: diskMonitor,
		diffEngine:  diffEngine,
		watcher:     fsWatcher,
		server:      srv,
		cancel:      cancel,
		stopped:     stopped,
	}, nil
}

func (r *projectRuntime) stop(printf func(string, ...interface{})) {
	r.cancel()
	r.watcher.Stop()
	r.diskMonitor.Stop()
	r.diffEngine.Close()
	r.server.Close()
	<-r.stopped
	if err := r.copier.Cleanup(); err != nil {
		printf("Error cleaning up shadow directory: %v\n", err)
	}
}

// projectSummary is the /api/projects wire format.
type projectSummary struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Path     string `json:"path"`
	BasePath string `json:"basePath"`
	Watching bool   `json:"watching"`
}

// multiRouter sits in front of the root server.Server (unchanged) and adds
// /api/projects plus lazily-started extra projects reachable under
// /p/{id}/... Everything not matching those two things falls through to the
// root handler untouched, so /ws, /api/agents, /api/health etc. keep working
// exactly as they do today for callers that don't know about multi-project
// mode (e.g. the Claude Code hook integration).
type multiRouter struct {
	root     http.Handler
	rootID   string
	rootName string
	rootPath string
	baseCfg  core.Config
	baseCtx  context.Context
	printf   func(string, ...interface{})

	mu       sync.Mutex
	byID     map[string]registry.Project
	order    []string
	runtimes map[string]*projectRuntime
}

func newMultiRouter(ctx context.Context, root http.Handler, rootID, rootName, rootPath string, baseCfg core.Config, projects []registry.Project, printf func(string, ...interface{})) *multiRouter {
	byID := make(map[string]registry.Project, len(projects))
	order := make([]string, 0, len(projects))
	for _, p := range projects {
		byID[p.ID] = p
		order = append(order, p.ID)
	}
	return &multiRouter{
		root:     root,
		rootID:   rootID,
		rootName: rootName,
		rootPath: rootPath,
		baseCfg:  baseCfg,
		baseCtx:  ctx,
		printf:   printf,
		byID:     byID,
		order:    order,
		runtimes: make(map[string]*projectRuntime),
	}
}

func (m *multiRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/projects" {
		m.handleProjects(w, r)
		return
	}

	if strings.HasPrefix(r.URL.Path, "/p/") {
		rest := strings.TrimPrefix(r.URL.Path, "/p/")
		id, sub, _ := strings.Cut(rest, "/")

		m.mu.Lock()
		_, known := m.byID[id]
		m.mu.Unlock()
		if !known {
			http.NotFound(w, r)
			return
		}

		rt, err := m.getOrStart(id)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		r2 := r.Clone(r.Context())
		r2.URL.Path = "/" + sub
		rt.server.ServeHTTP(w, r2)
		return
	}

	m.root.ServeHTTP(w, r)
}

func (m *multiRouter) getOrStart(id string) (*projectRuntime, error) {
	m.mu.Lock()
	if rt, ok := m.runtimes[id]; ok {
		m.mu.Unlock()
		return rt, nil
	}
	p := m.byID[id]
	m.mu.Unlock()

	rt, err := startProjectRuntime(m.baseCtx, m.baseCfg, m.printf, p)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	if existing, ok := m.runtimes[id]; ok {
		m.mu.Unlock()
		rt.stop(m.printf)
		return existing, nil
	}
	m.runtimes[id] = rt
	m.mu.Unlock()
	return rt, nil
}

func (m *multiRouter) handleProjects(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	list := make([]projectSummary, 0, len(m.order)+1)
	list = append(list, projectSummary{ID: m.rootID, Name: m.rootName, Path: m.rootPath, BasePath: "", Watching: true})
	for _, id := range m.order {
		p := m.byID[id]
		_, watching := m.runtimes[id]
		list = append(list, projectSummary{ID: p.ID, Name: p.Name, Path: p.Path, BasePath: "/p/" + p.ID, Watching: watching})
	}
	m.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(list)
}

// stopAll shuts down every lazily-started project runtime concurrently.
func (m *multiRouter) stopAll() {
	m.mu.Lock()
	runtimes := make([]*projectRuntime, 0, len(m.runtimes))
	for _, rt := range m.runtimes {
		runtimes = append(runtimes, rt)
	}
	m.mu.Unlock()

	var wg sync.WaitGroup
	for _, rt := range runtimes {
		wg.Add(1)
		go func(rt *projectRuntime) {
			defer wg.Done()
			rt.stop(m.printf)
		}(rt)
	}
	wg.Wait()
}
