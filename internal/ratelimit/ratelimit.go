package ratelimit

import (
	"sync"
	"time"
)

// Limiter defines the interface for rate limiting.
type Limiter interface {
	// Allow checks whether a request from the given key should be allowed.
	// Returns the number of remaining tokens and whether the request is allowed.
	Allow(key string) (remaining int, allowed bool)
	Limit() int
}

// InMemoryLimiter implements a per-process token bucket rate limiter.
// Note: state is not shared across replicas and resets on pod restart.
type InMemoryLimiter struct {
	mu       sync.Mutex
	capacity int
	refill   float64
	buckets  map[string]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

func NewInMemoryLimiter(maxRequests int, window time.Duration) *InMemoryLimiter {
	return &InMemoryLimiter{
		capacity: maxRequests,
		refill:   float64(maxRequests) / window.Seconds(),
		buckets:  make(map[string]*bucket),
	}
}

func (l *InMemoryLimiter) Limit() int { return l.capacity }

func (l *InMemoryLimiter) Allow(key string) (remaining int, allowed bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: float64(l.capacity), last: now}
		l.buckets[key] = b
	}

	elapsed := now.Sub(b.last).Seconds()
	b.tokens = min(float64(l.capacity), b.tokens+elapsed*l.refill)
	b.last = now

	if b.tokens >= 1 {
		b.tokens--
		return int(b.tokens), true
	}
	return 0, false
}
