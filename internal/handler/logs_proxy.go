package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"serverless-platform/internal/auth"
	"serverless-platform/internal/db"
)

// FunctionLookup is the narrow slice of the database the logs handler needs.
// Defined here so tests can satisfy it with a fake without pulling in *db.DB.
type FunctionLookup interface {
	GetWorkspaceBySlug(ctx context.Context, slug string) (*db.Workspace, error)
	GetFunction(ctx context.Context, workspaceID int64, name string) (*db.Function, error)
}

type LogsHandler struct {
	lokiURL string
	store   FunctionLookup
}

func NewLogsHandler(lokiURL string, store FunctionLookup) *LogsHandler {
	return &LogsHandler{
		lokiURL: lokiURL,
		store:   store,
	}
}

type logLine struct {
	Timestamp int64  `json:"ts"`
	Line      string `json:"line"`
	Level     string `json:"level,omitempty"`
}

// Stream opens an SSE connection and tails Loki logs for a single function.
// GET /api/v1/logs/stream?function=...&level=...
//
// The function parameter is required and is verified against the caller's
// workspace before any Loki query runs. Cross-tenant access returns 404 (not
// 403) to avoid leaking whether a function exists in another workspace.
func (h *LogsHandler) Stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	fn := r.URL.Query().Get("function")
	if fn == "" {
		writeError(w, http.StatusBadRequest, "function parameter is required")
		return
	}
	level := r.URL.Query().Get("level")

	// Cross-tenant guard: verify the function belongs to the caller's workspace.
	wsSlug := auth.WorkspaceSlugFromContext(r.Context())
	ws, err := h.store.GetWorkspaceBySlug(r.Context(), wsSlug)
	if err != nil || ws == nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve workspace")
		return
	}
	fnRecord, err := h.store.GetFunction(r.Context(), ws.ID, fn)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve function")
		return
	}
	if fnRecord == nil {
		writeError(w, http.StatusNotFound, "function not found")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Function pods run in the per-workspace namespace fn-{slug}, not in the
	// platform API's own namespace. Knative names pods
	// fn-{name}-{revision}-deployment-{hash}, so a regex anchored on the
	// function name matches all revisions.
	fnNamespace := "fn-" + wsSlug
	podPattern := fmt.Sprintf("fn-%s-.*", regexp.QuoteMeta(fn))
	selector := fmt.Sprintf(`{namespace=%q, pod=~%q}`, fnNamespace, podPattern)

	// Level filter uses the JSON parser so it works even for pods whose log
	// level isn't promoted to a stream label (Alloy only promotes `level` when
	// it can be extracted from the slog JSON stage).
	query := selector
	if level != "" {
		query += fmt.Sprintf(` | json | level=%q`, level)
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// Prime the window with the last 30 seconds so an initial connection sees
	// recent context instead of a blank screen.
	since := time.Now().Add(-30 * time.Second)

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			now := time.Now()
			lines, err := h.queryRange(r.Context(), query, since, now)
			if err != nil {
				// Transient upstream errors surface as SSE comment frames
				// (prefixed with `:`) which EventSource silently discards.
				// The stream stays open so the client sees logs resume as
				// soon as Loki recovers.
				_, _ = fmt.Fprintf(w, ": upstream error: %s\n\n", err.Error())
				flusher.Flush()
				since = now
				continue
			}
			for _, l := range lines {
				data, _ := json.Marshal(l)
				_, _ = fmt.Fprintf(w, "event: log\ndata: %s\n\n", data)
			}
			flusher.Flush()
			since = now
		}
	}
}

// queryRange hits Loki's /loki/api/v1/query_range and flattens the response
// into a flat []logLine ordered by source stream and timestamp.
func (h *LogsHandler) queryRange(ctx context.Context, logQL string, start, end time.Time) ([]logLine, error) {
	params := url.Values{}
	params.Set("query", logQL)
	params.Set("start", strconv.FormatInt(start.UnixNano(), 10))
	params.Set("end", strconv.FormatInt(end.UnixNano(), 10))
	params.Set("direction", "forward")
	params.Set("limit", "200")

	reqURL := h.lokiURL + "/loki/api/v1/query_range?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("loki returned status %d", resp.StatusCode)
	}

	var result struct {
		Data struct {
			Result []struct {
				Stream map[string]string `json:"stream"`
				Values [][2]string       `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var out []logLine
	for _, stream := range result.Data.Result {
		for _, v := range stream.Values {
			ns, parseErr := strconv.ParseInt(v[0], 10, 64)
			if parseErr != nil {
				continue
			}
			out = append(out, logLine{
				Timestamp: ns / int64(time.Millisecond),
				Line:      v[1],
				Level:     stream.Stream["level"],
			})
		}
	}
	return out, nil
}
