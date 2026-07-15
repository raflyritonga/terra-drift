package gitops

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/raflyritonga/terra-drift/internal/config"
)

// github targets GitHub (or Enterprise via api_base). Auth: GH_TOKEN / GITHUB_TOKEN.
type github struct {
	apiBase, owner, repo, token string
	http                        *http.Client
}

func newGitHub(cfg config.Git) (*github, error) {
	tok := os.Getenv("GH_TOKEN")
	if tok == "" {
		tok = os.Getenv("GITHUB_TOKEN")
	}
	if tok == "" {
		return nil, fmt.Errorf("set GH_TOKEN or GITHUB_TOKEN")
	}
	return &github{
		apiBase: base(cfg.APIBase, "https://api.github.com"),
		owner:   cfg.Workspace,
		repo:    cfg.Repo,
		token:   tok,
		http:    httpClient(),
	}, nil
}

func (g *github) OpenPR(ctx context.Context, pr PullRequest) (string, error) {
	body := map[string]string{
		"title": pr.Title,
		"body":  pr.Body,
		"head":  pr.SourceBranch,
		"base":  pr.TargetBranch,
	}
	url := fmt.Sprintf("%s/repos/%s/%s/pulls", g.apiBase, g.owner, g.repo)

	var out struct {
		HTMLURL string `json:"html_url"`
	}
	err := postJSON(ctx, g.http, url, body, &out, func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+g.token)
		req.Header.Set("Accept", "application/vnd.github+json")
	})
	if err != nil {
		return "", fmt.Errorf("github open PR: %w", err)
	}
	return out.HTMLURL, nil
}
