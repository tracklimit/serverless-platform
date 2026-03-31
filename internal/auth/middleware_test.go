package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"serverless-platform/internal/auth"
)

func TestMiddleware(t *testing.T) {
	svc := auth.NewTokenService(testSigningKey, time.Hour)
	validToken, _ := svc.Generate("alice", false, "my-ws", "owner")

	handler := auth.Middleware(svc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "alice", auth.UsernameFromContext(r.Context()))
		assert.Equal(t, false, auth.IsAdminFromContext(r.Context()))
		assert.Equal(t, "my-ws", auth.WorkspaceSlugFromContext(r.Context()))
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		name       string
		authHeader string
		wantStatus int
	}{
		{
			name:       "valid token",
			authHeader: "Bearer " + validToken,
			wantStatus: http.StatusOK,
		},
		{
			name:       "missing header",
			authHeader: "",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "no bearer prefix",
			authHeader: validToken,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "invalid token",
			authHeader: "Bearer invalid-token",
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			handler.ServeHTTP(rec, req)
			assert.Equal(t, tt.wantStatus, rec.Code)
		})
	}
}

func TestAdminOnly(t *testing.T) {
	svc := auth.NewTokenService(testSigningKey, time.Hour)
	adminToken, _ := svc.Generate("admin", true, "", "")
	userToken, _ := svc.Generate("alice", false, "ws", "member")

	handler := auth.Middleware(svc)(auth.AdminOnly(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	tests := []struct {
		name       string
		token      string
		wantStatus int
	}{
		{name: "admin allowed", token: adminToken, wantStatus: http.StatusOK},
		{name: "non-admin forbidden", token: userToken, wantStatus: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", "Bearer "+tt.token)

			handler.ServeHTTP(rec, req)
			assert.Equal(t, tt.wantStatus, rec.Code)
		})
	}
}

func TestWorkspaceRequired(t *testing.T) {
	svc := auth.NewTokenService(testSigningKey, time.Hour)
	withWorkspace, _ := svc.Generate("alice", false, "my-ws", "owner")
	withoutWorkspace, _ := svc.Generate("admin", true, "", "")

	handler := auth.Middleware(svc)(auth.WorkspaceRequired(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	tests := []struct {
		name       string
		token      string
		wantStatus int
	}{
		{name: "with workspace", token: withWorkspace, wantStatus: http.StatusOK},
		{name: "without workspace", token: withoutWorkspace, wantStatus: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", "Bearer "+tt.token)

			handler.ServeHTTP(rec, req)
			assert.Equal(t, tt.wantStatus, rec.Code)
		})
	}
}
