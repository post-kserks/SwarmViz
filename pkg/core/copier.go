package core

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)



type CopierOptions struct {
	RepoPath    string
	MaxRepoSize int64
	PID         int
}

type CopyResult struct {
	ShadowDir      string
	TotalSizeBytes int64
	CopiedCount    int
	CopiedFiles    []string
}

type ShadowCopier struct {
	opts      CopierOptions
	shadowDir string
}

func NewShadowCopier(opts CopierOptions) *ShadowCopier {
	if opts.PID <= 0 {
		opts.PID = os.Getpid()
	}
	shadowDir := filepath.Join(os.TempDir(), fmt.Sprintf("swarmviz_run_%d", opts.PID))
	return &ShadowCopier{
		opts:      opts,
		shadowDir: shadowDir,
	}
}

func (c *ShadowCopier) ShadowDir() string {
	return c.shadowDir
}

func (c *ShadowCopier) CreateBaseline() (*CopyResult, error) {
	// One git call to get all non-ignored files instead of per-file BatchCheckIgnore.
	files, err := listTrackedFiles(c.opts.RepoPath)
	if err != nil {
		return nil, fmt.Errorf("failed listing repo files: %w", err)
	}

	// Fail-fast size check before touching the filesystem.
	var totalSize int64
	for relPath := range files {
		info, err := os.Stat(filepath.Join(c.opts.RepoPath, relPath))
		if err != nil || info.IsDir() {
			continue
		}
		totalSize += info.Size()
		if totalSize > c.opts.MaxRepoSize {
			return nil, fmt.Errorf("Error: Repository too large for live tracking (>%s). Exiting to prevent OOM.", FormatBytes(c.opts.MaxRepoSize))
		}
	}

	// Create shadow directory with 0755 permissions
	if err := os.MkdirAll(c.shadowDir, 0755); err != nil {
		return nil, fmt.Errorf("failed creating shadow directory %s: %w", c.shadowDir, err)
	}

	result := &CopyResult{
		ShadowDir:      c.shadowDir,
		TotalSizeBytes: totalSize,
		CopiedFiles:    make([]string, 0, len(files)),
	}

	// Copy each tracked file; copyFileContents creates parent dirs automatically.
	for relPath := range files {
		srcPath := filepath.Join(c.opts.RepoPath, relPath)
		destPath := filepath.Join(c.shadowDir, relPath)

		if err := copyFileContents(srcPath, destPath); err != nil {
			_ = os.RemoveAll(c.shadowDir)
			return nil, err
		}
		result.CopiedCount++
		result.CopiedFiles = append(result.CopiedFiles, relPath)
	}

	return result, nil
}


func (c *ShadowCopier) CopyFile(relPath string) error {
	cleanRel := filepath.Clean(relPath)
	srcPath := filepath.Join(c.opts.RepoPath, cleanRel)
	destPath := filepath.Join(c.shadowDir, cleanRel)

	info, err := os.Stat(srcPath)
	if err != nil {
		return err
	}

	if info.IsDir() {
		return os.MkdirAll(destPath, 0755)
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}

	return copyFileContents(srcPath, destPath)
}

func (c *ShadowCopier) RemoveFile(relPath string) error {
	cleanRel := filepath.Clean(relPath)
	destPath := filepath.Join(c.shadowDir, cleanRel)
	return os.RemoveAll(destPath)
}

func (c *ShadowCopier) Cleanup() error {
	if c.shadowDir == "" {
		return nil
	}
	return os.RemoveAll(c.shadowDir)
}

func copyFileContents(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err = io.Copy(out, in); err != nil {
		return err
	}

	return out.Sync()
}

func FormatBytes(b int64) string {
	if b < 1024 {
		return fmt.Sprintf("%d B", b)
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	val := float64(b)
	i := 0
	for val >= 1024 && i < len(units)-1 {
		val /= 1024
		i++
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.1f", val), "0"), ".") + units[i]
}
