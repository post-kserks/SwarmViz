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
}
