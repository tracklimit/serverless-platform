package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"serverless-platform/internal/db"
	"serverless-platform/internal/handler"
)

func TestStream_EmitsSSEFrames(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(
			`{"data":{"result":[{"stream":{"level":"info"},"values":[["1700000000000000000","hello"]]}]}}`,
		))
	}))
	defer upstream.Close()

	store := newMockDeploymentStore()
	store.workspaces["ws-a"] = &db.Workspace{ID: 1, Slug: "ws-a", Name: "Workspace A"}
	store.functions["1:hello"] = &db.Function{ID: 5, WorkspaceID: 1, Name: "hello"}

	h := handler.NewLogsHandler(upstream.URL, "serverless-platform", store)

	// Stream loops on a 2s ticker; a 3s deadline guarantees one iteration then exit.
	req := deployRequest(http.MethodGet, "/?function=hello", "ws-a")
	ctx, cancel := context.WithTimeout(req.Context(), 3*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.Stream(rec, req)

	body := rec.Body.String()
	assert.Contains(t, body, "event: log")
	assert.Contains(t, body, `"line":"hello"`)
}

func TestStream_RejectsMissingFunction(t *testing.T) {
	store := newMockDeploymentStore()
	h := handler.NewLogsHandler("http://irrelevant", "serverless-platform", store)

	req := deployRequest(http.MethodGet, "/", "ws-a")
	rec := httptest.NewRecorder()
	h.Stream(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestStream_404OnCrossTenantFunction(t *testing.T) {
	store := newMockDeploymentStore()
	store.workspaces["ws-a"] = &db.Workspace{ID: 1, Slug: "ws-a", Name: "Workspace A"}
	// Deliberately do NOT register any function under workspace 1 — a caller
	// in ws-a asking for "hello" should get 404 regardless of whether "hello"
	// exists in some other workspace.

	h := handler.NewLogsHandler("http://irrelevant", "serverless-platform", store)

	req := deployRequest(http.MethodGet, "/?function=hello", "ws-a")
	rec := httptest.NewRecorder()
	h.Stream(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}
