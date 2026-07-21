// Package drift parses `terraform show -json` plan output.
// Only the fields the pipeline consumes are modeled.
package drift

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/raflyritonga/terra-drift/internal/provenance"
)

// Exit codes, mirroring `terraform plan -detailed-exitcode`.
// Also used by `check`: 0 clean, 2 drift, 1 error.
const (
	ExitClean = 0
	ExitError = 1
	ExitDrift = 2
)

type Plan struct {
	ResourceDrift []DriftedResource `json:"resource_drift"`
	Configuration json.RawMessage   `json:"configuration"`
}

type DriftedResource struct {
	Address string `json:"address"`
	Change  struct {
		Actions []string        `json:"actions"`
		Before  json.RawMessage `json:"before"`
		After   json.RawMessage `json:"after"`
	} `json:"change"`
}

// AttrDrift is a single top-level attribute whose live value differs from state.
type AttrDrift struct {
	Attribute string
	Before    json.RawMessage
	After     json.RawMessage
}

func ParsePlan(data []byte) (*Plan, error) {
	var p Plan
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse plan json: %w", err)
	}
	return &p, nil
}

func (p *Plan) HasDrift() bool { return len(p.ResourceDrift) > 0 }

// Deleted reports whether the live resource is gone (after is null).
func (r *DriftedResource) Deleted() bool {
	return len(r.Change.After) == 0 || string(r.Change.After) == "null"
}

// ChangedAttrs diffs before vs after and returns the drifted top-level attributes.
func (r *DriftedResource) ChangedAttrs() ([]AttrDrift, error) {
	var before, after map[string]json.RawMessage
	if err := json.Unmarshal(r.Change.Before, &before); err != nil {
		return nil, fmt.Errorf("%s: parse before: %w", r.Address, err)
	}
	if err := json.Unmarshal(r.Change.After, &after); err != nil {
		return nil, fmt.Errorf("%s: parse after: %w", r.Address, err)
	}

	keys := map[string]bool{}
	for k := range before {
		keys[k] = true
	}
	for k := range after {
		keys[k] = true
	}

	var out []AttrDrift
	for k := range keys {
		if jsonEqual(before[k], after[k]) {
			continue
		}
		out = append(out, AttrDrift{Attribute: k, Before: before[k], After: after[k]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Attribute < out[j].Attribute })
	return out, nil
}

func jsonEqual(a, b json.RawMessage) bool {
	var av, bv any
	json.Unmarshal(a, &av)
	json.Unmarshal(b, &bv)
	return reflect.DeepEqual(av, bv)
}

// ResourceReport is one line of the tiny drift summary: what changed, not how.
type ResourceReport struct {
	Address string
	Attrs   []string
	Deleted bool
	File    string // resource block location, when locatable
	Line    int
}

// Report lists the drifted resources with their changed attribute names.
// When dir is non-empty, each entry is located to its file:line in the HCL.
func (p *Plan) Report(dir string) ([]ResourceReport, error) {
	var out []ResourceReport
	for _, r := range p.ResourceDrift {
		rr := ResourceReport{Address: r.Address, Deleted: r.Deleted()}
		if !rr.Deleted {
			attrs, err := r.ChangedAttrs()
			if err != nil {
				return nil, err
			}
			for _, a := range attrs {
				rr.Attrs = append(rr.Attrs, a.Attribute)
			}
		}
		if dir != "" {
			rr.File, rr.Line = provenance.Locate(p.Configuration, r.Address, dir)
		}
		out = append(out, rr)
	}
	return out, nil
}

// RenderReport is the small, glanceable summary — one line per resource.
func RenderReport(items []ResourceReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "drift on %d resource(s):\n", len(items))
	for _, it := range items {
		loc := ""
		if it.File != "" {
			loc = fmt.Sprintf("  (%s:%d)", filepath.ToSlash(it.File), it.Line)
		}
		fmt.Fprintf(&b, "  %-42s %s%s\n", it.Address, attrSummary(it), loc)
	}
	return b.String()
}

func attrSummary(it ResourceReport) string {
	switch {
	case it.Deleted:
		return "deleted in live infra"
	case len(it.Attrs) == 0:
		return "changed"
	case len(it.Attrs) <= 3:
		return strings.Join(it.Attrs, ", ")
	default:
		return fmt.Sprintf("%s +%d more", strings.Join(it.Attrs[:3], ", "), len(it.Attrs)-3)
	}
}

// Summarize prints the tiny report (with file:line when dir is given) and
// returns the check exit code.
func Summarize(planJSON []byte, outFile, dir string) (int, error) {
	if outFile != "" {
		if err := os.WriteFile(outFile, planJSON, 0o644); err != nil {
			return ExitError, err
		}
	}
	p, err := ParsePlan(planJSON)
	if err != nil {
		return ExitError, err
	}
	if !p.HasDrift() {
		fmt.Println("no drift detected")
		return ExitClean, nil
	}
	items, err := p.Report(dir)
	if err != nil {
		return ExitError, err
	}
	fmt.Print(RenderReport(items))
	return ExitDrift, nil
}
