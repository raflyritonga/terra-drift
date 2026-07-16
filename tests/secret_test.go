package tests

import (
	"context"
	"testing"

	"github.com/raflyritonga/terra-drift/internal/secret"
)

func TestSecretEnvSource(t *testing.T) {
	t.Setenv("LLM_API_KEY", "sk-env")
	got, err := secret.Resolve(context.Background(), "env", "")
	if err != nil || got != "sk-env" {
		t.Fatalf("env source: got %q err %v", got, err)
	}
	// default source is env
	got, err = secret.Resolve(context.Background(), "", "")
	if err != nil || got != "sk-env" {
		t.Fatalf("default source: got %q err %v", got, err)
	}
}

func TestSecretEnvEmptyErrors(t *testing.T) {
	t.Setenv("LLM_API_KEY", "")
	if _, err := secret.Resolve(context.Background(), "env", ""); err == nil {
		t.Fatal("expected error when LLM_API_KEY is empty")
	}
}

func TestSecretManagerNeedsRef(t *testing.T) {
	if _, err := secret.Resolve(context.Background(), "aws-secrets-manager", ""); err == nil {
		t.Fatal("expected error when secret.ref is missing")
	}
}

func TestSecretUnknownSource(t *testing.T) {
	if _, err := secret.Resolve(context.Background(), "ouija-board", "x"); err == nil {
		t.Fatal("expected error for unknown source")
	}
}
