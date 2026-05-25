package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dlh/dlh-test-fw/controlplane/internal/api"
	"github.com/dlh/dlh-test-fw/controlplane/internal/auth"
	"github.com/dlh/dlh-test-fw/controlplane/internal/chaos"
	"github.com/dlh/dlh-test-fw/controlplane/internal/config"
	"github.com/dlh/dlh-test-fw/controlplane/internal/k8s"
	"github.com/dlh/dlh-test-fw/controlplane/internal/links"
	mio "github.com/dlh/dlh-test-fw/controlplane/internal/minio"
	"github.com/dlh/dlh-test-fw/controlplane/internal/priorities"
	"github.com/dlh/dlh-test-fw/controlplane/internal/runs"
	"github.com/dlh/dlh-test-fw/controlplane/internal/schedules"
	"github.com/dlh/dlh-test-fw/controlplane/internal/targets"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("config load failed", "err", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	clients, err := k8s.NewClients(os.Getenv("KUBECONFIG"))
	if err != nil {
		logger.Error("k8s clients", "err", err)
		os.Exit(1)
	}

	stopCh := make(chan struct{})
	go func() {
		<-ctx.Done()
		close(stopCh)
	}()

	wfLister, err := k8s.NewWorkflowLister(clients, cfg.K8sNamespace, stopCh)
	if err != nil {
		logger.Error("workflow informer", "err", err)
		os.Exit(1)
	}
	tmplLister := k8s.NewTemplateLister(clients, cfg.K8sNamespace)

	mc, err := mio.New(cfg.MinIOEndpoint, cfg.MinIOAccessKey, cfg.MinIOSecretKey, cfg.MinIOSecure)
	if err != nil {
		logger.Error("minio client", "err", err)
		os.Exit(1)
	}
	reports := mio.NewReportReader(mc, cfg.MinIOBucket)

	// Phase C: submission + manifest writes.
	manifests := &runs.ManifestWriter{Client: mc, Bucket: cfg.MinIOBucket}
	submitter := &runs.Submitter{
		Argo:      clients.Argo,
		Namespace: cfg.K8sNamespace,
		Defaults:  &priorities.Store{Client: clients.Core, Namespace: cfg.K8sNamespace, Name: cfg.PrioritiesConfigMapName},
	}
	syncer := &runs.Syncer{Source: wfLister, Manifests: manifests, Reports: reports}
	go syncer.Run(ctx)

	// Phase D: targets registry (must be built before chaosRouter).
	targetsReg := targets.NewRegistry()
	loader := &targets.Loader{Client: clients.Core, Namespace: cfg.K8sNamespace}
	refresher := &targets.Refresher{Loader: loader, Registry: targetsReg, Interval: 30 * time.Second}
	go refresher.Run(ctx)

	localChaos := &chaos.LocalChaosClient{Dyn: clients.Dynamic, Namespace: cfg.K8sNamespace}
	chaosRouter := &chaos.Router{Local: localChaos, Registry: targetsReg}

	// Watchdog: reap orphaned chaos resources every 30s.
	checker := chaos.RunsTerminalCheckerFunc(func(runID string) bool {
		wf, err := wfLister.Get(runID)
		if err != nil || wf == nil {
			// Workflow CR is gone (TTL'd) — treat as terminal; chaos shouldn't linger.
			return true
		}
		switch string(wf.Status.Phase) {
		case "Succeeded", "Failed", "Error":
			return true
		}
		return false
	})
	watchdog := &chaos.Watchdog{Chaos: chaosRouter, RunsTerminal: checker, Interval: 30 * time.Second}
	go watchdog.Run(ctx)

	var verifier auth.VerifierIface
	if cfg.AuthDisabled {
		logger.Warn("DLH_AUTH_DISABLED=true — accepting fake tokens; NEVER set this in prod")
		verifier = auth.FakeVerifier{}
	} else {
		v, err := auth.NewVerifier(ctx, cfg.OIDCIssuerURL, cfg.OIDCClientID, cfg.OIDCRequiredAudience, cfg.OIDCGroupsClaim)
		if err != nil {
			logger.Error("oidc verifier", "err", err)
			os.Exit(1)
		}
		verifier = v
	}
	roles, err := auth.NewRoles(ctx, clients.Core, cfg.RolesConfigMapNS, cfg.RolesConfigMapName)
	if err != nil {
		logger.Error("roles configmap", "err", err)
		os.Exit(1)
	}

	sessionIssuer := &auth.SessionIssuer{
		Key:      []byte(cfg.SessionSigningKey),
		Lifetime: time.Hour,
	}
	exchanger := &auth.Exchanger{
		TrustedIssuers:   cfg.CITrustedIssuers,
		RequiredAudience: cfg.CIAudience,
	}
	scheduleMgr := &schedules.Manager{Argo: clients.Argo, Namespace: cfg.K8sNamespace}

	deps := &api.Deps{
		Templates:  tmplLister,
		Workflows:  wfLister,
		Reports:    reports,
		Verdicts:   runs.NewVerdictCache(reports),
		Submitter:  submitter,
		Manifests:  manifests,
		ArgoClient: clients.Argo,
		Chaos:      chaosRouter,
		Targets:    targetsReg,
		SessionIssuer: sessionIssuer,
		Exchanger:     exchanger,
		Schedules:     scheduleMgr,
		Locks:         &api.ConfigMapLocks{Client: clients.Core, Namespace: cfg.K8sNamespace, Name: cfg.LocksConfigMapName},
		AuthInfo: api.AuthInfoConfig{
			OIDCIssuer:   cfg.OIDCIssuerURL,
			OIDCClientID: cfg.OIDCClientID,
			CIAudience:   cfg.CIAudience,
			AuthDisabled: cfg.AuthDisabled,
		},
		Links: links.Config{
			ArgoBaseURL:    cfg.ArgoBaseURL,
			GrafanaBaseURL: cfg.GrafanaBaseURL,
			Namespace:      cfg.K8sNamespace,
		},
	}
	authMW := auth.Middleware(verifier, roles, sessionIssuer)
	handler := api.NewRouter(deps, authMW, cfg.InternalToken)

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("listening", "addr", cfg.ListenAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("listen", "err", err)
			cancel()
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownGrace)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
}
