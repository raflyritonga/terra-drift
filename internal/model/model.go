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

// New builds a model for the provider. The apiKey is already resolved by the
// caller (from env or a secret manager); the model never fetches it.
//   - openai / openai-compatible: any /chat/completions endpoint (OpenAI, gateways, Ollama, ...)
//   - anthropic:                  Claude's native Messages API
//   - mock:                       deterministic canned edits, no network
func New(provider, id, baseURL, apiKey string) (Model, error) {
	switch provider {
	case "mock", "":
		return MockModel{}, nil
	case "openai", "openai-compatible":
		return newOpenAICompatible(id, baseURL, apiKey)
	case "anthropic":
		return newAnthropic(id, baseURL, apiKey)
	default:
		return nil, fmt.Errorf("unknown model provider %q (openai | anthropic | mock)", provider)
	}
}
