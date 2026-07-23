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

// OpenAICompatible calls any OpenAI chat-completions endpoint — a hosted model
// like x.llm.com, a gateway, or Ollama.
type OpenAICompatible struct {
	endpoint string
	model    string
	apiKey   string
	http     *http.Client
}

func newOpenAICompatible(id, baseURL, apiKey string) (OpenAICompatible, error) {
	if baseURL == "" {
		return OpenAICompatible{}, fmt.Errorf("openai model needs model.base_url (e.g. https://x.llm.com/v1)")
	}
	if id == "" {
		return OpenAICompatible{}, fmt.Errorf("openai model needs model.id")
	}
	return OpenAICompatible{
		endpoint: strings.TrimRight(baseURL, "/") + "/chat/completions",
		model:    id,
		apiKey:   apiKey,
		http:     &http.Client{Timeout: 90 * time.Second},
	}, nil
}

func (m OpenAICompatible) Complete(ctx context.Context, system, user string) (string, int, error) {
	reqBody, err := json.Marshal(map[string]any{
		"model":       m.model,
		"temperature": 0,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
	})
	if err != nil {
		return "", 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if m.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+m.apiKey)
	}

	resp, err := m.http.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("call model endpoint: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", 0, fmt.Errorf("model endpoint %s: %s", resp.Status, snippet(data))
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			TotalTokens int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", 0, fmt.Errorf("parse model response: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", 0, fmt.Errorf("model returned no choices")
	}
	return out.Choices[0].Message.Content, out.Usage.TotalTokens, nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
