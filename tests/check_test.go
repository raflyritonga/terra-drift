package tests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/raflyritonga/terra-drift/internal/drift"
)

// Exit-code contract of `check`: 0 clean / 2 drift / 1 error.
func TestCheckExitCodes(t *testing.T) {
	cases := []struct {
		fixture string
		json    []byte
		want    int
	}{
		{fixture: "clean.json", want: drift.ExitClean},
		{fixture: "drift_literal.json", want: drift.ExitDrift},
		{fixture: "drift_module_arg.json", want: drift.ExitDrift},
		{json: []byte("not json at all"), want: drift.ExitError},
	}
	for _, tc := range cases {
		data := tc.json
		if tc.fixture != "" {
			var err error
			data, err = os.ReadFile(filepath.Join("../testdata/plans", tc.fixture))
			if err != nil {
				t.Fatal(err)
			}
		}
		code, err := drift.Summarize(data, "", "")
		if code != tc.want {
			t.Errorf("%s: exit code = %d, want %d (err: %v)", tc.fixture, code, tc.want, err)
		}
		if tc.want == drift.ExitError && err == nil {
			t.Errorf("%s: want an error for exit 1", tc.fixture)
		}
	}
}

func TestCheckOutWritesPlanJSON(t *testing.T) {
	data, err := os.ReadFile("../testdata/plans/drift_literal.json")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "drift.json")
	if _, err := drift.Summarize(data, out, ""); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(out)
	if err != nil || len(written) != len(data) {
		t.Fatalf("plan json not written verbatim (err: %v)", err)
	}
}
