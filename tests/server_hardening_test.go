package tests

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/raflyritonga/terra-drift/internal/contract"
	"github.com/raflyritonga/terra-drift/internal/model"
	"github.com/raflyritonga/terra-drift/internal/redact"
	"github.com/raflyritonga/terra-drift/internal/serverconfig"
	"github.com/raflyritonga/terra-drift/internal/tool"
)

// S2: sensitive values are replaced before the model and restored after.
func TestRedactionRoundTrip(t *testing.T) {
	in := `cidrs = ["10.20.30.0/24", "203.0.113.7"], role = "arn:aws:iam::123456789012:role/deploy", key = "AKIAIOSFODNN7EXAMPLE", acct = 123456789012`
	r := redact.New()
	red := r.Redact(in)

	for _, leak := range []string{"10.20.30.0/24", "203.0.113.7", "arn:aws:iam::123456789012:role/deploy", "AKIAIOSFODNN7EXAMPLE", "123456789012"} {
		if strings.Contains(red, leak) {
			t.Errorf("redacted text still contains %q:\n%s", leak, red)
		}
	}
	if r.Count() == 0 || !strings.Contains(red, "__TD_REDACT_") {
		t.Fatalf("no placeholders produced:\n%s", red)
	}
	// a model reply that echoes placeholders restores byte-for-byte
	if got := r.Restore(red); got != in {
		t.Fatalf("round trip mismatch:\n got %q\nwant %q", got, in)
	}
	// the same value always maps to the same placeholder
	again := r.Redact(`x = "203.0.113.7"`)
	first := r.Redact(`y = "203.0.113.7"`)
	tok := strings.TrimPrefix(strings.SplitN(again, `"`, 3)[1], "")
	if !strings.Contains(first, tok) {
		t.Fatalf("placeholder not stable: %q vs %q", again, first)
	}
}

// S1: the server rejects edits outside the client's allowed set.
func TestValidateAllowed(t *testing.T) {
	out := contract.ProposalOutput{Edits: []contract.EditOut{
		{File: "a.tf", Attribute: "cidr_blocks", Op: contract.OpSet, Value: json.RawMessage(`[]`)},
	}}
	if err := tool.ValidateAllowed(out, []string{"cidr_blocks", "office_cidrs"}); err != nil {
		t.Fatalf("allowed edit rejected: %v", err)
	}
	bad := contract.ProposalOutput{Edits: []contract.EditOut{
		{File: "a.tf", Attribute: "description", Op: contract.OpSet, Value: json.RawMessage(`"x"`)},
	}}
	if err := tool.ValidateAllowed(bad, []string{"cidr_blocks"}); err == nil {
		t.Fatal("edit outside the allowed set must be rejected")
	}
	// no allowed list (older client) → no enforcement
	if err := tool.ValidateAllowed(bad, nil); err != nil {
		t.Fatalf("empty allowed set must not enforce: %v", err)
	}
}

// S1: an out-of-set proposal is retried (bounded), then fails typed.
func TestHandlerRetriesThenFailsTyped(t *testing.T) {
	h := &tool.Handler{
		Model:  rogueModel{},
		Limits: serverconfig.Limits{ValidateRetryMax: 1},
	}
	var in contract.ProposalInput
	in.ContractVersion = contract.Version
	in.Drift.Address = "aws_security_group.web"
	in.Drift.Attribute = "cidr_blocks"
	in.AllowedAttrs = []string{"cidr_blocks"}
	in.Provenance = []contract.ChainLinkDTO{{Kind: "resource_attr", File: "main.tf"}}

	_, _, err := h.ProposeHclEdits(context.Background(), nil, in)
	if err == nil {
		t.Fatal("rogue proposal must fail after bounded retries")
	}
	if !strings.HasPrefix(err.Error(), tool.ErrInvalidOutput) {
		t.Fatalf("want typed %s error, got: %v", tool.ErrInvalidOutput, err)
	}
}

// S9: a contract-major mismatch is refused before any model call.
func TestHandlerRejectsContractMismatch(t *testing.T) {
	h := &tool.Handler{Model: model.MockModel{}}
	var in contract.ProposalInput
	in.ContractVersion = "1.0"
	in.Provenance = []contract.ChainLinkDTO{{Kind: "resource_attr", File: "main.tf"}}
	_, _, err := h.ProposeHclEdits(context.Background(), nil, in)
	if err == nil || !strings.HasPrefix(err.Error(), tool.ErrContractMismatch) {
		t.Fatalf("want %s error, got: %v", tool.ErrContractMismatch, err)
	}
}

// S3: oversized prompts fail closed with a typed error.
func TestHandlerBudgetExceeded(t *testing.T) {
	h := &tool.Handler{Model: model.MockModel{}, Limits: serverconfig.Limits{MaxPromptBytes: 64}}
	var in contract.ProposalInput
	in.ContractVersion = contract.Version
	in.Drift.Address = strings.Repeat("aws_security_group.web.", 20)
	in.Provenance = []contract.ChainLinkDTO{{Kind: "resource_attr", File: "main.tf"}}
	_, _, err := h.ProposeHclEdits(context.Background(), nil, in)
	if err == nil || !strings.HasPrefix(err.Error(), tool.ErrBudgetExceeded) {
		t.Fatalf("want %s error, got: %v", tool.ErrBudgetExceeded, err)
	}
}

// S5: the cache key is stable, and sensitive to value/model/attr changes.
func TestCacheKey(t *testing.T) {
	var in contract.ProposalInput
	in.Drift.Address = "aws_security_group.web"
	in.Drift.Attribute = "cidr_blocks"
	in.Drift.After = json.RawMessage(`["10.0.0.0/8"]`)
	in.AllowedAttrs = []string{"b", "a"}

	k1 := tool.CacheKey(in, "openai", "gpt-4o-mini")
	k2 := tool.CacheKey(in, "openai", "gpt-4o-mini")
	if k1 != k2 {
		t.Fatal("cache key not stable")
	}
	// attr order must not matter
	in.AllowedAttrs = []string{"a", "b"}
	if tool.CacheKey(in, "openai", "gpt-4o-mini") != k1 {
		t.Fatal("allowed-attr order changed the key")
	}
	// different live value, model, or provider → different key
	in2 := in
	in2.Drift.After = json.RawMessage(`["10.0.0.0/16"]`)
	if tool.CacheKey(in2, "openai", "gpt-4o-mini") == k1 {
		t.Fatal("value change did not change the key")
	}
	if tool.CacheKey(in, "openai", "gpt-4o") == k1 || tool.CacheKey(in, "anthropic", "gpt-4o-mini") == k1 {
		t.Fatal("model/provider change did not change the key")
	}
}

// rogueModel always proposes an edit outside the allowed set.
type rogueModel struct{}

func (rogueModel) Complete(context.Context, string, string) (string, int, error) {
	return `{"edits":[{"file":"main.tf","attribute":"description","op":"set","value":"pwned"}],"rationale":"r"}`, 0, nil
}
