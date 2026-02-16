package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"serverless-platform/internal/model"
	"serverless-platform/internal/service"
)

type FunctionHandler struct {
	svc *service.FunctionService
}

func NewFunctionHandler(svc *service.FunctionService) *FunctionHandler {
	return &FunctionHandler{svc: svc}
}

func (h *FunctionHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/", h.Create)
	r.Get("/", h.List)
	r.Get("/{name}", h.Get)
	r.Put("/{name}", h.Update)
	r.Delete("/{name}", h.Delete)
	return r
}

func (h *FunctionHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req model.CreateFunctionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	fn, err := h.svc.Create(r.Context(), &req)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, fn)
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
	functions, err := h.svc.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if functions == nil {
		functions = []*model.Function{}
	}

	writeJSON(w, http.StatusOK, functions)
}

func (h *FunctionHandler) Update(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	var req model.UpdateFunctionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	fn, err := h.svc.Update(r.Context(), name, &req)
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
