package tf

import (
	"fmt"
	"strings"
)

// Refresh failure reasons, so a root can be skipped with a useful cause
// instead of hard-failing the run.
const (
	ReasonNoIdentityPolicy = "no-identity-policy"
	ReasonBoundaryDenied   = "boundary-denied"
	ReasonTrustDenied      = "trust-denied"
	ReasonProviderError    = "provider-error"
)

// RefreshError is a classified `terraform plan -refresh-only` failure.
type RefreshError struct {
	Reason string
	Detail string
}

func (e *RefreshError) Error() string {
	return fmt.Sprintf("refresh failed (%s): %s", e.Reason, e.Detail)
}

// Classify maps terraform/provider stderr to a skip reason.
func Classify(stderr string) *RefreshError {
	detail := strings.TrimSpace(stderr)
	if len(detail) > 400 {
		detail = detail[:400] + "…"
	}
	low := strings.ToLower(stderr)
	switch {
	case strings.Contains(low, "permissions boundary"):
		return &RefreshError{Reason: ReasonBoundaryDenied, Detail: detail}
	case strings.Contains(low, "assumerole") || strings.Contains(low, "sts:"):
		return &RefreshError{Reason: ReasonTrustDenied, Detail: detail}
	case strings.Contains(low, "accessdenied") || strings.Contains(low, "unauthorizedoperation") ||
		strings.Contains(low, "not authorized") || strings.Contains(low, "forbidden"):
		return &RefreshError{Reason: ReasonNoIdentityPolicy, Detail: detail}
	default:
		return &RefreshError{Reason: ReasonProviderError, Detail: detail}
	}
}
