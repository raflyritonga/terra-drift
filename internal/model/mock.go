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

// Complete parses the ProposalInput payload and proposes setting the drifted
// attribute to its live value at the first provenance link (the resource block).
func (MockModel) Complete(_ context.Context, _ string, userPayload string) (string, error) {
	var in contract.ProposalInput
	if err := json.Unmarshal([]byte(userPayload), &in); err != nil {
		return "", fmt.Errorf("mock model: unparseable payload: %w", err)
	}
	if len(in.Provenance) == 0 {
		return "", fmt.Errorf("mock model: proposal has no provenance chain")
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
	return string(raw), err
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
