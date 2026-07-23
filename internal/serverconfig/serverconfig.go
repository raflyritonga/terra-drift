// Package serverconfig loads terra-drift-mcp settings from yaml and env.
// Credentials come from env only — never from the file, never from the client.
package serverconfig

import (
	"fmt"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Transport string       `yaml:"transport"` // stdio | http
	Listen    string       `yaml:"listen"`    // http only
	Model     ModelConfig  `yaml:"model"`
	Secret    SecretConfig `yaml:"secret"`
	Limits    Limits       `yaml:"limits"`
	// AuthToken guards the http transport (Bearer). From env
	// TERRA_DRIFT_MCP_AUTH_TOKEN only — never from this file.
	AuthToken string `yaml:"-"`
}

type ModelConfig struct {
	Provider string `yaml:"provider"` // openai | anthropic | mock
	ID       string `yaml:"id"`
	BaseURL  string `yaml:"base_url"`
}

type SecretConfig struct {
	Source string `yaml:"source"` // env (default) | aws-secrets-manager
	Ref    string `yaml:"ref"`    // secret id/ARN, for a secret manager
}

// Limits are the cost/abuse controls. Zero values fall back to defaults.
type Limits struct {
	MaxPromptBytes   int `yaml:"max_prompt_bytes"`  // reject larger requests
	RequestTimeoutS  int `yaml:"request_timeout_s"` // per-request model deadline
	RatePerMinute    int `yaml:"rate_per_minute"`   // tool calls per minute
	CacheTTLMinutes  int `yaml:"cache_ttl_minutes"` // proposal cache lifetime
	ValidateRetryMax int `yaml:"validate_retry_max"`
}

func Default() Config {
	return Config{
		Transport: "stdio",
		Listen:    ":8080",
		Model:     ModelConfig{Provider: "mock"},
		Secret:    SecretConfig{Source: "env"},
		Limits: Limits{
			MaxPromptBytes:   64 * 1024,
			RequestTimeoutS:  60,
			RatePerMinute:    30,
			CacheTTLMinutes:  24 * 60,
			ValidateRetryMax: 1,
		},
	}
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
	if v := os.Getenv("TERRA_DRIFT_MCP_SECRET_SOURCE"); v != "" {
		cfg.Secret.Source = v
	}
	if v := os.Getenv("TERRA_DRIFT_MCP_SECRET_REF"); v != "" {
		cfg.Secret.Ref = v
	}
	if v := os.Getenv("TERRA_DRIFT_MCP_RATE_PER_MINUTE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Limits.RatePerMinute = n
		}
	}
	cfg.AuthToken = os.Getenv("TERRA_DRIFT_MCP_AUTH_TOKEN")

	d := Default()
	if cfg.Limits.MaxPromptBytes <= 0 {
		cfg.Limits.MaxPromptBytes = d.Limits.MaxPromptBytes
	}
	if cfg.Limits.RequestTimeoutS <= 0 {
		cfg.Limits.RequestTimeoutS = d.Limits.RequestTimeoutS
	}
	if cfg.Limits.RatePerMinute <= 0 {
		cfg.Limits.RatePerMinute = d.Limits.RatePerMinute
	}
	if cfg.Limits.CacheTTLMinutes <= 0 {
		cfg.Limits.CacheTTLMinutes = d.Limits.CacheTTLMinutes
	}
	if cfg.Limits.ValidateRetryMax < 0 {
		cfg.Limits.ValidateRetryMax = d.Limits.ValidateRetryMax
	}
	if cfg.Transport != "stdio" && cfg.Transport != "http" {
		return cfg, fmt.Errorf("transport must be stdio or http, got %q", cfg.Transport)
	}
	return cfg, nil
}
