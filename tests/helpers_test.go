package tests

import (
	"flag"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/raflyritonga/terra-drift/internal/drift"
	"github.com/raflyritonga/terra-drift/internal/provenance"
)

var update = flag.Bool("update", false, "rewrite golden files from actual output")

func loadPlan(t *testing.T, name string) *drift.Plan {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("../testdata/plans", name))
	if err != nil {
		t.Fatal(err)
	}
	p, err := drift.ParsePlan(data)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func walkFixture(t *testing.T, planName, hclDir, attribute string) provenance.Provenance {
	t.Helper()
	p := loadPlan(t, planName)
	prov, err := provenance.Walk(p.Configuration, p.ResourceDrift[0].Address, attribute,
		filepath.Join("../testdata/hcl", hclDir))
	if err != nil {
		t.Fatal(err)
	}
	return prov
}

// copyTree copies an HCL fixture dir to dst, skipping golden files.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		if rel == "." || strings.HasPrefix(rel, "golden") {
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func buildServer(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "terra-drift-mcp")
	cmd := exec.Command("go", "build", "-o", bin, "github.com/raflyritonga/terra-drift/cmd/terra-drift-mcp")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build server: %v\n%s", err, out)
	}
	return bin
}
