package gitops

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"

	"github.com/raflyritonga/terra-drift/internal/config"
)

// gitlab targets GitLab (or self-managed via api_base). Auth: GITLAB_TOKEN.
type gitlab struct {
	apiBase, project, token string
	http                    *http.Client
}

func newGitLab(cfg config.Git) (*gitlab, error) {
	tok := os.Getenv("GITLAB_TOKEN")
	if tok == "" {
		return nil, fmt.Errorf("set GITLAB_TOKEN")
	}
	return &gitlab{
		apiBase: base(cfg.APIBase, "https://gitlab.com"),
		project: url.PathEscape(cfg.Workspace + "/" + cfg.Repo),
		token:   tok,
		http:    httpClient(),
	}, nil
}

func (g *gitlab) OpenPR(ctx context.Context, pr PullRequest) (string, error) {
	body := map[string]string{
		"source_branch": pr.SourceBranch,
		"target_branch": pr.TargetBranch,
		"title":         pr.Title,
		"description":   pr.Body,
	}
	u := fmt.Sprintf("%s/api/v4/projects/%s/merge_requests", g.apiBase, g.project)

	var out struct {
		WebURL string `json:"web_url"`
	}
	err := postJSON(ctx, g.http, u, body, &out, func(req *http.Request) {
		req.Header.Set("PRIVATE-TOKEN", g.token)
	})
	if err != nil {
		return "", fmt.Errorf("gitlab open MR: %w", err)
	}
	return out.WebURL, nil
}
