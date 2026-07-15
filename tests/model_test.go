package tests

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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

func TestUnknownModelProvider(t *testing.T) {
	if _, err := model.New("magic", "", ""); err == nil {
		t.Fatal("expected error for unknown provider")
	}
}
