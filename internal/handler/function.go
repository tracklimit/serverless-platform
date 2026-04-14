package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"serverless-platform/internal/auth"
	"serverless-platform/internal/cache"
	"serverless-platform/internal/pagination"
	"strconv"

	"github.com/go-chi/chi/v5"

	"serverless-platform/internal/model"
	"serverless-platform/internal/service"
)

type FunctionHandler struct {
	svc   *service.FunctionService
	cache cache.Store
}

func NewFunctionHandler(svc *service.FunctionService, cacheStore cache.Store) *FunctionHandler {
	return &FunctionHandler{svc: svc, cache: cacheStore}
}

func (h *FunctionHandler) Routes(metrics *MetricsHandler) chi.Router {
	r := chi.NewRouter()
	r.Post("/", h.Create)
	r.Get("/", h.List)
	r.Get("/{name}", h.Get)
	r.Put("/{name}", h.Update)
	r.Delete("/{name}", h.Delete)
	r.Post("/{name}/invoke", h.Invoke)
	r.Get("/{name}/logs", h.Logs)
	r.Get("/{name}/metrics", metrics.Get)
	return r
}

func (h *FunctionHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req model.CreateFunctionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	fn, deployment, err := h.svc.Create(r.Context(), &req)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	workspace := auth.WorkspaceSlugFromContext(r.Context())
	_ = h.cache.Invalidate(r.Context(), "cache:"+workspace+":*")

	writeJSON(w, http.StatusAccepted, map[string]any{
		"function":   fn,
		"deployment": deployment,
	})
}

func (h *FunctionHandler) Get(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	fn, err := h.svc.Get(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if fn == nil {
		writeError(w, http.StatusNotFound, "function not found")
		return
	}

	writeJSON(w, http.StatusOK, fn)
}

func (h *FunctionHandler) List(w http.ResponseWriter, r *http.Request) {
	params := pagination.Parse(r)

	functions, total, err := h.svc.List(r.Context(), service.ListParams{
		Limit:      params.Limit,
		Offset:     params.Offset,
		Sort:       params.Sort,
		Order:      params.Order,
		Runtime:    r.URL.Query().Get("runtime"),
		DeployType: r.URL.Query().Get("deploy_type"),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if functions == nil {
		functions = []*model.Function{}
	}

	writeJSON(w, http.StatusOK, pagination.Response{
		Items:      functions,
		TotalCount: total,
		Limit:      params.Limit,
		Offset:     params.Offset,
	})
}

func (h *FunctionHandler) Update(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	var req model.UpdateFunctionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	fn, deployment, err := h.svc.Update(r.Context(), name, &req)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if fn == nil {
		writeError(w, http.StatusNotFound, "function not found")
		return
	}

	workspace := auth.WorkspaceSlugFromContext(r.Context())
	_ = h.cache.Invalidate(r.Context(), "cache:"+workspace+":*")

	writeJSON(w, http.StatusAccepted, map[string]any{
		"function":   fn,
		"deployment": deployment,
	})
}

func (h *FunctionHandler) Invoke(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	fn, err := h.svc.Get(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if fn == nil {
		writeError(w, http.StatusNotFound, "function not found")
		return
	}

	targetURL := h.svc.InvokeURL(r.Context(), name)

	proxyReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, targetURL, r.Body) //nolint:gosec // targetURL is from deployer.InvokeURL, not user input
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create proxy request")
		return
	}
	proxyReq.Header.Set("Content-Type", r.Header.Get("Content-Type"))

	resp, err := http.DefaultClient.Do(proxyReq) //nolint:gosec // targetURL is from deployer.InvokeURL, not user input
	if err != nil {
		writeError(w, http.StatusBadGateway, "function invocation failed: "+err.Error())
		return
	}
	defer func() { _ = resp.Body.Close() }()

	for key, values := range resp.Header {
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// InvokePublic handles anonymous invocations at /fn/{workspace}/{name}. Unlike
// Invoke, this route lives outside the auth middleware group — so the handler
// itself enforces the public gate by calling GetPublic, which returns nil for
// both missing and private functions. Any non-public function therefore looks
// identical to a typo to an outside caller.
//
// The request method and body are forwarded verbatim to Knative so public
// functions can expose whatever HTTP surface they want (GET, POST, webhooks,
// etc.) without the platform imposing a verb.
func (h *FunctionHandler) InvokePublic(w http.ResponseWriter, r *http.Request) {
	workspace := chi.URLParam(r, "workspace")
	name := chi.URLParam(r, "name")

	fn, err := h.svc.GetPublic(r.Context(), workspace, name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if fn == nil {
		writeError(w, http.StatusNotFound, "function not found")
		return
	}

	targetURL := h.svc.PublicInvokeTarget(workspace, name)

	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, r.Body) //nolint:gosec // targetURL is from deployer.InvokeURL, not user input
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create proxy request")
		return
	}
	if ct := r.Header.Get("Content-Type"); ct != "" {
		proxyReq.Header.Set("Content-Type", ct)
	}

	resp, err := http.DefaultClient.Do(proxyReq) //nolint:gosec // targetURL is from deployer.InvokeURL, not user input
	if err != nil {
		writeError(w, http.StatusBadGateway, "function invocation failed: "+err.Error())
		return
	}
	defer func() { _ = resp.Body.Close() }()

	for key, values := range resp.Header {
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (h *FunctionHandler) Logs(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	tail := int64(100)
	if t := r.URL.Query().Get("tail"); t != "" {
		if v, err := strconv.ParseInt(t, 10, 64); err == nil && v > 0 {
			tail = v
		}
	}

	logs, err := h.svc.Logs(r.Context(), name, tail)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"logs": logs})
}

func (h *FunctionHandler) Delete(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	if err := h.svc.Delete(r.Context(), name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
