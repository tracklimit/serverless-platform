package handler

import (
	"encoding/json"
	"net/http"

	"golang.org/x/crypto/bcrypt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"serverless-platform/internal/auth"
)

type AuthHandler struct {
	kubeClient   kubernetes.Interface
	tokenService *auth.TokenService
	namespace    string
}

func NewAuthHandler(kubeClient kubernetes.Interface, tokenService *auth.TokenService, namespace string) *AuthHandler {
	return &AuthHandler{
		kubeClient:   kubeClient,
		tokenService: tokenService,
		namespace:    namespace,
	}
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type loginResponse struct {
	Token string `json:"token"`
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

	secret, err := h.kubeClient.CoreV1().Secrets(h.namespace).Get(
		r.Context(), "platform-credentials", metav1.GetOptions{},
	)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	storedUsername := string(secret.Data["username"])
	storedHash := secret.Data["password-hash"]

	if req.Username != storedUsername {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	if err := bcrypt.CompareHashAndPassword(storedHash, []byte(req.Password)); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	token, err := h.tokenService.Generate(req.Username)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	writeJSON(w, http.StatusOK, loginResponse{Token: token})
}

func (h *AuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	var req changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.CurrentPassword == "" || req.NewPassword == "" {
		writeError(w, http.StatusBadRequest, "current_password and new_password are required")
		return
	}

	secret, err := h.kubeClient.CoreV1().Secrets(h.namespace).Get(
		r.Context(), "platform-credentials", metav1.GetOptions{},
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to retrieve credentials")
		return
	}

	if err := bcrypt.CompareHashAndPassword(secret.Data["password-hash"], []byte(req.CurrentPassword)); err != nil {
		writeError(w, http.StatusUnauthorized, "current password is incorrect")
		return
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	secret.Data["password-hash"] = newHash
	if _, err := h.kubeClient.CoreV1().Secrets(h.namespace).Update(
		r.Context(), secret, metav1.UpdateOptions{},
	); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update credentials")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
