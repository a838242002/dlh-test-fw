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
	"github.com/dlh/dlh-test-fw/controlplane/internal/config"
	"github.com/dlh/dlh-test-fw/controlplane/internal/k8s"
	mio "github.com/dlh/dlh-test-fw/controlplane/internal/minio"
	"github.com/dlh/dlh-test-fw/controlplane/internal/runs"
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
		// ChaosCancel wired in Task 11
	}
	authMW := auth.Middleware(verifier, roles)
	handler := api.NewRouter(deps, authMW)

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
