package api

import (
	"net/http"
	"strings"

	wfclient "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned"
	"github.com/go-chi/chi/v5"

	"github.com/dlh/dlh-test-fw/controlplane/internal/api/gen"
	"github.com/dlh/dlh-test-fw/controlplane/internal/auth"
	"github.com/dlh/dlh-test-fw/controlplane/internal/chaos"
	"github.com/dlh/dlh-test-fw/controlplane/internal/k8s"
	mio "github.com/dlh/dlh-test-fw/controlplane/internal/minio"
	"github.com/dlh/dlh-test-fw/controlplane/internal/runs"
)

// Deps groups runtime dependencies injected into API handlers.
type Deps struct {
	Templates  k8s.TemplateLister
	Workflows  k8s.WorkflowLister
	Reports    *mio.ReportReader
	Submitter  *runs.Submitter      // Phase C
	Manifests  *runs.ManifestWriter // Phase C
	ArgoClient wfclient.Interface   // Phase C — for terminate patch
	Chaos      chaos.Client         // Phase C — wired in Task 12
}

// NewRouter mounts the generated strict server onto chi with optional auth middleware.
//
// The OpenAPI spec uses full absolute paths (/api/scenarios, /api/runs, /healthz, …).
// HandlerFromMux registers those paths verbatim on the root chi.Router — no
// r.Mount("/api", …) prefix layer is used.
//
// Auth middleware (when enabled) is applied as a global r.Use that must be
// registered before any r.Get/r.Handle calls (chi rule).  The middleware
// itself skips non-/api/ paths so health probes and the UI bypass auth.
func NewRouter(deps *Deps, authMW func(http.Handler) http.Handler, internalToken string) http.Handler {
	r := chi.NewRouter()

	// ALL r.Use calls must come before any r.Get/r.Handle (chi requirement).
	// Path-aware auth: only /api/* requests are forwarded through authMW.
	if authMW != nil {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if strings.HasPrefix(req.URL.Path, "/api/") {
					authMW(next).ServeHTTP(w, req)
					return
				}
				next.ServeHTTP(w, req)
			})
		})
	}

	// Health probes — no auth.  Registered after r.Use (which is fine —
	// chi only requires Use before the first route registration that
	// triggers the middleware chain build, which is the first r.Get here).
	// Actually chi panics if Use is called after ANY route, so we keep
	// Use above and routes below.
	r.Get("/healthz", healthHandler)
	r.Get("/readyz", healthHandler)

	// Explicit SSE route — registered BEFORE HandlerFromMux so chi matches
	// this handler rather than the generated stub.
	sseH := &SSEHandler{Workflows: deps.Workflows}
	r.Get("/api/runs/{id}/events", sseH.Handle)

	// /internal/chaos — chi-direct mount, X-Internal-Token auth (not OIDC).
	// Must be registered BEFORE gen.HandlerFromMux; chi first-registered wins,
	// and the generated stub for DELETE /internal/chaos/{ref} always returns 401.
	if deps.Chaos != nil {
		intH := &InternalChaosHandler{Chaos: deps.Chaos}
		// Mount chaos sub-router with InternalToken middleware.
		// Register POST at both /internal/chaos and /internal/chaos/ so the WT
		// can call either form (chi's Route sub-router only matches the slash form).
		internalMW := auth.InternalTokenMiddleware(internalToken)
		r.With(internalMW).Post("/internal/chaos", intH.Create)
		r.With(internalMW).Post("/internal/chaos/", intH.Create)
		r.With(internalMW).Delete("/internal/chaos/{ref}", intH.Delete)
	}

	// Register all generated API routes (/api/scenarios, /api/runs, etc.)
	// plus the generated /healthz + /readyz stubs.  The manually registered
	// /healthz, /readyz, SSE, and /internal/chaos routes above win because
	// chi uses first-registered wins for identical patterns.
	h := &Handlers{deps: deps}
	strictSI := gen.NewStrictHandler(h, nil)
	gen.HandlerFromMux(strictSI, r)

	// Embedded React SPA — catch-all after all API routes.
	r.Handle("/*", UIHandler())
	return r
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}
