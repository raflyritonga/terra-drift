package tool

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/raflyritonga/terra-drift/internal/contract"
)

// CacheKey identifies a proposal: same resource, drifted attribute + live
// value, provider/model, and contract version → same edit. The daily cron
// re-seeing unchanged drift hits the cache instead of re-spending tokens.
func CacheKey(in contract.ProposalInput, provider, modelID string) string {
	attrs := append([]string(nil), in.AllowedAttrs...)
	sort.Strings(attrs)
	parts := []string{
		contract.Version, provider, modelID,
		in.Drift.Address, in.Drift.Attribute,
		string(in.Drift.After), strings.Join(attrs, ","),
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}

type cacheEntry struct {
	out     contract.ProposalOutput
	expires time.Time
}

// Cache is an in-memory TTL cache for validated proposals.
type Cache struct {
	mu  sync.Mutex
	ttl time.Duration
	m   map[string]cacheEntry
}

func NewCache(ttl time.Duration) *Cache {
	return &Cache{ttl: ttl, m: map[string]cacheEntry{}}
}

func (c *Cache) Get(key string) (contract.ProposalOutput, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok || time.Now().After(e.expires) {
		delete(c.m, key)
		return contract.ProposalOutput{}, false
	}
	return e.out, true
}

func (c *Cache) Put(key string, out contract.ProposalOutput) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = cacheEntry{out: out, expires: time.Now().Add(c.ttl)}
}
