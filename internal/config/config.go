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
	PushMode     string `yaml:"push_mode"`     // api (bitbucket default) | git
}

// ResolvedPushMode returns the effective transport for publishing the branch.
// Bitbucket defaults to "api" (Atlassian API tokens work for REST but are
// rejected by git-over-HTTPS); other providers default to "git".
func (g Git) ResolvedPushMode() (string, error) {
	switch g.PushMode {
	case "":
		if g.Provider == "bitbucket" || g.Provider == "" {
			return "api", nil
		}
		return "git", nil
	case "git":
		return "git", nil
	case "api":
		if g.Provider != "bitbucket" && g.Provider != "" {
			return "", fmt.Errorf("git.push_mode=api is only supported for the bitbucket provider (got %q)", g.Provider)
		}
		return "api", nil
	default:
		return "", fmt.Errorf("git.push_mode must be api or git, got %q", g.PushMode)
	}
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
