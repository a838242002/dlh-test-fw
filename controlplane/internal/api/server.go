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
	"github.com/dlh/dlh-test-fw/controlplane/internal/schedules"
	"github.com/dlh/dlh-test-fw/controlplane/internal/targets"
)

// Deps groups runtime dependencies injected into API handlers.
type Deps struct {
	Templates  k8s.TemplateLister
	Workflows  k8s.WorkflowLister
	Reports    *mio.ReportReader
	Submitter  *runs.Submitter      // Phase C
	Manifests  *runs.ManifestWriter // Phase C
	ArgoClient wfclient.Interface   // Phase C — for terminate patch
	Chaos         *chaos.Router        // Phase D — wired in Task 12
	Targets       *targets.Registry    // Phase D — wired in Task 9
	SessionIssuer *auth.SessionIssuer  // Phase E — wired in Task 7
	Exchanger     *auth.Exchanger      // Phase E — wired in Task 7
	AuthInfo      AuthInfoConfig       // Phase E — wired in Task 7
	Schedules     *schedules.Manager   // Phase F — wired in Task 6
}

// AuthInfoConfig holds the IdP configuration exposed via GET /api/auth/info.
type AuthInfoConfig struct {
	OIDCIssuer   string
	OIDCClientID string
	CIAudience   string
	AuthDisabled bool
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
				// /api/auth/info is public — the login command needs it to
				// discover OIDC config before the user has a token.
				if strings.HasPrefix(req.URL.Path, "/api/") && req.URL.Path != "/api/auth/info" {
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

	// Register all generated API routes (/api/scenarios, /api/runs, etc.)
	// plus the generated /healthz + /readyz stubs.  The manually registered
	// /healthz and /readyz above win because chi uses first-registered wins
	// for identical patterns.
	h := &Handlers{deps: deps}
	strictSI := gen.NewStrictHandler(h, nil)
	gen.HandlerFromMux(strictSI, r)

	// /internal/chaos — chi-direct mount, X-Internal-Token auth (not OIDC).
	// MUST be registered AFTER gen.HandlerFromMux: HandlerFromMux uses r.Group
	// which is merged into the routing tree. When we register AFTER the Group,
	// our handler wins (last-registered wins for identical patterns in chi v5).
	if deps.Chaos != nil {
		intH := &InternalChaosHandler{Chaos: deps.Chaos}
		internalMW := auth.InternalTokenMiddleware(internalToken)
		createH := internalMW(http.HandlerFunc(intH.Create))
		deleteH := internalMW(http.HandlerFunc(intH.Delete))
		r.Post("/internal/chaos", createH.ServeHTTP)
		r.Post("/internal/chaos/", createH.ServeHTTP)
		r.Delete("/internal/chaos/{ref}", deleteH.ServeHTTP)
	}

	// Embedded React SPA — catch-all after all API routes.
	r.Handle("/*", UIHandler())
	return r
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}
