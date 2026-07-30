package watcher

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestWatcher_StressRapidParallelWrites tests watcher debounce under rapid concurrent file modifications.
func TestWatcher_StressRapidParallelWrites(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "swarmviz_watcher_stress_rapid_*")
	if err != nil {
		t.Fatalf("failed temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	gitCmd := exec.Command("git", "init")
	gitCmd.Dir = tempDir
	_ = gitCmd.Run()

	w, err := NewWatcher(Config{
		RepoPath: tempDir,
		Interval: 80 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("failed creating watcher: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("failed starting watcher: %v", err)
	}
	defer w.Stop()

	targetFile := filepath.Join(tempDir, "rapid_write.txt")

	var wg sync.WaitGroup
	workers := 15
	iterations := 20

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = os.WriteFile(targetFile, []byte(fmt.Sprintf("worker %d iter %d\n", id, j)), 0644)
				time.Sleep(2 * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()

	// Wait for debounce interval + buffer
	time.Sleep(200 * time.Millisecond)

	received := 0
	done := false
	for !done {
		select {
		case ev := <-w.Events():
			if ev.RelPath != "rapid_write.txt" {
				t.Errorf("unexpected file in event: %s", ev.RelPath)
			}
			received++
		default:
			done = true
		}
	}

	if received == 0 {
		t.Fatalf("expected at least 1 FSEvent for rapid writes, got 0")
	}
	// Debounce should coalesce 300 rapid writes into a small number of events (much less than 300)
	if received > 10 {
		t.Errorf("debounce failed to coalesce rapid writes effectively: received %d events for 300 rapid writes", received)
	}
}

// TestWatcher_DynamicSubdirectoryCreationDeep tests dynamic registration of deep directory structures.
func TestWatcher_DynamicSubdirectoryCreationDeep(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "swarmviz_watcher_stress_deep_*")
	if err != nil {
		t.Fatalf("failed temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	gitCmd := exec.Command("git", "init")
	gitCmd.Dir = tempDir
	_ = gitCmd.Run()

	w, err := NewWatcher(Config{
		RepoPath: tempDir,
		Interval: 80 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("failed creating watcher: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("failed starting watcher: %v", err)
	}
	defer w.Stop()

	// Create a deep directory hierarchy dynamically: sub1/sub2/sub3/sub4
	deepPath := filepath.Join(tempDir, "sub1", "sub2", "sub3", "sub4")
	if err := os.MkdirAll(deepPath, 0755); err != nil {
		t.Fatalf("failed creating deep directory: %v", err)
	}

	// Give watcher time to process directory creation and add watches
	time.Sleep(150 * time.Millisecond)

	// Drain directory creation events
	for len(w.Events()) > 0 {
		<-w.Events()
	}

	// Write file deep inside the newly created directory structure
	deepFile := filepath.Join(deepPath, "nested_deep.txt")
	if err := os.WriteFile(deepFile, []byte("deep nested content"), 0644); err != nil {
		t.Fatalf("failed writing deep file: %v", err)
	}

	select {
	case ev := <-w.Events():
		expectedRel := filepath.Join("sub1", "sub2", "sub3", "sub4", "nested_deep.txt")
		if ev.RelPath != expectedRel {
			t.Errorf("expected relPath '%s', got '%s'", expectedRel, ev.RelPath)
		}
	case <-time.After(1500 * time.Millisecond):
		t.Fatalf("timed out waiting for FSEvent in deep dynamic directory")
	}
}

// TestWatcher_GitIgnoredSubdirectoryExclusion tests that dynamically added ignored directories produce no events.
func TestWatcher_GitIgnoredSubdirectoryExclusion(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "swarmviz_watcher_stress_ignore_*")
	if err != nil {
		t.Fatalf("failed temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	gitCmd := exec.Command("git", "init")
	gitCmd.Dir = tempDir
	_ = gitCmd.Run()

	// Create .gitignore
	gitignorePath := filepath.Join(tempDir, ".gitignore")
	_ = os.WriteFile(gitignorePath, []byte("ignored_dir/\n*.tmp\n"), 0644)

	w, err := NewWatcher(Config{
		RepoPath: tempDir,
		Interval: 80 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("failed creating watcher: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("failed starting watcher: %v", err)
	}
	defer w.Stop()

	// Create ignored directory and write files inside
	ignoredDir := filepath.Join(tempDir, "ignored_dir")
	_ = os.MkdirAll(ignoredDir, 0755)
	_ = os.WriteFile(filepath.Join(ignoredDir, "build.log"), []byte("log data"), 0644)

	// Write *.tmp file
	tmpFile := filepath.Join(tempDir, "scratch.tmp")
	_ = os.WriteFile(tmpFile, []byte("temp data"), 0644)

	time.Sleep(200 * time.Millisecond)

	select {
	case ev := <-w.Events():
		t.Fatalf("unexpected FSEvent for ignored file/directory: %+v", ev)
	default:
	}
}
