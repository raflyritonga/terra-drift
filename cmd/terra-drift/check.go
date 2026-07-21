package main

import (
	"context"
	"flag"

	"github.com/raflyritonga/terra-drift/internal/drift"
	"github.com/raflyritonga/terra-drift/internal/tf"
)

// runCheck detects drift only — the read-only pre-apply gate.
func runCheck(args []string) (int, error) {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	dir := fs.String("dir", ".", "Terraform root directory")
	out := fs.String("out", "", "write the plan JSON to this file")
	explain := fs.Bool("explain", false, "ask the model server for a short explanation of the drift")
	fs.Parse(args)

	ctx := context.Background()
	_, planJSON, err := tf.New(*dir).RefreshPlan(ctx)
	if err != nil {
		return drift.ExitError, err
	}
	code, err := drift.Summarize(planJSON, *out, *dir)
	if code == drift.ExitDrift && *explain {
		printExplanation(ctx, *dir, planJSON)
	}
	return code, err
}
