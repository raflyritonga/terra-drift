// Package prompt builds the model prompt from a proposal input.
// The safety rules travel inside the prompt so every backend sees them.
package prompt

import (
	"encoding/json"
	"strings"

	"github.com/raflyritonga/terra-drift/internal/contract"
)

const system = `You reconcile Terraform drift. Given a drifted attribute, its provenance
chain, and file excerpts, respond with ONLY a JSON object:
{"edits":[{"file":"...","block_addr":"...","attribute":"...","op":"set|append_to|ignore","value":<json>}],"rationale":"..."}
Never return file contents, prose, or markdown. Prefer editing the highest
provenance link (tfvars > module arg > module internals).`

func Build(in contract.ProposalInput) (string, string, error) {
	payload, err := json.Marshal(in)
	if err != nil {
		return "", "", err
	}
	sys := system
	if len(in.SafetyRules) > 0 {
		sys += "\nHard rules:\n- " + strings.Join(in.SafetyRules, "\n- ")
	}
	return sys, string(payload), nil
}
