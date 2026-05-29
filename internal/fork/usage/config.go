package usage

import (
	"path/filepath"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

const (
	defaultSQLitePath            = "usage.sqlite3"
	defaultMaxProviderErrorBytes = 8 * 1024
)

// Config controls the fork-owned persistent usage statistics feature.
type Config struct {
	Enabled               bool              `yaml:"enabled" json:"enabled"`
	SQLitePath            string            `yaml:"sqlite-path" json:"sqlite-path"`
	ProviderLabels        map[string]string `yaml:"provider-labels,omitempty" json:"provider-labels,omitempty"`
	MaxProviderErrorBytes int               `yaml:"max-provider-error-bytes" json:"max-provider-error-bytes"`
	ManagementPanel       ManagementPanel   `yaml:"management-panel,omitempty" json:"management-panel,omitempty"`
}

// ManagementPanel contains usage-related management panel routing options.
type ManagementPanel struct {
	RootRedirect bool `yaml:"root-redirect,omitempty" json:"root-redirect,omitempty"`
}

func normalizeConfig(cfg Config) Config {
	cfg.SQLitePath = strings.TrimSpace(cfg.SQLitePath)
	if cfg.SQLitePath == "" {
		cfg.SQLitePath = defaultSQLitePath
	}
	if cfg.MaxProviderErrorBytes <= 0 {
		cfg.MaxProviderErrorBytes = defaultMaxProviderErrorBytes
	}
	if cfg.ProviderLabels != nil {
		labels := make(map[string]string, len(cfg.ProviderLabels))
		for key, value := range cfg.ProviderLabels {
			key = strings.ToLower(strings.TrimSpace(key))
			value = strings.TrimSpace(value)
			if key == "" || value == "" {
				continue
			}
			labels[key] = value
		}
		if len(labels) > 0 {
			cfg.ProviderLabels = labels
		} else {
			cfg.ProviderLabels = nil
		}
	}
	return cfg
}

func FromAppConfig(cfg *config.Config) Config {
	if cfg == nil {
		return normalizeConfig(Config{})
	}
	return normalizeConfig(Config{
		Enabled:               cfg.Usage.Enabled,
		SQLitePath:            cfg.Usage.SQLitePath,
		ProviderLabels:        cfg.Usage.ProviderLabels,
		MaxProviderErrorBytes: cfg.Usage.MaxProviderErrorBytes,
		ManagementPanel: ManagementPanel{
			RootRedirect: cfg.Usage.ManagementPanel.RootRedirect,
		},
	})
}

func ResolveSQLitePath(pathValue, configFilePath string) string {
	pathValue = strings.TrimSpace(pathValue)
	if pathValue == "" {
		pathValue = defaultSQLitePath
	}
	if pathValue == ":memory:" || filepath.IsAbs(pathValue) {
		return pathValue
	}
	if configFilePath != "" {
		if dir := filepath.Dir(configFilePath); dir != "" && dir != "." {
			return filepath.Join(dir, pathValue)
		}
	}
	return pathValue
}
