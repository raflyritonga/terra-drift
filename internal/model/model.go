// Package model abstracts the LLM behind one tiny interface, so any backend
// is a swap of provider + base_url. Adding one is a new case here.
package model

import (
	"context"
	"fmt"
)

type Model interface {
	Complete(ctx context.Context, systemPrompt, userPayload string) (string, error)
}

// New builds a model for the provider:
//   - openai / openai-compatible: any /chat/completions endpoint (OpenAI, gateways, Ollama, ...)
//   - anthropic:                  Claude's native Messages API
//   - mock:                       deterministic canned edits, no network
func New(provider, id, baseURL string) (Model, error) {
	switch provider {
	case "mock", "":
		return MockModel{}, nil
	case "openai", "openai-compatible":
		return newOpenAICompatible(id, baseURL)
	case "anthropic":
		return newAnthropic(id, baseURL)
	default:
		return nil, fmt.Errorf("unknown model provider %q (openai | anthropic | mock)", provider)
	}
}
