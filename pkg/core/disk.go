package core

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type WarningHandler func(code string, message string)

type DiskMonitorOptions struct {
	ShadowDir      string
	MaxRepoSize    int64
	CheckInterval  time.Duration
	MaxFileLimitMB int64
	OnWarning      WarningHandler
}

type DiskMonitor struct {
	opts              DiskMonitorOptions
	mu                sync.RWMutex
	isDegraded        bool
	stopChan          chan struct{}
	wg                sync.WaitGroup
	maxFileLimitBytes int64
}

func NewDiskMonitor(opts DiskMonitorOptions) *DiskMonitor {
	if opts.CheckInterval <= 0 {
		opts.CheckInterval = 30 * time.Second
	}
	limitMB := opts.MaxFileLimitMB
	if limitMB <= 0 {
		limitMB = 10
	}
	return &DiskMonitor{
		opts:              opts,
		stopChan:          make(chan struct{}),
		maxFileLimitBytes: limitMB * 1024 * 1024,
	}
}

func (m *DiskMonitor) Start(ctx context.Context) {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ticker := time.NewTicker(m.opts.CheckInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-m.stopChan:
				return
			case <-ticker.C:
				m.CheckDiskUsage()
			}
		}
	}()
}

func (m *DiskMonitor) Stop() {
	m.mu.Lock()
	select {
	case <-m.stopChan:
		// Already stopped
	default:
		close(m.stopChan)
	}
	m.mu.Unlock()
	m.wg.Wait()
}

func (m *DiskMonitor) IsDegraded() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.isDegraded
}

func (m *DiskMonitor) ShouldSkipFile(fileSizeBytes int64) bool {
	m.mu.RLock()
	degraded := m.isDegraded
	m.mu.RUnlock()

	if !degraded {
		return false
	}
	return fileSizeBytes > m.maxFileLimitBytes
}

func (m *DiskMonitor) CheckDiskUsage() (int64, bool) {
	if m.opts.ShadowDir == "" {
		return 0, false
	}

	var totalSize int64
	_ = filepath.WalkDir(m.opts.ShadowDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			info, err := d.Info()
			if err == nil {
				totalSize += info.Size()
			}
		}
		return nil
	})

	m.mu.Lock()
	defer m.mu.Unlock()

	if totalSize > m.opts.MaxRepoSize && !m.isDegraded {
		m.isDegraded = true
		if m.opts.OnWarning != nil {
			m.opts.OnWarning("REPO_SIZE_EXCEEDED", "Repository grew beyond threshold during session — large files are no longer tracked")
		}
	}

	return totalSize, m.isDegraded
}
