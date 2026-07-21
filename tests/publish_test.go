package tests

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/raflyritonga/terra-drift/internal/config"
	"github.com/raflyritonga/terra-drift/internal/gitops"
)

// PublishBranch must read the target branch head, then POST /src as multipart
// with message/branch/parents fields and one field per repo-relative file path.
func TestBitbucketPublishBranch(t *testing.T) {
	t.Setenv("BITBUCKET_USERNAME", "ci-bot")
	t.Setenv("BITBUCKET_APP_PASSWORD", "secret")

	var srcReq *http.Request
	var form map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repositories/acme/infra/refs/branches/main":
			if u, p, _ := r.BasicAuth(); u != "ci-bot" || p != "secret" {
				t.Error("branch-head call missing basic auth")
			}
			io.WriteString(w, `{"target":{"hash":"abc123def"}}`)
		case r.Method == http.MethodPost && r.URL.Path == "/repositories/acme/infra/src":
			srcReq = r
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Errorf("not multipart: %v", err)
				return
			}
			form = r.MultipartForm.Value
			w.WriteHeader(http.StatusCreated)
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	f, err := gitops.NewForge(config.Git{Provider: "bitbucket", Workspace: "acme", Repo: "infra", APIBase: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	pub, ok := f.(gitops.BranchPublisher)
	if !ok {
		t.Fatal("bitbucket forge must implement BranchPublisher")
	}

	err = pub.PublishBranch(context.Background(), gitops.Commit{
		Branch:       "drift-sync/2026-07-21-060102",
		TargetBranch: "main",
		Message:      "drift-sync: update code",
		Files: map[string][]byte{
			"planpalasix/02_network/vpce.tf": []byte("resource {}\n"),
			"envs/stg/terraform.tfvars":      []byte("x = 1\n"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if u, p, _ := srcReq.BasicAuth(); u != "ci-bot" || p != "secret" {
		t.Fatal("/src call missing basic auth")
	}
	want := map[string]string{
		"message":                        "drift-sync: update code",
		"branch":                         "drift-sync/2026-07-21-060102",
		"parents":                        "abc123def",
		"planpalasix/02_network/vpce.tf": "resource {}\n",
		"envs/stg/terraform.tfvars":      "x = 1\n",
	}
	for field, val := range want {
		got := form[field]
		if len(got) != 1 || got[0] != val {
			t.Errorf("field %q = %v, want %q", field, got, val)
		}
	}
}

func TestBitbucketPublishSurfacesAPIError(t *testing.T) {
	t.Setenv("BITBUCKET_USERNAME", "ci-bot")
	t.Setenv("BITBUCKET_APP_PASSWORD", "secret")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "refs/branches") {
			io.WriteString(w, `{"target":{"hash":"abc"}}`)
			return
		}
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"error":{"message":"insufficient scope"}}`)
	}))
	defer srv.Close()

	f, _ := gitops.NewForge(config.Git{Provider: "bitbucket", Workspace: "a", Repo: "b", APIBase: srv.URL})
	err := f.(gitops.BranchPublisher).PublishBranch(context.Background(), gitops.Commit{
		Branch: "x", TargetBranch: "main", Message: "m",
		Files: map[string][]byte{"a.tf": []byte("x")},
	})
	if err == nil {
		t.Fatal("expected error on 403")
	}
	if !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "insufficient scope") {
		t.Fatalf("error should carry status + body snippet: %v", err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("error leaked a credential: %v", err)
	}
}

func TestBitbucketPublishRequiresFiles(t *testing.T) {
	t.Setenv("BITBUCKET_TOKEN", "tok")
	f, _ := gitops.NewForge(config.Git{Provider: "bitbucket", Workspace: "a", Repo: "b"})
	err := f.(gitops.BranchPublisher).PublishBranch(context.Background(), gitops.Commit{Branch: "x", TargetBranch: "main"})
	if err == nil {
		t.Fatal("expected error with no files")
	}
}

// push_mode defaults: api for bitbucket, git elsewhere; api rejected elsewhere.
func TestResolvedPushMode(t *testing.T) {
	cases := []struct {
		provider, mode, want string
		wantErr              bool
	}{
		{"bitbucket", "", "api", false},
		{"", "", "api", false},
		{"github", "", "git", false},
		{"gitlab", "", "git", false},
		{"bitbucket", "git", "git", false},
		{"bitbucket", "api", "api", false},
		{"github", "api", "", true},
		{"bitbucket", "carrier-pigeon", "", true},
	}
	for _, tc := range cases {
		got, err := config.Git{Provider: tc.provider, PushMode: tc.mode}.ResolvedPushMode()
		if tc.wantErr != (err != nil) || got != tc.want {
			t.Errorf("provider=%q mode=%q: got %q err %v", tc.provider, tc.mode, got, err)
		}
	}
}
