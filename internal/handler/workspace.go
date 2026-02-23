package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"serverless-platform/internal/auth"
	"serverless-platform/internal/db"
)

type WorkspaceHandler struct {
	db *db.DB
}

func NewWorkspaceHandler(database *db.DB) *WorkspaceHandler {
	return &WorkspaceHandler{db: database}
}

func (h *WorkspaceHandler) AdminRoutes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.List)
	r.Post("/", h.Create)
	r.Delete("/{slug}", h.Delete)
	r.Post("/{slug}/members", h.AddMember)
	r.Delete("/{slug}/members/{username}", h.RemoveMember)
	r.Get("/{slug}/members", h.ListMembers)
	return r
}

func (h *WorkspaceHandler) OwnerRoutes() chi.Router {
	r := chi.NewRouter()
	r.Post("/members", h.AddOwnMember)
	r.Delete("/members/{username}", h.RemoveOwnMember)
	r.Get("/members", h.ListOwnMembers)
	return r
}

type createWorkspaceRequest struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

type addMemberRequest struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}

type workspaceResponse struct {
	ID        int64  `json:"id"`
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

type memberResponse struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}

func (h *WorkspaceHandler) List(w http.ResponseWriter, r *http.Request) {
	workspaces, err := h.db.ListWorkspaces(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list workspaces")
		return
	}
	resp := make([]workspaceResponse, 0, len(workspaces))
	for _, ws := range workspaces {
		resp = append(resp, workspaceResponse{
			ID:        ws.ID,
			Slug:      ws.Slug,
			Name:      ws.Name,
			CreatedAt: ws.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *WorkspaceHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createWorkspaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Slug == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "slug and name are required")
		return
	}

	ws, err := h.db.CreateWorkspace(r.Context(), req.Slug, req.Name)
	if err != nil {
		writeError(w, http.StatusConflict, "workspace already exists or invalid data")
		return
	}
	writeJSON(w, http.StatusCreated, workspaceResponse{
		ID:        ws.ID,
		Slug:      ws.Slug,
		Name:      ws.Name,
		CreatedAt: ws.CreatedAt.Format("2006-01-02T15:04:05Z"),
	})
}

func (h *WorkspaceHandler) Delete(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if err := h.db.DeleteWorkspace(r.Context(), slug); err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *WorkspaceHandler) AddMember(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	h.addMember(w, r, slug)
}

func (h *WorkspaceHandler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	username := chi.URLParam(r, "username")
	h.removeMember(w, r, slug, username)
}

func (h *WorkspaceHandler) ListMembers(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	h.listMembers(w, r, slug)
}

// Owner routes — scoped to the caller's own workspace

func (h *WorkspaceHandler) AddOwnMember(w http.ResponseWriter, r *http.Request) {
	slug := auth.WorkspaceSlugFromContext(r.Context())
	h.addMember(w, r, slug)
}

func (h *WorkspaceHandler) RemoveOwnMember(w http.ResponseWriter, r *http.Request) {
	slug := auth.WorkspaceSlugFromContext(r.Context())
	username := chi.URLParam(r, "username")
	h.removeMember(w, r, slug, username)
}

func (h *WorkspaceHandler) ListOwnMembers(w http.ResponseWriter, r *http.Request) {
	slug := auth.WorkspaceSlugFromContext(r.Context())
	h.listMembers(w, r, slug)
}

func (h *WorkspaceHandler) addMember(w http.ResponseWriter, r *http.Request, slug string) {
	var req addMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Username == "" {
		writeError(w, http.StatusBadRequest, "username is required")
		return
	}
	if req.Role != "owner" && req.Role != "member" {
		req.Role = "member"
	}
	if err := h.db.AddWorkspaceMember(r.Context(), slug, req.Username, req.Role); err != nil {
		writeError(w, http.StatusBadRequest, "failed to add member")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *WorkspaceHandler) removeMember(w http.ResponseWriter, r *http.Request, slug, username string) {
	if err := h.db.RemoveWorkspaceMember(r.Context(), slug, username); err != nil {
		writeError(w, http.StatusNotFound, "member not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *WorkspaceHandler) listMembers(w http.ResponseWriter, r *http.Request, slug string) {
	members, err := h.db.ListWorkspaceMembers(r.Context(), slug)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list members")
		return
	}
	resp := make([]memberResponse, 0, len(members))
	for _, m := range members {
		resp = append(resp, memberResponse{Username: m.Username, Role: m.Role})
	}
	writeJSON(w, http.StatusOK, resp)
}
