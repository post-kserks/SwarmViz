package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDiskMonitor_TransitionToDegraded(t *testing.T) {
	tempShadowDir := t.TempDir()

	var warningTriggered int32
	var warningCode, warningMsg string

	opts := DiskMonitorOptions{
		ShadowDir:      tempShadowDir,
		MaxRepoSize:    1000, // 1000 bytes
		CheckInterval:  10 * time.Millisecond,
		MaxFileLimitMB: 10,
		OnWarning: func(code, message string) {
			atomic.AddInt32(&warningTriggered, 1)
			warningCode = code
			warningMsg = message
		},
	}

	monitor := NewDiskMonitor(opts)
	if monitor.IsDegraded() {
		t.Fatalf("expected initial state NOT to be degraded")
	}

	// Should not skip file when not degraded
	if monitor.ShouldSkipFile(20 * 1024 * 1024) {
		t.Errorf("should NOT skip file when NOT degraded")
	}

	// Create file in shadow dir exceeding 1000 bytes
	largeFile := filepath.Join(tempShadowDir, "data.bin")
	if err := os.WriteFile(largeFile, make([]byte, 2000), 0644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	monitor.Start(ctx)

	// Wait for ticker to run
	time.Sleep(50 * time.Millisecond)

	monitor.Stop()

	if !monitor.IsDegraded() {
		t.Fatalf("expected state to be DEGRADED after check")
	}

	if atomic.LoadInt32(&warningTriggered) != 1 {
		t.Fatalf("expected warning callback to be triggered once, got %d", warningTriggered)
	}

	if warningCode != "REPO_SIZE_EXCEEDED" {
		t.Errorf("expected warning code REPO_SIZE_EXCEEDED, got %s", warningCode)
	}

	if !strings.Contains(warningMsg, "Repository grew beyond threshold") {
		t.Errorf("expected warning message to contain threshold info, got %s", warningMsg)
	}

	// Now that it's degraded, test ShouldSkipFile
	// File size 5MB (below 10MB limit) -> false
	if monitor.ShouldSkipFile(5 * 1024 * 1024) {
		t.Errorf("should NOT skip 5MB file even in DEGRADED mode")
	}
	// File size 15MB (above 10MB limit) -> true
	if !monitor.ShouldSkipFile(15 * 1024 * 1024) {
		t.Errorf("SHOULD skip 15MB file in DEGRADED mode")
	}
}

func TestDiskMonitor_ManualCheckDiskUsage(t *testing.T) {
	tempShadowDir := t.TempDir()

	opts := DiskMonitorOptions{
		ShadowDir:      tempShadowDir,
		MaxRepoSize:    500,
		CheckInterval:  1 * time.Second,
		MaxFileLimitMB: 5,
	}

	monitor := NewDiskMonitor(opts)
	size, degraded := monitor.CheckDiskUsage()
	if size != 0 || degraded {
		t.Errorf("expected size 0 and degraded false for empty dir")
	}

	testFile := filepath.Join(tempShadowDir, "file.txt")
	if err := os.WriteFile(testFile, make([]byte, 600), 0644); err != nil {
		t.Fatal(err)
	}

	size, degraded = monitor.CheckDiskUsage()
	if size != 600 || !degraded {
		t.Errorf("expected size 600 and degraded true, got size=%d degraded=%v", size, degraded)
	}
}
