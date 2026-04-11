package metrics_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"serverless-platform/internal/metrics"
)

func TestHTTPMiddleware_IncrementsRequestCount(t *testing.T) {
	r := chi.NewRouter()
	r.Use(metrics.HTTP)
	r.Get("/test/count", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	before := testutil.ToFloat64(metrics.HTTPRequestsTotal.WithLabelValues("GET", "/test/count", "200"))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test/count", nil)
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	after := testutil.ToFloat64(metrics.HTTPRequestsTotal.WithLabelValues("GET", "/test/count", "200"))
	assert.Equal(t, before+1, after)
}

func TestHTTPMiddleware_RecordsStatusCodeLabel(t *testing.T) {
	r := chi.NewRouter()
	r.Use(metrics.HTTP)
	r.Get("/test/notfound", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	before := testutil.ToFloat64(metrics.HTTPRequestsTotal.WithLabelValues("GET", "/test/notfound", "404"))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test/notfound", nil)
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
	after := testutil.ToFloat64(metrics.HTTPRequestsTotal.WithLabelValues("GET", "/test/notfound", "404"))
	assert.Equal(t, before+1, after)
}

func TestHTTPMiddleware_RecordsLatencyObservation(t *testing.T) {
	r := chi.NewRouter()
	r.Use(metrics.HTTP)
	r.Get("/test/latency", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	before := histogramSampleCount(t, metrics.HTTPRequestDuration, "GET", "/test/latency")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test/latency", nil)
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	after := histogramSampleCount(t, metrics.HTTPRequestDuration, "GET", "/test/latency")
	assert.Equal(t, before+1, after)
}

// histogramSampleCount reads the current _count value for a labelled histogram
// directly from its internal state. testutil.ToFloat64 only supports Counter,
// Gauge, and Untyped metrics, so histograms need the dto.Metric roundtrip.
func histogramSampleCount(t *testing.T, hv *prometheus.HistogramVec, lvs ...string) uint64 {
	t.Helper()

	observer, err := hv.GetMetricWithLabelValues(lvs...)
	require.NoError(t, err)

	metric, ok := observer.(prometheus.Metric)
	require.True(t, ok, "histogram observer should also implement prometheus.Metric")

	var m dto.Metric
	require.NoError(t, metric.Write(&m))
	return m.GetHistogram().GetSampleCount()
}
