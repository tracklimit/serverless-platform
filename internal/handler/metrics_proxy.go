package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	maxQueryLen     = 2048
	upstreamTimeout = 5 * time.Second
)

// QueryRange proxies a PromQL range query to Prometheus.
// GET /api/v1/metrics/query_range?query=...&start=...&end=...&step=...
func (h *MetricsHandler) QueryRange(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("query")
	if err := validateQuery(query); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	params := url.Values{}
	params.Set("query", query)
	params.Set("start", r.URL.Query().Get("start"))
	params.Set("end", r.URL.Query().Get("end"))
	params.Set("step", r.URL.Query().Get("step"))

	ctx, cancel := context.WithTimeout(r.Context(), upstreamTimeout)
	defer cancel()

	reqURL := h.prometheusURL + "/api/v1/query_range?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "build upstream request")
		return
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "prometheus unreachable")
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 500 {
		writeError(w, http.StatusBadGateway, "prometheus error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func validateQuery(q string) error {
	if q == "" {
		return errors.New("query parameter is required")
	}
	if len(q) > maxQueryLen {
		return errors.New("query too long")
	}
	return nil
}
