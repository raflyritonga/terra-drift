package tests

import (
	"testing"

	"github.com/raflyritonga/terra-drift/internal/drift"
)

func TestParseCleanPlan(t *testing.T) {
	if loadPlan(t, "clean.json").HasDrift() {
		t.Fatal("clean plan reported drift")
	}
}

func TestParseDriftPlan(t *testing.T) {
	p := loadPlan(t, "drift_literal.json")
	if !p.HasDrift() {
		t.Fatal("drift plan reported clean")
	}
	r := p.ResourceDrift[0]
	if r.Address != "aws_security_group.web" {
		t.Fatalf("address = %q", r.Address)
	}
	if r.Deleted() {
		t.Fatal("update drift reported as deletion")
	}
}

func TestChangedAttrs(t *testing.T) {
	p := loadPlan(t, "drift_literal.json")
	attrs, err := p.ResourceDrift[0].ChangedAttrs()
	if err != nil {
		t.Fatal(err)
	}
	if len(attrs) != 1 {
		t.Fatalf("want 1 changed attr, got %d: %+v", len(attrs), attrs)
	}
	a := attrs[0]
	if a.Attribute != "description" {
		t.Fatalf("attribute = %q", a.Attribute)
	}
	if string(a.After) != `"hotfix: widened for the on-call incident"` {
		t.Fatalf("after = %s", a.After)
	}
}

func TestDeletedResource(t *testing.T) {
	p, err := drift.ParsePlan([]byte(`{"resource_drift":[{"address":"aws_sqs_queue.q","change":{"actions":["delete"],"before":{"name":"q"},"after":null}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !p.ResourceDrift[0].Deleted() {
		t.Fatal("null after not reported as deletion")
	}
}
