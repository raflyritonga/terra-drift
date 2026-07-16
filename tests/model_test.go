package tests

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/raflyritonga/terra-drift/internal/model"
)

// The openai-compatible client must send a chat request and return the reply text.
func TestOpenAICompatibleComplete(t *testing.T) {
	t.Setenv("LLM_API_KEY", "sk-test")

	var gotAuth, gotModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Model string `json:"model"`
		}
		json.Unmarshal(body, &req)
		gotModel = req.Model
		io.WriteString(w, `{"choices":[{"message":{"content":"{\"edits\":[]}"}}]}`)
	}))
	defer srv.Close()

	m, err := model.New("openai-compatible", "acme-model", srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	out, err := m.Complete(context.Background(), "sys", "user")
	if err != nil {
		t.Fatal(err)
	}
	if out != `{"edits":[]}` {
		t.Fatalf("content = %q", out)
	}
	if gotAuth != "Bearer sk-test" {
		t.Fatalf("auth = %q", gotAuth)
	}
	if gotModel != "acme-model" {
		t.Fatalf("model = %q", gotModel)
	}
}

func TestOpenAICompatibleNeedsBaseURL(t *testing.T) {
	if _, err := model.New("openai-compatible", "m", ""); err == nil {
		t.Fatal("expected error without base_url")
	}
}

// The key can come from a file (systemd credentials / mounted secret), not just env.
func TestOpenAICompatibleKeyFromFile(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "llm_api_key")
	os.WriteFile(keyFile, []byte("sk-from-file\n"), 0o600)
	t.Setenv("LLM_API_KEY", "")
	t.Setenv("LLM_API_KEY_FILE", keyFile)

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		io.WriteString(w, `{"choices":[{"message":{"content":"{}"}}]}`)
	}))
	defer srv.Close()

	m, err := model.New("openai-compatible", "acme-model", srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Complete(context.Background(), "s", "u"); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer sk-from-file" {
		t.Fatalf("auth = %q, want key read from file (trimmed)", gotAuth)
	}
}

func TestUnknownModelProvider(t *testing.T) {
	if _, err := model.New("magic", "", ""); err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

// The anthropic provider speaks Claude's native Messages API.
func TestAnthropicComplete(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant")

	var gotKey, gotVer, gotSystem string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-api-key")
		gotVer = r.Header.Get("anthropic-version")
		body, _ := io.ReadAll(r.Body)
		var req struct {
			System string `json:"system"`
		}
		json.Unmarshal(body, &req)
		gotSystem = req.System
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %q", r.URL.Path)
		}
		io.WriteString(w, `{"content":[{"type":"text","text":"{\"edits\":[]}"}]}`)
	}))
	defer srv.Close()

	m, err := model.New("anthropic", "claude-opus-4-8", srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	out, err := m.Complete(context.Background(), "be terse", "user")
	if err != nil {
		t.Fatal(err)
	}
	if out != `{"edits":[]}` {
		t.Fatalf("content = %q", out)
	}
	if gotKey != "sk-ant" || gotVer == "" || gotSystem != "be terse" {
		t.Fatalf("headers/body wrong: key=%q ver=%q system=%q", gotKey, gotVer, gotSystem)
	}
}
