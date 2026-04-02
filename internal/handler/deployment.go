package handler

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"serverless-platform/internal/auth"
	"serverless-platform/internal/db"
	"serverless-platform/internal/pagination"
)

type DeploymentHandler struct {
	db *db.DB
}

func NewDeploymentHandler(database *db.DB) *DeploymentHandler {
	return &DeploymentHandler{db: database}
}

// GetDeployment returns a single deployment by ID.
// GET /api/v1/deploys/{id}
func (h *DeploymentHandler) Get(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid deployment id")
		return
	}

	dep, err := h.db.GetDeployment(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get deployment")
		return
	}
	if dep == nil {
		writeError(w, http.StatusNotFound, "deployment not found")
		return
	}

	// Authorization: deployment must belong to the caller's workspace
	wsSlug := auth.WorkspaceSlugFromContext(r.Context())
	ws, err := h.db.GetWorkspaceBySlug(r.Context(), wsSlug)
	if err != nil || ws == nil || ws.ID != dep.WorkspaceID {
		writeError(w, http.StatusNotFound, "deployment not found")
		return
	}

	writeJSON(w, http.StatusOK, dep)
}

// ListByFunction returns paginated deploy history for a function.
// GET /api/v1/functions/{name}/deploys
func (h *DeploymentHandler) ListByFunction(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	params := pagination.Parse(r)

	wsSlug := auth.WorkspaceSlugFromContext(r.Context())
	ws, err := h.db.GetWorkspaceBySlug(r.Context(), wsSlug)
	if err != nil || ws == nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve workspace")
		return
	}

	fn, err := h.db.GetFunction(r.Context(), ws.ID, name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get function")
		return
	}
	if fn == nil {
		writeError(w, http.StatusNotFound, "function not found")
		return
	}

	deps, total, err := h.db.ListDeployments(r.Context(), db.ListDeploymentsParams{
		FunctionID:  fn.ID,
		WorkspaceID: ws.ID,
		Limit:       params.Limit,
		Offset:      params.Offset,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list deployments")
		return
	}

	writeJSON(w, http.StatusOK, pagination.Response{
		Items:      deps,
		TotalCount: total,
		Limit:      params.Limit,
		Offset:     params.Offset,
	})
}
