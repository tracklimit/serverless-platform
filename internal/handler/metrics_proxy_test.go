package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"serverless-platform/internal/handler"
)

func TestQueryRange_ProxiesToPrometheus(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/query_range", r.URL.Path)
		assert.Contains(t, r.URL.RawQuery, "query=")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
	}))
	defer upstream.Close()

	h := handler.NewMetricsHandler(upstream.URL)
	req := httptest.NewRequest(http.MethodGet,
		`/?query=sum(rate(platform_http_requests_total{job="api"}[5m]))&start=0&end=60&step=15`, nil)
	rec := httptest.NewRecorder()

	h.QueryRange(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "success", resp["status"])
}

func TestQueryRange_RejectsMissingQuery(t *testing.T) {
	h := handler.NewMetricsHandler("http://irrelevant")
	req := httptest.NewRequest(http.MethodGet, "/?start=0&end=60&step=15", nil)
	rec := httptest.NewRecorder()
	h.QueryRange(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestQueryRange_RejectsUnscopedQuery(t *testing.T) {
	h := handler.NewMetricsHandler("http://irrelevant")
	// Raw metric name with no label selector — should be rejected.
	req := httptest.NewRequest(http.MethodGet, "/?query=up&start=0&end=60&step=15", nil)
	rec := httptest.NewRecorder()
	h.QueryRange(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestQueryRange_502OnUpstreamDown(t *testing.T) {
	// Point at a closed port so the dial fails immediately.
	h := handler.NewMetricsHandler("http://127.0.0.1:1")
	req := httptest.NewRequest(http.MethodGet,
		`/?query=sum(rate(platform_http_requests_total{job="api"}[5m]))&start=0&end=60&step=15`, nil)
	rec := httptest.NewRecorder()
	h.QueryRange(rec, req)
	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

func TestQueryRange_502OnUpstream5xx(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()

	h := handler.NewMetricsHandler(upstream.URL)
	req := httptest.NewRequest(http.MethodGet,
		`/?query=sum(rate(platform_http_requests_total{job="api"}[5m]))&start=0&end=60&step=15`, nil)
	rec := httptest.NewRecorder()
	h.QueryRange(rec, req)
	assert.Equal(t, http.StatusBadGateway, rec.Code)
}
