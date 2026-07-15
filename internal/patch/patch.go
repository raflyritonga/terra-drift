// Package patch is the ONLY package that writes files.
// It applies contract.Edit instructions with hclwrite, preserving formatting.
package patch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/raflyritonga/terra-drift/internal/contract"
	"github.com/zclconf/go-cty/cty"
	ctyjson "github.com/zclconf/go-cty/cty/json"
)

type Edit = contract.Edit

// Apply writes one edit into the repo rooted at root. Op "ignore" is a no-op.
func Apply(root string, e Edit) error {
	switch e.Op {
	case contract.OpIgnore:
		return nil
	case contract.OpSet, contract.OpAppendTo:
	default:
		return fmt.Errorf("unknown edit op %q", e.Op)
	}

	path := filepath.Join(root, e.File)
	src, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", e.File, err)
	}
	f, diags := hclwrite.ParseConfig(src, e.File, hcl.InitialPos)
	if diags.HasErrors() {
		return fmt.Errorf("parse %s: %s", e.File, diags.Error())
	}

	body, err := resolveBody(f, e.BlockAddr)
	if err != nil {
		return fmt.Errorf("%s: %w", e.File, err)
	}

	val, err := jsonToCty(e.Value)
	if err != nil {
		return fmt.Errorf("edit value for %s: %w", e.Attribute, err)
	}

	if e.Op == contract.OpAppendTo {
		if err := appendToList(body, e.Attribute, val); err != nil {
			return fmt.Errorf("%s.%s: %w", e.BlockAddr, e.Attribute, err)
		}
	} else {
		body.SetAttributeValue(e.Attribute, val)
	}

	return os.WriteFile(path, f.Bytes(), 0o644)
}

// resolveBody navigates a BlockAddr ("", "variable.x", "module.x", "locals", "aws_x.y")
// to the hclwrite body holding the target attribute.
func resolveBody(f *hclwrite.File, blockAddr string) (*hclwrite.Body, error) {
	if blockAddr == "" {
		return f.Body(), nil
	}
	typ, labels := blockSpec(blockAddr)
	for _, b := range f.Body().Blocks() {
		if b.Type() == typ && labelsEqual(b.Labels(), labels) {
			return b.Body(), nil
		}
	}
	return nil, fmt.Errorf("block %q not found", blockAddr)
}

func blockSpec(addr string) (string, []string) {
	parts := strings.Split(addr, ".")
	switch {
	case addr == "locals":
		return "locals", nil
	case parts[0] == "variable" || parts[0] == "module" || parts[0] == "output" || parts[0] == "data":
		return parts[0], parts[1:]
	default:
		return "resource", parts
	}
}

func labelsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// appendToList inserts a value before the closing bracket of a list attribute.
func appendToList(body *hclwrite.Body, attr string, val cty.Value) error {
	a := body.GetAttribute(attr)
	if a == nil {
		body.SetAttributeValue(attr, cty.TupleVal([]cty.Value{val}))
		return nil
	}
	toks := a.Expr().BuildTokens(nil)
	closeIdx := -1
	for i := len(toks) - 1; i >= 0; i-- {
		if string(toks[i].Bytes) == "]" {
			closeIdx = i
			break
		}
	}
	if closeIdx < 0 {
		return fmt.Errorf("existing expression is not a list literal")
	}
	newToks := append(hclwrite.Tokens{}, toks[:closeIdx]...)
	if hasListItems(toks[:closeIdx]) {
		newToks = append(newToks, &hclwrite.Token{Type: hclsyntax.TokenComma, Bytes: []byte(",")})
	}
	newToks = append(newToks, hclwrite.TokensForValue(val)...)
	newToks = append(newToks, toks[closeIdx:]...)
	body.SetAttributeRaw(attr, newToks)
	return nil
}

func hasListItems(toks hclwrite.Tokens) bool {
	seenOpen := false
	for _, t := range toks {
		s := strings.TrimSpace(string(t.Bytes))
		if !seenOpen {
			seenOpen = s == "["
			continue
		}
		if s != "" {
			return true
		}
	}
	return false
}

func jsonToCty(raw []byte) (cty.Value, error) {
	if len(raw) == 0 {
		return cty.NilVal, fmt.Errorf("empty value")
	}
	t, err := ctyjson.ImpliedType(raw)
	if err != nil {
		return cty.NilVal, err
	}
	return ctyjson.Unmarshal(raw, t)
}
