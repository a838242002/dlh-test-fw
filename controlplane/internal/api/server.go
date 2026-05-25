package api

import (
	"context"
	"net/http"
	"strings"

	wfclient "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned"
	"github.com/go-chi/chi/v5"

	"github.com/dlh/dlh-test-fw/controlplane/internal/api/gen"
	"github.com/dlh/dlh-test-fw/controlplane/internal/auth"
	"github.com/dlh/dlh-test-fw/controlplane/internal/chaos"
	"github.com/dlh/dlh-test-fw/controlplane/internal/k8s"
	"github.com/dlh/dlh-test-fw/controlplane/internal/links"
	mio "github.com/dlh/dlh-test-fw/controlplane/internal/minio"
	"github.com/dlh/dlh-test-fw/controlplane/internal/queue"
	"github.com/dlh/dlh-test-fw/controlplane/internal/runs"
	"github.com/dlh/dlh-test-fw/controlplane/internal/schedules"
	"github.com/dlh/dlh-test-fw/controlplane/internal/targets"
)

// Deps groups runtime dependencies injected into API handlers.
type Deps struct {
	Templates  k8s.TemplateLister
	Workflows  k8s.WorkflowLister
	Reports    *mio.ReportReader
	Verdicts   *runs.VerdictCache   // populated from Reports; caches per-run score
	Submitter  *runs.Submitter      // Phase C
	Manifests  *runs.ManifestWriter // Phase C
	ArgoClient wfclient.Interface   // Phase C — for terminate patch
	Chaos         *chaos.Router        // Phase D — wired in Task 12
	Targets       *targets.Registry    // Phase D — wired in Task 9
	SessionIssuer *auth.SessionIssuer  // Phase E — wired in Task 7
	Exchanger     *auth.Exchanger      // Phase E — wired in Task 7
	AuthInfo      AuthInfoConfig       // Phase E — wired in Task 7
	Schedules     *schedules.Manager   // Phase F — wired in Task 6
	Locks         LocksReader          // Phase 2 — dlh-scenario-locks semaphore reader
	Priorities    PrioritiesStore      // Phase 3 — per-scenario default priority overrides
	Links links.Config // deep-link base URLs (Argo/Grafana)
}

// PrioritiesStore reads + writes per-scenario default priority overrides.
type PrioritiesStore interface {
	All(ctx context.Context) (map[string]int, error)
	Get(ctx context.Context, scenario string) (int, bool, error)
	Set(ctx context.Context, scenario string, priority int) error
}

// LocksReader returns the semaphore keys + slot counts (dlh-scenario-locks).
type LocksReader interface {
	Keys(ctx context.Context) ([]queue.LockKey, error)
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
	// The SSE route (/api/runs/{id}/events) is excluded here because
	// EventSource cannot set headers; its own auth guard below handles
	// both the auth-disabled bypass and the ?access_token= query param.
	if authMW != nil {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				// /api/auth/info is public — the login command needs it to
				// discover OIDC config before the user has a token.
				// /api/runs/{id}/events uses its own SSE auth guard (see below).
				isSSEPath := strings.HasPrefix(req.URL.Path, "/api/runs/") &&
					strings.HasSuffix(req.URL.Path, "/events")
				if strings.HasPrefix(req.URL.Path, "/api/") &&
					req.URL.Path != "/api/auth/info" &&
					!isSSEPath {
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

	// Register all generated API routes (/api/scenarios, /api/runs, etc.)
	// plus the generated /healthz + /readyz stubs.
	h := &Handlers{deps: deps}
	strictSI := gen.NewStrictHandler(h, nil)
	gen.HandlerFromMux(strictSI, r)

	// Role-gated routes registered AFTER HandlerFromMux so they override the
	// ungated generated routes (chi last-registration-wins). The global authMW
	// (r.Use above) has already populated the role in context; RequireRole just
	// checks it.
	{
		wrapper := gen.ServerInterfaceWrapper{Handler: strictSI}
		// Admin-only: edit per-scenario default priorities.
		r.With(auth.RequireRole(auth.RoleAdmin)).
			Put("/api/scenario-priorities/{id}", wrapper.PutScenarioPriority)
		// Runner-gated: live re-prioritize of a pending run. Registered after
		// HandlerFromMux so it overrides the ungated generated route. Global
		// authMW has already populated the role; RequireRole just checks it.
		r.With(auth.RequireRole(auth.RoleRunner)).
			Post("/api/runs/{id}/priority", wrapper.ReprioritizeRun)
	}

	// Explicit SSE route — registered AFTER HandlerFromMux so it wins.
	//
	// chi v5 uses last-registration-wins for identical route patterns
	// (tree.go:setEndpoint unconditionally overwrites the handler field).
	// HandlerFromMux registers the generated stub for /api/runs/{id}/events
	// via r.Group; registering the real handler here overwrites that stub,
	// consistent with the /internal/chaos routes below which use the same
	// post-HandlerFromMux pattern.
	//
	// Auth note: EventSource cannot send an Authorization header, so the
	// client passes its token via ?access_token=.  The global authMW is
	// excluded for this path (see r.Use above); instead a thin guard here:
	//   - serves directly when auth is disabled (no token check);
	//   - otherwise promotes the query token to the Authorization header and
	//     delegates to authMW, reusing the same session/OIDC verification path.
	sseH := &SSEHandler{Workflows: deps.Workflows}
	sseCore := http.HandlerFunc(sseH.Handle)
	var sseAuthHandler http.Handler
	if deps.AuthInfo.AuthDisabled || authMW == nil {
		sseAuthHandler = sseCore
	} else {
		sseAuthHandler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			// If there is no Authorization header, inject the ?access_token
			// value so authMW can verify it via the standard Bearer path.
			if req.Header.Get("Authorization") == "" {
				if tok := bearerOrQueryToken(req); tok != "" {
					req = req.Clone(req.Context())
					req.Header.Set("Authorization", "Bearer "+tok)
				}
			}
			authMW(sseCore).ServeHTTP(w, req)
		})
	}
	r.Get("/api/runs/{id}/events", sseAuthHandler.ServeHTTP)

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
