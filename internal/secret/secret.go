// Package secret resolves the model API key from one of two sources:
// the process environment (env file), or a cloud secret manager the host
// has an IAM role for.
package secret

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// fetchAWS is a package var so tests can stub the network call.
var fetchAWS = awsSecretsManager

// Resolve returns the model API key.
//   - source "env" (default): read LLM_API_KEY from the environment.
//   - source "aws-secrets-manager": fetch secret ref using the host's role.
func Resolve(ctx context.Context, source, ref string) (string, error) {
	switch source {
	case "", "env":
		key := os.Getenv("LLM_API_KEY")
		if key == "" {
			return "", fmt.Errorf("LLM_API_KEY is empty (secret.source=env)")
		}
		return key, nil
	case "aws-secrets-manager", "aws":
		if ref == "" {
			return "", fmt.Errorf("secret.ref (the secret id or ARN) is required for aws-secrets-manager")
		}
		return fetchAWS(ctx, ref)
	default:
		return "", fmt.Errorf("unknown secret.source %q (env | aws-secrets-manager)", source)
	}
}

// awsSecretsManager reads a secret by id/ARN using the default credential
// chain — on the host that means its instance/task IAM role.
func awsSecretsManager(ctx context.Context, ref string) (string, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return "", fmt.Errorf("load aws config: %w", err)
	}
	out, err := secretsmanager.NewFromConfig(cfg).GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: &ref,
	})
	if err != nil {
		return "", fmt.Errorf("get secret %q: %w", ref, err)
	}
	if out.SecretString == nil {
		return "", fmt.Errorf("secret %q has no string value", ref)
	}
	return *out.SecretString, nil
}
