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
	Chaos *chaos.Router
}

func (h *InternalChaosHandler) Create(w http.ResponseWriter, r *http.Request) {
	runID := r.URL.Query().Get("runID")
	if runID == "" {
		http.Error(w, "runID query param required", http.StatusBadRequest)
		return
	}
	targetID := r.URL.Query().Get("targetID") // optional; "" = local
	var res chaos.Resource
	if err := json.NewDecoder(r.Body).Decode(&res); err != nil {
		http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
		return
	}
	ref, err := h.Chaos.CreateForTarget(r.Context(), runID, targetID, res)
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
		"targetID":  targetID,
	})
}

func (h *InternalChaosHandler) Delete(w http.ResponseWriter, r *http.Request) {
	refStr := chi.URLParam(r, "ref")
	if refStr == "" {
		http.Error(w, "ref required", http.StatusBadRequest)
		return
	}
	targetID := r.URL.Query().Get("targetID")
	ref, err := chaos.DecodeRef(refStr)
	if err != nil {
		http.Error(w, "invalid ref: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.Chaos.DeleteForTarget(r.Context(), targetID, ref); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
