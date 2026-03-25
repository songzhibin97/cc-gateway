package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/songzhibin97/cc-gateway/internal/domain"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig          `yaml:"server"`
	Database DatabaseConfig        `yaml:"database"`
	Log      LogConfig             `yaml:"log"`
	Pricing  []domain.ModelPricing `yaml:"pricing"`
}

type ServerConfig struct {
	Listen       string `yaml:"listen"`
	AdminListen  string `yaml:"admin_listen"`
	ReadTimeout  string `yaml:"read_timeout"`
	WriteTimeout string `yaml:"write_timeout"`
	IdleTimeout  string `yaml:"idle_timeout"`
}

type DatabaseConfig struct {
	Path string `yaml:"path"`
}

type LogConfig struct {
	Level                string `yaml:"level"`
	PayloadRetentionDays int    `yaml:"payload_retention_days"`
	CleanupInterval      string `yaml:"cleanup_interval"`
}

var envVarRegex = regexp.MustCompile(`\$\{([^}]+)\}`)

// Load reads and parses a YAML config file, expanding ${ENV_VAR} references.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	expanded := envVarRegex.ReplaceAllStringFunc(string(data), func(match string) string {
		varName := strings.TrimSuffix(strings.TrimPrefix(match, "${"), "}")
		if val, ok := os.LookupEnv(varName); ok {
			return val
		}
		return match
	})

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if cfg.Server.Listen == "" {
		cfg.Server.Listen = ":8080"
	}
	if cfg.Server.AdminListen == "" {
		cfg.Server.AdminListen = ":8081"
	}
	if cfg.Server.ReadTimeout == "" {
		cfg.Server.ReadTimeout = "30s"
	}
	if cfg.Server.WriteTimeout == "" {
		cfg.Server.WriteTimeout = "300s"
	}
	if cfg.Server.IdleTimeout == "" {
		cfg.Server.IdleTimeout = "120s"
	}
	if cfg.Database.Path == "" {
		cfg.Database.Path = "./data/gateway.db"
	}
	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}
	if cfg.Log.PayloadRetentionDays == 0 {
		cfg.Log.PayloadRetentionDays = 7
	}
	if cfg.Log.CleanupInterval == "" {
		cfg.Log.CleanupInterval = "1h"
	}

	return &cfg, nil
}
