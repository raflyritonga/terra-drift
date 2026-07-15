// Package serverconfig loads terra-drift-mcp settings from yaml and env.
// Credentials come from env only — never from the file, never from the client.
package serverconfig

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Transport string      `yaml:"transport"` // stdio (Model A) | http (Model B)
	Listen    string      `yaml:"listen"`    // Model B only
	Model     ModelConfig `yaml:"model"`
}

type ModelConfig struct {
	Provider string `yaml:"provider"` // phase 1: mock
	ID       string `yaml:"id"`
	BaseURL  string `yaml:"base_url"`
}

func Default() Config {
	return Config{Transport: "stdio", Listen: ":8080", Model: ModelConfig{Provider: "mock"}}
}

// Load reads the optional yaml file, then lets env vars override.
func Load(path string) (Config, error) {
	cfg := Default()
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return cfg, err
		}
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return cfg, fmt.Errorf("parse %s: %w", path, err)
		}
	}
	if v := os.Getenv("TERRA_DRIFT_MCP_TRANSPORT"); v != "" {
		cfg.Transport = v
	}
	if v := os.Getenv("TERRA_DRIFT_MCP_LISTEN"); v != "" {
		cfg.Listen = v
	}
	if v := os.Getenv("TERRA_DRIFT_MCP_MODEL_PROVIDER"); v != "" {
		cfg.Model.Provider = v
	}
	if v := os.Getenv("TERRA_DRIFT_MCP_MODEL_ID"); v != "" {
		cfg.Model.ID = v
	}
	if v := os.Getenv("TERRA_DRIFT_MCP_MODEL_BASE_URL"); v != "" {
		cfg.Model.BaseURL = v
	}
	if cfg.Transport != "stdio" && cfg.Transport != "http" {
		return cfg, fmt.Errorf("transport must be stdio or http, got %q", cfg.Transport)
	}
	return cfg, nil
}
