package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/raflyritonga/terra-drift/internal/config"
	"github.com/raflyritonga/terra-drift/internal/contract"
	"github.com/raflyritonga/terra-drift/internal/drift"
	"github.com/raflyritonga/terra-drift/internal/gitops"
	"github.com/raflyritonga/terra-drift/internal/mcpclient"
	"github.com/raflyritonga/terra-drift/internal/patch"
	"github.com/raflyritonga/terra-drift/internal/provenance"
	"github.com/raflyritonga/terra-drift/internal/tf"
	"github.com/raflyritonga/terra-drift/internal/trust"
)

// change is one drifted attribute with its classification and resolved edits.
type change struct {
	Address string
	Attr    drift.AttrDrift
	Prov    provenance.Provenance
	Edits   []contract.Edit
	Note    string // set when no edit could be produced (tier 3 or rejected)
}

// runSync detects drift, reports it small, asks whose version to trust, and
// on "live" rewrites the code (→ verify → PR). On "code" it changes nothing.
func runSync(args []string) (int, error) {
	fs := flag.NewFlagSet("sync", flag.ExitOnError)
	dir := fs.String("dir", ".", "Terraform root directory")
	trustMode := fs.String("trust", "", "whose version to trust: code | live | partial (default: ask, or report-only in CI)")
	liveCSV := fs.String("live", "", "partial mode: comma-separated resource addresses to trust live")
	noPR := fs.Bool("no-pr", false, "on the live path, commit the branch but skip push/PR")
	explain := fs.Bool("explain", false, "ask the model server for a short explanation before deciding")
	fs.Parse(args)

	ctx := context.Background()
	cfg, err := config.Load(*dir)
	if err != nil {
		return 1, err
	}

	runner := tf.New(*dir)
	code, planJSON, err := runner.RefreshPlan(ctx)
	if err != nil {
		return 1, err
	}
	if code == drift.ExitClean {
		fmt.Println("no drift detected")
		return 0, nil
	}

	p, err := drift.ParsePlan(planJSON)
	if err != nil {
		return 1, err
	}
	items, err := p.Report(*dir)
	if err != nil {
		return 1, err
	}
	fmt.Print(drift.RenderReport(items))
	if *explain {
		printExplanation(ctx, *dir, planJSON)
	}
	fmt.Println("\ntrusting \"live\" rewrites your .tf to match reality — can be destructive.\n\"code\" changes nothing; the next terraform apply reverts the live drift.")

	live, decided, err := trust.Resolve(*trustMode, *liveCSV, items, isInteractive(), bufio.NewReader(os.Stdin), os.Stdout)
	if err != nil {
		return 1, err
	}
	if !decided {
		fmt.Println("\nno decision made — re-run with --trust code|live|partial to act.")
		return drift.ExitDrift, nil
	}
	if len(live) == 0 {
		fmt.Println("\nkeeping code as the source of truth. no changes made.")
		return 0, nil
	}

	changes, err := classify(ctx, cfg, p, *dir, live)
	if err != nil {
		return 1, err
	}
	editable := 0
	for _, c := range changes {
		if len(c.Edits) > 0 {
			editable++
		}
	}
	fmt.Printf("\ntrusting live for %d resource(s); %d attribute(s) editable\n", len(live), editable)
	if editable == 0 {
		fmt.Println("nothing could be edited automatically (tier 3). see the report above.")
		return 0, nil
	}

	repo := gitops.New(*dir)
	branch, err := repo.NewBranch(ctx, cfg.Git.BranchPrefix, time.Now())
	if err != nil {
		return 1, err
	}
	fmt.Println("created branch", branch)

	if err := applyAndCommit(ctx, repo, *dir, changes); err != nil {
		return 1, err
	}

	changes, err = verifyLoop(ctx, cfg, runner, *dir, repo, changes)
	if err != nil {
		return 1, err
	}

	if *noPR || !cfg.Git.OpenPR {
		fmt.Println("skipping PR (per flags/config); review branch", branch)
		return 0, nil
	}
	forge, err := gitops.NewForge(cfg.Git)
	if err != nil {
		return 1, err
	}
	if err := repo.Push(ctx, branch); err != nil {
		return 1, err
	}
	url, err := forge.OpenPR(ctx, gitops.PullRequest{
		Title:        prTitle(changes),
		Body:         prBody(changes),
		SourceBranch: branch,
		TargetBranch: cfg.Git.TargetBranch,
	})
	if err != nil {
		return 1, err
	}
	fmt.Println("opened PR:", url)
	return 0, nil
}

// classify walks provenance and resolves edits for the resources trusted live.
func classify(ctx context.Context, cfg config.Config, p *drift.Plan, dir string, live map[string]bool) ([]change, error) {
	var out []change
	for _, r := range p.ResourceDrift {
		if !live[r.Address] {
			continue
		}
		if r.Deleted() {
			out = append(out, change{Address: r.Address, Note: "resource deleted outside terraform (tier 3 — human decision)"})
			continue
		}
		attrs, err := r.ChangedAttrs()
		if err != nil {
			return nil, err
		}
		for _, a := range attrs {
			c := change{Address: r.Address, Attr: a}
			prov, err := provenance.Walk(p.Configuration, r.Address, a.Attribute, dir)
			if err != nil {
				c.Note = "provenance walk failed: " + err.Error()
				out = append(out, c)
				continue
			}
			c.Prov = prov
			resolveChange(ctx, cfg, dir, &c)
			out = append(out, c)
		}
	}
	return out, nil
}

// resolveChange fills c.Edits (tiers 0-2) or c.Note (tier 3 / rejections).
func resolveChange(ctx context.Context, cfg config.Config, dir string, c *change) {
	switch c.Prov.Tier {
	case provenance.Tier0Literal, provenance.Tier1Passthrough:
		c.Edits = []contract.Edit{{
			File:      c.Prov.Target.File,
			BlockAddr: c.Prov.Target.BlockAddr,
			Attribute: c.Prov.Target.Attribute,
			Op:        contract.OpSet,
			Value:     c.Attr.After,
		}}
	case provenance.Tier2Transforming:
		proposal, err := propose(ctx, cfg, dir, c)
		if err != nil {
			c.Note = "tier 2, server proposal failed: " + err.Error()
			return
		}
		for _, e := range proposal.Edits {
			if err := patch.Guard(e, cfg.ProtectedPaths, c.Prov); err != nil {
				c.Note = "tier 2, proposal rejected by safety guard: " + err.Error()
				c.Edits = nil
				return
			}
			c.Edits = append(c.Edits, e)
		}
	default:
		c.Note = "tier 3 (" + c.Prov.Note + ") — keep the code to revert, or edit manually"
	}
}

func propose(ctx context.Context, cfg config.Config, dir string, c *change) (contract.ProposalOutput, error) {
	var in contract.ProposalInput
	in.Drift.Address = c.Address
	in.Drift.Attribute = c.Attr.Attribute
	in.Drift.Before = c.Attr.Before
	in.Drift.After = c.Attr.After
	in.Provenance = c.Prov.Chain
	in.FileExcerpts = excerpts(dir, c.Prov.Chain)
	for _, pp := range cfg.ProtectedPaths {
		in.SafetyRules = append(in.SafetyRules, "never edit files under "+pp)
	}
	return mcpclient.New(cfg.MCP, version).Propose(ctx, in)
}

// excerpts loads each chain file (capped at 16KB) as model context.
// Chain paths are relative to the Terraform root dir.
func excerpts(dir string, chain []contract.ChainLinkDTO) map[string]string {
	out := map[string]string{}
	for _, link := range chain {
		if link.File == "" || out[link.File] != "" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, link.File))
		if err == nil && len(data) <= 16*1024 {
			out[link.File] = string(data)
		}
	}
	return out
}

// applyAndCommit writes the pending edits, one commit per change.
func applyAndCommit(ctx context.Context, repo *gitops.Repo, dir string, changes []change) error {
	for i := range changes {
		c := &changes[i]
		if len(c.Edits) == 0 {
			continue
		}
		var files []string
		for _, e := range c.Edits {
			if err := patch.Apply(dir, e); err != nil {
				c.Note = "apply failed: " + err.Error()
				c.Edits = nil
				break
			}
			files = append(files, e.File)
		}
		if len(files) == 0 {
			continue
		}
		msg := fmt.Sprintf("drift-sync: %s %s\n\n%s", c.Address, c.Attr.Attribute, renderChain(c.Prov.Chain))
		if err := repo.Commit(ctx, msg, files...); err != nil {
			return fmt.Errorf("commit %s: %w", c.Address, err)
		}
		fmt.Printf("  committed fix for %s.%s (%s)\n", c.Address, c.Attr.Attribute, c.Prov.Tier)
	}
	return nil
}

// verifyLoop re-plans and retries tier-2 residuals up to max_retries (§8 rule 3).
func verifyLoop(ctx context.Context, cfg config.Config, runner *tf.Runner, dir string, repo *gitops.Repo, changes []change) ([]change, error) {
	for attempt := 0; ; attempt++ {
		code, planJSON, err := runner.RefreshPlan(ctx)
		if err != nil {
			return changes, err
		}
		if code == drift.ExitClean {
			fmt.Println("verify: re-plan is clean")
			return changes, nil
		}
		p, err := drift.ParsePlan(planJSON)
		if err != nil {
			return changes, err
		}
		dirty := map[string]bool{}
		for _, r := range p.ResourceDrift {
			dirty[r.Address] = true
		}

		retried := false
		for i := range changes {
			c := &changes[i]
			if !dirty[c.Address] || c.Prov.Tier != provenance.Tier2Transforming || len(c.Edits) == 0 {
				continue
			}
			if attempt >= cfg.MCP.MaxRetries {
				c.Note = fmt.Sprintf("still drifted after %d retries — degraded to tier 3", cfg.MCP.MaxRetries)
				c.Edits = nil
				continue
			}
			// Retry against the residual diff from the fresh plan.
			if residual := findResidual(p, c.Address, c.Attr.Attribute); residual != nil {
				c.Attr = *residual
				resolveChange(ctx, cfg, dir, c)
				retried = true
			}
		}
		if !retried {
			markDirty(changes, dirty)
			fmt.Println("verify: some resources remain drifted; noted in the PR body")
			return changes, nil
		}
		if err := applyAndCommit(ctx, repo, dir, changes); err != nil {
			return changes, err
		}
	}
}

func findResidual(p *drift.Plan, address, attribute string) *drift.AttrDrift {
	for _, r := range p.ResourceDrift {
		if r.Address != address {
			continue
		}
		attrs, err := r.ChangedAttrs()
		if err != nil {
			return nil
		}
		for _, a := range attrs {
			if a.Attribute == attribute {
				return &a
			}
		}
	}
	return nil
}

func markDirty(changes []change, dirty map[string]bool) {
	for i := range changes {
		if dirty[changes[i].Address] && changes[i].Note == "" {
			changes[i].Note = "re-plan still shows drift on this resource"
		}
	}
}

func prTitle(changes []change) string {
	return fmt.Sprintf("drift-sync: update code to match live infrastructure (%d attribute(s))", len(changes))
}

func prBody(changes []change) string {
	var b strings.Builder
	b.WriteString("You chose to trust live infrastructure. Each commit rewrites the code to match reality.\n\n")
	b.WriteString("**Merge** to keep these changes. **Close** to abandon them (the next apply reverts the live drift).\n\n")
	for _, c := range changes {
		fmt.Fprintf(&b, "### `%s` — `%s`\n", c.Address, c.Attr.Attribute)
		fmt.Fprintf(&b, "- tier: %s\n", c.Prov.Tier)
		if len(c.Attr.Before) > 0 || len(c.Attr.After) > 0 {
			fmt.Fprintf(&b, "- value: `%s` → `%s`\n", compact(c.Attr.Before), compact(c.Attr.After))
		}
		if len(c.Prov.Chain) > 0 {
			b.WriteString("- provenance:\n")
			b.WriteString(renderChain(c.Prov.Chain))
		}
		if c.Note != "" {
			fmt.Fprintf(&b, "- ⚠ %s\n", c.Note)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func renderChain(chain []contract.ChainLinkDTO) string {
	var b strings.Builder
	for _, l := range chain {
		loc := l.File
		if l.Line > 0 {
			loc = fmt.Sprintf("%s:%d", filepath.ToSlash(l.File), l.Line)
		}
		fmt.Fprintf(&b, "  - %s: `%s` (%s)\n", l.Kind, l.Expr, loc)
	}
	return b.String()
}

func compact(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "∅"
	}
	s := string(raw)
	if len(s) > 80 {
		s = s[:77] + "..."
	}
	return s
}
