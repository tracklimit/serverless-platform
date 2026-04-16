package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"serverless-platform/internal/auth"
)

type MetricsHandler struct {
	prometheusURL string
}

func NewMetricsHandler(prometheusURL string) *MetricsHandler {
	return &MetricsHandler{prometheusURL: prometheusURL}
}

type DataPoint struct {
	T int64   `json:"t"`
	V float64 `json:"v"`
}

type MetricsResponse struct {
	RequestRate []*DataPoint `json:"request_rate"`
	LatencyP99  []*DataPoint `json:"latency_p99"`
}

func (h *MetricsHandler) Get(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	workspace := auth.WorkspaceSlugFromContext(r.Context())

	end := time.Now()
	start := end.Add(-1 * time.Hour)
	step := "60"

	requestRate, err := h.queryRange(r.Context(),
		fmt.Sprintf(`sum(rate(platform_function_invocations_total{workspace="%s",function="%s"}[2m]))`, workspace, name),
		start, end, step,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to query metrics: "+err.Error())
		return
	}

	latencyP99, err := h.queryRange(r.Context(),
		fmt.Sprintf(`histogram_quantile(0.99, sum(rate(platform_function_invocation_duration_seconds_bucket{workspace="%s",function="%s"}[2m])) by (le))`, workspace, name),
		start, end, step,
	)
	if err != nil {
		latencyP99 = []*DataPoint{}
	}

	writeJSON(w, http.StatusOK, MetricsResponse{
		RequestRate: requestRate,
		LatencyP99:  latencyP99,
	})
}

func (h *MetricsHandler) GetWorkspace(w http.ResponseWriter, r *http.Request) {
	workspace := auth.WorkspaceSlugFromContext(r.Context())

	end := time.Now()
	start := end.Add(-1 * time.Hour)
	step := "60"

	requestRate, err := h.queryRange(r.Context(),
		fmt.Sprintf(`sum(rate(platform_function_invocations_total{workspace="%s"}[2m]))`, workspace),
		start, end, step,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to query metrics: "+err.Error())
		return
	}

	latencyP99, err := h.queryRange(r.Context(),
		fmt.Sprintf(`histogram_quantile(0.99, sum(rate(platform_function_invocation_duration_seconds_bucket{workspace="%s"}[2m])) by (le))`, workspace),
		start, end, step,
	)
	if err != nil {
		latencyP99 = []*DataPoint{}
	}

	writeJSON(w, http.StatusOK, MetricsResponse{
		RequestRate: requestRate,
		LatencyP99:  latencyP99,
	})
}

// prometheusRangeResponse is the subset of the Prometheus HTTP API response we need.
type prometheusRangeResponse struct {
	Status string `json:"status"`
	Data   struct {
		Result []struct {
			Values [][2]json.RawMessage `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

func (h *MetricsHandler) queryRange(ctx context.Context, query string, start, end time.Time, step string) ([]*DataPoint, error) {
	params := url.Values{}
	params.Set("query", query)
	params.Set("start", strconv.FormatInt(start.Unix(), 10))
	params.Set("end", strconv.FormatInt(end.Unix(), 10))
	params.Set("step", step)

	reqURL := h.prometheusURL + "/api/v1/query_range?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var result prometheusRangeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if result.Status != "success" {
		return nil, fmt.Errorf("prometheus returned status %q", result.Status)
	}

	points := []*DataPoint{}
	if len(result.Data.Result) == 0 {
		return points, nil
	}

	for _, pair := range result.Data.Result[0].Values {
		var ts float64
		var valStr string
		if err := json.Unmarshal(pair[0], &ts); err != nil {
			continue
		}
		if err := json.Unmarshal(pair[1], &valStr); err != nil {
			continue
		}
		v, err := strconv.ParseFloat(valStr, 64)
		if err != nil || math.IsNaN(v) {
			v = 0
		}
		points = append(points, &DataPoint{T: int64(ts), V: v})
	}
	return points, nil
}
