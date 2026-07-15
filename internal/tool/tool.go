// Package tool implements the propose_hcl_edits MCP tool handler.
// It is the only bridge between the MCP surface and the model.
package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/raflyritonga/terra-drift/internal/contract"
	"github.com/raflyritonga/terra-drift/internal/model"
	"github.com/raflyritonga/terra-drift/internal/prompt"
)

type Handler struct {
	Model model.Model
}

// ProposeHclEdits prompts the model and returns strictly-parsed structured edits.
func (h *Handler) ProposeHclEdits(ctx context.Context, _ *mcp.CallToolRequest, in contract.ProposalInput) (*mcp.CallToolResult, contract.ProposalOutput, error) {
	var out contract.ProposalOutput

	sys, payload, err := prompt.Build(in)
	if err != nil {
		return nil, out, fmt.Errorf("build prompt: %w", err)
	}

	reply, err := h.Model.Complete(ctx, sys, payload)
	if err != nil {
		return nil, out, fmt.Errorf("model: %w", err)
	}

	out, err = ParseStrict(reply)
	if err != nil {
		return nil, contract.ProposalOutput{}, fmt.Errorf("model returned unusable output: %w", err)
	}
	return nil, out, nil
}

// ParseStrict rejects anything that is not a pure structured-edits JSON object.
func ParseStrict(reply string) (contract.ProposalOutput, error) {
	var out contract.ProposalOutput
	dec := json.NewDecoder(strings.NewReader(reply))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return out, fmt.Errorf("not structured edits: %w", err)
	}
	if dec.More() {
		return out, fmt.Errorf("trailing content after the JSON object")
	}
	if len(out.Edits) == 0 {
		return out, fmt.Errorf("no edits proposed")
	}
	for i, e := range out.Edits {
		switch e.Op {
		case contract.OpSet, contract.OpAppendTo, contract.OpIgnore:
		default:
			return out, fmt.Errorf("edit %d: invalid op %q", i, e.Op)
		}
		if e.Op != contract.OpIgnore && (e.File == "" || e.Attribute == "") {
			return out, fmt.Errorf("edit %d: missing file or attribute", i)
		}
	}
	return out, nil
}
