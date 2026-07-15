package contract

import "github.com/google/jsonschema-go/jsonschema"

// anyJSON accepts any JSON value — drift values and edit values are free-form.
func anyJSON() *jsonschema.Schema { return &jsonschema.Schema{} }

func str() *jsonschema.Schema { return &jsonschema.Schema{Type: "string"} }

// ProposalInputSchema is the explicit tool input schema. Hand-written because
// schema inference maps json.RawMessage to "array", rejecting scalar values.
func ProposalInputSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type:     "object",
		Required: []string{"drift"},
		Properties: map[string]*jsonschema.Schema{
			"drift": {
				Type:     "object",
				Required: []string{"address", "attribute"},
				Properties: map[string]*jsonschema.Schema{
					"address":   str(),
					"attribute": str(),
					"before":    anyJSON(),
					"after":     anyJSON(),
				},
			},
			"provenance": {
				Type: "array",
				Items: &jsonschema.Schema{
					Type: "object",
					Properties: map[string]*jsonschema.Schema{
						"kind": str(), "file": str(), "expr": str(),
						"line": {Type: "integer"},
					},
				},
			},
			"file_excerpts": {Type: "object", AdditionalProperties: str()},
			"siblings":      {Type: "array", Items: anyJSON()},
			"safety_rules":  {Type: "array", Items: str()},
		},
	}
}

// ProposalOutputSchema is the explicit tool output schema.
func ProposalOutputSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type:     "object",
		Required: []string{"edits", "rationale"},
		Properties: map[string]*jsonschema.Schema{
			"edits": {
				Type: "array",
				Items: &jsonschema.Schema{
					Type:     "object",
					Required: []string{"op"},
					Properties: map[string]*jsonschema.Schema{
						"file":       str(),
						"block_addr": str(),
						"attribute":  str(),
						"op":         {Type: "string", Enum: []any{OpSet, OpAppendTo, OpIgnore}},
						"value":      anyJSON(),
					},
				},
			},
			"rationale": str(),
		},
	}
}
