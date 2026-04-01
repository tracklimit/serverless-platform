package ratelimit_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"serverless-platform/internal/ratelimit"
)

func TestInMemoryLimiter_AllowsUnderLimit(t *testing.T) {
	limiter := ratelimit.NewInMemoryLimiter(10, time.Minute)

	for i := 0; i < 10; i++ {
		remaining, allowed := limiter.Allow("alice")
		assert.True(t, allowed, "request %d should be allowed", i+1)
		assert.Equal(t, 9-i, remaining)
	}
}

func TestInMemoryLimiter_BlocksOverLimit(t *testing.T) {
	limiter := ratelimit.NewInMemoryLimiter(5, time.Minute)

	for i := 0; i < 5; i++ {
		_, allowed := limiter.Allow("bob")
		assert.True(t, allowed)
	}

	remaining, allowed := limiter.Allow("bob")
	assert.False(t, allowed)
	assert.Equal(t, 0, remaining)
}

func TestInMemoryLimiter_IsolatesUsers(t *testing.T) {
	limiter := ratelimit.NewInMemoryLimiter(1, time.Minute)

	_, allowed := limiter.Allow("alice")
	assert.True(t, allowed)

	_, allowed = limiter.Allow("alice")
	assert.False(t, allowed)

	// Bob is independent
	_, allowed = limiter.Allow("bob")
	assert.True(t, allowed)
}

func TestInMemoryLimiter_Limit(t *testing.T) {
	limiter := ratelimit.NewInMemoryLimiter(100, time.Minute)
	assert.Equal(t, 100, limiter.Limit())
}
