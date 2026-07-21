// Package gitops handles branch/commit/push via git; PRs go through a Forge.
package gitops

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type Repo struct {
	Dir string
}

func New(dir string) *Repo { return &Repo{Dir: dir} }

func (r *Repo) run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = r.Dir
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s %s: %s", name, strings.Join(args, " "), strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

func (r *Repo) IsRepo(ctx context.Context) bool {
	_, err := r.run(ctx, "git", "rev-parse", "--git-dir")
	return err == nil
}

// TopLevel returns the repository root, for mapping edits to repo-relative paths.
func (r *Repo) TopLevel(ctx context.Context) (string, error) {
	return r.run(ctx, "git", "rev-parse", "--show-toplevel")
}

// BranchName is the unique timestamped branch, e.g. drift-sync/2026-07-15-060112.
func BranchName(prefix string, now time.Time) string {
	return prefix + now.Format("2006-01-02-150405")
}

// NewBranch creates and checks out BranchName locally (git push mode).
func (r *Repo) NewBranch(ctx context.Context, prefix string, now time.Time) (string, error) {
	name := BranchName(prefix, now)
	if _, err := r.run(ctx, "git", "checkout", "-b", name); err != nil {
		return "", err
	}
	return name, nil
}

func (r *Repo) Commit(ctx context.Context, message string, files ...string) error {
	args := append([]string{"add", "--"}, files...)
	if _, err := r.run(ctx, "git", args...); err != nil {
		return err
	}
	_, err := r.run(ctx, "git", "commit", "-m", message)
	return err
}

func (r *Repo) Push(ctx context.Context, branch string) error {
	_, err := r.run(ctx, "git", "push", "-u", "origin", branch)
	return err
}
