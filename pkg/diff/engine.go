package diff

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sergi/go-diff/diffmatchpatch"
	"github.com/swarmviz/swarmviz/pkg/hub"
	"github.com/swarmviz/swarmviz/pkg/watcher"
)

const (
	MaxFileSizeBytesDefault = 10 * 1024 * 1024 // 10MB limit in DEGRADED mode
	BinaryCheckBytes        = 8000
	MaxHunkLines            = 500
)

type LockManager struct {
	mu    sync.RWMutex
	locks map[string]*sync.Mutex
}

func NewLockManager() *LockManager {
	return &LockManager{
		locks: make(map[string]*sync.Mutex),
	}
}

func (m *LockManager) GetLock(relPath string) *sync.Mutex {
	clean := filepath.Clean(relPath)
	m.mu.RLock()
	l, exists := m.locks[clean]
	m.mu.RUnlock()
	if exists {
		return l
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if l, exists = m.locks[clean]; exists {
		return l
	}
	l = &sync.Mutex{}
	m.locks[clean] = l
	return l
}

type Engine struct {
	repoPath   string
	shadowDir  string
	eventHub   hub.AgentEventHub
	attrEngine *AttributionEngine
	isDegraded func() bool
	lockMgr    *LockManager
	dmp        *diffmatchpatch.DiffMatchPatch
}

func NewEngine(repoPath, shadowDir string, eventHub hub.AgentEventHub, isDegraded func() bool) *Engine {
	var attrEngine *AttributionEngine
	if eventHub != nil {
		attrEngine = NewAttributionEngine(eventHub)
	}

	return &Engine{
		repoPath:   repoPath,
		shadowDir:  shadowDir,
		eventHub:   eventHub,
		attrEngine: attrEngine,
		isDegraded: isDegraded,
		lockMgr:    NewLockManager(),
		dmp:        diffmatchpatch.New(),
	}
}

func (e *Engine) Close() {
	if e.attrEngine != nil {
		e.attrEngine.Close()
	}
}

func (e *Engine) ProcessEvent(event watcher.FSEvent) (*DiffResult, error) {
	relPath := filepath.Clean(event.RelPath)
	fileLock := e.lockMgr.GetLock(relPath)
	fileLock.Lock()
	defer fileLock.Unlock()

	absPath := filepath.Join(e.repoPath, relPath)
	shadowPath := filepath.Join(e.shadowDir, relPath)

	attrRes := AttributionResult{
		Attribution: AttributionExternal,
		AgentID:     AgentIDExternal,
		File:        relPath,
	}
	if e.attrEngine != nil {
		attrRes = e.attrEngine.Attribute(relPath)
	}

	result := &DiffResult{
		AgentID:     attrRes.AgentID,
		File:        relPath,
		Status:      StatusInProgress,
		Attribution: attrRes.Attribution,
		ClaimIDs:    attrRes.ClaimIDs,
	}

	if event.Op == watcher.OpRemove {
		result.Status = StatusDeleted
		_ = os.Remove(shadowPath)
		return result, nil
	}

	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			result.Status = StatusDeleted
			_ = os.Remove(shadowPath)
			return result, nil
		}
		return nil, fmt.Errorf("failed to stat file %s: %w", absPath, err)
	}

	if e.isDegraded != nil && e.isDegraded() && info.Size() > MaxFileSizeBytesDefault {
		result.Status = StatusSkipped
		result.Skipped = true
		result.Reason = "size_limit"
		return result, nil
	}

	bytesB, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed reading working file %s: %w", absPath, err)
	}

	var bytesA []byte
	if _, statErr := os.Stat(shadowPath); statErr == nil {
		bytesA, _ = os.ReadFile(shadowPath)
	}

	if isBinary(bytesA) || isBinary(bytesB) {
		result.Status = StatusBinary
		result.Binary = true
		_ = e.updateShadowCopy(shadowPath, bytesB)
		return result, nil
	}

	textA := string(bytesA)
	textB := string(bytesB)

	lineA, lineB, lineArray := e.dmp.DiffLinesToChars(textA, textB)
	diffs := e.dmp.DiffMain(lineA, lineB, false)
	diffs = e.dmp.DiffCharsToLines(diffs, lineArray)
	diffs = e.dmp.DiffCleanupMerge(diffs)

	hunk, added, removed, truncated := formatUnifiedDiff(diffs)
	result.Hunk = hunk
	result.Added = added
	result.Removed = removed
	result.Truncated = truncated

	if err := e.updateShadowCopy(shadowPath, bytesB); err != nil {
		return nil, fmt.Errorf("failed updating shadow copy: %w", err)
	}

	return result, nil
}

func isBinary(data []byte) bool {
	limit := BinaryCheckBytes
	if len(data) < limit {
		limit = len(data)
	}
	return bytes.IndexByte(data[:limit], 0) != -1
}

func (e *Engine) updateShadowCopy(shadowPath string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(shadowPath), 0755); err != nil {
		return err
	}
	return os.WriteFile(shadowPath, content, 0644)
}

func formatUnifiedDiff(diffs []diffmatchpatch.Diff) (hunk string, added int, removed int, truncated bool) {
	var sb strings.Builder
	lineCount := 0

	for _, d := range diffs {
		lines := strings.Split(d.Text, "\n")
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		for _, line := range lines {
			lineCount++
			if lineCount > MaxHunkLines {
				truncated = true
				break
			}
			switch d.Type {
			case diffmatchpatch.DiffInsert:
				added++
				sb.WriteString("+")
				sb.WriteString(line)
				sb.WriteString("\n")
			case diffmatchpatch.DiffDelete:
				removed++
				sb.WriteString("-")
				sb.WriteString(line)
				sb.WriteString("\n")
			case diffmatchpatch.DiffEqual:
				sb.WriteString(" ")
				sb.WriteString(line)
				sb.WriteString("\n")
			}
		}
		if truncated {
			break
		}
	}
	return sb.String(), added, removed, truncated
}
