package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
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

// Outcome buckets, reported in logs and the PR body.
const (
	statusPending   = "pending"
	statusApplied   = "applied" // provisional until the verify re-plan
	statusFixed     = "fixed"
	statusRemaining = "remaining" // applied, but the re-plan still shows drift
	statusProtected = "skipped-protected"
	statusFailed    = "could-not-fix"
)

// change is one drifted attribute with its classification and resolved edits.
type change struct {
	Address string
	Attr    drift.AttrDrift
	Prov    provenance.Provenance
	Edits   []contract.Edit
	Note    string
	Status  string
}

// runSync detects drift, reports it small, asks whose version to trust, and
// on "live" rewrites the code (→ fmt/validate → verify → PR). Exit codes:
// 0 = no drift, 2 = drift was found (whatever action was taken), 1 = error.
func runSync(args []string) (int, error) {
	fs := flag.NewFlagSet("sync", flag.ExitOnError)
	dir := fs.String("dir", ".", "Terraform root directory")
	trustMode := fs.String("trust", "", "whose version to trust: code | live | partial (default: ask, or report-only in CI)")
	liveCSV := fs.String("live", "", "partial mode: comma-separated resource addresses to trust live")
	noPR := fs.Bool("no-pr", false, "on the live path, commit the branch but skip push/PR")
	dryRun := fs.Bool("dry-run", false, "rewrite and print the diff, then restore; no branch/commit/PR")
	explain := fs.Bool("explain", false, "ask the model server for a short explanation before deciding")
	fs.Parse(args)

	ctx := context.Background()
	cfg, err := config.Load(*dir)
	if err != nil {
		return 1, err
	}
	pushMode, err := cfg.Git.ResolvedPushMode()
	if err != nil {
		return 1, err
	}

	runner := tf.New(*dir)
	code, planJSON, err := runner.RefreshPlan(ctx)
	if err != nil {
		// C4: classified refresh failures skip the root with a reason
		// instead of an opaque hard failure.
		if re, ok := errors.AsType[*tf.RefreshError](err); ok {
			slog.Error("root skipped", "dir", *dir, "reason", re.Reason)
			fmt.Printf("summary: roots covered 0/1, skipped 1 (%s)\n%s\n", re.Reason, re.Detail)
			return 1, nil
		}
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
	hash, err := p.Hash()
	if err != nil {
		return 1, err
	}
	items, err := p.Report(*dir)
	if err != nil {
		return 1, err
	}
	slog.Info("drift detected", "resources", len(items), "drift_hash", hash)
	fmt.Print(drift.RenderReport(items))
	if *explain {
		printExplanation(ctx, *dir, planJSON)
	}

	// C3: an open PR already covering this exact drift set means stop early.
	var existing *gitops.ExistingPR
	if !*dryRun && !*noPR && cfg.Git.OpenPR {
		existing = findExistingPR(ctx, cfg)
		if existing != nil && strings.Contains(existing.Body, hashMarker(hash)) {
			slog.Info("dedupe: open PR already covers this drift", "url", existing.URL)
			fmt.Printf("\nan open PR already covers this exact drift: %s\nnothing to do.\n", existing.URL)
			return drift.ExitDrift, nil
		}
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
		return drift.ExitDrift, nil
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
		fmt.Println("nothing could be edited automatically. see the report above.")
		printBuckets(changes)
		return drift.ExitDrift, nil
	}

	if *dryRun {
		if err := dryRunApply(ctx, *dir, changes); err != nil {
			return 1, err
		}
		return drift.ExitDrift, nil
	}

	fmtValidate := func(ctx context.Context) error {
		if err := runner.Fmt(ctx); err != nil {
			return err
		}
		return runner.Validate(ctx)
	}

	// Two publish transports: "git" branches/commits/pushes locally; "api"
	// (bitbucket default) edits the working tree only and publishes the
	// branch + commit over REST — Atlassian API tokens can't git push.
	repo := gitops.New(*dir)
	var branch string
	var apply applier
	touched := map[string]bool{}
	if pushMode == "api" {
		branch = gitops.BranchName(cfg.Git.BranchPrefix, time.Now())
		if existing != nil {
			// C3: append to the open drift PR's branch instead of a new one.
			branch = existing.SourceBranch
			slog.Info("dedupe: updating existing drift PR", "branch", branch, "url", existing.URL)
		}
		apply = func(c context.Context, chs []change) error {
			if err := applyTrack(*dir, chs, touched); err != nil {
				return err
			}
			return fmtValidate(c)
		}
		fmt.Printf("branch %s will be published via the %s API\n", branch, cfg.Git.Provider)
	} else {
		branch, err = repo.NewBranch(ctx, cfg.Git.BranchPrefix, time.Now())
		if err != nil {
			return 1, err
		}
		apply = func(c context.Context, chs []change) error {
			return applyAndCommit(c, repo, *dir, chs, fmtValidate)
		}
		fmt.Println("created branch", branch)
	}

	if err := apply(ctx, changes); err != nil {
		return 1, err
	}

	// C1: not "fixed" until a fresh re-plan proves the drift is gone.
	changes, err = verifyLoop(ctx, cfg, runner, *dir, apply, changes)
	if err != nil {
		return 1, err
	}
	finalizeStatuses(changes)
	printBuckets(changes)

	if *noPR || !cfg.Git.OpenPR {
		if pushMode == "api" {
			fmt.Println("skipping publish/PR (per flags/config); edits are in the working tree")
		} else {
			fmt.Println("skipping PR (per flags/config); review branch", branch)
		}
		return drift.ExitDrift, nil
	}
	forge, err := gitops.NewForge(cfg.Git)
	if err != nil {
		return 1, err
	}

	if pushMode == "api" {
		pub, ok := forge.(gitops.BranchPublisher)
		if !ok {
			return 1, fmt.Errorf("git.push_mode=api is not supported by the %s provider", cfg.Git.Provider)
		}
		files, err := collectFiles(ctx, repo, *dir, touched)
		if err != nil {
			return 1, err
		}
		if err := pub.PublishBranch(ctx, gitops.Commit{
			Branch:       branch,
			TargetBranch: cfg.Git.TargetBranch,
			Message:      commitMessage(changes),
			Files:        files,
		}); err != nil {
			return 1, err
		}
		slog.Info("published branch via API", "branch", branch, "files", len(files))
	} else {
		if err := repo.Push(ctx, branch); err != nil {
			return 1, err
		}
	}

	title, body := prTitle(changes), prBody(changes, hash)
	if existing != nil {
		if mgr, ok := forge.(gitops.PRManager); ok {
			if err := mgr.UpdatePR(ctx, existing.ID, title, body); err != nil {
				return 1, err
			}
			slog.Info("updated existing PR", "url", existing.URL)
			fmt.Println("updated PR:", existing.URL)
			return drift.ExitDrift, nil
		}
	}
	url, err := forge.OpenPR(ctx, gitops.PullRequest{
		Title:        title,
		Body:         body,
		SourceBranch: branch,
		TargetBranch: cfg.Git.TargetBranch,
	})
	if err != nil {
		return 1, err
	}
	slog.Info("opened PR", "url", url)
	fmt.Println("opened PR:", url)
	return drift.ExitDrift, nil
}

// findExistingPR looks for an open drift PR; failures just disable dedupe.
func findExistingPR(ctx context.Context, cfg config.Config) *gitops.ExistingPR {
	forge, err := gitops.NewForge(cfg.Git)
	if err != nil {
		return nil
	}
	mgr, ok := forge.(gitops.PRManager)
	if !ok {
		return nil
	}
	pr, err := mgr.FindOpenPR(ctx, cfg.Git.BranchPrefix)
	if err != nil {
		slog.Warn("dedupe lookup failed; proceeding without it", "err", err.Error())
		return nil
	}
	return pr
}

func hashMarker(hash string) string { return "drift-hash: " + hash }

// dryRunApply rewrites the files, prints the diff, and restores the originals.
func dryRunApply(ctx context.Context, dir string, changes []change) error {
	originals := map[string][]byte{}
	for _, c := range changes {
		for _, e := range c.Edits {
			if _, ok := originals[e.File]; !ok {
				data, err := os.ReadFile(filepath.Join(dir, e.File))
				if err != nil {
					return err
				}
				originals[e.File] = data
			}
		}
	}
	touched := map[string]bool{}
	if err := applyTrack(dir, changes, touched); err != nil {
		return err
	}
	defer func() {
		for f, data := range originals {
			os.WriteFile(filepath.Join(dir, f), data, 0o644)
		}
	}()

	files := make([]string, 0, len(touched))
	for f := range touched {
		files = append(files, f)
	}
	diff, err := gitops.New(dir).Diff(ctx, files...)
	if err != nil {
		return fmt.Errorf("dry-run diff: %w", err)
	}
	fmt.Println("\n--- dry run: proposed diff (no branch/commit/PR) ---")
	fmt.Println(diff)
	return nil
}

// finalizeStatuses upgrades provisional "applied" to fixed/remaining.
func finalizeStatuses(changes []change) {
	for i := range changes {
		c := &changes[i]
		if c.Status != statusApplied {
			continue
		}
		if strings.Contains(c.Note, "re-plan still shows drift") || strings.Contains(c.Note, "still drifted") {
			c.Status = statusRemaining
		} else {
			c.Status = statusFixed
		}
	}
}

// printBuckets logs the three outcome buckets (C5).
func printBuckets(changes []change) {
	counts := map[string]int{}
	for _, c := range changes {
		counts[bucket(c.Status)]++
	}
	slog.Info("outcome",
		"fixed", counts[statusFixed],
		"skipped_protected", counts[statusProtected],
		"could_not_fix", counts[statusFailed])
	fmt.Printf("\noutcome: %d fixed, %d skipped-protected, %d could-not-fix\n",
		counts[statusFixed], counts[statusProtected], counts[statusFailed])
}

// bucket folds statuses into the three reported buckets.
func bucket(status string) string {
	switch status {
	case statusFixed:
		return statusFixed
	case statusProtected:
		return statusProtected
	default:
		return statusFailed
	}
}

// applier applies the resolved edits; git mode also commits, api mode only
// tracks which files were touched.
type applier func(ctx context.Context, changes []change) error

// applyTrack writes edits to the working tree and records the touched files.
func applyTrack(dir string, changes []change, touched map[string]bool) error {
	for i := range changes {
		c := &changes[i]
		if len(c.Edits) == 0 {
			continue
		}
		applied := false
		for _, e := range c.Edits {
			if err := patch.Apply(dir, e); err != nil {
				c.Note = "apply failed: " + err.Error()
				c.Status = statusFailed
				c.Edits = nil
				applied = false
				break
			}
			touched[e.File] = true
			applied = true
		}
		if applied {
			c.Status = statusApplied
			fmt.Printf("  applied fix for %s.%s (%s)\n", c.Address, c.Attr.Attribute, c.Prov.Tier)
		}
	}
	return nil
}

// collectFiles reads the touched files and keys them by repo-relative path,
// which is what the Bitbucket /src endpoint expects as field names.
func collectFiles(ctx context.Context, repo *gitops.Repo, dir string, touched map[string]bool) (map[string][]byte, error) {
	top, err := repo.TopLevel(ctx)
	if err != nil {
		return nil, fmt.Errorf("find repo root: %w", err)
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	for f := range touched {
		abs := filepath.Clean(filepath.Join(absDir, f))
		rel, err := filepath.Rel(top, abs)
		if err != nil || strings.HasPrefix(rel, "..") {
			return nil, fmt.Errorf("edited file %s is outside the repository %s", abs, top)
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return nil, err
		}
		out[filepath.ToSlash(rel)] = data
	}
	return out, nil
}

// commitMessage summarizes all changes for the single API-mode commit.
func commitMessage(changes []change) string {
	var b strings.Builder
	b.WriteString(prTitle(changes))
	for _, c := range changes {
		if len(c.Edits) > 0 {
			fmt.Fprintf(&b, "\n- %s %s", c.Address, c.Attr.Attribute)
		}
	}
	return b.String()
}

// classify walks provenance and resolves edits for the resources trusted live.
func classify(ctx context.Context, cfg config.Config, p *drift.Plan, dir string, live map[string]bool) ([]change, error) {
	var out []change
	for _, r := range p.ResourceDrift {
		if !live[r.Address] {
			continue
		}
		if r.Deleted() {
			out = append(out, change{Address: r.Address, Status: statusFailed, Note: "resource deleted outside terraform (tier 3 — human decision)"})
			continue
		}
		attrs, err := r.ChangedAttrs()
		if err != nil {
			return nil, err
		}
		for _, a := range attrs {
			c := change{Address: r.Address, Attr: a, Status: statusPending}
			prov, err := provenance.Walk(p.Configuration, r.Address, a.Attribute, dir)
			if err != nil {
				c.Note = "provenance walk failed: " + err.Error()
				c.Status = statusFailed
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

// resolveChange fills c.Edits (tiers 0-2) or c.Note + status on rejection.
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
			c.Status = statusFailed
			return
		}
		allowed := patch.AllowedFor(c.Attr.Attribute, c.Prov)
		for _, e := range proposal.Edits {
			if err := patch.Guard(e, cfg.ProtectedPaths, c.Prov); err != nil {
				c.Note = "skipped: " + err.Error()
				c.Status = statusProtected
				c.Edits = nil
				return
			}
			// C2: reject any proposed edit outside the drifted set.
			if err := patch.GuardMinimal(e, allowed); err != nil {
				c.Note = "rejected: " + err.Error()
				c.Status = statusFailed
				c.Edits = nil
				slog.Warn("minimal-diff guard rejected proposal", "address", c.Address, "err", err.Error())
				return
			}
			c.Edits = append(c.Edits, e)
		}
	default:
		c.Note = "tier 3 (" + c.Prov.Note + ") — keep the code to revert, or edit manually"
		c.Status = statusFailed
	}
}

func propose(ctx context.Context, cfg config.Config, dir string, c *change) (contract.ProposalOutput, error) {
	var in contract.ProposalInput
	in.ContractVersion = contract.Version
	in.Drift.Address = c.Address
	in.Drift.Attribute = c.Attr.Attribute
	in.Drift.Before = c.Attr.Before
	in.Drift.After = c.Attr.After
	in.Provenance = c.Prov.Chain
	in.FileExcerpts = excerpts(dir, c.Prov.Chain)
	for a := range patch.AllowedFor(c.Attr.Attribute, c.Prov).Attrs {
		in.AllowedAttrs = append(in.AllowedAttrs, a)
	}
	for _, pp := range cfg.ProtectedPaths {
		in.SafetyRules = append(in.SafetyRules, "never edit files under "+pp)
	}
	return mcpclient.New(cfg.MCP, version).Propose(ctx, in)
}

// excerpts sends only the target block per chain link — never whole files.
func excerpts(dir string, chain []contract.ChainLinkDTO) map[string]string {
	out := map[string]string{}
	for _, link := range chain {
		if link.File == "" || out[link.File] != "" {
			continue
		}
		if snip := provenance.BlockSnippet(dir, link.File, link.Line); snip != "" {
			out[link.File] = snip
		}
	}
	return out
}

// applyAndCommit applies every pending edit, formats/validates the tree, then
// commits one commit per change — so the committed files are already formatted.
func applyAndCommit(ctx context.Context, repo *gitops.Repo, dir string, changes []change, fmtValidate func(context.Context) error) error {
	filesByChange := map[int][]string{}
	for i := range changes {
		c := &changes[i]
		if len(c.Edits) == 0 || c.Status == statusApplied {
			continue
		}
		for _, e := range c.Edits {
			if err := patch.Apply(dir, e); err != nil {
				c.Note = "apply failed: " + err.Error()
				c.Status = statusFailed
				c.Edits = nil
				delete(filesByChange, i)
				break
			}
			filesByChange[i] = append(filesByChange[i], e.File)
		}
	}
	if len(filesByChange) == 0 {
		return nil
	}
	if err := fmtValidate(ctx); err != nil {
		return err
	}
	for i, files := range filesByChange {
		c := &changes[i]
		msg := fmt.Sprintf("drift-sync: %s %s\n\n%s", c.Address, c.Attr.Attribute, renderChain(c.Prov.Chain))
		if err := repo.Commit(ctx, msg, files...); err != nil {
			return fmt.Errorf("commit %s: %w", c.Address, err)
		}
		c.Status = statusApplied
		fmt.Printf("  committed fix for %s.%s (%s)\n", c.Address, c.Attr.Attribute, c.Prov.Tier)
	}
	return nil
}

// verifyLoop re-plans and retries tier-2 residuals up to max_retries.
func verifyLoop(ctx context.Context, cfg config.Config, runner *tf.Runner, dir string, apply applier, changes []change) ([]change, error) {
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
				c.Status = statusRemaining
				c.Edits = nil
				continue
			}
			// Retry against the residual diff from the fresh plan.
			if residual := findResidual(p, c.Address, c.Attr.Attribute); residual != nil {
				c.Attr = *residual
				c.Status = statusPending
				resolveChange(ctx, cfg, dir, c)
				retried = true
			}
		}
		if !retried {
			markDirty(changes, dirty)
			fmt.Println("verify: some resources remain drifted; noted in the PR body")
			return changes, nil
		}
		if err := apply(ctx, changes); err != nil {
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

// prBody groups changes into the three outcome buckets, adds a verification
// summary, and embeds the drift-hash marker used for dedupe.
func prBody(changes []change, hash string) string {
	var b strings.Builder
	b.WriteString("You chose to trust live infrastructure. Each change rewrites the code to match reality.\n\n")
	b.WriteString("**Merge** to keep these changes. **Close** to abandon them (the next apply reverts the live drift).\n\n")

	counts := map[string]int{}
	for _, c := range changes {
		counts[bucket(c.Status)]++
	}
	fmt.Fprintf(&b, "**Verification:** %d fixed (re-plan clean), %d skipped (protected paths), %d could not be fixed.\n\n",
		counts[statusFixed], counts[statusProtected], counts[statusFailed])

	sections := []struct{ title, want string }{
		{"Fixed — drift resolved, verified by re-plan", statusFixed},
		{"Skipped — protected paths", statusProtected},
		{"Could not fix", statusFailed},
	}
	for _, s := range sections {
		var body strings.Builder
		for _, c := range changes {
			if bucket(c.Status) != s.want {
				continue
			}
			fmt.Fprintf(&body, "### `%s` — `%s`\n", c.Address, c.Attr.Attribute)
			fmt.Fprintf(&body, "- tier: %s\n", c.Prov.Tier)
			if len(c.Attr.Before) > 0 || len(c.Attr.After) > 0 {
				fmt.Fprintf(&body, "- value: `%s` → `%s`\n", compact(c.Attr.Before), compact(c.Attr.After))
			}
			if len(c.Prov.Chain) > 0 {
				body.WriteString("- provenance:\n")
				body.WriteString(renderChain(c.Prov.Chain))
			}
			if c.Note != "" {
				fmt.Fprintf(&body, "- ⚠ %s\n", c.Note)
			}
			body.WriteString("\n")
		}
		if body.Len() > 0 {
			fmt.Fprintf(&b, "## %s\n\n%s", s.title, body.String())
		}
	}

	b.WriteString("---\n" + hashMarker(hash) + "\n")
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
