// Package tool implements the MCP tool handlers.
// It is the only bridge between the MCP surface and the model, and enforces
// the contract: redaction, size/rate limits, output validation, caching.
package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/raflyritonga/terra-drift/internal/contract"
	"github.com/raflyritonga/terra-drift/internal/model"
	"github.com/raflyritonga/terra-drift/internal/prompt"
	"github.com/raflyritonga/terra-drift/internal/redact"
	"github.com/raflyritonga/terra-drift/internal/serverconfig"
)

var reqID atomic.Int64

type Handler struct {
	Model    model.Model
	Limits   serverconfig.Limits
	Provider string
	ModelID  string
	Cache    *Cache
	Metrics  *Metrics
	limiter  *limiter
}

// NewHandler wires the production handler; zero-value Handler{Model: m}
// (used in tests) runs with no limits, cache, or metrics.
func NewHandler(m model.Model, cfg serverconfig.Config) *Handler {
	return &Handler{
		Model:    m,
		Limits:   cfg.Limits,
		Provider: cfg.Model.Provider,
		ModelID:  cfg.Model.ID,
		Cache:    NewCache(time.Duration(cfg.Limits.CacheTTLMinutes) * time.Minute),
		Metrics:  &Metrics{},
		limiter:  newLimiter(cfg.Limits.RatePerMinute),
	}
}

// ProposeHclEdits prompts the model and returns strictly-parsed, validated,
// minimal structured edits. Live values are redacted before the model call.
func (h *Handler) ProposeHclEdits(ctx context.Context, _ *mcp.CallToolRequest, in contract.ProposalInput) (*mcp.CallToolResult, contract.ProposalOutput, error) {
	var out contract.ProposalOutput
	id := reqID.Add(1)
	start := time.Now()
	logOutcome := func(outcome string, tokens int, cacheHit bool) {
		if h.Metrics != nil {
			h.Metrics.Tokens.Add(int64(tokens))
			if outcome != "ok" {
				h.Metrics.Errors.Add(1)
			}
			if cacheHit {
				h.Metrics.CacheHits.Add(1)
			}
		}
		slog.Info("propose_hcl_edits",
			"request_id", id, "address", in.Drift.Address, "attribute", in.Drift.Attribute,
			"outcome", outcome, "cache_hit", cacheHit, "tokens", tokens,
			"latency_ms", time.Since(start).Milliseconds())
	}
	if h.Metrics != nil {
		h.Metrics.Requests.Add(1)
	}

	if err := h.gate(in.ContractVersion); err != nil {
		logOutcome(errCode(err), 0, false)
		return nil, out, err
	}

	key := CacheKey(in, h.Provider, h.ModelID)
	if h.Cache != nil {
		if cached, ok := h.Cache.Get(key); ok {
			logOutcome("ok", 0, true)
			return nil, cached, nil
		}
	}

	sys, payload, err := prompt.Build(in)
	if err != nil {
		logOutcome(ErrInvalidOutput, 0, false)
		return nil, out, fmt.Errorf("%s: build prompt: %w", ErrInvalidOutput, err)
	}
	if h.Limits.MaxPromptBytes > 0 && len(payload) > h.Limits.MaxPromptBytes {
		logOutcome(ErrBudgetExceeded, 0, false)
		return nil, out, fmt.Errorf("%s: prompt is %d bytes (max %d)", ErrBudgetExceeded, len(payload), h.Limits.MaxPromptBytes)
	}
	if h.Limits.RequestTimeoutS > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(h.Limits.RequestTimeoutS)*time.Second)
		defer cancel()
	}

	// S2: sensitive live values never reach the model.
	red := redact.New()
	sys, payload = red.Redact(sys), red.Redact(payload)

	totalTokens := 0
	var lastErr error
	for attempt := 0; attempt <= h.Limits.ValidateRetryMax; attempt++ {
		reply, tokens, err := h.Model.Complete(ctx, sys, payload)
		totalTokens += tokens
		if err != nil {
			logOutcome(ErrGatewayDown, totalTokens, false)
			return nil, out, fmt.Errorf("%s: %w", ErrGatewayDown, err)
		}
		out, err = ParseStrict(red.Restore(reply))
		if err == nil {
			err = ValidateAllowed(out, in.AllowedAttrs)
		}
		if err == nil {
			if h.Cache != nil {
				h.Cache.Put(key, out)
			}
			logOutcome("ok", totalTokens, false)
			return nil, out, nil
		}
		lastErr = err
		payload += "\n\nYour previous reply was rejected: " + err.Error() + ". Reply again, correctly."
	}
	logOutcome(ErrInvalidOutput, totalTokens, false)
	return nil, contract.ProposalOutput{}, fmt.Errorf("%s: %v", ErrInvalidOutput, lastErr)
}

// gate applies the checks shared by both tools: contract version + rate limit.
func (h *Handler) gate(clientVersion string) error {
	if clientVersion != "" && major(clientVersion) != major(contract.Version) {
		return fmt.Errorf("%s: client speaks contract %s, server speaks %s", ErrContractMismatch, clientVersion, contract.Version)
	}
	if h.limiter != nil && !h.limiter.allow() {
		return fmt.Errorf("%s: more than %d requests in the last minute", ErrRateLimited, h.Limits.RatePerMinute)
	}
	return nil
}

func major(v string) string {
	if i := strings.Index(v, "."); i >= 0 {
		return v[:i]
	}
	return v
}

func errCode(err error) string {
	s := err.Error()
	if i := strings.Index(s, ":"); i > 0 {
		return s[:i]
	}
	return "error"
}

// ExplainDrift is the read path: the model describes the drift and its risk.
// Plain-text reply, length-capped; it never proposes or applies edits.
func (h *Handler) ExplainDrift(ctx context.Context, _ *mcp.CallToolRequest, in contract.ExplainInput) (*mcp.CallToolResult, contract.ExplainOutput, error) {
	var out contract.ExplainOutput
	if len(in.Drifts) == 0 {
		return nil, out, fmt.Errorf("no drifts to explain")
	}
	if err := h.gate(""); err != nil {
		return nil, out, err
	}

	sys, payload, err := prompt.BuildExplain(in)
	if err != nil {
		return nil, out, fmt.Errorf("build prompt: %w", err)
	}
	if h.Limits.MaxPromptBytes > 0 && len(payload) > h.Limits.MaxPromptBytes {
		return nil, out, fmt.Errorf("%s: prompt is %d bytes (max %d)", ErrBudgetExceeded, len(payload), h.Limits.MaxPromptBytes)
	}
	if h.Limits.RequestTimeoutS > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(h.Limits.RequestTimeoutS)*time.Second)
		defer cancel()
	}

	red := redact.New()
	reply, tokens, err := h.Model.Complete(ctx, red.Redact(sys), red.Redact(payload))
	if err != nil {
		return nil, out, fmt.Errorf("%s: %w", ErrGatewayDown, err)
	}
	if h.Metrics != nil {
		h.Metrics.Tokens.Add(int64(tokens))
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
