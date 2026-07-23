package model

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/raflyritonga/terra-drift/internal/contract"
)

// MockModel returns deterministic structured edits so the full two-binary
// pipeline is testable end-to-end with no cloud, no keys, no real model.
type MockModel struct{}

// Complete answers both tools deterministically: explain payloads get a canned
// summary; proposal payloads get an edit at the first provenance link.
func (MockModel) Complete(_ context.Context, _ string, userPayload string) (string, int, error) {
	var ex contract.ExplainInput
	if err := json.Unmarshal([]byte(userPayload), &ex); err == nil && len(ex.Drifts) > 0 {
		return fmt.Sprintf("mock: %d drifted attribute(s); review the values before trusting live.", len(ex.Drifts)), 0, nil
	}

	var in contract.ProposalInput
	if err := json.Unmarshal([]byte(userPayload), &in); err != nil {
		return "", 0, fmt.Errorf("mock model: unparseable payload: %w", err)
	}
	if len(in.Provenance) == 0 {
		return "", 0, fmt.Errorf("mock model: proposal has no provenance chain")
	}

	out := contract.ProposalOutput{
		Edits: []contract.EditOut{{
			File:      in.Provenance[0].File,
			BlockAddr: localAddr(in.Drift.Address),
			Attribute: in.Drift.Attribute,
			Op:        contract.OpSet,
			Value:     in.Drift.After,
		}},
		Rationale: "mock: set the drifted attribute to its live value at the resource block",
	}
	raw, err := json.Marshal(out)
	return string(raw), 0, err
}

// localAddr strips module prefixes and index keys: module.n.aws_x.y["k"] → aws_x.y
func localAddr(address string) string {
	if i := strings.Index(address, "["); i >= 0 {
		address = address[:i]
	}
	segs := strings.Split(address, ".")
	for len(segs) > 2 && segs[0] == "module" {
		segs = segs[2:]
	}
	return strings.Join(segs, ".")
}
