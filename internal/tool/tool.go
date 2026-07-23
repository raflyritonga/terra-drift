// Package tool implements the MCP tool handlers.
// It is the only bridge between the MCP surface and the model, and enforces
// the contract: redaction, output validation, bounded retry.
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
	"github.com/raflyritonga/terra-drift/internal/redact"
)

// Typed error codes the client can react to (fall back to report-only).
const (
	ErrGatewayDown      = "gateway-down"
	ErrInvalidOutput    = "invalid-output"
	ErrContractMismatch = "contract-mismatch"
)

// validateRetryMax bounds how often an invalid proposal is retried.
const validateRetryMax = 1

type Handler struct {
	Model model.Model
}

// ProposeHclEdits prompts the model and returns strictly-parsed, validated,
// minimal structured edits. Live values are redacted before the model call.
func (h *Handler) ProposeHclEdits(ctx context.Context, _ *mcp.CallToolRequest, in contract.ProposalInput) (*mcp.CallToolResult, contract.ProposalOutput, error) {
	var out contract.ProposalOutput
	if err := checkVersion(in.ContractVersion); err != nil {
		return nil, out, err
	}

	sys, payload, err := prompt.Build(in)
	if err != nil {
		return nil, out, fmt.Errorf("%s: build prompt: %w", ErrInvalidOutput, err)
	}

	// S2: sensitive live values never reach the model.
	red := redact.New()
	sys, payload = red.Redact(sys), red.Redact(payload)

	var lastErr error
	for attempt := 0; attempt <= validateRetryMax; attempt++ {
		reply, _, err := h.Model.Complete(ctx, sys, payload)
		if err != nil {
			return nil, out, fmt.Errorf("%s: %w", ErrGatewayDown, err)
		}
		out, err = ParseStrict(red.Restore(reply))
		if err == nil {
			err = ValidateAllowed(out, in.AllowedAttrs)
		}
		if err == nil {
			return nil, out, nil
		}
		lastErr = err
		payload += "\n\nYour previous reply was rejected: " + err.Error() + ". Reply again, correctly."
	}
	return nil, contract.ProposalOutput{}, fmt.Errorf("%s: %v", ErrInvalidOutput, lastErr)
}

// checkVersion refuses a contract-major mismatch before any model call.
func checkVersion(clientVersion string) error {
	if clientVersion != "" && major(clientVersion) != major(contract.Version) {
		return fmt.Errorf("%s: client speaks contract %s, server speaks %s", ErrContractMismatch, clientVersion, contract.Version)
	}
	return nil
}

func major(v string) string {
	if i := strings.Index(v, "."); i >= 0 {
		return v[:i]
	}
	return v
}

// ExplainDrift is the read path: the model describes the drift and its risk.
// Plain-text reply, length-capped; it never proposes or applies edits.
func (h *Handler) ExplainDrift(ctx context.Context, _ *mcp.CallToolRequest, in contract.ExplainInput) (*mcp.CallToolResult, contract.ExplainOutput, error) {
	var out contract.ExplainOutput
	if len(in.Drifts) == 0 {
		return nil, out, fmt.Errorf("no drifts to explain")
	}

	sys, payload, err := prompt.BuildExplain(in)
	if err != nil {
		return nil, out, fmt.Errorf("build prompt: %w", err)
	}

	red := redact.New()
	reply, _, err := h.Model.Complete(ctx, red.Redact(sys), red.Redact(payload))
	if err != nil {
		return nil, out, fmt.Errorf("%s: %w", ErrGatewayDown, err)
	}

	reply = strings.TrimSpace(red.Restore(reply))
	if reply == "" {
		return nil, out, fmt.Errorf("%s: model returned an empty explanation", ErrInvalidOutput)
	}
	if len(reply) > 1200 {
		reply = reply[:1200] + "…"
	}
	return nil, contract.ExplainOutput{Summary: reply}, nil
}

// ValidateAllowed rejects any edit whose attribute is outside the client's
// allowed set — the server-side half of the minimal-diff guarantee.
func ValidateAllowed(out contract.ProposalOutput, allowed []string) error {
	if len(allowed) == 0 {
		return nil
	}
	set := map[string]bool{}
	for _, a := range allowed {
		set[a] = true
	}
	for i, e := range out.Edits {
		if e.Op == contract.OpIgnore {
			continue
		}
		if !set[e.Attribute] {
			return fmt.Errorf("edit %d changes attribute %q, which is outside the allowed set", i, e.Attribute)
		}
	}
	return nil
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
