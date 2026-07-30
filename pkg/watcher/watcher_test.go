package watcher

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestWatcher_DebounceAndExclusions(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "swarmviz_watcher_test_*")
	if err != nil {
		t.Fatalf("failed creating temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Init git repo and .gitignore
	gitCmd := exec.Command("git", "init")
	gitCmd.Dir = tempDir
	if err := gitCmd.Run(); err != nil {
		t.Fatalf("failed initializing git repo: %v", err)
	}

	gitignorePath := filepath.Join(tempDir, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte("ignored.txt\n*.log\n"), 0644); err != nil {
		t.Fatalf("failed creating .gitignore: %v", err)
	}

	w, err := NewWatcher(Config{
		RepoPath: tempDir,
		Interval: 150 * time.Millisecond,
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

	// 1. Write to ignored file -> expect NO event
	ignoredFile := filepath.Join(tempDir, "ignored.txt")
	_ = os.WriteFile(ignoredFile, []byte("test"), 0644)

	gitHead := filepath.Join(tempDir, ".git", "HEAD")
	_ = os.WriteFile(gitHead, []byte("ref: refs/heads/main"), 0644)

	time.Sleep(200 * time.Millisecond)

	select {
	case ev := <-w.Events():
		t.Fatalf("unexpected event for ignored file or .git: %+v", ev)
	default:
	}

	// 2. Rapid multiple writes to valid file -> expect EXACTLY 1 debounced event
	targetFile := filepath.Join(tempDir, "sample.go")
	for i := 0; i < 5; i++ {
		_ = os.WriteFile(targetFile, []byte("package main\n"), 0644)
		time.Sleep(20 * time.Millisecond)
	}

	select {
	case ev := <-w.Events():
		if ev.RelPath != "sample.go" {
			t.Errorf("expected relPath 'sample.go', got '%s'", ev.RelPath)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for debounced FSEvent")
	}

	// Verify no additional events follow immediately
	select {
	case ev := <-w.Events():
		t.Fatalf("unexpected duplicate event: %+v", ev)
	default:
	}
}

func TestWatcher_DynamicDirectory(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "swarmviz_watcher_dynamic_*")
	if err != nil {
		t.Fatalf("failed creating temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	gitCmd := exec.Command("git", "init")
	gitCmd.Dir = tempDir
	_ = gitCmd.Run()

	w, err := NewWatcher(Config{
		RepoPath: tempDir,
		Interval: 100 * time.Millisecond,
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

	// Create subfolder dynamically
	subDir := filepath.Join(tempDir, "subfolder")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("failed creating subfolder: %v", err)
	}

	time.Sleep(150 * time.Millisecond)

	// Drain any directory creation event
	select {
	case <-w.Events():
	default:
	}

	// Write file inside newly created subfolder
	subFile := filepath.Join(subDir, "nested.txt")
	if err := os.WriteFile(subFile, []byte("nested content"), 0644); err != nil {
		t.Fatalf("failed writing nested file: %v", err)
	}

	select {
	case ev := <-w.Events():
		expectedRel := filepath.Join("subfolder", "nested.txt")
		if ev.RelPath != expectedRel {
			t.Errorf("expected relPath '%s', got '%s'", expectedRel, ev.RelPath)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for event in dynamically registered directory")
	}
}
