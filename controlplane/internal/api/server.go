package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/dlh/dlh-test-fw/controlplane/internal/api/gen"
	"github.com/dlh/dlh-test-fw/controlplane/internal/k8s"
	mio "github.com/dlh/dlh-test-fw/controlplane/internal/minio"
)

// Deps groups runtime dependencies injected into API handlers.
type Deps struct {
	Templates k8s.TemplateLister
	Workflows k8s.WorkflowLister
	Reports   *mio.ReportReader
}

// NewRouter mounts the generated strict server onto chi with optional auth middleware.
//
// Routes mounted outside the /api prefix (healthz, readyz) bypass the strict
// handler and the auth middleware entirely — they answer before auth is checked.
func NewRouter(deps *Deps, authMW func(http.Handler) http.Handler) http.Handler {
	r := chi.NewRouter()

	// Health probes — no auth, no /api prefix.
	r.Get("/healthz", healthHandler)
	r.Get("/readyz", healthHandler)

	apiGroup := chi.NewRouter()
	if authMW != nil {
		apiGroup.Use(authMW)
	}

	// Explicit SSE route — must be registered BEFORE the strict handler
	// so chi routes the SSE-shaped request here, not to the strict
	// handler's stub.
	sseH := &SSEHandler{Workflows: deps.Workflows}
	apiGroup.Get("/runs/{id}/events", sseH.Handle)

	h := &Handlers{deps: deps}
	// NewStrictHandler wraps our StrictServerInterface into the generated
	// ServerInterface. HandlerFromMux registers each route onto the chi router.
	strictSI := gen.NewStrictHandler(h, nil)
	gen.HandlerFromMux(strictSI, apiGroup)

	r.Mount("/api", apiGroup)
	return r
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}
