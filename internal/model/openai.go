package model

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// OpenAICompatible calls any OpenAI chat-completions endpoint — a hosted model
// like x.llm.com, a gateway, or Ollama. The API key comes from env only
// (LLM_API_KEY, or OPENAI_API_KEY), never from config or the client.
type OpenAICompatible struct {
	endpoint string
	model    string
	apiKey   string
	http     *http.Client
}

func newOpenAICompatible(id, baseURL string) (OpenAICompatible, error) {
	if baseURL == "" {
		return OpenAICompatible{}, fmt.Errorf("openai-compatible model needs model.base_url (e.g. https://x.llm.com/v1)")
	}
	if id == "" {
		return OpenAICompatible{}, fmt.Errorf("openai-compatible model needs model.id")
	}
	key := readSecret("LLM_API_KEY")
	if key == "" {
		key = readSecret("OPENAI_API_KEY")
	}
	return OpenAICompatible{
		endpoint: strings.TrimRight(baseURL, "/") + "/chat/completions",
		model:    id,
		apiKey:   key,
		http:     &http.Client{Timeout: 90 * time.Second},
	}, nil
}

// readSecret returns $NAME, or the trimmed contents of the file at $NAME_FILE.
// The file form lets the key come from systemd credentials or a mounted secret
// instead of living in the process environment.
func readSecret(name string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	if path := os.Getenv(name + "_FILE"); path != "" {
		if b, err := os.ReadFile(path); err == nil {
			return strings.TrimSpace(string(b))
		}
	}
	return ""
}

func (m OpenAICompatible) Complete(ctx context.Context, system, user string) (string, error) {
	reqBody, err := json.Marshal(map[string]any{
		"model":       m.model,
		"temperature": 0,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if m.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+m.apiKey)
	}

	resp, err := m.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("call model endpoint: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("model endpoint %s: %s", resp.Status, snippet(data))
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("parse model response: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("model returned no choices")
	}
	return out.Choices[0].Message.Content, nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
