// Package contract holds the types shared by both binaries.
// The compiler enforces that client and server agree on the edit format.
package contract

import "encoding/json"

// Edit ops understood by the client's patch engine.
const (
	OpSet      = "set"
	OpAppendTo = "append_to"
	OpIgnore   = "ignore"
)

// Edit is a structured file edit. The server proposes them, the client applies them.
type Edit struct {
	File      string          `json:"file"`
	BlockAddr string          `json:"block_addr"`
	Attribute string          `json:"attribute,omitempty"`
	Op        string          `json:"op"`
	Value     json.RawMessage `json:"value,omitempty"`
}

// EditOut is the server-side name for the same wire type.
type EditOut = Edit

// ChainLinkDTO is one hop of a provenance walk.
// Kind: resource_attr | module_arg | root_var | local | tfvars | default
type ChainLinkDTO struct {
	Kind string `json:"kind"`
	File string `json:"file"`
	Expr string `json:"expr"`
	Line int    `json:"line"`
}

// ProposalInput is the input schema of the propose_hcl_edits tool.
type ProposalInput struct {
	Drift struct {
		Address   string          `json:"address"`
		Attribute string          `json:"attribute"`
		Before    json.RawMessage `json:"before"`
		After     json.RawMessage `json:"after"`
	} `json:"drift"`
	Provenance   []ChainLinkDTO    `json:"provenance,omitempty"`
	FileExcerpts map[string]string `json:"file_excerpts,omitempty"`
	Siblings     []json.RawMessage `json:"siblings,omitempty"`
	SafetyRules  []string          `json:"safety_rules,omitempty"`
}

// ProposalOutput is the output schema of the propose_hcl_edits tool.
type ProposalOutput struct {
	Edits     []EditOut `json:"edits"`
	Rationale string    `json:"rationale"`
}
