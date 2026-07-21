package gitops

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/raflyritonga/terra-drift/internal/config"
)

// PullRequest is the provider-agnostic request to open a PR/MR.
type PullRequest struct {
	Title, Body, SourceBranch, TargetBranch string
}

// Forge opens a pull/merge request via a git host's REST API.
// The host can be anywhere the runner can reach — it need not host the runner.
type Forge interface {
	OpenPR(ctx context.Context, pr PullRequest) (string, error)
}

// Commit is a branch + one commit's worth of file contents, published via the
// host's REST API instead of git push.
type Commit struct {
	Branch, TargetBranch, Message string
	Files                         map[string][]byte // repo-relative path → contents
}

// BranchPublisher is implemented by forges that can create the branch and
// commit over REST (no git transport). Bitbucket only, today.
type BranchPublisher interface {
	PublishBranch(ctx context.Context, c Commit) error
}

func NewForge(cfg config.Git) (Forge, error) {
	if cfg.Workspace == "" || cfg.Repo == "" {
		return nil, fmt.Errorf("git.workspace and git.repo are required to open a PR")
	}
	switch cfg.Provider {
	case "bitbucket", "":
		return newBitbucket(cfg)
	case "github":
		return newGitHub(cfg)
	case "gitlab":
		return newGitLab(cfg)
	default:
		return nil, fmt.Errorf("unknown git.provider %q (bitbucket | github | gitlab)", cfg.Provider)
	}
}

func httpClient() *http.Client { return &http.Client{Timeout: 30 * time.Second} }

func base(url, fallback string) string {
	if url == "" {
		return fallback
	}
	return strings.TrimRight(url, "/")
}

// postJSON sends body as JSON, applies auth, and decodes a 2xx response into out.
func postJSON(ctx context.Context, c *http.Client, url string, body, out any, auth func(*http.Request)) error {
	buf, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	auth(req)

	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s: %s", resp.Status, snippet(data))
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

// getJSON GETs url with auth and decodes a 2xx response into out.
func getJSON(ctx context.Context, c *http.Client, url string, out any, auth func(*http.Request)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	auth(req)

	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s: %s", resp.Status, snippet(data))
	}
	return json.Unmarshal(data, out)
}

// postForm POSTs a prebuilt multipart body with auth; success needs no decoding.
func postForm(ctx context.Context, c *http.Client, url, contentType string, body *bytes.Buffer, auth func(*http.Request)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	auth(req)

	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s: %s", resp.Status, snippet(data))
	}
	return nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
