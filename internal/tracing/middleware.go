package tracing

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// HTTP returns middleware that creates a span for each HTTP request.
func HTTP(next http.Handler) http.Handler {
	return otelhttp.NewHandler(next, "http.request")
}
