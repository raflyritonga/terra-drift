package tests

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/raflyritonga/terra-drift/internal/contract"
	"github.com/raflyritonga/terra-drift/internal/patch"
	"github.com/raflyritonga/terra-drift/internal/provenance"
)

type goldenCase struct {
	name      string
	plan      string
	hclDir    string
	attribute string
}

// Full detect→walk→edit cycle per fixture; edited files must match golden byte-for-byte.
func TestGoldenEdits(t *testing.T) {
	cases := []goldenCase{
		{"literal", "drift_literal.json", "literal", "description"},
		{"var_tfvars", "drift_var_tfvars.json", "var_tfvars", "cidr_blocks"},
		{"module_arg", "drift_module_arg.json", "module_arg", "cidr_blocks"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { runGoldenCase(t, tc) })
	}
}

func runGoldenCase(t *testing.T, tc goldenCase) {
	src := filepath.Join("../testdata/hcl", tc.hclDir)
	tmp := t.TempDir()
	copyTree(t, src, tmp)

	p := loadPlan(t, tc.plan)
	r := p.ResourceDrift[0]
	prov, err := provenance.Walk(p.Configuration, r.Address, tc.attribute, tmp)
	if err != nil {
		t.Fatal(err)
	}
	if prov.Tier > provenance.Tier1Passthrough {
		t.Fatalf("fixture should be deterministic, got %v (%s)", prov.Tier, prov.Note)
	}

	attrs, err := r.ChangedAttrs()
	if err != nil {
		t.Fatal(err)
	}
	var after json.RawMessage
	for _, a := range attrs {
		if a.Attribute == tc.attribute {
			after = a.After
		}
	}

	edit := contract.Edit{
		File:      prov.Target.File,
		BlockAddr: prov.Target.BlockAddr,
		Attribute: prov.Target.Attribute,
		Op:        contract.OpSet,
		Value:     after,
	}
	if err := patch.Apply(tmp, edit); err != nil {
		t.Fatal(err)
	}

	compareToGolden(t, src, tmp, edit.File)
	assertOthersUntouched(t, src, tmp, edit.File)
}

func compareToGolden(t *testing.T, src, tmp, editedFile string) {
	t.Helper()
	goldenPath := filepath.Join(src, "golden", editedFile)
	got, err := os.ReadFile(filepath.Join(tmp, editedFile))
	if err != nil {
		t.Fatal(err)
	}
	if *update {
		os.MkdirAll(filepath.Dir(goldenPath), 0o755)
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("missing golden (run with -update): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("edited %s differs from golden:\n--- got ---\n%s\n--- want ---\n%s", editedFile, got, want)
	}
}

func assertOthersUntouched(t *testing.T, src, tmp, editedFile string) {
	t.Helper()
	filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.Contains(path, "golden") {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		if rel == editedFile {
			return nil
		}
		want, _ := os.ReadFile(path)
		got, _ := os.ReadFile(filepath.Join(tmp, rel))
		if !bytes.Equal(got, want) {
			t.Errorf("untouched file %s was modified", rel)
		}
		return nil
	})
}

func TestAppendToList(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "vars.tfvars"), []byte("cidrs = [\"10.0.0.0/8\"]\n"), 0o644)
	e := contract.Edit{File: "vars.tfvars", Attribute: "cidrs", Op: contract.OpAppendTo, Value: json.RawMessage(`"1.2.3.4/32"`)}
	if err := patch.Apply(tmp, e); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(tmp, "vars.tfvars"))
	want := "cidrs = [\"10.0.0.0/8\", \"1.2.3.4/32\"]\n"
	if string(got) != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestIgnoreOpWritesNothing(t *testing.T) {
	e := contract.Edit{File: "does-not-exist.tf", Op: contract.OpIgnore}
	if err := patch.Apply(t.TempDir(), e); err != nil {
		t.Fatal(err)
	}
}

// Safety rule #1: a caller-settable value is never edited under modules/**,
// even when the server proposes exactly that.
func TestGuardRejectsProtectedEditWhenCallerSettable(t *testing.T) {
	prov := provenance.Provenance{
		Tier: provenance.Tier2Transforming,
		Chain: []provenance.ChainLink{
			{Kind: "resource_attr", File: "modules/network/sg.tf", Expr: "aws_security_group.web.cidr_blocks", Line: 3},
			{Kind: "module_arg", File: "main.tf", Expr: "network.allowed_cidrs", Line: 1},
		},
	}
	serverEdit := contract.Edit{File: "modules/network/sg.tf", BlockAddr: "aws_security_group.web", Attribute: "cidr_blocks", Op: contract.OpSet, Value: json.RawMessage(`["4.5.6.7/32"]`)}
	if err := patch.Guard(serverEdit, []string{"modules/**"}, prov); err == nil {
		t.Fatal("guard allowed an edit under modules/** with a caller-settable alternative")
	}
}

func TestGuardAllowsProtectedEditWithoutAlternative(t *testing.T) {
	prov := provenance.Provenance{
		Tier: provenance.Tier0Literal,
		Chain: []provenance.ChainLink{
			{Kind: "resource_attr", File: "modules/network/sg.tf", Expr: "aws_security_group.web.description", Line: 3},
		},
	}
	e := contract.Edit{File: "modules/network/sg.tf", BlockAddr: "aws_security_group.web", Attribute: "description", Op: contract.OpSet, Value: json.RawMessage(`"x"`)}
	if err := patch.Guard(e, []string{"modules/**"}, prov); err != nil {
		t.Fatalf("guard rejected a hardcoded module literal with no alternative: %v", err)
	}
}

func TestProtectedMatchesRelativePaths(t *testing.T) {
	globs := []string{"modules/**"}
	for path, want := range map[string]bool{
		"modules/network/sg.tf":       true,
		"../modules/network/sg.tf":    true,
		"../../modules/network/sg.tf": true,
		"main.tf":                     false,
		"envs/prod/main.tf":           false,
	} {
		if got := patch.Protected(path, globs); got != want {
			t.Errorf("Protected(%q) = %v, want %v", path, got, want)
		}
	}
}
