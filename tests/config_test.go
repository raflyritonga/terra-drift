package tests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/raflyritonga/terra-drift/internal/config"
	"github.com/raflyritonga/terra-drift/internal/serverconfig"
)

func TestClientConfigDefaults(t *testing.T) {
	cfg, err := config.Load(t.TempDir()) // no file → defaults
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Git.Provider != "bitbucket" || cfg.Git.TargetBranch != "main" {
		t.Fatalf("git defaults wrong: %+v", cfg.Git)
	}
	if cfg.MCP.Transport != "stdio" || cfg.MCP.MaxRetries != 2 {
		t.Fatalf("mcp defaults wrong: %+v", cfg.MCP)
	}
}

func TestClientConfigLoadsFileOverDefaults(t *testing.T) {
	dir := t.TempDir()
	yaml := "mcp:\n  transport: http\n  url: http://x:8080\ngit:\n  provider: github\n  workspace: acme\n  repo: infra\n"
	os.WriteFile(filepath.Join(dir, config.FileName), []byte(yaml), 0o644)

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MCP.Transport != "http" || cfg.Git.Provider != "github" || cfg.Git.Repo != "infra" {
		t.Fatalf("file did not override defaults: %+v", cfg)
	}
}

func TestClientConfigRejectsBadTransport(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, config.FileName), []byte("mcp:\n  transport: carrier-pigeon\n"), 0o644)
	if _, err := config.Load(dir); err == nil {
		t.Fatal("expected error for invalid mcp.transport")
	}
}

func TestServerConfigEnvOverrides(t *testing.T) {
	t.Setenv("TERRA_DRIFT_MCP_TRANSPORT", "http")
	t.Setenv("TERRA_DRIFT_MCP_MODEL_PROVIDER", "openai-compatible")
	t.Setenv("TERRA_DRIFT_MCP_MODEL_BASE_URL", "https://x.llm.com/v1")

	cfg, err := serverconfig.Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Transport != "http" || cfg.Model.Provider != "openai-compatible" || cfg.Model.BaseURL == "" {
		t.Fatalf("env did not override: %+v", cfg)
	}
}
