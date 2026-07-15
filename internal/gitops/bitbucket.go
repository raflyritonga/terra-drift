package gitops

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/raflyritonga/terra-drift/internal/config"
)

// bitbucket targets Bitbucket Cloud (or Server via api_base). Auth: a workspace
// access token (BITBUCKET_TOKEN) or username + app password.
type bitbucket struct {
	apiBase, workspace, repo string
	token, user, pass        string
	http                     *http.Client
}

func newBitbucket(cfg config.Git) (*bitbucket, error) {
	b := &bitbucket{
		apiBase:   base(cfg.APIBase, "https://api.bitbucket.org/2.0"),
		workspace: cfg.Workspace,
		repo:      cfg.Repo,
		token:     os.Getenv("BITBUCKET_TOKEN"),
		user:      os.Getenv("BITBUCKET_USERNAME"),
		pass:      os.Getenv("BITBUCKET_APP_PASSWORD"),
		http:      httpClient(),
	}
	if b.token == "" && (b.user == "" || b.pass == "") {
		return nil, fmt.Errorf("set BITBUCKET_TOKEN, or BITBUCKET_USERNAME + BITBUCKET_APP_PASSWORD")
	}
	return b, nil
}

func (b *bitbucket) OpenPR(ctx context.Context, pr PullRequest) (string, error) {
	body := map[string]any{
		"title":       pr.Title,
		"description": pr.Body,
		"source":      map[string]any{"branch": map[string]string{"name": pr.SourceBranch}},
		"destination": map[string]any{"branch": map[string]string{"name": pr.TargetBranch}},
	}
	url := fmt.Sprintf("%s/repositories/%s/%s/pullrequests", b.apiBase, b.workspace, b.repo)

	var out struct {
		Links struct {
			HTML struct {
				Href string `json:"href"`
			} `json:"html"`
		} `json:"links"`
	}
	err := postJSON(ctx, b.http, url, body, &out, func(req *http.Request) {
		if b.token != "" {
			req.Header.Set("Authorization", "Bearer "+b.token)
		} else {
			req.SetBasicAuth(b.user, b.pass)
		}
	})
	if err != nil {
		return "", fmt.Errorf("bitbucket open PR: %w", err)
	}
	return out.Links.HTML.Href, nil
}
