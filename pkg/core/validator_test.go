package core

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseSizeString(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
		wantErr  bool
	}{
		{"500MB", 500 * 1024 * 1024, false},
		{"1GB", 1 * 1024 * 1024 * 1024, false},
		{"10KB", 10 * 1024, false},
		{"100B", 100, false},
		{"2G", 2 * 1024 * 1024 * 1024, false},
		{"500M", 500 * 1024 * 1024, false},
		{"", 0, true},
		{"invalid", 0, true},
	}

	for _, tt := range tests {
		got, err := ParseSizeString(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseSizeString(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if got != tt.expected {
			t.Errorf("ParseSizeString(%q) = %d, want %d", tt.input, got, tt.expected)
		}
	}
}

func TestValidatePath(t *testing.T) {
	tempDir := t.TempDir()

	// Case 1: Non-existent path
	nonExistent := filepath.Join(tempDir, "missing_dir")
	cfg := &Config{Path: nonExistent}
	v := NewValidator(cfg)
	err := v.ValidateAll()
	if err == nil || !strings.Contains(err.Error(), "Error: Path does not exist:") {
		t.Errorf("expected path does not exist error, got %v", err)
	}

	// Case 2: Path is a file, not a directory
	filePath := filepath.Join(tempDir, "somefile.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg = &Config{Path: filePath}
	v = NewValidator(cfg)
	err = v.ValidateAll()
	if err == nil || !strings.Contains(err.Error(), "Error: Path is not a directory:") {
		t.Errorf("expected path is not a directory error, got %v", err)
	}
}

func getFreePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		l, err = net.Listen("tcp", ":0")
		if err != nil {
			return 8942
		}
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func TestValidateGitRepo(t *testing.T) {
	tempDir := t.TempDir()
	freePort := getFreePort(t)

	// Case: Not a git repo
	cfg := &Config{
		Path:           tempDir,
		Port:           freePort,
		Host:           "127.0.0.1",
		MaxRepoSizeStr: "500MB",
	}
	v := NewValidator(cfg)
	err := v.ValidateAll()
	if err == nil || !strings.Contains(err.Error(), "Not a git repository") {
		t.Errorf("expected not a git repo error, got %v", err)
	}

	// Create .git directory
	gitDir := filepath.Join(tempDir, ".git")
	if err := os.Mkdir(gitDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Initialize git repository structure so git check-ignore works
	cmd := exec.Command("git", "init")
	cmd.Dir = tempDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to git init test directory: %v, output: %s", err, string(out))
	}

	err = v.ValidateAll()
	if err != nil && strings.Contains(err.Error(), "Failed to listen on") {
		err = v.validateGitRepo(tempDir)
	}
	if err != nil {
		t.Errorf("expected validation success, got error: %v", err)
	}
}

func TestValidatePort(t *testing.T) {
	tempDir := t.TempDir()

	// Init git repo
	cmd := exec.Command("git", "init", tempDir)
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}

	// Bind a port manually
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		l, err = net.Listen("tcp", ":0")
		if err != nil {
			t.Skip("skipping port binding test because net.Listen is restricted in sandbox")
			return
		}
	}
	defer l.Close()

	boundPort := l.Addr().(*net.TCPAddr).Port

	cfg := &Config{
		Path:           tempDir,
		Port:           boundPort,
		Host:           "127.0.0.1",
		MaxRepoSizeStr: "500MB",
	}
	v := NewValidator(cfg)
	err = v.ValidateAll()
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("Error: Port %d already in use", boundPort)) {
		t.Errorf("expected port in use error, got %v", err)
	}

	// Case 2: Non-listen error (invalid host address)
	cfgInvalidHost := &Config{
		Path:           tempDir,
		Port:           8942,
		Host:           "256.256.256.256",
		MaxRepoSizeStr: "500MB",
	}
	vInvalidHost := NewValidator(cfgInvalidHost)
	err = vInvalidHost.ValidateAll()
	if err == nil || !strings.Contains(err.Error(), "Error: Failed to listen on") {
		t.Errorf("expected failed to listen error for invalid host, got %v", err)
	}
}

func TestValidateRepoSizeExceeded(t *testing.T) {
	tempDir := t.TempDir()
	freePort := getFreePort(t)

	cmd := exec.Command("git", "init", tempDir)
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}

	// Write a file larger than 100 bytes
	largeFilePath := filepath.Join(tempDir, "large.dat")
	largeData := make([]byte, 500)
	if err := os.WriteFile(largeFilePath, largeData, 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &Config{
		Path:              tempDir,
		Port:              freePort,
		Host:              "127.0.0.1",
		MaxRepoSizeStr:    "100B",
		DiskCheckInterval: 30 * time.Second,
	}

	v := NewValidator(cfg)
	err := v.ValidateAll()
	if err != nil && strings.Contains(err.Error(), "Failed to listen on") {
		maxBytes, _ := ParseSizeString(cfg.MaxRepoSizeStr)
		err = v.validateRepoSize(tempDir, maxBytes)
	}
	if err == nil || !strings.Contains(err.Error(), "Error: Repository too large for live tracking (>100B)") {
		t.Errorf("expected repository too large error, got %v", err)
	}
}

func TestBatchCheckIgnore(t *testing.T) {
	tempDir := t.TempDir()

	cmd := exec.Command("git", "init", tempDir)
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}

	// Write .gitignore
	gitignorePath := filepath.Join(tempDir, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte("ignored_dir/\n*.log\n"), 0644); err != nil {
		t.Fatal(err)
	}

	paths := []string{"ignored_dir/", "test.log", "main.go"}
	ignoredMap, err := BatchCheckIgnore(tempDir, paths)
	if err != nil {
		t.Fatalf("unexpected BatchCheckIgnore error: %v", err)
	}

	if !ignoredMap["ignored_dir/"] {
		t.Errorf("expected ignored_dir/ to be ignored")
	}
	if !ignoredMap["test.log"] {
		t.Errorf("expected test.log to be ignored")
	}
	if ignoredMap["main.go"] {
		t.Errorf("expected main.go NOT to be ignored")
	}
}
