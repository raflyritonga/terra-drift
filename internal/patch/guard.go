package patch

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/raflyritonga/terra-drift/internal/contract"
	"github.com/raflyritonga/terra-drift/internal/provenance"
)

// Guard rejects edits under protected paths when the value is settable higher up.
// Enforced client-side regardless of what the server proposed (safety rule #1).
func Guard(e Edit, protectedGlobs []string, prov provenance.Provenance) error {
	if e.Op == contract.OpIgnore {
		return nil
	}
	if !Protected(e.File, protectedGlobs) {
		return nil
	}
	for _, link := range prov.Chain {
		if link.File != "" && !Protected(link.File, protectedGlobs) && link.Kind != "resource_attr" {
			return fmt.Errorf("edit targets protected path %q but the value is settable from %s (%s)", e.File, link.File, link.Kind)
		}
	}
	return nil
}

// Protected reports whether path matches any protected glob. `dir/**` matches
// everything under dir; leading ../ segments are ignored so globs match
// module paths reached from a nested Terraform root like envs/prod.
func Protected(path string, globs []string) bool {
	p := filepath.ToSlash(path)
	trimmed := p
	for strings.HasPrefix(trimmed, "../") {
		trimmed = strings.TrimPrefix(trimmed, "../")
	}
	for _, g := range globs {
		if matchGlob(filepath.ToSlash(g), p) || matchGlob(filepath.ToSlash(g), trimmed) {
			return true
		}
	}
	return false
}

func matchGlob(glob, path string) bool {
	if prefix, ok := strings.CutSuffix(glob, "/**"); ok {
		return path == prefix || strings.HasPrefix(path, prefix+"/")
	}
	ok, _ := filepath.Match(glob, path)
	return ok
}
