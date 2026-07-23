// Package redact replaces sensitive infrastructure values with stable
// placeholders before text leaves for the model, and restores them in the
// model's reply. Raw secrets, ARNs, IPs, and account ids never reach the LLM.
package redact

import (
	"fmt"
	"regexp"
	"strings"
)

// Ordered: composite values first (an ARN contains an account id).
var patterns = []*regexp.Regexp{
	regexp.MustCompile(`arn:[a-z-]+:[a-z0-9-]*:[a-z0-9-]*:\d{12}:[^\s"',\\]+`), // ARNs
	regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`),                        // AWS access key ids
	regexp.MustCompile(`\bghp_[A-Za-z0-9]{20,}\b`),                             // GitHub tokens
	regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}\b`),                     // Slack tokens
	regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}(?:/\d{1,2})?\b`),             // IPv4 / CIDR
	regexp.MustCompile(`\b\d{12}\b`),                                           // bare AWS account ids
}

// Redactor is a per-request substitution table: the same value always maps to
// the same placeholder, so the model can still reason about equality.
type Redactor struct {
	byValue map[string]string
	byToken map[string]string
	n       int
}

func New() *Redactor {
	return &Redactor{byValue: map[string]string{}, byToken: map[string]string{}}
}

// Redact replaces every sensitive value in s with its placeholder.
func (r *Redactor) Redact(s string) string {
	for _, re := range patterns {
		s = re.ReplaceAllStringFunc(s, r.placeholder)
	}
	return s
}

// Restore substitutes the original values back into the model's reply.
func (r *Redactor) Restore(s string) string {
	for token, value := range r.byToken {
		s = strings.ReplaceAll(s, token, value)
	}
	return s
}

// Count reports how many distinct values were redacted (for logs).
func (r *Redactor) Count() int { return r.n }

func (r *Redactor) placeholder(value string) string {
	if tok, ok := r.byValue[value]; ok {
		return tok
	}
	r.n++
	tok := fmt.Sprintf("__TD_REDACT_%d__", r.n)
	r.byValue[value] = tok
	r.byToken[tok] = value
	return tok
}
