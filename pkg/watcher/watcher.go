package watcher

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/swarmviz/swarmviz/pkg/core"
)

type EventOp string

const (
	OpCreate EventOp = "CREATE"
	OpWrite  EventOp = "WRITE"
	OpRemove EventOp = "REMOVE"
	OpRename EventOp = "RENAME"
)

type FSEvent struct {
	Path      string    `json:"path"`
	RelPath   string    `json:"rel_path"`
	Op        EventOp   `json:"op"`
	Timestamp time.Time `json:"ts"`
}

type Config struct {
	RepoPath string        `json:"repo_path"`
	Interval time.Duration `json:"interval"`
}

type debounceItem struct {
	timer *time.Timer
	event FSEvent
}

type Watcher struct {
	cfg       Config
	fsWatcher *fsnotify.Watcher
	events    chan FSEvent
	errors    chan error
	stopCh    chan struct{}
	doneCh    chan struct{}

	mu      sync.Mutex
	pending map[string]*debounceItem
}

func NewWatcher(cfg Config) (*Watcher, error) {
	absRepo, err := filepath.Abs(cfg.RepoPath)
	if err != nil {
		return nil, fmt.Errorf("invalid repo path: %w", err)
	}
	cfg.RepoPath = absRepo

	if cfg.Interval <= 0 {
		cfg.Interval = 300 * time.Millisecond
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}

	return &Watcher{
		cfg:       cfg,
		fsWatcher: fsw,
		events:    make(chan FSEvent, 100),
		errors:    make(chan error, 50),
		stopCh:    make(chan struct{}),
		doneCh:    make(chan struct{}),
		pending:   make(map[string]*debounceItem),
	}, nil
}

func (w *Watcher) Events() <-chan FSEvent {
	return w.events
}

func (w *Watcher) Errors() <-chan error {
	return w.errors
}

func (w *Watcher) AddRecursive(root string) error {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}

	return filepath.WalkDir(absRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		relPath, err := filepath.Rel(w.cfg.RepoPath, path)
		if err != nil {
			return nil
		}

		if relPath == "." {
			return w.fsWatcher.Add(path)
		}

		if d.IsDir() && (d.Name() == ".git" || relPath == ".git" || strings.HasPrefix(relPath, ".git/")) {
			return filepath.SkipDir
		}

		checkPath := relPath
		if d.IsDir() && !strings.HasSuffix(checkPath, "/") {
			checkPath += "/"
		}

		ignoredMap, err := core.BatchCheckIgnore(w.cfg.RepoPath, []string{checkPath, relPath})
		if err == nil && (ignoredMap[checkPath] || ignoredMap[relPath]) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			return w.fsWatcher.Add(path)
		}
		return nil
	})
}

func (w *Watcher) Start(ctx context.Context) error {
	if err := w.AddRecursive(w.cfg.RepoPath); err != nil {
		return fmt.Errorf("failed to initialize recursive watcher: %w", err)
	}

	go w.loop(ctx)
	return nil
}

func (w *Watcher) loop(ctx context.Context) {
	defer close(w.doneCh)

	for {
		select {
		case <-ctx.Done():
			w.stopPendingTimers()
			return
		case <-w.stopCh:
			w.stopPendingTimers()
			return
		case raw, ok := <-w.fsWatcher.Events:
			if !ok {
				return
			}
			w.handleRawEvent(raw)
		case err, ok := <-w.fsWatcher.Errors:
			if !ok {
				return
			}
			select {
			case w.errors <- err:
			default:
			}
		}
	}
}

func (w *Watcher) Stop() {
	w.mu.Lock()
	select {
	case <-w.stopCh:
		w.mu.Unlock()
		return
	default:
		close(w.stopCh)
	}
	w.mu.Unlock()

	w.stopPendingTimers()
	_ = w.fsWatcher.Close()
	<-w.doneCh
}

func (w *Watcher) stopPendingTimers() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for relPath, item := range w.pending {
		if item.timer != nil {
			item.timer.Stop()
		}
		delete(w.pending, relPath)
	}
}

func (w *Watcher) handleRawEvent(raw fsnotify.Event) {
	if raw.Name == "" {
		return
	}

	relPath, err := filepath.Rel(w.cfg.RepoPath, raw.Name)
	if err != nil {
		return
	}

	if relPath == ".git" || strings.HasPrefix(relPath, ".git/") {
		return
	}

	ignoredMap, err := core.BatchCheckIgnore(w.cfg.RepoPath, []string{relPath})
	if err == nil && ignoredMap[relPath] {
		return
	}

	if raw.Op&fsnotify.Create != 0 {
		if info, err := os.Stat(raw.Name); err == nil && info.IsDir() {
			_ = w.AddRecursive(raw.Name)
		}
	}

	op := parseOp(raw.Op)
	event := FSEvent{
		Path:      raw.Name,
		RelPath:   relPath,
		Op:        op,
		Timestamp: time.Now().UTC(),
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	item, exists := w.pending[relPath]
	if exists {
		item.timer.Stop()
		if item.event.Op == OpCreate && op != OpRemove {
			event.Op = OpCreate
		}
	}

	timer := time.AfterFunc(w.cfg.Interval, func() {
		w.fireEvent(relPath)
	})

	w.pending[relPath] = &debounceItem{
		timer: timer,
		event: event,
	}
}

func (w *Watcher) fireEvent(relPath string) {
	w.mu.Lock()
	item, exists := w.pending[relPath]
	if !exists {
		w.mu.Unlock()
		return
	}
	delete(w.pending, relPath)
	event := item.event
	w.mu.Unlock()

	select {
	case w.events <- event:
	case <-w.stopCh:
	}
}

func parseOp(op fsnotify.Op) EventOp {
	switch {
	case op&fsnotify.Create != 0:
		return OpCreate
	case op&fsnotify.Write != 0:
		return OpWrite
	case op&fsnotify.Remove != 0:
		return OpRemove
	case op&fsnotify.Rename != 0:
		return OpRename
	default:
		return OpWrite
	}
}
