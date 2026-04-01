package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"serverless-platform/internal/auth"
	"serverless-platform/internal/cache"
	"strings"
	"time"
)

type cacheWriter struct {
	http.ResponseWriter
	statusCode int
	body       bytes.Buffer
}

func (w *cacheWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *cacheWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

// Cache returns middleware that caches successful GET responses using the provided Store.
// With NoOpStore, this is a pass-through (every request hits the handler).
func Cache(store cache.Store, ttl time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				next.ServeHTTP(w, r)
				return
			}

			if strings.HasSuffix(r.URL.Path, "/logs") || strings.HasSuffix(r.URL.Path, "/metrics") {
				next.ServeHTTP(w, r)
				return
			}

			workspace := auth.WorkspaceSlugFromContext(r.Context())
			key := "cache:" + workspace + ":" + r.URL.RequestURI()

			cached, err := store.Get(r.Context(), key)
			if err == nil {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Cache", "HIT")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(cached) //nolint:gosec // cached data is from our own store, not user input
				return
			}

			w.Header().Set("X-Cache", "MISS")
			cw := &cacheWriter{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(cw, r)

			if cw.statusCode == http.StatusOK {
				if err := store.Set(r.Context(), key, cw.body.Bytes(), ttl); err != nil {
					slog.WarnContext(r.Context(), "cache set failed", "error", err)
				}
			}
		})
	}
}
