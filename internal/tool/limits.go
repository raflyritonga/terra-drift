package tool

import (
	"sync"
	"sync/atomic"
	"time"
)

// Typed error codes the client can react to (fall back to report-only).
const (
	ErrGatewayDown      = "gateway-down"
	ErrInvalidOutput    = "invalid-output"
	ErrBudgetExceeded   = "budget-exceeded"
	ErrRateLimited      = "rate-limited"
	ErrContractMismatch = "contract-mismatch"
)

// limiter is a fixed-window rate limiter: at most n calls per minute.
type limiter struct {
	mu     sync.Mutex
	n      int
	count  int
	window time.Time
}

func newLimiter(perMinute int) *limiter { return &limiter{n: perMinute} }

func (l *limiter) allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if now.Sub(l.window) >= time.Minute {
		l.window = now
		l.count = 0
	}
	l.count++
	return l.count <= l.n
}

// Metrics are process counters exposed at /metrics for scraping.
type Metrics struct {
	Requests  atomic.Int64
	Errors    atomic.Int64
	CacheHits atomic.Int64
	Tokens    atomic.Int64
}

// Render emits a minimal Prometheus-style exposition.
func (m *Metrics) Render() string {
	return "" +
		"terra_drift_mcp_requests_total " + itoa(m.Requests.Load()) + "\n" +
		"terra_drift_mcp_errors_total " + itoa(m.Errors.Load()) + "\n" +
		"terra_drift_mcp_cache_hits_total " + itoa(m.CacheHits.Load()) + "\n" +
		"terra_drift_mcp_tokens_total " + itoa(m.Tokens.Load()) + "\n"
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
