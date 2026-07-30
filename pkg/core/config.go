package core

import "time"

type Config struct {
	Port              int           `mapstructure:"port"`
	Path              string        `mapstructure:"path"`
	Interval          time.Duration `mapstructure:"interval"`
	ClaimTTL          time.Duration `mapstructure:"claim-ttl"`
	BufferSize        int           `mapstructure:"buffer-size"`
	Host              string        `mapstructure:"host"`
	LogLevel          string        `mapstructure:"log-level"`
	MaxRepoSizeStr    string        `mapstructure:"max-repo-size"`
	MaxRepoSizeBytes  int64         `mapstructure:"-"`
	DiskCheckInterval time.Duration `mapstructure:"disk-check-interval"`
	APIToken          string        `mapstructure:"api-token"`
	// ProjectsRoot, when set, enables multi-project mode: directories directly
	// under it that contain a .git entry are discovered and made available at
	// /p/{id}/... alongside the always-watched root project at Path.
	ProjectsRoot string `mapstructure:"projects-root"`
	// Projects is a comma-separated allowlist of directory names to keep from
	// ProjectsRoot; empty means "watch everything found there".
	Projects string `mapstructure:"projects"`
}
