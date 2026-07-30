package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestShadowCopier_CreateBaseline(t *testing.T) {
	tempDir := t.TempDir()

	// Init git repo
	cmd := exec.Command("git", "init", tempDir)
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}

	// Create .gitignore
	gitignorePath := filepath.Join(tempDir, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte("ignored/\n*.tmp\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create regular source files
	srcDir := filepath.Join(tempDir, "src")
	if err := os.Mkdir(srcDir, 0755); err != nil {
		t.Fatal(err)
	}
	mainFile := filepath.Join(srcDir, "main.go")
	if err := os.WriteFile(mainFile, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create ignored files
	ignoredDir := filepath.Join(tempDir, "ignored")
	if err := os.Mkdir(ignoredDir, 0755); err != nil {
		t.Fatal(err)
	}
	ignoredFile := filepath.Join(ignoredDir, "cache.dat")
	if err := os.WriteFile(ignoredFile, []byte("cache data"), 0644); err != nil {
		t.Fatal(err)
	}
	tmpFile := filepath.Join(tempDir, "temp.tmp")
	if err := os.WriteFile(tmpFile, []byte("temp data"), 0644); err != nil {
		t.Fatal(err)
	}

	copier := NewShadowCopier(CopierOptions{
		RepoPath:    tempDir,
		MaxRepoSize: 10 * 1024 * 1024,
		PID:         99999,
	})
	defer copier.Cleanup()

	res, err := copier.CreateBaseline()
	if err != nil {
		t.Fatalf("unexpected CreateBaseline error: %v", err)
	}

	// Verify shadow dir exists
	if _, err := os.Stat(res.ShadowDir); err != nil {
		t.Fatalf("shadow directory stat error: %v", err)
	}

	// Verify copied file main.go exists in shadow dir
	shadowMain := filepath.Join(res.ShadowDir, "src", "main.go")
	if _, err := os.Stat(shadowMain); err != nil {
		t.Errorf("expected copied src/main.go in shadow dir, got error: %v", err)
	}

	// Verify .git is NOT copied
	shadowGit := filepath.Join(res.ShadowDir, ".git")
	if _, err := os.Stat(shadowGit); !os.IsNotExist(err) {
		t.Errorf("expected .git NOT to be copied into shadow dir")
	}

	// Verify ignored file is NOT copied
	shadowIgnored := filepath.Join(res.ShadowDir, "ignored", "cache.dat")
	if _, err := os.Stat(shadowIgnored); !os.IsNotExist(err) {
		t.Errorf("expected ignored file NOT to be copied into shadow dir")
	}
}

func TestShadowCopier_CreateBaseline_ExceedsSize(t *testing.T) {
	tempDir := t.TempDir()

	cmd := exec.Command("git", "init", tempDir)
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}

	largeFile := filepath.Join(tempDir, "large.bin")
	if err := os.WriteFile(largeFile, make([]byte, 200), 0644); err != nil {
		t.Fatal(err)
	}

	copier := NewShadowCopier(CopierOptions{
		RepoPath:    tempDir,
		MaxRepoSize: 100,
		PID:         99998,
	})
	defer copier.Cleanup()

	_, err := copier.CreateBaseline()
	if err == nil || !strings.Contains(err.Error(), "Repository too large") {
		t.Fatalf("expected repository too large error, got %v", err)
	}
}

func TestShadowCopier_CopyAndRemoveFile(t *testing.T) {
	tempDir := t.TempDir()

	cmd := exec.Command("git", "init", tempDir)
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}

	copier := NewShadowCopier(CopierOptions{
		RepoPath:    tempDir,
		MaxRepoSize: 10 * 1024 * 1024,
		PID:         99997,
	})
	defer copier.Cleanup()

	_, err := copier.CreateBaseline()
	if err != nil {
		t.Fatal(err)
	}

	// Write new file in repo
	newFilePath := filepath.Join(tempDir, "newfile.go")
	if err := os.WriteFile(newFilePath, []byte("package new"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := copier.CopyFile("newfile.go"); err != nil {
		t.Fatalf("CopyFile failed: %v", err)
	}

	shadowNew := filepath.Join(copier.ShadowDir(), "newfile.go")
	if _, err := os.Stat(shadowNew); err != nil {
		t.Fatalf("expected newfile.go in shadow dir: %v", err)
	}

	if err := copier.RemoveFile("newfile.go"); err != nil {
		t.Fatalf("RemoveFile failed: %v", err)
	}

	if _, err := os.Stat(shadowNew); !os.IsNotExist(err) {
		t.Fatalf("expected newfile.go to be deleted from shadow dir")
	}
}
