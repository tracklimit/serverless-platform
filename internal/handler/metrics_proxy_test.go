package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"serverless-platform/internal/auth"
	"serverless-platform/internal/handler"
)

func scopedReq(rawQuery string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/?"+rawQuery, nil)
	return req.WithContext(auth.WithWorkspace(req.Context(), "tenant-a"))
}

func TestQueryRange_ProxiesToPrometheus(t *testing.T) {
	var captured string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/query_range", r.URL.Path)
		captured = r.URL.Query().Get("query")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
	}))
	defer upstream.Close()

	h := handler.NewMetricsHandler(upstream.URL)
	rec := httptest.NewRecorder()
	params := url.Values{
		"query": {`sum(rate(platform_function_invocations_total{function="hello"}[5m]))`},
		"start": {"0"}, "end": {"60"}, "step": {"15"},
	}
	h.QueryRange(rec, scopedReq(params.Encode()))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, captured, `workspace="tenant-a"`,
		"workspace matcher must be injected into the query sent upstream")

	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "success", resp["status"])
}

func TestQueryRange_RejectsMissingQuery(t *testing.T) {
	h := handler.NewMetricsHandler("http://irrelevant")
	rec := httptest.NewRecorder()
	h.QueryRange(rec, scopedReq("start=0&end=60&step=15"))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestQueryRange_RejectsNonTenantMetric(t *testing.T) {
	h := handler.NewMetricsHandler("http://irrelevant")
	rec := httptest.NewRecorder()
	params := url.Values{
		"query": {`sum(rate(platform_http_requests_total[1m]))`},
		"start": {"0"}, "end": {"60"}, "step": {"15"},
	}
	h.QueryRange(rec, scopedReq(params.Encode()))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestQueryRange_RejectsCrossTenantQuery(t *testing.T) {
	h := handler.NewMetricsHandler("http://irrelevant")
	rec := httptest.NewRecorder()
	params := url.Values{
		"query": {`platform_function_invocations_total{workspace="tenant-b"}`},
		"start": {"0"}, "end": {"60"}, "step": {"15"},
	}
	h.QueryRange(rec, scopedReq(params.Encode()))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestQueryRange_502OnUpstreamDown(t *testing.T) {
	h := handler.NewMetricsHandler("http://127.0.0.1:1")
	rec := httptest.NewRecorder()
	params := url.Values{
		"query": {`sum(rate(platform_function_invocations_total{function="hello"}[5m]))`},
		"start": {"0"}, "end": {"60"}, "step": {"15"},
	}
	h.QueryRange(rec, scopedReq(params.Encode()))
	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

func TestQueryRange_502OnUpstream5xx(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()

	h := handler.NewMetricsHandler(upstream.URL)
	rec := httptest.NewRecorder()
	params := url.Values{
		"query": {`sum(rate(platform_function_invocations_total{function="hello"}[5m]))`},
		"start": {"0"}, "end": {"60"}, "step": {"15"},
	}
	h.QueryRange(rec, scopedReq(params.Encode()))
	assert.Equal(t, http.StatusBadGateway, rec.Code)
}
