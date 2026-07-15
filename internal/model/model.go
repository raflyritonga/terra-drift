// Package model abstracts the LLM behind one tiny interface.
// Phase 1 ships only MockModel; a real backend (Bedrock/OpenAI-compatible) is Phase 2.
package model

import (
	"context"
	"fmt"
)

type Model interface {
	Complete(ctx context.Context, systemPrompt, userPayload string) (string, error)
}

func New(provider, id, baseURL string) (Model, error) {
	switch provider {
	case "mock", "":
		return MockModel{}, nil
	case "openai-compatible":
		return newOpenAICompatible(id, baseURL)
	default:
		return nil, fmt.Errorf("unknown model provider %q (mock | openai-compatible)", provider)
	}
}
