package handler_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"serverless-platform/internal/auth"
	"serverless-platform/internal/db"
	"serverless-platform/internal/handler"
	"serverless-platform/internal/pagination"
)

// mockDeploymentStore implements handler.DeploymentStore for testing.
type mockDeploymentStore struct {
	deployments map[int64]*db.Deployment
	workspaces  map[string]*db.Workspace
	functions   map[string]*db.Function
}

func newMockDeploymentStore() *mockDeploymentStore {
	return &mockDeploymentStore{
		deployments: make(map[int64]*db.Deployment),
		workspaces:  make(map[string]*db.Workspace),
		functions:   make(map[string]*db.Function),
	}
}

func (m *mockDeploymentStore) GetDeployment(_ context.Context, id int64) (*db.Deployment, error) {
	dep, ok := m.deployments[id]
	if !ok {
		return nil, nil
	}
	return dep, nil
}

func (m *mockDeploymentStore) GetWorkspaceBySlug(_ context.Context, slug string) (*db.Workspace, error) {
	ws, ok := m.workspaces[slug]
	if !ok {
		return nil, nil
	}
	return ws, nil
}

func (m *mockDeploymentStore) GetFunction(_ context.Context, workspaceID int64, name string) (*db.Function, error) {
	key := fmt.Sprintf("%d:%s", workspaceID, name)
	fn, ok := m.functions[key]
	if !ok {
		return nil, nil
	}
	return fn, nil
}

func (m *mockDeploymentStore) ListDeployments(_ context.Context, p db.ListDeploymentsParams) ([]*db.Deployment, int, error) {
	var matched []*db.Deployment
	for _, dep := range m.deployments {
		if dep.FunctionID == p.FunctionID && dep.WorkspaceID == p.WorkspaceID {
			matched = append(matched, dep)
		}
	}
	total := len(matched)
	if p.Offset >= total {
		return []*db.Deployment{}, total, nil
	}
	end := p.Offset + p.Limit
	if end > total {
		end = total
	}
	return matched[p.Offset:end], total, nil
}

func deployRequest(method, path, workspace string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	ctx := auth.WithUsername(req.Context(), "testuser")
	ctx = auth.WithWorkspace(ctx, workspace)
	return req.WithContext(ctx)
}

func withChi(r *http.Request, params map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestGetDeployment_HappyPath(t *testing.T) {
	store := newMockDeploymentStore()
	store.workspaces["my-ws"] = &db.Workspace{ID: 1, Slug: "my-ws", Name: "My Workspace"}
	store.deployments[10] = &db.Deployment{
		ID:           10,
		FunctionID:   5,
		FunctionName: "hello",
		WorkspaceID:  1,
		Status:       "running",
		CreatedAt:    time.Now(),
	}

	h := handler.NewDeploymentHandler(store)
	rec := httptest.NewRecorder()
	req := deployRequest(http.MethodGet, "/api/v1/deploys/10", "my-ws")
	req = withChi(req, map[string]string{"id": "10"})

	h.Get(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var got db.Deployment
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, int64(10), got.ID)
	assert.Equal(t, "running", got.Status)
}

func TestGetDeployment_WrongWorkspace(t *testing.T) {
	store := newMockDeploymentStore()
	store.workspaces["ws-a"] = &db.Workspace{ID: 1, Slug: "ws-a", Name: "Workspace A"}
	store.workspaces["ws-b"] = &db.Workspace{ID: 2, Slug: "ws-b", Name: "Workspace B"}
	store.deployments[10] = &db.Deployment{
		ID:          10,
		WorkspaceID: 1,
		CreatedAt:   time.Now(),
	}

	h := handler.NewDeploymentHandler(store)
	rec := httptest.NewRecorder()
	req := deployRequest(http.MethodGet, "/api/v1/deploys/10", "ws-b")
	req = withChi(req, map[string]string{"id": "10"})

	h.Get(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestListByFunction_Pagination(t *testing.T) {
	store := newMockDeploymentStore()
	store.workspaces["my-ws"] = &db.Workspace{ID: 1, Slug: "my-ws", Name: "My Workspace"}
	store.functions["1:hello"] = &db.Function{ID: 5, WorkspaceID: 1, Name: "hello"}

	for i := 0; i < 25; i++ {
		store.deployments[int64(i+1)] = &db.Deployment{
			ID:           int64(i + 1),
			FunctionID:   5,
			WorkspaceID:  1,
			FunctionName: "hello",
			Status:       "running",
			CreatedAt:    time.Now(),
		}
	}

	h := handler.NewDeploymentHandler(store)
	rec := httptest.NewRecorder()
	req := deployRequest(http.MethodGet, "/api/v1/functions/hello/deploys?limit=10&offset=0", "my-ws")
	req = withChi(req, map[string]string{"name": "hello"})

	h.ListByFunction(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var got pagination.Response
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, 25, got.TotalCount)
	assert.Equal(t, 10, got.Limit)
}

func TestListByFunction_FunctionNotFound(t *testing.T) {
	store := newMockDeploymentStore()
	store.workspaces["my-ws"] = &db.Workspace{ID: 1, Slug: "my-ws", Name: "My Workspace"}

	h := handler.NewDeploymentHandler(store)
	rec := httptest.NewRecorder()
	req := deployRequest(http.MethodGet, "/api/v1/functions/nonexistent/deploys", "my-ws")
	req = withChi(req, map[string]string{"name": "nonexistent"})

	h.ListByFunction(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}
