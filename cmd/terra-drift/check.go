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
	fs.Parse(args)

	_, planJSON, err := tf.New(*dir).RefreshPlan(context.Background())
	if err != nil {
		return drift.ExitError, err
	}
	return drift.Summarize(planJSON, *out)
}
