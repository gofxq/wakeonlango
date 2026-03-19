package remote

import (
	"sync"
	"time"
)

type FixedWindowLimiter struct {
	mu      sync.Mutex
	window  time.Duration
	maxHits int
	hits    map[string][]time.Time
}

func NewFixedWindowLimiter(window time.Duration, maxHits int) *FixedWindowLimiter {
	return &FixedWindowLimiter{window: window, maxHits: maxHits, hits: map[string][]time.Time{}}
}

func (l *FixedWindowLimiter) Allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	list := l.hits[key]
	kept := list[:0]
	for _, t := range list {
		if now.Sub(t) <= l.window {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.maxHits {
		l.hits[key] = kept
		return false
	}
	kept = append(kept, now)
	l.hits[key] = kept
	return true
}
