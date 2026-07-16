// Package config loads the client's .terra-drift.yaml from the Terraform root.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const FileName = ".terra-drift.yaml"

type Config struct {
	ProtectedPaths []string `yaml:"protected_paths"`
	MCP            MCP      `yaml:"mcp"`
	Git            Git      `yaml:"git"`
}

type MCP struct {
	Transport  string `yaml:"transport"`
	ServerBin  string `yaml:"server_bin"`
	URL        string `yaml:"url"`
	Tool       string `yaml:"tool"`
	MaxRetries int    `yaml:"max_retries"`
}

type Git struct {
	BranchPrefix string `yaml:"branch_prefix"`
	OpenPR       bool   `yaml:"open_pr"`
	Provider     string `yaml:"provider"`      // bitbucket (default) | github | gitlab
	Workspace    string `yaml:"workspace"`     // bitbucket workspace / github owner / gitlab group
	Repo         string `yaml:"repo"`          // repository slug/name
	TargetBranch string `yaml:"target_branch"` // PR base, default main
	APIBase      string `yaml:"api_base"`      // override for self-hosted servers
}

func Default() Config {
	return Config{
		ProtectedPaths: []string{"modules/**"},
		MCP: MCP{
			Transport:  "stdio",
			ServerBin:  "terra-drift-mcp",
			Tool:       "propose_hcl_edits",
			MaxRetries: 2,
		},
		Git: Git{
			BranchPrefix: "drift-sync/",
			OpenPR:       true,
			Provider:     "bitbucket",
			TargetBranch: "main",
		},
	}
}

// Load reads dir/.terra-drift.yaml over the defaults; a missing file is fine.
func Load(dir string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", FileName, err)
	}
	if cfg.MCP.Transport != "stdio" && cfg.MCP.Transport != "http" {
		return cfg, fmt.Errorf("mcp.transport must be stdio or http, got %q", cfg.MCP.Transport)
	}
	return cfg, nil
}
