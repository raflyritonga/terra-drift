package main

import (
	"context"
	"flag"
	"fmt"
	"os/exec"

	"github.com/raflyritonga/terra-drift/internal/config"
	"github.com/raflyritonga/terra-drift/internal/gitops"
	"github.com/raflyritonga/terra-drift/internal/tf"
)

func runDoctor(args []string) (int, error) {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	dir := fs.String("dir", ".", "Terraform root directory")
	fs.Parse(args)

	ctx := context.Background()
	failed := false
	report := func(name string, err error) {
		if err != nil {
			failed = true
			fmt.Printf("  ✗ %-16s %v\n", name, err)
		} else {
			fmt.Printf("  ✓ %-16s ok\n", name)
		}
	}

	runner := tf.New(*dir)
	if v, err := runner.Version(ctx); err != nil {
		report("terraform", fmt.Errorf("not found on PATH: %w", err))
	} else {
		report("terraform", nil)
		fmt.Printf("    %s\n", v)
	}

	if !tf.IsRoot(*dir) {
		report("tf root", fmt.Errorf("no .tf files in %s", *dir))
	} else {
		report("tf root", nil)
	}

	if !gitops.New(*dir).IsRepo(ctx) {
		report("git repo", fmt.Errorf("%s is not inside a git repository", *dir))
	} else {
		report("git repo", nil)
	}

	cfg, err := config.Load(*dir)
	report("config", err)

	if _, err := exec.LookPath(cfg.MCP.ServerBin); err != nil && cfg.MCP.Transport == "stdio" {
		report("mcp server", fmt.Errorf("%q not found on PATH (tier-2 drift will degrade to tier 3)", cfg.MCP.ServerBin))
	} else {
		report("mcp server", nil)
	}

	if cfg.Git.OpenPR {
		_, err := gitops.NewForge(cfg.Git)
		report("pr provider", err)
	}

	report("state", runner.StateReachable(ctx))

	if failed {
		return 1, nil
	}
	fmt.Println("all checks passed")
	return 0, nil
}
