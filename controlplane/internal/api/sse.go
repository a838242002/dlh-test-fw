package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/dlh/dlh-test-fw/controlplane/internal/k8s"
)

// SSEHandler streams events for a single run. It subscribes to the
// shared workflow informer and writes ADDED/MODIFIED/DELETED events that
// match the requested run id.
type SSEHandler struct {
	Workflows k8s.WorkflowLister
}

func (s *SSEHandler) Handle(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "id")
	if runID == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	events, unsubscribe := s.Workflows.Subscribe()
	defer unsubscribe()

	// Send the initial snapshot if the workflow already exists.
	if wf, err := s.Workflows.Get(runID); err == nil {
		writeSSE(w, flusher, "snapshot", map[string]any{
			"phase": string(wf.Status.Phase),
			"name":  wf.Name,
		})
	}

	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-keepalive.C:
			_, _ = fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		case ev, ok := <-events:
			if !ok {
				return
			}
			if ev.Workflow == nil || ev.Workflow.Name != runID {
				continue
			}
			writeSSE(w, flusher, ev.Type, map[string]any{
				"phase":   string(ev.Workflow.Status.Phase),
				"name":    ev.Workflow.Name,
				"updated": time.Now().UTC().Format(time.RFC3339),
			})
		}
	}
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, event string, data any) {
	payload, err := json.Marshal(data)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, payload)
	flusher.Flush()
}
