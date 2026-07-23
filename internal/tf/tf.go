// Package tf shells out to the terraform CLI.
package tf

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/raflyritonga/terra-drift/internal/drift"
	"path/filepath"
)

type Runner struct {
	Dir string // Terraform root to run in
	Bin string // terraform binary, default "terraform"
}

func New(dir string) *Runner { return &Runner{Dir: dir, Bin: "terraform"} }

// RefreshPlan runs a refresh-only plan and returns (exitCode, planJSON, error).
// planJSON is the `terraform show -json` output of the saved plan file.
func (r *Runner) RefreshPlan(ctx context.Context) (int, []byte, error) {
	tmp, err := os.CreateTemp("", "terra-drift-*.plan")
	if err != nil {
		return drift.ExitError, nil, err
	}
	planFile := tmp.Name()
	tmp.Close()
	defer os.Remove(planFile)

	cmd := exec.CommandContext(ctx, r.Bin, "plan", "-refresh-only", "-lock=false",
		"-detailed-exitcode", "-input=false", "-no-color", "-out="+planFile)
	cmd.Dir = r.Dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	code := drift.ExitClean
	if err := cmd.Run(); err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			return drift.ExitError, nil, fmt.Errorf("terraform plan: %w", err)
		}
		code = exitErr.ExitCode()
		if code != drift.ExitDrift {
			return drift.ExitError, nil, fmt.Errorf("terraform plan failed: %s", stderr.String())
		}
	}

	show := exec.CommandContext(ctx, r.Bin, "show", "-json", planFile)
	show.Dir = r.Dir
	out, err := show.Output()
	if err != nil {
		return drift.ExitError, nil, fmt.Errorf("terraform show -json: %w", err)
	}
	return code, out, nil
}

// Version returns the terraform version string, e.g. "Terraform v1.9.0".
func (r *Runner) Version(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, r.Bin, "version").Output()
	if err != nil {
		return "", err
	}
	return string(bytes.SplitN(out, []byte("\n"), 2)[0]), nil
}

// Fmt formats the whole tree under dir so applied edits match house style.
func (r *Runner) Fmt(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, r.Bin, "fmt", "-recursive")
	cmd.Dir = r.Dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("terraform fmt: %s", stderr.String())
	}
	return nil
}

// Validate confirms the edited configuration still parses and type-checks.
func (r *Runner) Validate(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, r.Bin, "validate", "-no-color")
	cmd.Dir = r.Dir
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("terraform validate: %s%s", out.String(), stderr.String())
	}
	return nil
}

// StateReachable checks the backend responds; requires an init'd root.
func (r *Runner) StateReachable(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, r.Bin, "state", "list")
	cmd.Dir = r.Dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("terraform state list: %s", stderr.String())
	}
	return nil
}

func IsRoot(dir string) bool {
	matches, _ := filepath.Glob(filepath.Join(dir, "*.tf"))
	return len(matches) > 0
}
