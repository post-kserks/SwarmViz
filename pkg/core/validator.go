package core

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

var ErrRepoTooLarge = errors.New("repository too large")

type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

type Validator struct {
	Cfg *Config
	// SkipPortCheck bypasses the bind-and-close port availability check.
	// Extra projects in multi-project mode share a port already bound by
	// the root HTTP server, so re-checking it per project would always fail.
	SkipPortCheck bool
}

func NewValidator(cfg *Config) *Validator {
	return &Validator{Cfg: cfg}
}

func (v *Validator) ValidateAll() error {
	// 0. Host warning check
	v.checkHostWarning()

	// 1. Path exists and is directory
	absPath, err := v.validatePath()
	if err != nil {
		return err
	}
	v.Cfg.Path = absPath

	// 2. .git exists
	if err := v.validateGitRepo(absPath); err != nil {
		return err
	}

	// 3. Port available
	if !v.SkipPortCheck {
		if err := v.validatePort(); err != nil {
			return err
		}
	}

	// 4. Git binary in PATH
	if err := v.validateGitExecutable(); err != nil {
		return err
	}

	// 5. Repo size check
	maxBytes, err := ParseSizeString(v.Cfg.MaxRepoSizeStr)
	if err != nil {
		return &ValidationError{Message: fmt.Sprintf("Error: Invalid --max-repo-size value: %s", v.Cfg.MaxRepoSizeStr)}
	}
	v.Cfg.MaxRepoSizeBytes = maxBytes

	if err := v.validateRepoSize(absPath, maxBytes); err != nil {
		return err
	}

	return nil
}

func (v *Validator) checkHostWarning() {
	if v.Cfg.Host == "0.0.0.0" {
		fmt.Println("⚠ Listening on non-local interface — anyone on this network can observe your agent activity")
	}
}

func (v *Validator) validatePath() (string, error) {
	p := strings.TrimSpace(v.Cfg.Path)
	if p == "" {
		p = "./"
	}
	absPath, err := filepath.Abs(p)
	if err != nil {
		return "", &ValidationError{Message: fmt.Sprintf("Error: Path does not exist: %s", v.Cfg.Path)}
	}
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", &ValidationError{Message: fmt.Sprintf("Error: Path does not exist: %s", v.Cfg.Path)}
		}
		return "", &ValidationError{Message: fmt.Sprintf("Error: Cannot access path: %s", v.Cfg.Path)}
	}
	if !info.IsDir() {
		return "", &ValidationError{Message: fmt.Sprintf("Error: Path is not a directory: %s", v.Cfg.Path)}
	}
	return absPath, nil
}

func (v *Validator) validateGitRepo(absPath string) error {
	gitDir := filepath.Join(absPath, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		return &ValidationError{Message: fmt.Sprintf("Error: Not a git repository: %s. Run 'git init' first.", v.Cfg.Path)}
	}
	return nil
}

func (v *Validator) validatePort() error {
	// Port 0 means "let the kernel pick a free one" — there is nothing to
	// check for availability, and the chosen port is printed at startup.
	if v.Cfg.Port == 0 {
		return nil
	}
	if v.Cfg.Port < 0 || v.Cfg.Port > 65535 {
		return &ValidationError{Message: fmt.Sprintf("Error: Invalid port number: %d", v.Cfg.Port)}
	}
	address := fmt.Sprintf("%s:%d", v.Cfg.Host, v.Cfg.Port)
	l, err := net.Listen("tcp", address)
	if err != nil {
		if strings.Contains(err.Error(), "already in use") || strings.Contains(err.Error(), "address already in use") {
			return &ValidationError{Message: fmt.Sprintf("Error: Port %d already in use. Try --port %d", v.Cfg.Port, v.Cfg.Port+1)}
		}
		return &ValidationError{Message: fmt.Sprintf("Error: Failed to listen on %s: %v", address, err)}
	}
	_ = l.Close()
	return nil
}

func (v *Validator) validateGitExecutable() error {
	_, err := exec.LookPath("git")
	if err != nil {
		return &ValidationError{Message: "Error: git executable not found in PATH"}
	}
	return nil
}

func (v *Validator) validateRepoSize(absPath string, maxBytes int64) error {
	_, err := CalculateRepoSize(absPath, maxBytes)
	if err != nil {
		if errors.Is(err, ErrRepoTooLarge) {
			return &ValidationError{Message: fmt.Sprintf("Error: Repository too large for live tracking (>%s). Exiting to prevent OOM.", v.Cfg.MaxRepoSizeStr)}
		}
		return &ValidationError{Message: fmt.Sprintf("Error scanning repository size: %v", err)}
	}
	return nil
}

func ParseSizeString(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToUpper(s))
	if s == "" {
		return 0, fmt.Errorf("empty size string")
	}
	var multiplier int64 = 1
	if strings.HasSuffix(s, "GB") || strings.HasSuffix(s, "G") {
		multiplier = 1024 * 1024 * 1024
		s = strings.TrimSuffix(strings.TrimSuffix(s, "GB"), "G")
	} else if strings.HasSuffix(s, "MB") || strings.HasSuffix(s, "M") {
		multiplier = 1024 * 1024
		s = strings.TrimSuffix(strings.TrimSuffix(s, "MB"), "M")
	} else if strings.HasSuffix(s, "KB") || strings.HasSuffix(s, "K") {
		multiplier = 1024
		s = strings.TrimSuffix(strings.TrimSuffix(s, "KB"), "K")
	} else if strings.HasSuffix(s, "B") {
		s = strings.TrimSuffix(s, "B")
	}
	val, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0, err
	}
	return val * multiplier, nil
}

// listTrackedFiles returns the set of non-gitignored files in repoPath using
// a single git subprocess instead of one per file. Both tracked (committed/staged)
// and untracked-but-not-gitignored files are included.
func listTrackedFiles(repoPath string) (map[string]struct{}, error) {
	cmd := exec.Command("git", "ls-files", "--cached", "--others", "--exclude-standard")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return make(map[string]struct{}), nil
		}
		return nil, fmt.Errorf("git ls-files: %w", err)
	}

	files := make(map[string]struct{})
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			files[line] = struct{}{}
		}
	}
	return files, nil
}

func CalculateRepoSize(repoPath string, maxSizeBytes int64) (int64, error) {
	files, err := listTrackedFiles(repoPath)
	if err != nil {
		return 0, err
	}

	var totalSize int64
	for relPath := range files {
		info, err := os.Stat(filepath.Join(repoPath, relPath))
		if err != nil || info.IsDir() {
			continue
		}
		totalSize += info.Size()
		if maxSizeBytes > 0 && totalSize > maxSizeBytes {
			return totalSize, ErrRepoTooLarge
		}
	}
	return totalSize, nil
}

func BatchCheckIgnore(repoPath string, relPaths []string) (map[string]bool, error) {
	result := make(map[string]bool, len(relPaths))
	if len(relPaths) == 0 {
		return result, nil
	}

	// Use git check-ignore --stdin for fast batch checking
	cmd := exec.Command("git", "check-ignore", "--stdin")
	cmd.Dir = repoPath

	var stdinBuf bytes.Buffer
	for _, p := range relPaths {
		stdinBuf.WriteString(p)
		stdinBuf.WriteByte('\n')
	}
	cmd.Stdin = &stdinBuf

	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			// Exit code 1 means no files were ignored, which is normal
			return result, nil
		}
		return result, err
	}

	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			result[line] = true
		}
	}

	return result, nil
}
