package tests

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/raflyritonga/terra-drift/internal/provenance"
)

func TestWalkLiteral(t *testing.T) {
	prov := walkFixture(t, "drift_literal.json", "literal", "description")
	if prov.Tier != provenance.Tier0Literal {
		t.Fatalf("tier = %v, note = %s", prov.Tier, prov.Note)
	}
	want := provenance.EditTarget{File: "main.tf", BlockAddr: "aws_security_group.web", Attribute: "description"}
	if prov.Target != want {
		t.Fatalf("target = %+v", prov.Target)
	}
}

func TestWalkVarToTfvars(t *testing.T) {
	prov := walkFixture(t, "drift_var_tfvars.json", "var_tfvars", "cidr_blocks")
	if prov.Tier != provenance.Tier1Passthrough {
		t.Fatalf("tier = %v, note = %s", prov.Tier, prov.Note)
	}
	want := provenance.EditTarget{File: "terraform.tfvars", BlockAddr: "", Attribute: "allowed_cidrs"}
	if prov.Target != want {
		t.Fatalf("target = %+v", prov.Target)
	}
	if kinds(prov) != "resource_attr→tfvars" {
		t.Fatalf("chain = %s", kinds(prov))
	}
}

func TestWalkModuleArgToRootVarToTfvars(t *testing.T) {
	prov := walkFixture(t, "drift_module_arg.json", "module_arg", "cidr_blocks")
	if prov.Tier != provenance.Tier1Passthrough {
		t.Fatalf("tier = %v, note = %s", prov.Tier, prov.Note)
	}
	want := provenance.EditTarget{File: "terraform.tfvars", BlockAddr: "", Attribute: "office_cidrs"}
	if prov.Target != want {
		t.Fatalf("target = %+v", prov.Target)
	}
	if kinds(prov) != "resource_attr→module_arg→tfvars" {
		t.Fatalf("chain = %s", kinds(prov))
	}
}

func TestWalkNonBareExpressionIsTier2(t *testing.T) {
	cfg := json.RawMessage(`{"root_module":{"resources":[{"address":"aws_security_group.web",
		"expressions":{"cidr_blocks":{"references":["var.a","var.b"]}}}]}}`)
	prov, err := provenance.Walk(cfg, "aws_security_group.web", "cidr_blocks", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if prov.Tier != provenance.Tier2Transforming {
		t.Fatalf("tier = %v", prov.Tier)
	}
}

func TestWalkDataReferenceIsTier3(t *testing.T) {
	cfg := json.RawMessage(`{"root_module":{"resources":[{"address":"aws_instance.app",
		"expressions":{"ami":{"references":["data.aws_ami.latest"]}}}]}}`)
	prov, err := provenance.Walk(cfg, "aws_instance.app", "ami", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if prov.Tier != provenance.Tier3Opaque {
		t.Fatalf("tier = %v", prov.Tier)
	}
}

func TestWalkRegistryModuleIsTier3(t *testing.T) {
	cfg := json.RawMessage(`{"root_module":{"module_calls":{"vpc":{"source":"terraform-aws-modules/vpc/aws","module":{}}}}}`)
	prov, err := provenance.Walk(cfg, "module.vpc.aws_vpc.this", "cidr_block", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if prov.Tier != provenance.Tier3Opaque {
		t.Fatalf("tier = %v", prov.Tier)
	}
}

func TestWalkIndexedAddressStripsKey(t *testing.T) {
	p := loadPlan(t, "drift_literal.json")
	got, err := provenance.Walk(p.Configuration, `aws_security_group.web["a"]`, "description", "../testdata/hcl/literal")
	if err != nil {
		t.Fatal(err)
	}
	if got.Tier != provenance.Tier0Literal {
		t.Fatalf("tier = %v", got.Tier)
	}
}

func kinds(p provenance.Provenance) string {
	parts := make([]string, len(p.Chain))
	for i, l := range p.Chain {
		parts[i] = l.Kind
	}
	return strings.Join(parts, "→")
}
