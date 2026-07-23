package tests

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/raflyritonga/terra-drift/internal/contract"
	"github.com/raflyritonga/terra-drift/internal/patch"
	"github.com/raflyritonga/terra-drift/internal/provenance"
	"github.com/raflyritonga/terra-drift/internal/tf"
)

func sgProvenance() provenance.Provenance {
	return provenance.Provenance{
		Tier: provenance.Tier2Transforming,
		Chain: []provenance.ChainLink{
			{Kind: "resource_attr", File: "main.tf", Expr: "aws_security_group.web.cidr_blocks", Line: 1},
			{Kind: "tfvars", File: "terraform.tfvars", Expr: "office_cidrs", Line: 1},
		},
		Target: provenance.EditTarget{File: "terraform.tfvars", Attribute: "office_cidrs"},
	}
}

// C2: only files on the provenance chain and only drifted/origin attrs may change.
func TestMinimalDiffGuard(t *testing.T) {
	allowed := patch.AllowedFor("cidr_blocks", sgProvenance())

	ok := contract.Edit{File: "terraform.tfvars", Attribute: "office_cidrs", Op: contract.OpSet, Value: json.RawMessage(`[]`)}
	if err := patch.GuardMinimal(ok, allowed); err != nil {
		t.Fatalf("legitimate origin edit rejected: %v", err)
	}
	ok2 := contract.Edit{File: "main.tf", Attribute: "cidr_blocks", Op: contract.OpSet, Value: json.RawMessage(`[]`)}
	if err := patch.GuardMinimal(ok2, allowed); err != nil {
		t.Fatalf("drifted-attribute edit rejected: %v", err)
	}

	wrongAttr := contract.Edit{File: "main.tf", Attribute: "description", Op: contract.OpSet, Value: json.RawMessage(`"x"`)}
	if err := patch.GuardMinimal(wrongAttr, allowed); err == nil {
		t.Fatal("edit to an undrifted attribute must be rejected")
	}
	wrongFile := contract.Edit{File: "unrelated.tf", Attribute: "cidr_blocks", Op: contract.OpSet, Value: json.RawMessage(`[]`)}
	if err := patch.GuardMinimal(wrongFile, allowed); err == nil {
		t.Fatal("edit to a file off the provenance chain must be rejected")
	}
	ignore := contract.Edit{Op: contract.OpIgnore}
	if err := patch.GuardMinimal(ignore, allowed); err != nil {
		t.Fatalf("ignore op must pass: %v", err)
	}
}

// C3: the drift hash is stable across runs and changes with content.
func TestDriftHashStable(t *testing.T) {
	h1, err := loadPlan(t, "drift_module_arg.json").Hash()
	if err != nil {
		t.Fatal(err)
	}
	h2, _ := loadPlan(t, "drift_module_arg.json").Hash()
	if h1 == "" || h1 != h2 {
		t.Fatalf("hash not stable: %q vs %q", h1, h2)
	}
	other, _ := loadPlan(t, "drift_literal.json").Hash()
	if other == h1 {
		t.Fatal("different drift sets must hash differently")
	}
}

// C4: refresh failures classify into actionable skip reasons.
func TestRefreshErrorClassification(t *testing.T) {
	cases := map[string]string{
		"Error: AccessDenied: User is not authorized to perform s3:GetObject": tf.ReasonNoIdentityPolicy,
		"api error Forbidden: Forbidden":                                      tf.ReasonNoIdentityPolicy,
		"explicit deny in a permissions boundary":                             tf.ReasonBoundaryDenied,
		"error calling sts:AssumeRole: AccessDenied":                          tf.ReasonTrustDenied,
		"connection reset by peer":                                            tf.ReasonProviderError,
	}
	for stderr, want := range cases {
		if got := tf.Classify(stderr).Reason; got != want {
			t.Errorf("%q → %s, want %s", stderr, got, want)
		}
	}
}

// S1 client half: excerpts are minimal block snippets, not whole files.
func TestBlockSnippetIsMinimal(t *testing.T) {
	snip := provenance.BlockSnippet("../testdata/hcl/literal", "main.tf", 2)
	if snip == "" {
		t.Fatal("no snippet extracted")
	}
	if !strings.Contains(snip, `resource "aws_security_group" "web"`) || !strings.Contains(snip, "description") {
		t.Fatalf("snippet missing the block: %q", snip)
	}
	whole, err := os.ReadFile("../testdata/hcl/literal/main.tf")
	if err != nil {
		t.Fatal(err)
	}
	if len(snip) >= len(whole) {
		t.Fatal("snippet is not smaller than the whole file")
	}
}
