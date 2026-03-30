package middleware

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"serverless-platform/internal/logging"
)

const RequestIDHeader = "X-Request-ID"

// RequestID generates a UUID per request and injects it into the context.
// If the client sends an X-Request-ID header, it is accepted (for tracing
// across services). Otherwise, a new UUID is generated.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get(RequestIDHeader)
		if requestID == "" {
			requestID = uuid.New().String()
		}

		ctx := context.WithValue(r.Context(), logging.RequestIDKey, requestID)
		w.Header().Set(RequestIDHeader, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
