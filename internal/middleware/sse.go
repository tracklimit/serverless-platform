package middleware

import (
	"encoding/json"
	"math"
	"net/http"
	"sync"
	"sync/atomic"

	"serverless-platform/internal/auth"
)

// SSEConnectionLimit caps the number of concurrent server-sent-event streams
// per authenticated user. Since SSE handlers occupy a goroutine for the full
// lifetime of the connection (often minutes), an unbounded number per user is
// a ready-made resource exhaustion vector.
//
// The counter is decremented when the handler returns, which happens either
// on client disconnect (context cancellation) or when the handler voluntarily
// ends (e.g., a deployment reaching a terminal state).
//
// Must be mounted *after* auth middleware so the username is in context.
func SSEConnectionLimit(max int) func(http.Handler) http.Handler {
	// Clamp the caller-supplied limit into int32 range at construction
	// time. A negative or zero max is treated as 1 (effectively: allow one
	// connection) since a zero cap would reject everything and a negative
	// one is nonsensical. An out-of-range positive value is clamped to
	// MaxInt32, which is far higher than any realistic per-user cap.
	limit := int32(1)
	switch {
	case max > math.MaxInt32:
		limit = math.MaxInt32
	case max > 1:
		limit = int32(max)
	}

	var counts sync.Map // map[string]*atomic.Int32

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := auth.UsernameFromContext(r.Context())
			if user == "" {
				// No authenticated user in context — let the request proceed;
				// auth middleware should already have rejected unauthenticated
				// traffic, and rejecting again here would mask the real error.
				next.ServeHTTP(w, r)
				return
			}

			counter, _ := counts.LoadOrStore(user, &atomic.Int32{})
			c := counter.(*atomic.Int32)

			if c.Add(1) > limit {
				c.Add(-1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "too many open streams"})
				return
			}
			defer c.Add(-1)

			next.ServeHTTP(w, r)
		})
	}
}
