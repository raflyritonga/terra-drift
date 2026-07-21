package tests

import (
	"strings"
	"testing"

	"github.com/raflyritonga/terra-drift/internal/drift"
)

// The report is one line per resource — small, not plan-sized.
func TestRenderReportIsSmall(t *testing.T) {
	p := loadPlan(t, "drift_literal.json")
	items, err := p.Report("")
	if err != nil {
		t.Fatal(err)
	}
	out := drift.RenderReport(items)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	// header + exactly one line per resource
	if len(lines) != 1+len(items) {
		t.Fatalf("report has %d lines for %d resources:\n%s", len(lines), len(items), out)
	}
	if !strings.Contains(out, "aws_security_group.web") || !strings.Contains(out, "description") {
		t.Fatalf("report missing resource or attr:\n%s", out)
	}
	// no before→after value dump
	if strings.Contains(out, "→") || strings.Contains(out, "hotfix") {
		t.Fatalf("report leaked plan-style detail:\n%s", out)
	}
}

// With a dir, each report line carries the resource block's file:line.
func TestReportIncludesFileLine(t *testing.T) {
	p := loadPlan(t, "drift_module_arg.json")
	items, err := p.Report("../testdata/hcl/module_arg")
	if err != nil {
		t.Fatal(err)
	}
	out := drift.RenderReport(items)
	if !strings.Contains(out, "modules/network/sg.tf:1") {
		t.Fatalf("report missing file:line of the resource block:\n%s", out)
	}
	if lines := strings.Split(strings.TrimRight(out, "\n"), "\n"); len(lines) != 1+len(items) {
		t.Fatalf("file:line must not add lines:\n%s", out)
	}
}

func TestAttrSummaryCollapsesMany(t *testing.T) {
	items := []drift.ResourceReport{
		{Address: "aws_x.y", Attrs: []string{"a", "b", "c", "d", "e"}},
		{Address: "aws_z.w", Deleted: true},
	}
	out := drift.RenderReport(items)
	if !strings.Contains(out, "+2 more") {
		t.Fatalf("expected collapse of >3 attrs:\n%s", out)
	}
	if !strings.Contains(out, "deleted in live infra") {
		t.Fatalf("expected deletion note:\n%s", out)
	}
}
