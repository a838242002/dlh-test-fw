package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/dlh/dlh-test-fw/controlplane/internal/chaos"
)

// InternalChaosHandler serves the /internal/chaos POST + DELETE routes.
// Mounted directly on the root chi router after InternalTokenMiddleware.
type InternalChaosHandler struct {
	Chaos chaos.Client
}

func (h *InternalChaosHandler) Create(w http.ResponseWriter, r *http.Request) {
	runID := r.URL.Query().Get("runID")
	if runID == "" {
		http.Error(w, "runID query param required", http.StatusBadRequest)
		return
	}
	var res chaos.Resource
	if err := json.NewDecoder(r.Body).Decode(&res); err != nil {
		http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
		return
	}
	ref, err := h.Chaos.Create(r.Context(), runID, res)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"ref":       ref.Encode(),
		"kind":      ref.Resource,
		"name":      ref.Name,
		"namespace": ref.Namespace,
	})
}

func (h *InternalChaosHandler) Delete(w http.ResponseWriter, r *http.Request) {
	refStr := chi.URLParam(r, "ref")
	if refStr == "" {
		http.Error(w, "ref required", http.StatusBadRequest)
		return
	}
	ref, err := chaos.DecodeRef(refStr)
	if err != nil {
		http.Error(w, "invalid ref: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.Chaos.Delete(r.Context(), ref); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
