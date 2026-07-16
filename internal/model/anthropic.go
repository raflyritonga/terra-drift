package model

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Anthropic calls Claude's native Messages API.
// base_url defaults to the public API.
type Anthropic struct {
	endpoint  string
	model     string
	apiKey    string
	maxTokens int
	http      *http.Client
}

func newAnthropic(id, baseURL, apiKey string) (Anthropic, error) {
	if id == "" {
		return Anthropic{}, fmt.Errorf("anthropic model needs model.id (e.g. claude-opus-4-8)")
	}
	base := baseURL
	if base == "" {
		base = "https://api.anthropic.com"
	}
	return Anthropic{
		endpoint:  strings.TrimRight(base, "/") + "/v1/messages",
		model:     id,
		apiKey:    apiKey,
		maxTokens: 4096,
		http:      &http.Client{Timeout: 90 * time.Second},
	}, nil
}

func (m Anthropic) Complete(ctx context.Context, system, user string) (string, error) {
	body, err := json.Marshal(map[string]any{
		"model":      m.model,
		"max_tokens": m.maxTokens,
		"system":     system,
		"messages":   []map[string]string{{"role": "user", "content": user}},
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	if m.apiKey != "" {
		req.Header.Set("x-api-key", m.apiKey)
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
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("parse model response: %w", err)
	}
	var b strings.Builder
	for _, c := range out.Content {
		if c.Type == "text" {
			b.WriteString(c.Text)
		}
	}
	if b.Len() == 0 {
		return "", fmt.Errorf("model returned no text content")
	}
	return b.String(), nil
}
