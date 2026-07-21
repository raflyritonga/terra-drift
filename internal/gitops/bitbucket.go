package gitops

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"os"
	"sort"

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

// auth applies the same credentials used for every Bitbucket REST call.
func (b *bitbucket) auth(req *http.Request) {
	if b.token != "" {
		req.Header.Set("Authorization", "Bearer "+b.token)
	} else {
		req.SetBasicAuth(b.user, b.pass)
	}
}

// branchHead returns the commit SHA at the tip of branch.
func (b *bitbucket) branchHead(ctx context.Context, branch string) (string, error) {
	url := fmt.Sprintf("%s/repositories/%s/%s/refs/branches/%s", b.apiBase, b.workspace, b.repo, branch)
	var out struct {
		Target struct {
			Hash string `json:"hash"`
		} `json:"target"`
	}
	if err := getJSON(ctx, b.http, url, &out, b.auth); err != nil {
		return "", fmt.Errorf("bitbucket branch head %q: %w", branch, err)
	}
	if out.Target.Hash == "" {
		return "", fmt.Errorf("bitbucket branch head %q: response has no target.hash", branch)
	}
	return out.Target.Hash, nil
}

// PublishBranch creates the branch and its commit in one POST /src call —
// pure REST, so Atlassian API tokens work (git-over-HTTPS rejects them).
func (b *bitbucket) PublishBranch(ctx context.Context, c Commit) error {
	if len(c.Files) == 0 {
		return fmt.Errorf("bitbucket publish: no files to commit")
	}
	parent, err := b.branchHead(ctx, c.TargetBranch)
	if err != nil {
		return err
	}

	body, contentType, err := buildSrcForm(c, parent)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("%s/repositories/%s/%s/src", b.apiBase, b.workspace, b.repo)
	if err := postForm(ctx, b.http, url, contentType, body, b.auth); err != nil {
		return fmt.Errorf("bitbucket create commit on %q: %w", c.Branch, err)
	}
	return nil
}

// buildSrcForm assembles the multipart form for POST /src: message/branch/
// parents fields plus one field per file, named by its repo-relative path.
func buildSrcForm(c Commit, parent string) (*bytes.Buffer, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range map[string]string{
		"message": c.Message,
		"branch":  c.Branch,
		"parents": parent,
	} {
		if err := w.WriteField(k, v); err != nil {
			return nil, "", err
		}
	}
	// sorted for a deterministic body (and testable ordering)
	paths := make([]string, 0, len(c.Files))
	for p := range c.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		if err := w.WriteField(p, string(c.Files[p])); err != nil {
			return nil, "", err
		}
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return &buf, w.FormDataContentType(), nil
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
	if err := postJSON(ctx, b.http, url, body, &out, b.auth); err != nil {
		return "", fmt.Errorf("bitbucket open PR: %w", err)
	}
	return out.Links.HTML.Href, nil
}
