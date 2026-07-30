// Package stats aggregates the workspace state that widgets 3 (file tree) and
// 4 (LOC delta chart) render: a tree of the tracked files with per-file edit
// counts, and the running total of added/removed lines.
package stats

import (
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// DefaultMaxHistory bounds the retained LOC series. The UI downsamples with
// LTTB anyway, so keeping more than this only costs memory.
const DefaultMaxHistory = 5000

// FileNode mirrors the FileTreeNode shape the frontend expects.
type FileNode struct {
	Name      string      `json:"name"`
	Path      string      `json:"path"`
	IsDir     bool        `json:"isDir"`
	EditCount int         `json:"editCount"`
	Children  []*FileNode `json:"children,omitempty"`
}

// LOCPoint is one sample of the cumulative line delta.
type LOCPoint struct {
	Timestamp time.Time `json:"ts"`
	Added     int64     `json:"added"`
	Removed   int64     `json:"removed"`
}

// Tracker is safe for concurrent use: the diff pipeline writes to it while
// HTTP/WebSocket handlers read snapshots from it.
type Tracker struct {
	mu         sync.RWMutex
	root       *FileNode
	index      map[string]*FileNode
	added      int64
	removed    int64
	history    []LOCPoint
	maxHistory int
}

// New builds a tracker seeded with the repository's tracked files. relPaths is
// typically core.CopyResult.CopiedFiles, so the tree shows exactly the files
// the shadow copy watches — nothing git-ignored.
func New(repoName string, relPaths []string) *Tracker {
	if repoName == "" {
		repoName = "workspace"
	}
	t := &Tracker{
		root: &FileNode{
			Name:  repoName,
			Path:  "",
			IsDir: true,
		},
		index:      make(map[string]*FileNode),
		maxHistory: DefaultMaxHistory,
	}
	for _, rel := range relPaths {
		t.ensureNode(rel)
	}
	t.sortTree(t.root)
	return t
}

// normalize converts an OS-specific relative path into the forward-slash form
// used as the node key on both sides of the wire.
func normalize(rel string) string {
	cleaned := path.Clean(filepath.ToSlash(rel))
	cleaned = strings.TrimPrefix(cleaned, "./")
	if cleaned == "." || cleaned == "/" {
		return ""
	}
	return strings.TrimPrefix(cleaned, "/")
}

// ensureNode walks (creating as needed) the directory chain down to rel and
// returns the leaf. Caller must hold the write lock, except during New.
func (t *Tracker) ensureNode(rel string) *FileNode {
	key := normalize(rel)
	if key == "" {
		return nil
	}
	if n, ok := t.index[key]; ok {
		return n
	}

	segments := strings.Split(key, "/")
	current := t.root
	for i, seg := range segments {
		childPath := strings.Join(segments[:i+1], "/")
		isLeaf := i == len(segments)-1

		existing, ok := t.index[childPath]
		if !ok {
			existing = &FileNode{
				Name:  seg,
				Path:  childPath,
				IsDir: !isLeaf,
			}
			t.index[childPath] = existing
			current.Children = append(current.Children, existing)
		}
		current = existing
	}
	return current
}

func (t *Tracker) sortTree(node *FileNode) {
	if node == nil || len(node.Children) == 0 {
		return
	}
	// Directories first, then alphabetical — the conventional file-tree order.
	sort.SliceStable(node.Children, func(i, j int) bool {
		a, b := node.Children[i], node.Children[j]
		if a.IsDir != b.IsDir {
			return a.IsDir
		}
		return a.Name < b.Name
	})
	for _, child := range node.Children {
		t.sortTree(child)
	}
}

// RecordEdit accounts for one processed diff and returns the file's new edit
// count plus the cumulative LOC totals. Files created after startup are added
// to the tree on first sight.
func (t *Tracker) RecordEdit(relPath string, added, removed int) (editCount int, totalAdded, totalRemoved int64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	key := normalize(relPath)
	if key == "" {
		return 0, t.added, t.removed
	}

	node, existed := t.index[key]
	if !existed {
		node = t.ensureNode(key)
		t.sortTree(t.root)
	}
	if node == nil {
		return 0, t.added, t.removed
	}

	node.EditCount++
	if added > 0 {
		t.added += int64(added)
	}
	if removed > 0 {
		t.removed += int64(removed)
	}

	t.appendPointLocked(time.Now().UTC())
	return node.EditCount, t.added, t.removed
}

func (t *Tracker) appendPointLocked(ts time.Time) {
	t.history = append(t.history, LOCPoint{
		Timestamp: ts,
		Added:     t.added,
		Removed:   t.removed,
	})
	if len(t.history) > t.maxHistory {
		// Drop the oldest half at once so this reallocates rarely.
		drop := t.maxHistory / 2
		t.history = append([]LOCPoint(nil), t.history[drop:]...)
	}
}

// EditCount reports how many diffs have been observed for a file.
func (t *Tracker) EditCount(relPath string) int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if n, ok := t.index[normalize(relPath)]; ok {
		return n.EditCount
	}
	return 0
}

// GetFileTree implements server.FileTreeProvider. It returns a deep copy so
// concurrent mutation cannot race with JSON encoding of the snapshot.
func (t *Tracker) GetFileTree() interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return cloneNode(t.root)
}

func cloneNode(node *FileNode) *FileNode {
	if node == nil {
		return nil
	}
	clone := &FileNode{
		Name:      node.Name,
		Path:      node.Path,
		IsDir:     node.IsDir,
		EditCount: node.EditCount,
	}
	if len(node.Children) > 0 {
		clone.Children = make([]*FileNode, 0, len(node.Children))
		for _, child := range node.Children {
			clone.Children = append(clone.Children, cloneNode(child))
		}
	}
	return clone
}

// GetLOCDelta implements server.LOCDeltaProvider.
func (t *Tracker) GetLOCDelta() (added int64, removed int64) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.added, t.removed
}

// GetLOCHistory implements server.LOCHistoryProvider, giving a reconnecting
// client the chart it had before the socket dropped.
func (t *Tracker) GetLOCHistory() interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]LOCPoint, len(t.history))
	copy(out, t.history)
	return out
}
