package main

import (
	"context"
	"fmt"
	"os"

	"github.com/raflyritonga/terra-drift/internal/config"
	"github.com/raflyritonga/terra-drift/internal/contract"
	"github.com/raflyritonga/terra-drift/internal/drift"
	"github.com/raflyritonga/terra-drift/internal/mcpclient"
	"github.com/raflyritonga/terra-drift/internal/provenance"
)

// printExplanation asks the model server to describe the drift (read-only).
// Failures are non-fatal: the report already printed, so just note and go on.
func printExplanation(ctx context.Context, dir string, planJSON []byte) {
	cfg, err := config.Load(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "explain: skipped:", err)
		return
	}
	p, err := drift.ParsePlan(planJSON)
	if err != nil {
		fmt.Fprintln(os.Stderr, "explain: skipped:", err)
		return
	}

	var in contract.ExplainInput
	for _, r := range p.ResourceDrift {
		file, line := provenance.Locate(p.Configuration, r.Address, dir)
		if r.Deleted() {
			in.Drifts = append(in.Drifts, contract.DriftFact{Address: r.Address, File: file, Line: line})
			continue
		}
		attrs, err := r.ChangedAttrs()
		if err != nil {
			continue
		}
		for _, a := range attrs {
			in.Drifts = append(in.Drifts, contract.DriftFact{
				Address: r.Address, Attribute: a.Attribute,
				File: file, Line: line,
				Before: a.Before, After: a.After,
			})
		}
	}
	if len(in.Drifts) == 0 {
		return
	}

	out, err := mcpclient.New(cfg.MCP, version).Explain(ctx, in)
	if err != nil {
		fmt.Fprintln(os.Stderr, "explain: unavailable:", err)
		return
	}
	fmt.Println("\nexplanation:")
	fmt.Println("  " + out.Summary)
}
