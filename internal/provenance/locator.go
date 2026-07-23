package provenance

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

// locator finds blocks and tfvars attributes in the repo's HCL files.
// Parsed files are cached per directory; paths are relative to root.
type locator struct {
	root   string
	parser *hclparse.Parser
}

func newLocator(root string) *locator {
	return &locator{root: root, parser: hclparse.NewParser()}
}

// findBlock returns file (relative to root) and line of the block matching type+labels.
func (l *locator) findBlock(dir string, typeAndLabels []string) (string, int) {
	for _, path := range l.tfFiles(dir, ".tf") {
		body := l.parse(path)
		if body == nil {
			continue
		}
		for _, b := range body.Blocks {
			if b.Type != typeAndLabels[0] || len(b.Labels) != len(typeAndLabels)-1 {
				continue
			}
			match := true
			for i, lbl := range b.Labels {
				if lbl != typeAndLabels[i+1] {
					match = false
				}
			}
			if match {
				return path, b.DefRange().Start.Line
			}
		}
	}
	return "", 0
}

// findTfvarsAttr looks for a top-level attribute in terraform.tfvars / *.auto.tfvars.
// Files are checked in terraform's load order; the last definition wins.
func (l *locator) findTfvarsAttr(dir, name string) (string, int, bool) {
	var candidates []string
	for _, path := range l.tfFiles(dir, ".tfvars") {
		base := filepath.Base(path)
		if base == "terraform.tfvars" {
			candidates = append([]string{path}, candidates...)
		} else if strings.HasSuffix(base, ".auto.tfvars") {
			candidates = append(candidates, path)
		}
	}
	file, line, found := "", 0, false
	for _, path := range candidates {
		body := l.parse(path)
		if body == nil {
			continue
		}
		if attr, ok := body.Attributes[name]; ok {
			file, line, found = path, attr.SrcRange.Start.Line, true
		}
	}
	return file, line, found
}

func (l *locator) tfFiles(dir, ext string) []string {
	entries, err := os.ReadDir(filepath.Join(l.root, dir))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ext) {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out
}

func (l *locator) parse(relPath string) *hclsyntax.Body {
	abs := filepath.Join(l.root, relPath)
	src, err := os.ReadFile(abs)
	if err != nil {
		return nil
	}
	f, _ := l.parser.ParseHCL(src, relPath)
	if f == nil {
		return nil
	}
	body, _ := f.Body.(*hclsyntax.Body)
	return body
}

// BlockSnippet returns just the source text of the block (or top-level
// attribute) starting at line in file — the minimal context a model needs.
func BlockSnippet(root, file string, line int) string {
	abs := filepath.Join(root, file)
	src, err := os.ReadFile(abs)
	if err != nil {
		return ""
	}
	f, _ := hclparse.NewParser().ParseHCL(src, file)
	if f == nil {
		return ""
	}
	body, ok := f.Body.(*hclsyntax.Body)
	if !ok {
		return ""
	}
	slice := func(from, to int) string {
		lines := strings.Split(string(src), "\n")
		if from < 1 || to > len(lines) || from > to {
			return ""
		}
		return strings.Join(lines[from-1:to], "\n")
	}
	for _, b := range body.Blocks {
		if b.DefRange().Start.Line == line {
			return slice(b.Range().Start.Line, b.Range().End.Line)
		}
	}
	for _, a := range body.Attributes {
		if a.SrcRange.Start.Line == line {
			return slice(a.SrcRange.Start.Line, a.Expr.Range().End.Line)
		}
	}
	return ""
}
