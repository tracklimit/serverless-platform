package cache

import (
	"context"
	"errors"
	"time"
)

// Store defines the interface for response caching.
type Store interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Invalidate(ctx context.Context, pattern string) error
}

// ErrCacheMiss is returned when a key is not found in the cache.
var ErrCacheMiss = errors.New("cache miss")

type NoOpStore struct{}

func NewNoOpStore() *NoOpStore { return &NoOpStore{} }

func (s *NoOpStore) Get(_ context.Context, _ string) ([]byte, error) {
	return nil, ErrCacheMiss
}

func (s *NoOpStore) Set(_ context.Context, _ string, _ []byte, _ time.Duration) error {
	return nil
}

func (s *NoOpStore) Invalidate(_ context.Context, _ string) error {
	return nil
}
