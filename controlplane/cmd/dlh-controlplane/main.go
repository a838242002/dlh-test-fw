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
	mio "github.com/dlh/dlh-test-fw/controlplane/internal/minio"
	"github.com/dlh/dlh-test-fw/controlplane/internal/runs"
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
	submitter := &runs.Submitter{Argo: clients.Argo, Namespace: cfg.K8sNamespace}
	syncer := &runs.Syncer{Source: wfLister, Manifests: manifests, Reports: reports}
	go syncer.Run(ctx)

	chaosClient := &chaos.LocalChaosClient{Dyn: clients.Dynamic, Namespace: cfg.K8sNamespace}

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
	watchdog := &chaos.Watchdog{Chaos: chaosClient, RunsTerminal: checker, Interval: 30 * time.Second}
	go watchdog.Run(ctx)

	// Phase D: targets registry.
	targetsReg := targets.NewRegistry()
	loader := &targets.Loader{Client: clients.Core, Namespace: cfg.K8sNamespace}
	refresher := &targets.Refresher{Loader: loader, Registry: targetsReg, Interval: 30 * time.Second}
	go refresher.Run(ctx)

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

	deps := &api.Deps{
		Templates:  tmplLister,
		Workflows:  wfLister,
		Reports:    reports,
		Submitter:  submitter,
		Manifests:  manifests,
		ArgoClient: clients.Argo,
		Chaos:      chaosClient,
		Targets:    targetsReg,
	}
	authMW := auth.Middleware(verifier, roles)
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
