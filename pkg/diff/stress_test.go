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

// TestStress_ParallelWritesSameFile tests rapid parallel writes to the exact same file.
func TestStress_ParallelWritesSameFile(t *testing.T) {
	repoDir, err := os.MkdirTemp("", "swarmviz_stress_same_repo_*")
	if err != nil {
		t.Fatalf("failed temp dir: %v", err)
	}
	defer os.RemoveAll(repoDir)

	shadowDir, err := os.MkdirTemp("", "swarmviz_stress_same_shadow_*")
	if err != nil {
		t.Fatalf("failed temp dir: %v", err)
	}
	defer os.RemoveAll(shadowDir)

	engine := NewEngine(repoDir, shadowDir, nil, func() bool { return false })
	defer engine.Close()

	relPath := "concurrent/shared.txt"
	absPath := filepath.Join(repoDir, relPath)
	_ = os.MkdirAll(filepath.Dir(absPath), 0755)

	var wg sync.WaitGroup
	workers := 25
	iterations := 20

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				content := fmt.Sprintf("Worker %d Line %d timestamp %d\n", workerID, j, time.Now().UnixNano())
				if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
					t.Errorf("write error: %v", err)
					return
				}
				res, err := engine.ProcessEvent(watcher.FSEvent{
					Path:      absPath,
					RelPath:   relPath,
					Op:        watcher.OpWrite,
					Timestamp: time.Now(),
				})
				if err != nil {
					t.Errorf("ProcessEvent error: %v", err)
					return
				}
				if res == nil {
					t.Errorf("expected non-nil DiffResult")
				}
			}
		}(i)
	}

	wg.Wait()
}

// TestStress_ParallelWritesDifferentFiles tests concurrent diff processing across multiple files simultaneously.
// This tests thread safety of the shared diffmatchpatch instance.
func TestStress_ParallelWritesDifferentFiles(t *testing.T) {
	repoDir, err := os.MkdirTemp("", "swarmviz_stress_diff_repo_*")
	if err != nil {
		t.Fatalf("failed temp dir: %v", err)
	}
	defer os.RemoveAll(repoDir)

	shadowDir, err := os.MkdirTemp("", "swarmviz_stress_diff_shadow_*")
	if err != nil {
		t.Fatalf("failed temp dir: %v", err)
	}
	defer os.RemoveAll(shadowDir)

	engine := NewEngine(repoDir, shadowDir, nil, func() bool { return false })
	defer engine.Close()

	var wg sync.WaitGroup
	filesCount := 30
	iterations := 15

	for i := 0; i < filesCount; i++ {
		wg.Add(1)
		go func(fileID int) {
			defer wg.Done()
			relPath := fmt.Sprintf("file_%d.txt", fileID)
			absPath := filepath.Join(repoDir, relPath)

			for j := 0; j < iterations; j++ {
				content := fmt.Sprintf("File %d iteration %d content line 1\nline 2\nline 3\n", fileID, j)
				_ = os.WriteFile(absPath, []byte(content), 0644)

				res, err := engine.ProcessEvent(watcher.FSEvent{
					Path:      absPath,
					RelPath:   relPath,
					Op:        watcher.OpWrite,
					Timestamp: time.Now(),
				})
				if err != nil {
					t.Errorf("ProcessEvent error on file %d: %v", fileID, err)
					return
				}
				if res == nil || res.File != relPath {
					t.Errorf("invalid result for file %d", fileID)
				}
			}
		}(i)
	}

	wg.Wait()
}

// TestStress_ZeroVsMultipleClaims tests zero claims, single claim, multiple claims (conflict), and TTL transitions.
func TestStress_ZeroVsMultipleClaims(t *testing.T) {
	h := hub.NewEventHub(hub.WithClaimTTL(100 * time.Millisecond))
	engine := NewAttributionEngine(h)
	defer engine.Close()

	file := "src/core/server.go"

	// 1. Zero claims
	res0 := engine.Attribute(file)
	if res0.Attribution != AttributionExternal || res0.AgentID != AgentIDExternal {
		t.Fatalf("expected external attribution for zero claims, got %+v", res0)
	}

	// 2. Single claim
	c1 := h.ClaimFile("agent-A", file)
	res1 := engine.Attribute(file)
	if res1.Attribution != AttributionClaimed || res1.AgentID != "agent-A" {
		t.Fatalf("expected agent-A claimed, got %+v", res1)
	}

	// 3. Multiple claims (conflict)
	c2 := h.ClaimFile("agent-B", file)
	res2 := engine.Attribute(file)
	if res2.Attribution != AttributionConflict || res2.AgentID != AgentIDMulti {
		t.Fatalf("expected conflict multi, got %+v", res2)
	}
	if len(res2.ClaimIDs) != 2 {
		t.Fatalf("expected 2 claims, got %d", len(res2.ClaimIDs))
	}

	// 4. Add third claim
	c3 := h.ClaimFile("agent-C", file)
	res3 := engine.Attribute(file)
	if res3.Attribution != AttributionConflict || len(res3.ClaimIDs) != 3 {
		t.Fatalf("expected conflict with 3 claims, got %+v", res3)
	}

	// 5. Release claims one by one
	h.ReleaseClaim(c1)
	time.Sleep(10 * time.Millisecond)
	res4 := engine.Attribute(file)
	if res4.Attribution != AttributionConflict || len(res4.ClaimIDs) != 2 {
		t.Fatalf("expected conflict with 2 remaining claims, got %+v", res4)
	}

	h.ReleaseClaim(c2)
	time.Sleep(10 * time.Millisecond)
	res5 := engine.Attribute(file)
	if res5.Attribution != AttributionClaimed || res5.AgentID != "agent-C" {
		t.Fatalf("expected agent-C claimed after c1 & c2 released, got %+v", res5)
	}

	h.ReleaseClaim(c3)
	time.Sleep(10 * time.Millisecond)
	res6 := engine.Attribute(file)
	if res6.Attribution != AttributionExternal || res6.AgentID != AgentIDExternal {
		t.Fatalf("expected external after all released, got %+v", res6)
	}

	// 6. Test TTL auto-expiry under conflict
	c1 = h.ClaimFile("agent-A", file)
	c2 = h.ClaimFile("agent-B", file)
	time.Sleep(150 * time.Millisecond) // Wait for TTL expiry (100ms)

	resTTL := engine.Attribute(file)
	if resTTL.Attribution != AttributionExternal {
		t.Fatalf("expected external after TTL expiry, got %+v", resTTL)
	}
}

// TestStress_BinaryFileTransitions tests text->binary and binary->text file transitions.
func TestStress_BinaryFileTransitions(t *testing.T) {
	repoDir, _ := os.MkdirTemp("", "swarmviz_stress_bin_repo_*")
	defer os.RemoveAll(repoDir)
	shadowDir, _ := os.MkdirTemp("", "swarmviz_stress_bin_shadow_*")
	defer os.RemoveAll(shadowDir)

	engine := NewEngine(repoDir, shadowDir, nil, func() bool { return false })
	defer engine.Close()

	relPath := "data/test.bin"
	absPath := filepath.Join(repoDir, relPath)
	_ = os.MkdirAll(filepath.Dir(absPath), 0755)

	// Step 1: Write text file
	_ = os.WriteFile(absPath, []byte("plain text content\nline 2\n"), 0644)
	res1, err := engine.ProcessEvent(watcher.FSEvent{
		Path:      absPath,
		RelPath:   relPath,
		Op:        watcher.OpCreate,
		Timestamp: time.Now(),
	})
	if err != nil || res1.Binary {
		t.Fatalf("expected text diff, got err=%v, res=%+v", err, res1)
	}

	// Step 2: Overwrite with binary content (null byte at byte 100)
	binData := make([]byte, 200)
	for i := range binData {
		binData[i] = 'A'
	}
	binData[100] = 0x00 // Null byte triggers binary detection
	_ = os.WriteFile(absPath, binData, 0644)

	res2, err := engine.ProcessEvent(watcher.FSEvent{
		Path:      absPath,
		RelPath:   relPath,
		Op:        watcher.OpWrite,
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res2.Binary || res2.Status != StatusBinary || res2.Hunk != "" {
		t.Fatalf("expected binary status with empty hunk, got %+v", res2)
	}

	// Step 3: Overwrite back to text content
	_ = os.WriteFile(absPath, []byte("plain text content updated\nline 2 modified\n"), 0644)
	res3, err := engine.ProcessEvent(watcher.FSEvent{
		Path:      absPath,
		RelPath:   relPath,
		Op:        watcher.OpWrite,
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Note: res3 will compare against previous shadow (which was binary).
	// Since shadow bytesA had a null byte, isBinary(bytesA) is true, so res3 returns StatusBinary and updates shadow to text.
	if res3.Status != StatusBinary {
		t.Fatalf("expected status binary when shadow was binary, got %s", res3.Status)
	}

	// Step 4: Next write to text file now compares text vs text!
	_ = os.WriteFile(absPath, []byte("plain text content updated v2\nline 2 modified\n"), 0644)
	res4, err := engine.ProcessEvent(watcher.FSEvent{
		Path:      absPath,
		RelPath:   relPath,
		Op:        watcher.OpWrite,
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res4.Binary || res4.Status != StatusInProgress || res4.Hunk == "" {
		t.Fatalf("expected text diff on second text write after binary transition, got %+v", res4)
	}
}

// TestStress_DegradedModeThresholds tests exact 10MB file size boundary conditions in DEGRADED mode.
func TestStress_DegradedModeThresholds(t *testing.T) {
	repoDir, _ := os.MkdirTemp("", "swarmviz_stress_deg_repo_*")
	defer os.RemoveAll(repoDir)
	shadowDir, _ := os.MkdirTemp("", "swarmviz_stress_deg_shadow_*")
	defer os.RemoveAll(shadowDir)

	degradedState := true
	engine := NewEngine(repoDir, shadowDir, nil, func() bool { return degradedState })
	defer engine.Close()

	relPath := "large_file.txt"
	absPath := filepath.Join(repoDir, relPath)

	// Test 1: Exactly 10MB (10 * 1024 * 1024 bytes) in DEGRADED mode -> Should NOT be skipped (> 10MB is false)
	f, _ := os.Create(absPath)
	_ = f.Truncate(10 * 1024 * 1024)
	_ = f.Close()

	res1, err := engine.ProcessEvent(watcher.FSEvent{
		Path:      absPath,
		RelPath:   relPath,
		Op:        watcher.OpWrite,
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res1.Skipped {
		t.Fatalf("expected 10MB file to NOT be skipped (threshold is > 10MB), got skipped=%v", res1.Skipped)
	}

	// Test 2: 10MB + 1 byte in DEGRADED mode -> Should BE skipped
	f2, _ := os.Create(absPath)
	_ = f2.Truncate(10*1024*1024 + 1)
	_ = f2.Close()

	res2, err := engine.ProcessEvent(watcher.FSEvent{
		Path:      absPath,
		RelPath:   relPath,
		Op:        watcher.OpWrite,
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res2.Skipped || res2.Reason != "size_limit" {
		t.Fatalf("expected 10MB+1 byte file to be skipped, got skipped=%v, reason=%s", res2.Skipped, res2.Reason)
	}

	// Test 3: 10MB + 1 byte in NORMAL mode (degradedState = false) -> Should NOT be skipped
	degradedState = false
	res3, err := engine.ProcessEvent(watcher.FSEvent{
		Path:      absPath,
		RelPath:   relPath,
		Op:        watcher.OpWrite,
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res3.Skipped {
		t.Fatalf("expected 10MB+1 byte file to NOT be skipped in NORMAL mode, got skipped=%v", res3.Skipped)
	}
}
