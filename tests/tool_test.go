package tests

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/raflyritonga/terra-drift/internal/contract"
	"github.com/raflyritonga/terra-drift/internal/model"
	"github.com/raflyritonga/terra-drift/internal/tool"
)

func TestParseStrictAccepts(t *testing.T) {
	out, err := tool.ParseStrict(`{"edits":[{"file":"main.tf","block_addr":"aws_sg.web","attribute":"description","op":"set","value":"x"}],"rationale":"r"}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Edits) != 1 || out.Edits[0].Op != contract.OpSet {
		t.Fatalf("out = %+v", out)
	}
}

func TestParseStrictRejects(t *testing.T) {
	cases := map[string]string{
		"prose":          `Sure! Here is the edit you asked for.`,
		"markdown fence": "```json\n{\"edits\":[]}\n```",
		"unknown field":  `{"edits":[{"file":"a","attribute":"b","op":"set","value":1}],"rationale":"r","confidence":0.9}`,
		"empty edits":    `{"edits":[],"rationale":"nothing"}`,
		"bad op":         `{"edits":[{"file":"a","attribute":"b","op":"rewrite","value":1}],"rationale":"r"}`,
		"missing file":   `{"edits":[{"attribute":"b","op":"set","value":1}],"rationale":"r"}`,
		"trailing junk":  `{"edits":[{"file":"a","attribute":"b","op":"set","value":1}],"rationale":"r"} extra`,
	}
	for name, reply := range cases {
		if _, err := tool.ParseStrict(reply); err == nil {
			t.Errorf("%s: accepted %q", name, reply)
		}
	}
}

func TestMockModelIsDeterministicAndStructured(t *testing.T) {
	var in contract.ProposalInput
	in.Drift.Address = "module.network.aws_security_group.web"
	in.Drift.Attribute = "cidr_blocks"
	in.Drift.After = json.RawMessage(`["4.5.6.7/32"]`)
	in.Provenance = []contract.ChainLinkDTO{
		{Kind: "resource_attr", File: "modules/network/sg.tf", Expr: "aws_security_group.web.cidr_blocks", Line: 3},
	}
	payload, _ := json.Marshal(in)

	m := model.MockModel{}
	first, _, err := m.Complete(context.Background(), "", string(payload))
	if err != nil {
		t.Fatal(err)
	}
	second, _, _ := m.Complete(context.Background(), "", string(payload))
	if first != second {
		t.Fatal("mock model is not deterministic")
	}

	out, err := tool.ParseStrict(first)
	if err != nil {
		t.Fatalf("mock output failed strict parsing: %v", err)
	}
	e := out.Edits[0]
	if e.BlockAddr != "aws_security_group.web" || e.Attribute != "cidr_blocks" || e.Op != contract.OpSet {
		t.Fatalf("edit = %+v", e)
	}
}

func TestHandlerRejectsUnusableModelOutput(t *testing.T) {
	h := &tool.Handler{Model: proseModel{}}
	var in contract.ProposalInput
	in.Provenance = []contract.ChainLinkDTO{{Kind: "resource_attr", File: "main.tf"}}
	if _, _, err := h.ProposeHclEdits(context.Background(), nil, in); err == nil {
		t.Fatal("handler accepted prose from the model")
	}
}

// The explain tool is the read path: prose in, summary out, no edits anywhere.
func TestExplainDrift(t *testing.T) {
	h := &tool.Handler{Model: model.MockModel{}}
	in := contract.ExplainInput{Drifts: []contract.DriftFact{
		{Address: "aws_security_group.web", Attribute: "ingress", File: "main.tf", Line: 3},
		{Address: `module.rt["a"].aws_route_table.this`, Attribute: "route"},
	}}
	_, out, err := h.ExplainDrift(context.Background(), nil, in)
	if err != nil {
		t.Fatal(err)
	}
	if out.Summary == "" {
		t.Fatal("empty explanation")
	}

	if _, _, err := h.ExplainDrift(context.Background(), nil, contract.ExplainInput{}); err == nil {
		t.Fatal("expected error for empty drift list")
	}
}

type proseModel struct{}

func (proseModel) Complete(context.Context, string, string) (string, int, error) {
	return "I think you should change the security group.", 0, nil
}
