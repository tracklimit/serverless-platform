package auth

import "net/http"

func WorkspaceRequired(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if WorkspaceSlugFromContext(r.Context()) == "" {
			http.Error(w, `{"error":"no workspace assigned"}`, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
