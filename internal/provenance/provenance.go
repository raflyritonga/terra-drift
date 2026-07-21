// Package provenance traces a drifted attribute back to the literal that feeds it,
// classifying each drift into an edit tier.
package provenance

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/raflyritonga/terra-drift/internal/contract"
)

type Tier int

const (
	Tier0Literal Tier = iota
	Tier1Passthrough
	Tier2Transforming
	Tier3Opaque
)

func (t Tier) String() string {
	return [...]string{"tier0-literal", "tier1-passthrough", "tier2-transforming", "tier3-opaque"}[t]
}

type ChainLink = contract.ChainLinkDTO

// Provenance is the walk result. When Tier<=1, Target is the block/attribute to edit.
type Provenance struct {
	Tier   Tier
	Chain  []ChainLink
	Target EditTarget
	Note   string
}

// EditTarget pinpoints where the deterministic edit lands.
type EditTarget struct {
	File      string // relative to repoRoot
	BlockAddr string // "" = top-level attr (tfvars), else "variable.x", "module.x", "aws_x.y"
	Attribute string
}

// plan-JSON configuration shapes (only what the walker reads)
type cfgRoot struct {
	RootModule cfgModule `json:"root_module"`
}
type cfgModule struct {
	Resources   []cfgResource            `json:"resources"`
	ModuleCalls map[string]cfgModuleCall `json:"module_calls"`
	Variables   map[string]cfgVariable   `json:"variables"`
}
type cfgResource struct {
	Address     string                     `json:"address"`
	Expressions map[string]json.RawMessage `json:"expressions"`
}
type cfgModuleCall struct {
	Source      string                     `json:"source"`
	Expressions map[string]json.RawMessage `json:"expressions"`
	Module      cfgModule                  `json:"module"`
}
type cfgVariable struct {
	Default json.RawMessage `json:"default"`
}
type cfgExpr struct {
	ConstantValue json.RawMessage `json:"constant_value"`
	References    []string        `json:"references"`
}

// frame is one level of the module tree, with the call site that produced it.
type frame struct {
	mod  cfgModule
	dir  string // module dir relative to repoRoot
	call *cfgModuleCall
	name string
}

// resolveFrames descends the module path, tracking each module's source dir.
// A non-local module source returns ok=false (registry/git — v1 limit).
func resolveFrames(root cfgRoot, modPath []string) ([]frame, bool, error) {
	frames := []frame{{mod: root.RootModule, dir: "."}}
	for _, name := range modPath {
		cur := frames[len(frames)-1]
		call, ok := cur.mod.ModuleCalls[name]
		if !ok {
			return nil, false, fmt.Errorf("module call %q not found in configuration", name)
		}
		if !strings.HasPrefix(call.Source, "./") && !strings.HasPrefix(call.Source, "../") {
			return nil, false, nil
		}
		frames = append(frames, frame{mod: call.Module, dir: filepath.Join(cur.dir, call.Source), call: &call, name: name})
	}
	return frames, true, nil
}

// Locate returns the file (relative to repoRoot) and line of the resource
// block at address; empty file when it cannot be found.
func Locate(cfg json.RawMessage, address, repoRoot string) (string, int) {
	var root cfgRoot
	if err := json.Unmarshal(cfg, &root); err != nil {
		return "", 0
	}
	modPath, localAddr := splitModulePath(address)
	localAddr = stripIndexKey(localAddr)
	frames, ok, err := resolveFrames(root, modPath)
	if err != nil || !ok {
		return "", 0
	}
	return newLocator(repoRoot).findBlock(frames[len(frames)-1].dir, blockLabels(localAddr))
}

// Walk traces attribute on the resource at address through the configuration.
func Walk(cfg json.RawMessage, address, attribute, repoRoot string) (Provenance, error) {
	var root cfgRoot
	if err := json.Unmarshal(cfg, &root); err != nil {
		return Provenance{}, fmt.Errorf("parse configuration: %w", err)
	}

	modPath, localAddr := splitModulePath(address)
	localAddr = stripIndexKey(localAddr)

	frames, ok, err := resolveFrames(root, modPath)
	if err != nil {
		return Provenance{}, err
	}
	if !ok {
		return Provenance{Tier: Tier3Opaque, Note: "module uses a non-local source (v1 limit)"}, nil
	}

	depth := len(frames) - 1
	res := findResource(frames[depth].mod, localAddr)
	if res == nil {
		return Provenance{}, fmt.Errorf("resource %q not found in configuration", localAddr)
	}

	loc := newLocator(repoRoot)
	var chain []ChainLink

	resFile, resLine := loc.findBlock(frames[depth].dir, blockLabels(localAddr))
	chain = append(chain, ChainLink{Kind: "resource_attr", File: resFile, Expr: localAddr + "." + attribute, Line: resLine})

	expr, err := decodeExpr(res.Expressions[attribute])
	if err != nil {
		return Provenance{}, err
	}

	// Attribute absent or literal in the resource block: edit it in place.
	if expr == nil || expr.isConstant() {
		return Provenance{
			Tier:   Tier0Literal,
			Chain:  chain,
			Target: EditTarget{File: resFile, BlockAddr: localAddr, Attribute: attribute},
		}, nil
	}

	// Follow bare references upward until a literal or an opaque expression.
	for range 32 {
		ref, ok := expr.bareRef()
		if !ok {
			return Provenance{Tier: Tier2Transforming, Chain: chain, Note: "non-bare expression: " + strings.Join(expr.References, ", ")}, nil
		}
		switch {
		case strings.HasPrefix(ref, "var."):
			name := strings.TrimPrefix(ref, "var.")
			if depth > 0 {
				fr := frames[depth]
				parent := frames[depth-1]
				callFile, callLine := loc.findBlock(parent.dir, []string{"module", fr.name})
				chain = append(chain, ChainLink{Kind: "module_arg", File: callFile, Expr: fr.name + "." + name, Line: callLine})
				argExpr, err := decodeExpr(fr.call.Expressions[name])
				if err != nil {
					return Provenance{}, err
				}
				if argExpr == nil {
					return Provenance{Tier: Tier3Opaque, Chain: chain, Note: fmt.Sprintf("module argument %q not set by caller (default inside module)", name)}, nil
				}
				if argExpr.isConstant() {
					return Provenance{
						Tier:   Tier1Passthrough,
						Chain:  chain,
						Target: EditTarget{File: callFile, BlockAddr: "module." + fr.name, Attribute: name},
					}, nil
				}
				depth--
				expr = argExpr
				continue
			}
			return resolveRootVar(loc, frames[0], name, chain)
		case strings.HasPrefix(ref, "local."):
			return Provenance{Tier: Tier2Transforming, Chain: chain, Note: "locals resolution is a Phase 2 item; routed to the server"}, nil
		case strings.HasPrefix(ref, "each.") || strings.HasPrefix(ref, "count."):
			return Provenance{Tier: Tier2Transforming, Chain: chain, Note: "for_each/count shaping; routed to the server"}, nil
		default:
			return Provenance{Tier: Tier3Opaque, Chain: chain, Note: "value comes from " + ref}, nil
		}
	}
	return Provenance{Tier: Tier3Opaque, Chain: chain, Note: "reference chain too deep"}, nil
}

// resolveRootVar terminates a walk at a root variable: tfvars wins over the default.
func resolveRootVar(loc *locator, root frame, name string, chain []ChainLink) (Provenance, error) {
	if file, line, ok := loc.findTfvarsAttr(root.dir, name); ok {
		chain = append(chain, ChainLink{Kind: "tfvars", File: file, Expr: name, Line: line})
		return Provenance{
			Tier:   Tier1Passthrough,
			Chain:  chain,
			Target: EditTarget{File: file, BlockAddr: "", Attribute: name},
		}, nil
	}
	if v, ok := root.mod.Variables[name]; ok && len(v.Default) > 0 {
		file, line := loc.findBlock(root.dir, []string{"variable", name})
		chain = append(chain, ChainLink{Kind: "default", File: file, Expr: name, Line: line})
		return Provenance{
			Tier:   Tier1Passthrough,
			Chain:  chain,
			Target: EditTarget{File: file, BlockAddr: "variable." + name, Attribute: "default"},
		}, nil
	}
	chain = append(chain, ChainLink{Kind: "root_var", Expr: name})
	return Provenance{Tier: Tier3Opaque, Chain: chain, Note: fmt.Sprintf("variable %q has no tfvars entry or default (-var flag? v1 limit)", name)}, nil
}

func decodeExpr(raw json.RawMessage) (*cfgExpr, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var e cfgExpr
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil, fmt.Errorf("parse expression: %w", err)
	}
	return &e, nil
}

func (e *cfgExpr) isConstant() bool { return len(e.ConstantValue) > 0 && len(e.References) == 0 }

// bareRef reports the single reference this expression passes through, if any.
func (e *cfgExpr) bareRef() (string, bool) {
	if len(e.ConstantValue) > 0 || len(e.References) != 1 {
		return "", false
	}
	return e.References[0], true
}

func findResource(m cfgModule, addr string) *cfgResource {
	for i := range m.Resources {
		if m.Resources[i].Address == addr {
			return &m.Resources[i]
		}
	}
	return nil
}

// splitModulePath separates module.a.module.b.type.name into ([a b], type.name).
func splitModulePath(address string) ([]string, string) {
	parts := strings.Split(address, ".")
	var mods []string
	i := 0
	for i+1 < len(parts) && parts[i] == "module" {
		mods = append(mods, parts[i+1])
		i += 2
	}
	return mods, strings.Join(parts[i:], ".")
}

// stripIndexKey drops for_each/count keys: aws_route.r["a"] → aws_route.r
func stripIndexKey(addr string) string {
	if i := strings.IndexAny(addr, "["); i >= 0 {
		return addr[:i]
	}
	return addr
}

// blockLabels maps a resource address to hclsyntax block type+labels.
func blockLabels(addr string) []string {
	parts := strings.SplitN(addr, ".", 2)
	if parts[0] == "data" {
		rest := strings.SplitN(parts[1], ".", 2)
		return append([]string{"data"}, rest...)
	}
	return []string{"resource", parts[0], parts[1]}
}
