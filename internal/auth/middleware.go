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

			next.ServeHTTP(w, r.WithContext(claimsContext(r.Context(), claims)))
		})
	}
}

// MiddlewareSSE is the SSE-flavored variant of Middleware. Browsers can't set
// custom headers on EventSource, so the token arrives as an `access_token`
// query parameter (Bearer header is still accepted when present, e.g. for
// curl/tests). After validation the parameter is stripped from r.URL so that
// the token doesn't leak into access logs, tracing spans, or any downstream
// code that serializes the full URL.
func MiddlewareSSE(tokenService *TokenService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var token string
			if header := r.Header.Get("Authorization"); header != "" {
				t, found := strings.CutPrefix(header, "Bearer ")
				if !found {
					http.Error(w, `{"error":"invalid authorization format"}`, http.StatusUnauthorized)
					return
				}
				token = t
			} else {
				token = r.URL.Query().Get("access_token")
			}

			if token == "" {
				http.Error(w, `{"error":"missing authorization"}`, http.StatusUnauthorized)
				return
			}

			claims, err := tokenService.Validate(token)
			if err != nil {
				http.Error(w, `{"error":"invalid or expired token"}`, http.StatusUnauthorized)
				return
			}

			// Scrub access_token from the URL so it doesn't reach request
			// loggers, tracing spans, or any handler code reading r.URL.
			if r.URL.Query().Has("access_token") {
				q := r.URL.Query()
				q.Del("access_token")
				r.URL.RawQuery = q.Encode()
			}

			next.ServeHTTP(w, r.WithContext(claimsContext(r.Context(), claims)))
		})
	}
}

func claimsContext(ctx context.Context, claims *Claims) context.Context {
	ctx = context.WithValue(ctx, usernameContextKey, claims.Username)
	ctx = context.WithValue(ctx, isAdminContextKey, claims.IsAdmin)
	ctx = context.WithValue(ctx, workspaceSlugContextKey, claims.WorkspaceSlug)
	ctx = context.WithValue(ctx, workspaceRoleContextKey, claims.WorkspaceRole)
	return ctx
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

func WithWorkspace(ctx context.Context, slug string) context.Context {
	return context.WithValue(ctx, workspaceSlugContextKey, slug)
}
