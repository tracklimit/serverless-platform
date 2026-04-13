package handler_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"serverless-platform/internal/db"
	"serverless-platform/internal/handler"
)

// These tests exercise the auth/validation paths that return before the
// handler ever touches NATS, so a nil JetStream is safe. A happy-path SSE
// test would require a real in-process NATS server (see
// internal/worker/deploy_test.go for that pattern) and is out of scope here.

func TestDeployEvents_RejectsBadID(t *testing.T) {
	store := newMockDeploymentStore()
	h := handler.NewDeployEventsHandler(store, nil)

	req := deployRequest(http.MethodGet, "/", "ws-a")
	req = withChi(req, map[string]string{"id": "not-a-number"})

	rec := httptest.NewRecorder()
	h.Stream(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestDeployEvents_404WhenDeploymentMissing(t *testing.T) {
	store := newMockDeploymentStore()
	store.workspaces["ws-a"] = &db.Workspace{ID: 1, Slug: "ws-a", Name: "Workspace A"}

	h := handler.NewDeployEventsHandler(store, nil)

	req := deployRequest(http.MethodGet, "/", "ws-a")
	req = withChi(req, map[string]string{"id": "99"})

	rec := httptest.NewRecorder()
	h.Stream(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// TestDeployEvents_404OnCrossTenant is the security-critical case: a caller
// in workspace B asking for deployment 10 (which belongs to workspace A) must
// receive 404, not 403. 403 would leak the fact that the deployment exists.
func TestDeployEvents_404OnCrossTenant(t *testing.T) {
	store := newMockDeploymentStore()
	store.workspaces["ws-a"] = &db.Workspace{ID: 1, Slug: "ws-a", Name: "Workspace A"}
	store.workspaces["ws-b"] = &db.Workspace{ID: 2, Slug: "ws-b", Name: "Workspace B"}
	store.deployments[10] = &db.Deployment{
		ID:           10,
		WorkspaceID:  1,
		FunctionName: "hello",
		CreatedAt:    time.Now(),
	}

	h := handler.NewDeployEventsHandler(store, nil)

	// Caller authenticated as ws-b asking for deployment 10 (owned by ws-a).
	req := deployRequest(http.MethodGet, "/", "ws-b")
	req = withChi(req, map[string]string{"id": "10"})

	rec := httptest.NewRecorder()
	h.Stream(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}
