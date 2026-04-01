package middleware

import (
	"encoding/json"
	"net/http"
	"serverless-platform/internal/auth"
	"serverless-platform/internal/ratelimit"
	"strconv"
)

// RateLimit returns middleware that enforces per-user rate limiting
// using the provided Limiter implementation.
func RateLimit(limiter ratelimit.Limiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			username := auth.UsernameFromContext(r.Context())
			if username == "" {
				next.ServeHTTP(w, r)
				return
			}

			remaining, allowed := limiter.Allow(username)

			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limiter.Limit()))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))

			if !allowed {
				w.Header().Set("Retry-After", "1")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded"})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
