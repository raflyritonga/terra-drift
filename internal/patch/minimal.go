package patch

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/raflyritonga/terra-drift/internal/contract"
	"github.com/raflyritonga/terra-drift/internal/provenance"
)

// AllowedEdit is the closed set a proposed patch may touch: only files on the
// provenance chain, and only the drifted attribute or its origin names.
type AllowedEdit struct {
	Files map[string]bool
	Attrs map[string]bool
}

// AllowedFor derives the permitted files/attributes from a drifted attribute's
// provenance. Origin variable names (and "default") are allowed because tier-1
// style fixes legitimately land there.
func AllowedFor(driftedAttr string, prov provenance.Provenance) AllowedEdit {
	a := AllowedEdit{Files: map[string]bool{}, Attrs: map[string]bool{"default": true}}
	a.Attrs[driftedAttr] = true
	if prov.Target.File != "" {
		a.Files[filepath.ToSlash(prov.Target.File)] = true
	}
	if prov.Target.Attribute != "" {
		a.Attrs[prov.Target.Attribute] = true
	}
	for _, link := range prov.Chain {
		if link.File != "" {
			a.Files[filepath.ToSlash(link.File)] = true
		}
		// last segment of the expr is the attribute/variable name at that hop
		if i := strings.LastIndex(link.Expr, "."); i >= 0 {
			a.Attrs[link.Expr[i+1:]] = true
		} else if link.Expr != "" {
			a.Attrs[link.Expr] = true
		}
	}
	return a
}

// GuardMinimal rejects a proposed edit that reaches outside the drifted set —
// any file not on the provenance chain, or any attribute that didn't drift.
func GuardMinimal(e Edit, allowed AllowedEdit) error {
	if e.Op == contract.OpIgnore {
		return nil
	}
	if !allowed.Files[filepath.ToSlash(e.File)] {
		return fmt.Errorf("minimal-diff guard: edit targets %q, which is not on the drifted attribute's provenance chain", e.File)
	}
	if !allowed.Attrs[e.Attribute] {
		return fmt.Errorf("minimal-diff guard: edit changes attribute %q, which is outside the drifted set", e.Attribute)
	}
	return nil
}
