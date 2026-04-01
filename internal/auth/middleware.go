package auth

import (
	"context"
	"net/http"
	"strings"
)

type contextKey string

const (
	usernameContextKey      contextKey = "username"
	isAdminContextKey       contextKey = "is_admin"
	workspaceSlugContextKey contextKey = "workspace_slug"
	workspaceRoleContextKey contextKey = "workspace_role"
)

func Middleware(tokenService *TokenService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if header == "" {
				http.Error(w, `{"error":"missing authorization header"}`, http.StatusUnauthorized)
				return
			}

			token, found := strings.CutPrefix(header, "Bearer ")
			if !found {
				http.Error(w, `{"error":"invalid authorization format"}`, http.StatusUnauthorized)
				return
			}

			claims, err := tokenService.Validate(token)
			if err != nil {
				http.Error(w, `{"error":"invalid or expired token"}`, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), usernameContextKey, claims.Username)
			ctx = context.WithValue(ctx, isAdminContextKey, claims.IsAdmin)
			ctx = context.WithValue(ctx, workspaceSlugContextKey, claims.WorkspaceSlug)
			ctx = context.WithValue(ctx, workspaceRoleContextKey, claims.WorkspaceRole)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func UsernameFromContext(ctx context.Context) string {
	v, _ := ctx.Value(usernameContextKey).(string)
	return v
}

func IsAdminFromContext(ctx context.Context) bool {
	v, _ := ctx.Value(isAdminContextKey).(bool)
	return v
}

func WorkspaceSlugFromContext(ctx context.Context) string {
	v, _ := ctx.Value(workspaceSlugContextKey).(string)
	return v
}

func WithUsername(ctx context.Context, username string) context.Context {
	return context.WithValue(ctx, usernameContextKey, username)
}
