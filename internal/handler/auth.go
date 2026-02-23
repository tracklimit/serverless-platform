package handler

import (
	"encoding/json"
	"net/http"

	"golang.org/x/crypto/bcrypt"

	"serverless-platform/internal/auth"
	"serverless-platform/internal/db"
)

type AuthHandler struct {
	db           *db.DB
	tokenService *auth.TokenService
}

func NewAuthHandler(database *db.DB, tokenService *auth.TokenService) *AuthHandler {
	return &AuthHandler{db: database, tokenService: tokenService}
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token string `json:"token"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "username and password are required")
		return
	}

	user, err := h.db.GetUserByUsername(r.Context(), req.Username)
	if err != nil || user == nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	workspaceSlug := ""
	workspaceRole := ""
	if !user.IsAdmin {
		ws, role, err := h.db.GetUserWorkspace(r.Context(), user.Username)
		if err == nil && ws != nil {
			workspaceSlug = ws.Slug
			workspaceRole = role
		}
	}

	token, err := h.tokenService.Generate(user.Username, user.IsAdmin, workspaceSlug, workspaceRole)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}
	writeJSON(w, http.StatusOK, loginResponse{Token: token})
}

func (h *AuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	username := auth.UsernameFromContext(r.Context())

	var req changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.CurrentPassword == "" || req.NewPassword == "" {
		writeError(w, http.StatusBadRequest, "current_password and new_password are required")
		return
	}

	user, err := h.db.GetUserByUsername(r.Context(), username)
	if err != nil || user == nil {
		writeError(w, http.StatusInternalServerError, "failed to retrieve user")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.CurrentPassword)); err != nil {
		writeError(w, http.StatusUnauthorized, "current password is incorrect")
		return
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	if err := h.db.UpdatePasswordHash(r.Context(), username, string(newHash)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update password")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
