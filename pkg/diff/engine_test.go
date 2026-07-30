package diff

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/swarmviz/swarmviz/pkg/hub"
	"github.com/swarmviz/swarmviz/pkg/watcher"
)

func TestEngine_ProcessEvent_MyersDiff(t *testing.T) {
	repoDir, err := os.MkdirTemp("", "swarmviz_diff_repo_*")
	if err != nil {
		t.Fatalf("failed creating repo temp dir: %v", err)
	}
	defer os.RemoveAll(repoDir)

	shadowDir, err := os.MkdirTemp("", "swarmviz_diff_shadow_*")
	if err != nil {
		t.Fatalf("failed creating shadow temp dir: %v", err)
	}
	defer os.RemoveAll(shadowDir)

	h := hub.NewEventHub()
	_ = h.ClaimFile("agent-worker", "src/main.go")

	engine := NewEngine(repoDir, shadowDir, h, func() bool { return false })
	defer engine.Close()

	relPath := "src/main.go"
	absPath := filepath.Join(repoDir, relPath)
	shadowPath := filepath.Join(shadowDir, relPath)

	_ = os.MkdirAll(filepath.Dir(absPath), 0755)
	_ = os.MkdirAll(filepath.Dir(shadowPath), 0755)

	// Step 1: Initial state A (shadow)
	_ = os.WriteFile(shadowPath, []byte("package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"), 0644)
	// Step 2: Modified state B (working tree)
	_ = os.WriteFile(absPath, []byte("package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello world\")\n}\n"), 0644)

	res, err := engine.ProcessEvent(watcher.FSEvent{
		Path:      absPath,
		RelPath:   relPath,
		Op:        watcher.OpWrite,
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error processing diff: %v", err)
	}

	if res.Attribution != AttributionClaimed {
		t.Errorf("expected attribution 'claimed', got '%s'", res.Attribution)
	}
	if res.AgentID != "agent-worker" {
		t.Errorf("expected agent_id 'agent-worker', got '%s'", res.AgentID)
	}
	if res.Added < 1 || res.Removed < 1 {
		t.Errorf("expected added > 0 and removed > 0, got added=%d, removed=%d", res.Added, res.Removed)
	}
	if res.Hunk == "" {
		t.Errorf("expected non-empty diff hunk")
	}

	// Verify shadow file updated to State B
	shadowContent, _ := os.ReadFile(shadowPath)
	if string(shadowContent) != "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello world\")\n}\n" {
		t.Errorf("shadow content was not updated to State B")
	}
}

func TestEngine_ProcessEvent_BinaryFile(t *testing.T) {
	repoDir, _ := os.MkdirTemp("", "swarmviz_diff_binary_repo_*")
	defer os.RemoveAll(repoDir)
	shadowDir, _ := os.MkdirTemp("", "swarmviz_diff_binary_shadow_*")
	defer os.RemoveAll(shadowDir)

	engine := NewEngine(repoDir, shadowDir, nil, func() bool { return false })
	defer engine.Close()

	relPath := "binary.dat"
	absPath := filepath.Join(repoDir, relPath)
	_ = os.WriteFile(absPath, []byte{0x00, 0x01, 0x02, 0xFF, 0xFE}, 0644)

	res, err := engine.ProcessEvent(watcher.FSEvent{
		Path:      absPath,
		RelPath:   relPath,
		Op:        watcher.OpCreate,
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !res.Binary || res.Status != StatusBinary {
		t.Errorf("expected binary status, got binary=%v status=%s", res.Binary, res.Status)
	}
	if res.Hunk != "" {
		t.Errorf("expected empty hunk for binary file")
	}
}

func TestEngine_ProcessEvent_DegradedMode(t *testing.T) {
	repoDir, _ := os.MkdirTemp("", "swarmviz_diff_degraded_repo_*")
	defer os.RemoveAll(repoDir)
	shadowDir, _ := os.MkdirTemp("", "swarmviz_diff_degraded_shadow_*")
	defer os.RemoveAll(shadowDir)

	engine := NewEngine(repoDir, shadowDir, nil, func() bool { return true })
	defer engine.Close()

	relPath := "large.txt"
	absPath := filepath.Join(repoDir, relPath)

	f, err := os.Create(absPath)
	if err != nil {
		t.Fatalf("failed creating large file: %v", err)
	}
	// Truncate file to 11MB
	_ = f.Truncate(11 * 1024 * 1024)
	_ = f.Close()

	res, err := engine.ProcessEvent(watcher.FSEvent{
		Path:      absPath,
		RelPath:   relPath,
		Op:        watcher.OpWrite,
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !res.Skipped || res.Reason != "size_limit" {
		t.Errorf("expected skipped due to size_limit, got skipped=%v reason=%s", res.Skipped, res.Reason)
	}
}

func TestEngine_ProcessEvent_FileRemoval(t *testing.T) {
	repoDir, _ := os.MkdirTemp("", "swarmviz_diff_remove_repo_*")
	defer os.RemoveAll(repoDir)
	shadowDir, _ := os.MkdirTemp("", "swarmviz_diff_remove_shadow_*")
	defer os.RemoveAll(shadowDir)

	engine := NewEngine(repoDir, shadowDir, nil, func() bool { return false })
	defer engine.Close()

	relPath := "deleted.go"
	shadowPath := filepath.Join(shadowDir, relPath)
	_ = os.MkdirAll(filepath.Dir(shadowPath), 0755)
	_ = os.WriteFile(shadowPath, []byte("content"), 0644)

	res, err := engine.ProcessEvent(watcher.FSEvent{
		Path:      filepath.Join(repoDir, relPath),
		RelPath:   relPath,
		Op:        watcher.OpRemove,
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Status != StatusDeleted {
		t.Errorf("expected StatusDeleted, got '%s'", res.Status)
	}

	if _, err := os.Stat(shadowPath); !os.IsNotExist(err) {
		t.Errorf("expected shadow file to be removed")
	}
}

func TestEngine_PerFileMutex_Race(t *testing.T) {
	repoDir, _ := os.MkdirTemp("", "swarmviz_diff_race_repo_*")
	defer os.RemoveAll(repoDir)
	shadowDir, _ := os.MkdirTemp("", "swarmviz_diff_race_shadow_*")
	defer os.RemoveAll(shadowDir)

	engine := NewEngine(repoDir, shadowDir, nil, func() bool { return false })
	defer engine.Close()

	relPath := "shared/file.go"
	absPath := filepath.Join(repoDir, relPath)
	_ = os.MkdirAll(filepath.Dir(absPath), 0755)

	var wg sync.WaitGroup
	goroutines := 20
	iterations := 25

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				content := fmt.Sprintf("goroutine %d iteration %d\n", gid, j)
				_ = os.WriteFile(absPath, []byte(content), 0644)
				res, err := engine.ProcessEvent(watcher.FSEvent{
					Path:      absPath,
					RelPath:   relPath,
					Op:        watcher.OpWrite,
					Timestamp: time.Now(),
				})
				if err != nil {
					t.Errorf("error in goroutine %d: %v", gid, err)
				}
				if res == nil {
					t.Errorf("expected non-nil res")
				}
			}
		}(i)
	}

	wg.Wait()
}
