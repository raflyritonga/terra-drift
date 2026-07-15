package tests

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/raflyritonga/terra-drift/internal/config"
	"github.com/raflyritonga/terra-drift/internal/gitops"
)

type recorder struct {
	req  *http.Request
	body []byte
}

// fakeForge stands up a server returning reply, opens a PR, returns the URL + what was sent.
func fakeForge(t *testing.T, cfg config.Git, reply string) (string, *recorder) {
	t.Helper()
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.req = r
		rec.body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)

	cfg.APIBase = srv.URL
	f, err := gitops.NewForge(cfg)
	if err != nil {
		t.Fatal(err)
	}
	url, err := f.OpenPR(context.Background(), gitops.PullRequest{
		Title: "drift", Body: "b", SourceBranch: "drift-sync/x", TargetBranch: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	return url, rec
}

func TestBitbucketOpenPR(t *testing.T) {
	t.Setenv("BITBUCKET_USERNAME", "rina")
	t.Setenv("BITBUCKET_APP_PASSWORD", "secret")

	url, rec := fakeForge(t,
		config.Git{Provider: "bitbucket", Workspace: "acme", Repo: "infra"},
		`{"links":{"html":{"href":"https://bitbucket.org/acme/infra/pull-requests/1"}}}`)

	if url != "https://bitbucket.org/acme/infra/pull-requests/1" {
		t.Fatalf("url = %q", url)
	}
	if rec.req.URL.Path != "/repositories/acme/infra/pullrequests" {
		t.Fatalf("path = %q", rec.req.URL.Path)
	}
	if u, p, _ := rec.req.BasicAuth(); u != "rina" || p != "secret" {
		t.Fatal("basic auth not sent")
	}
	var sent map[string]any
	json.Unmarshal(rec.body, &sent)
	src := sent["source"].(map[string]any)["branch"].(map[string]any)["name"]
	if src != "drift-sync/x" {
		t.Fatalf("source branch = %v", src)
	}
}

func TestBitbucketMissingCreds(t *testing.T) {
	if _, err := gitops.NewForge(config.Git{Provider: "bitbucket", Workspace: "a", Repo: "b"}); err == nil {
		t.Fatal("expected error without any bitbucket credentials")
	}
}

func TestGitHubOpenPR(t *testing.T) {
	t.Setenv("GH_TOKEN", "tok")
	url, rec := fakeForge(t,
		config.Git{Provider: "github", Workspace: "acme", Repo: "infra"},
		`{"html_url":"https://github.com/acme/infra/pull/2"}`)
	if url != "https://github.com/acme/infra/pull/2" {
		t.Fatalf("url = %q", url)
	}
	if rec.req.Header.Get("Authorization") != "Bearer tok" {
		t.Fatal("bearer token not sent")
	}
}

func TestGitLabOpenMR(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "tok")
	url, rec := fakeForge(t,
		config.Git{Provider: "gitlab", Workspace: "acme", Repo: "infra"},
		`{"web_url":"https://gitlab.com/acme/infra/-/merge_requests/3"}`)
	if url != "https://gitlab.com/acme/infra/-/merge_requests/3" {
		t.Fatalf("url = %q", url)
	}
	if rec.req.URL.EscapedPath() != "/api/v4/projects/acme%2Finfra/merge_requests" {
		t.Fatalf("path = %q", rec.req.URL.EscapedPath())
	}
	if rec.req.Header.Get("PRIVATE-TOKEN") != "tok" {
		t.Fatal("private-token not sent")
	}
}

func TestUnknownProvider(t *testing.T) {
	if _, err := gitops.NewForge(config.Git{Provider: "svn", Workspace: "a", Repo: "b"}); err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestForgeRequiresWorkspaceAndRepo(t *testing.T) {
	if _, err := gitops.NewForge(config.Git{Provider: "bitbucket"}); err == nil {
		t.Fatal("expected error when workspace/repo missing")
	}
}
