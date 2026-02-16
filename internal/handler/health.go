package handler

import (
	"encoding/json"
	"net/http"

	"k8s.io/client-go/kubernetes"
)

type HealthHandler struct {
	kubeClient kubernetes.Interface
}

func NewHealthHandler(kubeClient kubernetes.Interface) *HealthHandler {
	return &HealthHandler{kubeClient: kubeClient}
}

func (h *HealthHandler) Healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (h *HealthHandler) Readyz(w http.ResponseWriter, _ *http.Request) {
	_, err := h.kubeClient.Discovery().ServerVersion()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "unavailable", "error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
