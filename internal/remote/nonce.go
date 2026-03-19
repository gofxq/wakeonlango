package remote

import (
	"sync"
	"time"
)

type NonceCache struct {
	mu     sync.Mutex
	values map[string]time.Time
	ttl    time.Duration
}

func NewNonceCache(ttl time.Duration) *NonceCache {
	return &NonceCache{values: map[string]time.Time{}, ttl: ttl}
}

func (n *NonceCache) SeenOrStore(value string, now time.Time) bool {
	n.mu.Lock()
	defer n.mu.Unlock()

	for k, t := range n.values {
		if now.Sub(t) > n.ttl {
			delete(n.values, k)
		}
	}
	if _, ok := n.values[value]; ok {
		return true
	}
	n.values[value] = now
	return false
}
