package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"

	"serverless-platform/internal/db"
	"serverless-platform/internal/pagination"
)

type UserHandler struct {
	db *db.DB
}

func NewUserHandler(database *db.DB) *UserHandler {
	return &UserHandler{db: database}
}

func (h *UserHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.List)
	r.Post("/", h.Create)
	r.Delete("/{username}", h.Delete)
	return r
}

type createUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	IsAdmin  bool   `json:"is_admin"`
}

type userResponse struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	IsAdmin   bool   `json:"is_admin"`
	CreatedAt string `json:"created_at"`
}

func (h *UserHandler) List(w http.ResponseWriter, r *http.Request) {
	params := pagination.Parse(r)

	users, total, err := h.db.ListUsers(r.Context(), db.ListUsersParams{
		Limit:  params.Limit,
		Offset: params.Offset,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list users")
		return
	}

	resp := make([]userResponse, 0, len(users))
	for _, u := range users {
		resp = append(resp, userResponse{
			ID:        u.ID,
			Username:  u.Username,
			IsAdmin:   u.IsAdmin,
			CreatedAt: u.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	writeJSON(w, http.StatusOK, pagination.Response{
		Items:      resp,
		TotalCount: total,
		Limit:      params.Limit,
		Offset:     params.Offset,
	})
}

func (h *UserHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "username and password are required")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	user, err := h.db.CreateUser(r.Context(), req.Username, string(hash), req.IsAdmin)
	if err != nil {
		writeError(w, http.StatusConflict, "user already exists or invalid data")
		return
	}
	writeJSON(w, http.StatusCreated, userResponse{
		ID:        user.ID,
		Username:  user.Username,
		IsAdmin:   user.IsAdmin,
		CreatedAt: user.CreatedAt.Format("2006-01-02T15:04:05Z"),
	})
}

func (h *UserHandler) Delete(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")
	if username == "admin" {
		writeError(w, http.StatusBadRequest, "cannot delete the admin user")
		return
	}
	if err := h.db.DeleteUser(r.Context(), username); err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
