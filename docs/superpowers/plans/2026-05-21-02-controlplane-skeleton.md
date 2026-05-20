# dlh-controlplane Phase B (Read-Only Skeleton) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a read-only `dlh-controlplane` service that exposes OIDC-authenticated REST endpoints (`/api/scenarios`, `/api/runs`, `/api/runs/{id}`, SSE events) plus a minimal embedded React UI, deployed via the `dlh-controlplane` Argo CD Application that Plan 14 reserved. Scenario submission is **out of scope** for this phase — it stays on `run-scenario.sh`.

**Architecture:** A single Go binary serves both the JSON API and the embedded React UI (`go:embed dist/*`). OpenAPI 3.1 is the single source of truth — `oapi-codegen` generates the Go server stub; `openapi-typescript` generates the TS client. The backend speaks to the framework cluster's k8s API via an informer-based client (Workflows + WorkflowTemplates) and to MinIO via the official `minio-go` client (read `report.json` if present). OIDC token verification is delegated to `github.com/coreos/go-oidc/v3`; role binding is a small ConfigMap loaded at startup. Deployment is plain k8s manifests in `controlplane/deploy/` (matching Plan 14's `directory:` source on the Argo CD Application).

**Tech Stack:** Go 1.26 (matching `verdict-job/`), chi router, go-oidc, client-go, argo workflow client, minio-go, oapi-codegen; Vite + React + TypeScript + Tailwind + react-router for the UI; multi-stage Dockerfile.

**Reference spec:** `docs/superpowers/specs/2026-05-21-dlh-controlplane-design.md` (especially §5 domain model, §7 API surface, §9 auth/RBAC, §12 Phase B). Re-read §3 non-goals before starting — submission, scenario editing, comparison views, etc. are explicitly out for this phase.

**Branch & worktree:** Per `CLAUDE.md`, work on `feat/plan15-controlplane-skeleton` in worktree `../dlh-test-fw-plan15`. Task 1 from the main worktree creates it.

**Plan-time decisions / deviations from spec:**

1. **`controlplane/deploy/` is plain k8s YAML, not a Helm chart fragment.** Plan 14's Argo CD Application points at it as a `directory:` source. The spec's "Helm chart fragment" wording was loose; we keep the manifests plain so Plan 14's Application requires no source-shape change.
2. **The UI ships as `go:embed`-ed assets in the same binary.** No separate `dlh-ui` image. Spec §14 open question #3 resolved in favor of embedded.
3. **Postgres-free per spec §6.** Live state comes from k8s informers; historical state from MinIO `report.json` reads. No database. For Phase B's read-only viewer the manifest writes from spec §6 are NOT needed — they appear in Phase C.
4. **IdP for tests is a fake issuer.** Real IdP (Dex/Google/Okta) wiring is per-environment configuration handled at bootstrap time, not in this plan. We ship the OIDC verifier + a fake issuer for tests.
5. **Frontend is minimal-viable.** Three pages render API data into legible tables/cards with no design polish. A later plan can iterate on UX once the data path is proven.
6. **Natural pause points** if Phase B feels too large: after Task 10 (backend read-only API complete, no UI yet), after Task 16 (UI complete, not yet deployed), after Task 19 (deployed but Application still manual sync). Each is a working state that could ship behind a feature flag or be merged independently.

---

## File Structure

**New top-level directories:**

```
controlplane/
├── go.mod                                    # new Go module
├── go.sum
├── Makefile                                  # mirror verdict-job/Makefile shape
├── Dockerfile                                # multi-stage: ui-build + go-build
├── README.md                                 # orientation
├── api/
│   └── openapi.yaml                          # OpenAPI 3.1 spec (single source of truth)
├── cmd/dlh-controlplane/
│   └── main.go                               # entry point
├── internal/
│   ├── api/
│   │   ├── server.go                         # chi router + handler attachment
│   │   ├── handlers.go                       # scenarios/runs/run-detail
│   │   ├── sse.go                            # SSE event streaming
│   │   └── gen/                              # oapi-codegen output (committed)
│   │       ├── server.gen.go
│   │       └── types.gen.go
│   ├── auth/
│   │   ├── oidc.go                           # token verifier
│   │   ├── fake.go                           # fake issuer for tests
│   │   ├── rbac.go                           # role ConfigMap loader + check
│   │   ├── middleware.go                     # http auth middleware
│   │   ├── oidc_test.go
│   │   └── rbac_test.go
│   ├── k8s/
│   │   ├── client.go                         # in-cluster config + clients
│   │   ├── workflows.go                      # informer + list/get
│   │   ├── templates.go                      # WorkflowTemplate list/get
│   │   └── workflows_test.go                 # envtest harness
│   ├── minio/
│   │   ├── client.go                         # s3 client init
│   │   ├── reports.go                        # read report.json
│   │   └── reports_test.go                   # testcontainers minio
│   ├── model/
│   │   └── types.go                          # joins gen types + k8s + minio
│   └── config/
│       └── config.go                         # env vars / flags
└── deploy/                                   # k8s manifests, Argo CD source
    ├── deployment.yaml
    ├── service.yaml
    ├── ingress.yaml
    ├── serviceaccount.yaml
    ├── role.yaml
    ├── rolebinding.yaml
    └── roles-configmap.yaml

controlplane/web/                             # vite app (sibling of go src to keep go tooling clean)
├── package.json
├── pnpm-lock.yaml                            # pnpm chosen for fast installs
├── vite.config.ts
├── tsconfig.json
├── tailwind.config.js
├── postcss.config.js
├── index.html
└── src/
    ├── main.tsx
    ├── App.tsx
    ├── api/
    │   ├── client.ts                         # openapi-fetch wrapper
    │   └── gen.ts                            # generated types (openapi-typescript)
    ├── pages/
    │   ├── ScenariosPage.tsx
    │   ├── RunsPage.tsx
    │   └── RunDetailPage.tsx
    └── components/
        ├── Layout.tsx
        └── StatusBadge.tsx
```

**Existing files modified:**

- `argocd/apps/dlh-controlplane.yaml` — enable auto-sync + finalizer (Plan 14 left it manual).
- `argocd/values/framework/chart-values.yaml` — add controlplane-related config knobs if needed (e.g., OIDC issuer URL, MinIO endpoint).
- `.github/workflows/ci.yml` — new `controlplane` job (go vet / go test / ui build / openapi lint).
- `CLAUDE.md` — append a controlplane section.
- `docs/FINDINGS.md` — append Plan 15 findings.
- `README.md` — Plan 15 row.

**Unchanged:** umbrella chart, WorkflowTemplates, scenarios, dashboards, verdict-job, k6 image, run-scenario.sh.

---

## Task 1: Baseline + worktree creation

No commits.

- [ ] **Step 1: Confirm clean main**

```bash
cd /Users/allen/repo/dlh-test-fw
git status
git log --first-parent --oneline -5
```

Expected: clean tree; HEAD includes `144d14e` (FINDINGS update) or newer; the Plan 14 merge `130a0c1` visible.

- [ ] **Step 2: Confirm CI green on main**

```bash
gh run list --branch main --limit 1
```

Expected: status `success`.

- [ ] **Step 3: Create feature worktree**

```bash
git worktree add ../dlh-test-fw-plan15 -b feat/plan15-controlplane-skeleton main
cd ../dlh-test-fw-plan15
git status
```

Expected: clean tree on `feat/plan15-controlplane-skeleton`.

- [ ] **Step 4: Confirm Plan 14 outputs are present**

```bash
ls argocd/apps/dlh-controlplane.yaml controlplane/deploy/.gitkeep
```

Expected: both files exist (Plan 14's placeholder).

All remaining tasks run from `/Users/allen/repo/dlh-test-fw-plan15`.

---

## Task 2: Go module + project scaffolding

**Files:**
- Create: `controlplane/go.mod`, `controlplane/Makefile`, `controlplane/README.md`, `controlplane/cmd/dlh-controlplane/main.go`, `controlplane/.gitignore`

- [ ] **Step 1: Initialize the Go module**

```bash
cd /Users/allen/repo/dlh-test-fw-plan15/controlplane
go mod init github.com/dlh/dlh-test-fw/controlplane
```

- [ ] **Step 2: Pin Go version + initial dependencies**

Edit `controlplane/go.mod` to read exactly (matches verdict-job's Go version):

```
module github.com/dlh/dlh-test-fw/controlplane

go 1.26.3
```

Then add dependencies (these will be downloaded by later tasks; pinning here is just for go.mod presence):

```bash
go get github.com/go-chi/chi/v5@v5.1.0
go get github.com/coreos/go-oidc/v3@v3.11.0
go get golang.org/x/oauth2@v0.24.0
go get k8s.io/client-go@v0.30.0
go get k8s.io/api@v0.30.0
go get k8s.io/apimachinery@v0.30.0
go get github.com/argoproj/argo-workflows/v3@v3.6.10
go get github.com/minio/minio-go/v7@v7.0.77
go mod tidy
```

- [ ] **Step 3: Write controlplane/Makefile**

```makefile
SHELL := /usr/bin/env bash
.ONESHELL:
.SHELLFLAGS := -euo pipefail -c

IMG ?= ghcr.io/dlh/dlh-controlplane:0.1.0

.PHONY: test lint build ui-build ui-install image load-image reload-minikube codegen clean

test:
	go test ./...

lint:
	go vet ./...

build: ui-build
	CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/dlh-controlplane ./cmd/dlh-controlplane

ui-install:
	cd web && pnpm install --frozen-lockfile

ui-build: ui-install
	cd web && pnpm build

codegen:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
	    -config api/oapi-codegen-server.yaml \
	    api/openapi.yaml
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
	    -config api/oapi-codegen-types.yaml \
	    api/openapi.yaml
	cd web && pnpm openapi-typescript ../api/openapi.yaml -o src/api/gen.ts

image:
	docker build -t $(IMG) .

load-image: image
	minikube image load $(IMG)

reload-minikube: image
	minikube ssh -- "docker ps -aq --filter ancestor=$(IMG) | xargs -r docker rm -f"
	minikube ssh -- docker rmi -f $(IMG) || true
	minikube image load $(IMG)

clean:
	rm -rf bin web/dist
```

- [ ] **Step 4: Write controlplane/.gitignore**

```
bin/
web/node_modules/
web/dist/
*.tmp
```

- [ ] **Step 5: Write controlplane/README.md**

```markdown
# dlh-controlplane

Read-only viewer + (later) submission API for dlh-test-fw scenarios.
Phase B ships only the read path. See
`docs/superpowers/specs/2026-05-21-dlh-controlplane-design.md`.

## Build

```
make codegen    # regenerate from api/openapi.yaml
make ui-build   # build the React app into web/dist
make build      # build the Go binary (embeds web/dist)
make image      # docker build
make reload-minikube   # force kubelet to pick up the new image
```

## Layout

- `cmd/dlh-controlplane/main.go` — entry point
- `api/openapi.yaml` — single source of truth for the API
- `internal/` — backend packages
- `web/` — React UI (Vite + Tailwind)
- `deploy/` — k8s manifests applied by Argo CD
```

- [ ] **Step 6: Write a placeholder cmd/dlh-controlplane/main.go**

```go
package main

import (
	"fmt"
	"net/http"
)

func main() {
	http.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})
	if err := http.ListenAndServe(":8080", nil); err != nil {
		panic(err)
	}
}
```

- [ ] **Step 7: Verify it builds and tests pass**

```bash
cd /Users/allen/repo/dlh-test-fw-plan15/controlplane
go build ./...
go test ./...
```

Expected: build succeeds; `go test` exits 0 with `? [no test files]`.

- [ ] **Step 8: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw-plan15
git add controlplane/go.mod controlplane/go.sum controlplane/Makefile \
        controlplane/.gitignore controlplane/README.md \
        controlplane/cmd/dlh-controlplane/main.go
git commit -m "feat(controlplane): scaffold Go module + Makefile + placeholder main"
```

---

## Task 3: OpenAPI specification

The OpenAPI document is the single source of truth. Backend handlers and frontend client both generate from it.

**Files:**
- Create: `controlplane/api/openapi.yaml`
- Create: `controlplane/api/oapi-codegen-server.yaml`, `controlplane/api/oapi-codegen-types.yaml`

- [ ] **Step 1: Write controlplane/api/openapi.yaml**

```yaml
openapi: 3.1.0
info:
  title: dlh-controlplane
  version: 0.1.0
  description: |
    Read-only Phase B API for dlh-test-fw. Submission endpoints (POST
    /api/runs, DELETE /api/runs/{id}) are reserved for Phase C.
servers:
  - url: /
paths:
  /healthz:
    get:
      operationId: getHealthz
      security: []
      responses:
        "200":
          description: OK
  /readyz:
    get:
      operationId: getReadyz
      security: []
      responses:
        "200":
          description: OK
        "503":
          description: Not ready
  /api/scenarios:
    get:
      operationId: listScenarios
      responses:
        "200":
          description: scenario catalog
          content:
            application/json:
              schema:
                type: object
                required: [items]
                properties:
                  items:
                    type: array
                    items:
                      $ref: "#/components/schemas/Scenario"
  /api/scenarios/{id}:
    get:
      operationId: getScenario
      parameters:
        - in: path
          name: id
          required: true
          schema: { type: string }
      responses:
        "200":
          description: scenario detail
          content:
            application/json:
              schema: { $ref: "#/components/schemas/Scenario" }
        "404":
          description: not found
  /api/runs:
    get:
      operationId: listRuns
      parameters:
        - in: query
          name: scenario
          schema: { type: string }
        - in: query
          name: status
          schema: { type: string }
        - in: query
          name: since
          schema: { type: string, format: date-time }
        - in: query
          name: limit
          schema: { type: integer, minimum: 1, maximum: 500, default: 100 }
      responses:
        "200":
          description: run history
          content:
            application/json:
              schema:
                type: object
                required: [items]
                properties:
                  items:
                    type: array
                    items:
                      $ref: "#/components/schemas/Run"
  /api/runs/{id}:
    get:
      operationId: getRun
      parameters:
        - in: path
          name: id
          required: true
          schema: { type: string }
      responses:
        "200":
          description: run detail
          content:
            application/json:
              schema: { $ref: "#/components/schemas/RunDetail" }
        "404":
          description: not found
  /api/runs/{id}/events:
    get:
      operationId: streamRunEvents
      parameters:
        - in: path
          name: id
          required: true
          schema: { type: string }
      responses:
        "200":
          description: server-sent events stream
          content:
            text/event-stream:
              schema:
                type: string
components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
      bearerFormat: JWT
  schemas:
    Scenario:
      type: object
      required: [id, displayName]
      properties:
        id:           { type: string }
        displayName:  { type: string }
        description:  { type: string }
        targetType:   { type: string, description: "e.g. mysql, kafka, doris" }
        parameters:
          type: array
          items:
            type: object
            required: [name]
            properties:
              name:        { type: string }
              default:     { type: string }
              description: { type: string }
    Run:
      type: object
      required: [id, scenario, status, startedAt]
      properties:
        id:         { type: string }
        scenario:   { type: string }
        status:     { type: string, enum: [Pending, Running, Succeeded, Failed, Error, Unknown] }
        startedAt:  { type: string, format: date-time }
        finishedAt: { type: string, format: date-time }
        score:      { type: number, format: double, nullable: true }
        workflowName: { type: string }
    RunDetail:
      allOf:
        - $ref: "#/components/schemas/Run"
        - type: object
          properties:
            parameters:
              type: object
              additionalProperties: { type: string }
            steps:
              type: array
              items:
                type: object
                required: [name, phase]
                properties:
                  name:      { type: string }
                  phase:     { type: string }
                  startedAt: { type: string, format: date-time }
                  finishedAt:{ type: string, format: date-time }
                  message:   { type: string }
            verdict:
              type: object
              nullable: true
              description: "Decoded from MinIO report.json. Absent if no report yet."
              additionalProperties: true
            grafanaUrls:
              type: array
              items:
                type: object
                required: [label, url]
                properties:
                  label: { type: string }
                  url:   { type: string }
security:
  - bearerAuth: []
```

- [ ] **Step 2: Write controlplane/api/oapi-codegen-server.yaml**

```yaml
package: gen
output: internal/api/gen/server.gen.go
generate:
  chi-server: true
  strict-server: true
  embedded-spec: true
output-options:
  skip-prune: true
```

- [ ] **Step 3: Write controlplane/api/oapi-codegen-types.yaml**

```yaml
package: gen
output: internal/api/gen/types.gen.go
generate:
  models: true
```

- [ ] **Step 4: Add oapi-codegen tool dependency + run codegen**

```bash
cd /Users/allen/repo/dlh-test-fw-plan15/controlplane
go get -tool github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.4.1
mkdir -p internal/api/gen
make codegen
```

(If `go get -tool` is unavailable in this Go version, use `go install` to a local bin and document the path; the Makefile already uses `go run` which auto-fetches.)

Expected: `internal/api/gen/server.gen.go` and `internal/api/gen/types.gen.go` created without errors.

- [ ] **Step 5: Verify codegen output builds**

```bash
go build ./...
```

Expected: clean build.

- [ ] **Step 6: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw-plan15
git add controlplane/api/ controlplane/internal/api/gen/ controlplane/go.mod controlplane/go.sum
git commit -m "feat(controlplane): OpenAPI 3.1 spec + oapi-codegen output

Read-only Phase B endpoints. Submission endpoints reserved for Phase C.
Codegen output committed (no generated-at-build dance for now)."
```

---

## Task 4: Config + main entry with health checks

**Files:**
- Create: `controlplane/internal/config/config.go`
- Modify: `controlplane/cmd/dlh-controlplane/main.go`

- [ ] **Step 1: Write controlplane/internal/config/config.go**

```go
package config

import (
	"fmt"
	"os"
	"time"
)

// Config holds runtime configuration. All knobs come from environment
// variables — the binary takes no flags so it slots cleanly into a k8s
// Deployment's env block.
type Config struct {
	ListenAddr        string
	OIDCIssuerURL     string
	OIDCClientID      string
	OIDCRequiredAudience string
	OIDCGroupsClaim   string
	RolesConfigMapNS  string
	RolesConfigMapName string
	K8sNamespace      string
	MinIOEndpoint     string
	MinIOBucket       string
	MinIOAccessKey    string
	MinIOSecretKey    string
	MinIOSecure       bool
	ShutdownGrace     time.Duration
	// AuthDisabled bypasses OIDC. ONLY for local dev — never set in prod.
	AuthDisabled bool
}

// Load reads env vars and returns a populated Config or an error if any
// required field is missing.
func Load() (*Config, error) {
	c := &Config{
		ListenAddr:           getenv("DLH_LISTEN_ADDR", ":8080"),
		OIDCIssuerURL:        os.Getenv("DLH_OIDC_ISSUER_URL"),
		OIDCClientID:         os.Getenv("DLH_OIDC_CLIENT_ID"),
		OIDCRequiredAudience: os.Getenv("DLH_OIDC_AUDIENCE"),
		OIDCGroupsClaim:      getenv("DLH_OIDC_GROUPS_CLAIM", "groups"),
		RolesConfigMapNS:     getenv("DLH_ROLES_NAMESPACE", "dlh-test-fw"),
		RolesConfigMapName:   getenv("DLH_ROLES_CONFIGMAP", "dlh-roles"),
		K8sNamespace:         getenv("DLH_K8S_NAMESPACE", "dlh-test-fw"),
		MinIOEndpoint:        getenv("DLH_MINIO_ENDPOINT", "dlh-minio.dlh-test-fw.svc.cluster.local:9000"),
		MinIOBucket:          getenv("DLH_MINIO_BUCKET", "artifacts"),
		MinIOAccessKey:       os.Getenv("DLH_MINIO_ACCESS_KEY"),
		MinIOSecretKey:       os.Getenv("DLH_MINIO_SECRET_KEY"),
		MinIOSecure:          os.Getenv("DLH_MINIO_SECURE") == "true",
		ShutdownGrace:        15 * time.Second,
		AuthDisabled:         os.Getenv("DLH_AUTH_DISABLED") == "true",
	}
	if !c.AuthDisabled {
		if c.OIDCIssuerURL == "" {
			return nil, fmt.Errorf("DLH_OIDC_ISSUER_URL is required when auth is enabled")
		}
		if c.OIDCClientID == "" {
			return nil, fmt.Errorf("DLH_OIDC_CLIENT_ID is required when auth is enabled")
		}
	}
	return c, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
```

- [ ] **Step 2: Write controlplane/internal/config/config_test.go**

```go
package config

import (
	"testing"
)

func TestLoad_AuthDisabledBypassesIssuerCheck(t *testing.T) {
	t.Setenv("DLH_AUTH_DISABLED", "true")
	t.Setenv("DLH_OIDC_ISSUER_URL", "")
	t.Setenv("DLH_OIDC_CLIENT_ID", "")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if !c.AuthDisabled {
		t.Fatal("expected AuthDisabled=true")
	}
}

func TestLoad_AuthEnabledRequiresIssuer(t *testing.T) {
	t.Setenv("DLH_AUTH_DISABLED", "")
	t.Setenv("DLH_OIDC_ISSUER_URL", "")
	t.Setenv("DLH_OIDC_CLIENT_ID", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected error when auth enabled and issuer missing")
	}
}

func TestLoad_DefaultsApplied(t *testing.T) {
	t.Setenv("DLH_AUTH_DISABLED", "true")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if c.ListenAddr != ":8080" {
		t.Errorf("ListenAddr default: got %q", c.ListenAddr)
	}
	if c.K8sNamespace != "dlh-test-fw" {
		t.Errorf("K8sNamespace default: got %q", c.K8sNamespace)
	}
}
```

- [ ] **Step 3: Run tests**

```bash
cd /Users/allen/repo/dlh-test-fw-plan15/controlplane
go test ./internal/config/...
```

Expected: PASS, 3 tests.

- [ ] **Step 4: Replace cmd/dlh-controlplane/main.go**

```go
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

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/dlh/dlh-test-fw/controlplane/internal/config"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("config load failed", "err", err)
		os.Exit(1)
	}

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	r.Get("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		// Phase B: always ready as long as we're serving. Phase C will
		// add deeper checks (k8s informer synced, MinIO reachable).
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	go func() {
		logger.Info("listening", "addr", cfg.ListenAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("listen error", "err", err)
			cancel()
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownGrace)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
}
```

- [ ] **Step 5: Build + smoke run**

```bash
cd /Users/allen/repo/dlh-test-fw-plan15/controlplane
DLH_AUTH_DISABLED=true go build ./... && \
  DLH_AUTH_DISABLED=true go run ./cmd/dlh-controlplane &
SRV=$!
sleep 1
curl -fsS localhost:8080/healthz
curl -fsS localhost:8080/readyz
kill $SRV
```

Expected: two `ok` responses, no errors.

- [ ] **Step 6: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw-plan15
git add controlplane/internal/config/ controlplane/cmd/dlh-controlplane/main.go controlplane/go.sum
git commit -m "feat(controlplane): config package + chi-routed health endpoints"
```

---

## Task 5: K8s client + WorkflowTemplate listing

**Files:**
- Create: `controlplane/internal/k8s/client.go`
- Create: `controlplane/internal/k8s/templates.go`
- Create: `controlplane/internal/k8s/templates_test.go`

- [ ] **Step 1: Write controlplane/internal/k8s/client.go**

```go
package k8s

import (
	"fmt"

	wfclient "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Clients groups the Kubernetes + Argo client-go clients we need.
type Clients struct {
	Core kubernetes.Interface
	Argo wfclient.Interface
}

// NewClients builds an in-cluster client if KUBECONFIG is unset, else
// uses the kubeconfig path. The two paths share the same dynamic config
// after rest.Config is built.
func NewClients(kubeconfigPath string) (*Clients, error) {
	var cfg *rest.Config
	var err error
	if kubeconfigPath != "" {
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	} else {
		cfg, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, fmt.Errorf("build rest.Config: %w", err)
	}
	core, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("core client: %w", err)
	}
	argo, err := wfclient.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("argo client: %w", err)
	}
	return &Clients{Core: core, Argo: argo}, nil
}
```

- [ ] **Step 2: Write controlplane/internal/k8s/templates.go**

```go
package k8s

import (
	"context"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TemplateLister abstracts WorkflowTemplate retrieval for handler tests.
type TemplateLister interface {
	ListTemplates(ctx context.Context) ([]wfv1.WorkflowTemplate, error)
	GetTemplate(ctx context.Context, name string) (*wfv1.WorkflowTemplate, error)
}

type templateLister struct {
	c         *Clients
	namespace string
}

// NewTemplateLister returns a TemplateLister scoped to the given namespace.
func NewTemplateLister(c *Clients, namespace string) TemplateLister {
	return &templateLister{c: c, namespace: namespace}
}

func (l *templateLister) ListTemplates(ctx context.Context) ([]wfv1.WorkflowTemplate, error) {
	list, err := l.c.Argo.ArgoprojV1alpha1().WorkflowTemplates(l.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

func (l *templateLister) GetTemplate(ctx context.Context, name string) (*wfv1.WorkflowTemplate, error) {
	return l.c.Argo.ArgoprojV1alpha1().WorkflowTemplates(l.namespace).Get(ctx, name, metav1.GetOptions{})
}
```

- [ ] **Step 3: Write controlplane/internal/k8s/templates_test.go**

This test uses a fake argo client (no envtest needed for read-only behavior).

```go
package k8s

import (
	"context"
	"testing"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	wfake "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newFakeClients(objs ...interface{}) *Clients {
	wfFake := wfake.NewSimpleClientset(asRuntimeObjects(objs)...)
	return &Clients{Core: nil, Argo: wfFake}
}

func asRuntimeObjects(in []interface{}) []runtimeObject {
	out := make([]runtimeObject, 0, len(in))
	for _, x := range in {
		if r, ok := x.(runtimeObject); ok {
			out = append(out, r)
		}
	}
	return out
}

// runtimeObject is a tiny shim so we don't import k8s.io/apimachinery/pkg/runtime here.
type runtimeObject interface{}

func TestListTemplates(t *testing.T) {
	tmpl := &wfv1.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-pod-delete", Namespace: "dlh-test-fw"},
	}
	c := newFakeClients(tmpl)
	l := NewTemplateLister(c, "dlh-test-fw")

	got, err := l.ListTemplates(context.Background())
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	if len(got) != 1 || got[0].Name != "mysql-pod-delete" {
		t.Errorf("unexpected templates: %+v", got)
	}
}

func TestGetTemplate(t *testing.T) {
	tmpl := &wfv1.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "kafka-broker-partition", Namespace: "dlh-test-fw"},
	}
	c := newFakeClients(tmpl)
	l := NewTemplateLister(c, "dlh-test-fw")
	got, err := l.GetTemplate(context.Background(), "kafka-broker-partition")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	if got.Name != "kafka-broker-partition" {
		t.Errorf("got: %+v", got)
	}
}
```

Note: the test uses `wfake.NewSimpleClientset(...)`. If the runtime-object shim above doesn't compile cleanly (the Argo fake expects `...runtime.Object`), replace `asRuntimeObjects` with a direct call:

```go
func newFakeClients(objs ...runtime.Object) *Clients {
	return &Clients{Argo: wfake.NewSimpleClientset(objs...)}
}
```

and import `"k8s.io/apimachinery/pkg/runtime"`. Use whichever shape your generated fake expects — verify by reading the fake.go signature in the argo-workflows module.

- [ ] **Step 4: Run tests**

```bash
cd /Users/allen/repo/dlh-test-fw-plan15/controlplane
go test ./internal/k8s/...
```

Expected: PASS, 2 tests. If the runtime.Object shim issue surfaces (Step 3 note), fix it now per the alternative pattern.

- [ ] **Step 5: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw-plan15
git add controlplane/internal/k8s/ controlplane/go.sum controlplane/go.mod
git commit -m "feat(controlplane): k8s clients + WorkflowTemplate lister"
```

---

## Task 6: Workflow informer + list/get

**Files:**
- Create: `controlplane/internal/k8s/workflows.go`
- Create: `controlplane/internal/k8s/workflows_test.go`

- [ ] **Step 1: Write controlplane/internal/k8s/workflows.go**

```go
package k8s

import (
	"context"
	"sort"
	"sync"
	"time"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	wfinformers "github.com/argoproj/argo-workflows/v3/pkg/client/informers/externalversions"
	wflisters "github.com/argoproj/argo-workflows/v3/pkg/client/listers/workflow/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// WorkflowLister abstracts the read operations the API handlers need.
type WorkflowLister interface {
	List(filter WorkflowFilter) ([]*wfv1.Workflow, error)
	Get(name string) (*wfv1.Workflow, error)
	Subscribe() (<-chan WorkflowEvent, func())
}

// WorkflowFilter narrows a list query.
type WorkflowFilter struct {
	Scenario string
	Status   string
	Since    *time.Time
	Limit    int
}

// WorkflowEvent is emitted by the informer for SSE consumers.
type WorkflowEvent struct {
	Type     string // ADDED / MODIFIED / DELETED
	Workflow *wfv1.Workflow
}

type workflowLister struct {
	informerFactory wfinformers.SharedInformerFactory
	lister          wflisters.WorkflowLister
	namespace       string

	mu          sync.Mutex
	subscribers map[chan WorkflowEvent]struct{}
}

// NewWorkflowLister starts a SharedInformerFactory + Workflow informer
// for the namespace. The returned lister is safe for concurrent use.
// stopCh terminates the informer when closed.
func NewWorkflowLister(c *Clients, namespace string, stopCh <-chan struct{}) (WorkflowLister, error) {
	factory := wfinformers.NewSharedInformerFactoryWithOptions(c.Argo, 30*time.Second,
		wfinformers.WithNamespace(namespace))
	informer := factory.Argoproj().V1alpha1().Workflows()
	wl := &workflowLister{
		informerFactory: factory,
		lister:          informer.Lister(),
		namespace:       namespace,
		subscribers:     map[chan WorkflowEvent]struct{}{},
	}
	_, _ = informer.Informer().AddEventHandler(wl.eventHandlerFuncs())
	factory.Start(stopCh)
	if synced := factory.WaitForCacheSync(stopCh); !cacheSyncedAll(synced) {
		return nil, contextDeadlineLikeError("informer cache did not sync")
	}
	return wl, nil
}

func cacheSyncedAll(m map[string]bool) bool {
	for _, ok := range m {
		if !ok {
			return false
		}
	}
	return true
}

type contextDeadlineLikeError string

func (e contextDeadlineLikeError) Error() string { return string(e) }

func (w *workflowLister) List(f WorkflowFilter) ([]*wfv1.Workflow, error) {
	all, err := w.lister.Workflows(w.namespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}
	filtered := all[:0:0]
	for _, wf := range all {
		if f.Scenario != "" && wf.Labels["dlh.scenario"] != f.Scenario && templateRef(wf) != f.Scenario {
			continue
		}
		if f.Status != "" && string(wf.Status.Phase) != f.Status {
			continue
		}
		if f.Since != nil && wf.CreationTimestamp.Time.Before(*f.Since) {
			continue
		}
		filtered = append(filtered, wf)
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].CreationTimestamp.After(filtered[j].CreationTimestamp.Time)
	})
	if f.Limit > 0 && len(filtered) > f.Limit {
		filtered = filtered[:f.Limit]
	}
	return filtered, nil
}

func (w *workflowLister) Get(name string) (*wfv1.Workflow, error) {
	return w.lister.Workflows(w.namespace).Get(name)
}

func (w *workflowLister) Subscribe() (<-chan WorkflowEvent, func()) {
	ch := make(chan WorkflowEvent, 16)
	w.mu.Lock()
	w.subscribers[ch] = struct{}{}
	w.mu.Unlock()
	cancel := func() {
		w.mu.Lock()
		if _, ok := w.subscribers[ch]; ok {
			delete(w.subscribers, ch)
			close(ch)
		}
		w.mu.Unlock()
	}
	return ch, cancel
}

func (w *workflowLister) broadcast(ev WorkflowEvent) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for ch := range w.subscribers {
		select {
		case ch <- ev:
		default:
			// Subscriber too slow — drop. Better than blocking the informer.
		}
	}
}

func templateRef(wf *wfv1.Workflow) string {
	if wf.Spec.WorkflowTemplateRef != nil {
		return wf.Spec.WorkflowTemplateRef.Name
	}
	return ""
}

// Static metav1 import keeps the linter quiet if no other use exists.
var _ = metav1.NamespaceAll
```

- [ ] **Step 2: Add the informer's event handler functions** (separate file or add to the same one — keep in `workflows.go`):

Append to `workflows.go`:

```go
import "k8s.io/client-go/tools/cache"

func (w *workflowLister) eventHandlerFuncs() cache.ResourceEventHandler {
	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			if wf, ok := obj.(*wfv1.Workflow); ok {
				w.broadcast(WorkflowEvent{Type: "ADDED", Workflow: wf})
			}
		},
		UpdateFunc: func(_, newObj interface{}) {
			if wf, ok := newObj.(*wfv1.Workflow); ok {
				w.broadcast(WorkflowEvent{Type: "MODIFIED", Workflow: wf})
			}
		},
		DeleteFunc: func(obj interface{}) {
			if wf, ok := obj.(*wfv1.Workflow); ok {
				w.broadcast(WorkflowEvent{Type: "DELETED", Workflow: wf})
			}
		},
	}
}
```

(Merge the `import` with the existing import block; don't leave two separate `import` clauses.)

- [ ] **Step 3: Write controlplane/internal/k8s/workflows_test.go**

```go
package k8s

import (
	"testing"
	"time"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Construct a workflowLister directly with a static slice to test
// filtering without needing a running informer.
type staticLister struct {
	items []*wfv1.Workflow
}

func (s *staticLister) listAll() []*wfv1.Workflow { return s.items }

// We test filter logic via a small extracted helper.
func filterWorkflows(items []*wfv1.Workflow, f WorkflowFilter) []*wfv1.Workflow {
	out := []*wfv1.Workflow{}
	for _, wf := range items {
		if f.Scenario != "" && wf.Labels["dlh.scenario"] != f.Scenario && templateRef(wf) != f.Scenario {
			continue
		}
		if f.Status != "" && string(wf.Status.Phase) != f.Status {
			continue
		}
		if f.Since != nil && wf.CreationTimestamp.Time.Before(*f.Since) {
			continue
		}
		out = append(out, wf)
	}
	return out
}

func TestFilter_Scenario(t *testing.T) {
	now := metav1.Now()
	items := []*wfv1.Workflow{
		{ObjectMeta: metav1.ObjectMeta{Name: "a", Labels: map[string]string{"dlh.scenario": "mysql-pod-delete"}, CreationTimestamp: now}},
		{ObjectMeta: metav1.ObjectMeta{Name: "b", Labels: map[string]string{"dlh.scenario": "kafka-broker-partition"}, CreationTimestamp: now}},
	}
	got := filterWorkflows(items, WorkflowFilter{Scenario: "mysql-pod-delete"})
	if len(got) != 1 || got[0].Name != "a" {
		t.Errorf("got %+v", got)
	}
}

func TestFilter_Since(t *testing.T) {
	cutoff := time.Now().Add(-1 * time.Hour)
	items := []*wfv1.Workflow{
		{ObjectMeta: metav1.ObjectMeta{Name: "old", CreationTimestamp: metav1.NewTime(cutoff.Add(-2 * time.Hour))}},
		{ObjectMeta: metav1.ObjectMeta{Name: "new", CreationTimestamp: metav1.NewTime(cutoff.Add(time.Hour))}},
	}
	got := filterWorkflows(items, WorkflowFilter{Since: &cutoff})
	if len(got) != 1 || got[0].Name != "new" {
		t.Errorf("got %+v", got)
	}
}
```

- [ ] **Step 4: Run tests**

```bash
cd /Users/allen/repo/dlh-test-fw-plan15/controlplane
go test ./internal/k8s/...
```

Expected: 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw-plan15
git add controlplane/internal/k8s/workflows.go controlplane/internal/k8s/workflows_test.go controlplane/go.sum
git commit -m "feat(controlplane): Workflow informer + filtered lister + subscriber channel"
```

---

## Task 7: MinIO client + report.json reader

**Files:**
- Create: `controlplane/internal/minio/client.go`
- Create: `controlplane/internal/minio/reports.go`
- Create: `controlplane/internal/minio/reports_test.go`

- [ ] **Step 1: Write controlplane/internal/minio/client.go**

```go
package minio

import (
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// New returns a configured MinIO client. The endpoint should be host:port
// without scheme; pass secure=true for HTTPS.
func New(endpoint, accessKey, secretKey string, secure bool) (*minio.Client, error) {
	return minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: secure,
	})
}
```

- [ ] **Step 2: Write controlplane/internal/minio/reports.go**

```go
package minio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/minio/minio-go/v7"
)

// ReportReader fetches verdict-job's report.json from the artifact bucket.
type ReportReader struct {
	client *minio.Client
	bucket string
}

// NewReportReader binds a client to a bucket.
func NewReportReader(client *minio.Client, bucket string) *ReportReader {
	return &ReportReader{client: client, bucket: bucket}
}

// ErrReportNotFound signals "no report yet", distinct from transport errors.
var ErrReportNotFound = errors.New("report not found")

// Read returns the parsed report.json for a workflow name. If the object
// is absent, returns (nil, ErrReportNotFound). The object key follows the
// existing convention: <workflow>/<workflow>-main-*/verdict/report.json.
// In Phase B we only support the canonical path; Phase C will add a
// manifest-driven lookup.
func (r *ReportReader) Read(ctx context.Context, workflowName string) (map[string]any, error) {
	// Verdict-job writes the artifact at a path Argo expands; for Phase B
	// we look at a deterministic suffix and return the first match.
	prefix := fmt.Sprintf("%s/", workflowName)
	for objInfo := range r.client.ListObjects(ctx, r.bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	}) {
		if objInfo.Err != nil {
			return nil, objInfo.Err
		}
		if hasReportSuffix(objInfo.Key) {
			obj, err := r.client.GetObject(ctx, r.bucket, objInfo.Key, minio.GetObjectOptions{})
			if err != nil {
				return nil, err
			}
			defer obj.Close()
			raw, err := io.ReadAll(obj)
			if err != nil {
				return nil, err
			}
			out := map[string]any{}
			if err := json.Unmarshal(raw, &out); err != nil {
				return nil, fmt.Errorf("decode report.json: %w", err)
			}
			return out, nil
		}
	}
	return nil, ErrReportNotFound
}

func hasReportSuffix(key string) bool {
	const suffix = "/verdict/report.json"
	return len(key) >= len(suffix) && key[len(key)-len(suffix):] == suffix
}
```

- [ ] **Step 3: Write controlplane/internal/minio/reports_test.go**

Test the path-matching helper without needing a real MinIO:

```go
package minio

import "testing"

func TestHasReportSuffix(t *testing.T) {
	cases := []struct {
		key  string
		want bool
	}{
		{"mysql-pod-delete-20260521-001523/mysql-pod-delete-20260521-001523-main-123/verdict/report.json", true},
		{"mysql-pod-delete-20260521-001523/something-else.txt", false},
		{"", false},
		{"verdict/report.json", true},
	}
	for _, c := range cases {
		if got := hasReportSuffix(c.key); got != c.want {
			t.Errorf("hasReportSuffix(%q) = %v, want %v", c.key, got, c.want)
		}
	}
}
```

- [ ] **Step 4: Run tests**

```bash
cd /Users/allen/repo/dlh-test-fw-plan15/controlplane
go test ./internal/minio/...
```

Expected: PASS, 1 test (4 subcases).

- [ ] **Step 5: Commit**

```bash
git add controlplane/internal/minio/ controlplane/go.sum
git commit -m "feat(controlplane): MinIO client + report.json reader"
```

---

## Task 8: API handlers — scenarios + runs

**Files:**
- Create: `controlplane/internal/api/server.go`
- Create: `controlplane/internal/api/handlers.go`
- Create: `controlplane/internal/api/handlers_test.go`
- Create: `controlplane/internal/model/types.go`

- [ ] **Step 1: Write controlplane/internal/model/types.go**

Translates k8s types into OpenAPI DTOs.

```go
package model

import (
	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"

	"github.com/dlh/dlh-test-fw/controlplane/internal/api/gen"
)

// ScenarioFromTemplate maps a WorkflowTemplate to the OpenAPI Scenario.
func ScenarioFromTemplate(t *wfv1.WorkflowTemplate) gen.Scenario {
	s := gen.Scenario{
		Id:          t.Name,
		DisplayName: t.Name,
	}
	if v := t.Annotations["dlh.description"]; v != "" {
		desc := v
		s.Description = &desc
	}
	if v := t.Annotations["dlh.target-type"]; v != "" {
		tt := v
		s.TargetType = &tt
	}
	if t.Spec.Arguments.Parameters != nil {
		params := make([]struct {
			Name        string  `json:"name"`
			Default     *string `json:"default,omitempty"`
			Description *string `json:"description,omitempty"`
		}, 0, len(t.Spec.Arguments.Parameters))
		for _, p := range t.Spec.Arguments.Parameters {
			entry := struct {
				Name        string  `json:"name"`
				Default     *string `json:"default,omitempty"`
				Description *string `json:"description,omitempty"`
			}{Name: p.Name}
			if p.Default != nil {
				d := p.Default.String()
				entry.Default = &d
			}
			if p.Description != nil {
				ds := p.Description.String()
				entry.Description = &ds
			}
			params = append(params, entry)
		}
		s.Parameters = &params
	}
	return s
}

// RunFromWorkflow maps a Workflow CR to the OpenAPI Run summary.
func RunFromWorkflow(wf *wfv1.Workflow) gen.Run {
	r := gen.Run{
		Id:        wf.Name,
		StartedAt: wf.CreationTimestamp.Time,
		Status:    gen.RunStatus(mapPhase(string(wf.Status.Phase))),
	}
	if wf.Spec.WorkflowTemplateRef != nil {
		r.Scenario = wf.Spec.WorkflowTemplateRef.Name
	} else if v := wf.Labels["dlh.scenario"]; v != "" {
		r.Scenario = v
	}
	if !wf.Status.FinishedAt.IsZero() {
		t := wf.Status.FinishedAt.Time
		r.FinishedAt = &t
	}
	name := wf.Name
	r.WorkflowName = &name
	return r
}

func mapPhase(phase string) string {
	switch phase {
	case "":
		return "Pending"
	case "Pending", "Running", "Succeeded", "Failed", "Error":
		return phase
	default:
		return "Unknown"
	}
}
```

**Note for the engineer:** the exact field types of `gen.Scenario.Parameters` depend on what `oapi-codegen` emitted. If the generated type is `*[]gen.ScenarioParameters` (or similar), adjust the conversion code in Step 1 to use that type — the principle (loop, copy, assign pointer) stays the same. Run `go build` after this step to surface the exact signature.

- [ ] **Step 2: Write controlplane/internal/api/server.go**

```go
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/dlh/dlh-test-fw/controlplane/internal/api/gen"
)

// Deps groups the runtime dependencies handlers need.
type Deps struct {
	Templates TemplateSource
	Workflows WorkflowSource
	Reports   ReportSource
}

// Interfaces for testability. The k8s and minio packages satisfy these.
type TemplateSource interface {
	ListTemplates(ctx contextLike) ([]templateLite, error)
	GetTemplate(ctx contextLike, name string) (*templateLite, error)
}
type WorkflowSource interface {
	List(filter workflowFilter) ([]workflowLite, error)
	Get(name string) (*workflowLite, error)
	Subscribe() (<-chan workflowEventLite, func())
}
type ReportSource interface {
	Read(ctx contextLike, workflowName string) (map[string]any, error)
}

// NewRouter mounts the generated server onto chi with our handlers + middlewares.
func NewRouter(deps *Deps, authMW func(http.Handler) http.Handler) http.Handler {
	r := chi.NewRouter()
	r.Get("/healthz", healthHandler)
	r.Get("/readyz", healthHandler)

	apiGroup := chi.NewRouter()
	if authMW != nil {
		apiGroup.Use(authMW)
	}
	h := &Handlers{deps: deps}
	gen.HandlerFromMux(gen.NewStrictHandler(h, nil), apiGroup)

	r.Mount("/api", apiGroup)
	return r
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}
```

**IMPORTANT:** the precise way to mount `gen.HandlerFromMux` / `gen.NewStrictHandler` depends on the codegen output. After Task 3 completes, inspect `internal/api/gen/server.gen.go` to find the actual entry function — `HandlerFromMux(si, r)` or `RegisterHandlers(r, si)` or similar. Adjust Step 2 above to use whatever the generated code exposes. The principle stays the same: wrap the strict handler interface, mount it under `/api`.

If the contextLike / templateLite / workflowLite interfaces clash with what handlers.go actually needs, simplify — make the interfaces use the concrete types from `internal/k8s` and `internal/minio`. The above is a sketch to keep this file decoupled; you can collapse it to direct concrete-type usage if cleaner.

- [ ] **Step 3: Write controlplane/internal/api/handlers.go**

```go
package api

import (
	"context"
	"errors"
	"net/http"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"

	"github.com/dlh/dlh-test-fw/controlplane/internal/api/gen"
	"github.com/dlh/dlh-test-fw/controlplane/internal/k8s"
	mio "github.com/dlh/dlh-test-fw/controlplane/internal/minio"
	"github.com/dlh/dlh-test-fw/controlplane/internal/model"
)

// concrete handler deps — replaces the sketch interfaces in server.go.
type RealDeps struct {
	Templates k8s.TemplateLister
	Workflows k8s.WorkflowLister
	Reports   *mio.ReportReader
}

// Handlers implements the strict-server interface generated by oapi-codegen.
type Handlers struct {
	deps *RealDeps
}

// Replace the Deps reference in NewRouter (server.go) with *RealDeps and
// remove the *Lite interfaces — the api package can depend directly on
// internal/k8s and internal/minio. This is the simplest path; the
// indirection in server.go was premature.

// ListScenarios — GET /api/scenarios
func (h *Handlers) ListScenarios(ctx context.Context, _ gen.ListScenariosRequestObject) (gen.ListScenariosResponseObject, error) {
	tmpls, err := h.deps.Templates.ListTemplates(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gen.Scenario, 0, len(tmpls))
	for i := range tmpls {
		out = append(out, model.ScenarioFromTemplate(&tmpls[i]))
	}
	return gen.ListScenarios200JSONResponse{Items: out}, nil
}

// GetScenario — GET /api/scenarios/{id}
func (h *Handlers) GetScenario(ctx context.Context, req gen.GetScenarioRequestObject) (gen.GetScenarioResponseObject, error) {
	tmpl, err := h.deps.Templates.GetTemplate(ctx, req.Id)
	if err != nil {
		return gen.GetScenario404Response{}, nil
	}
	s := model.ScenarioFromTemplate(tmpl)
	return gen.GetScenario200JSONResponse(s), nil
}

// ListRuns — GET /api/runs
func (h *Handlers) ListRuns(_ context.Context, req gen.ListRunsRequestObject) (gen.ListRunsResponseObject, error) {
	f := k8s.WorkflowFilter{}
	if req.Params.Scenario != nil {
		f.Scenario = *req.Params.Scenario
	}
	if req.Params.Status != nil {
		f.Status = *req.Params.Status
	}
	if req.Params.Since != nil {
		t := *req.Params.Since
		f.Since = &t
	}
	if req.Params.Limit != nil {
		f.Limit = *req.Params.Limit
	}
	wfs, err := h.deps.Workflows.List(f)
	if err != nil {
		return nil, err
	}
	items := make([]gen.Run, 0, len(wfs))
	for _, wf := range wfs {
		items = append(items, model.RunFromWorkflow(wf))
	}
	return gen.ListRuns200JSONResponse{Items: items}, nil
}

// GetRun — GET /api/runs/{id}
func (h *Handlers) GetRun(ctx context.Context, req gen.GetRunRequestObject) (gen.GetRunResponseObject, error) {
	wf, err := h.deps.Workflows.Get(req.Id)
	if err != nil {
		return gen.GetRun404Response{}, nil
	}
	detail := buildRunDetail(wf)
	if report, err := h.deps.Reports.Read(ctx, wf.Name); err == nil {
		detail.Verdict = &report
	} else if !errors.Is(err, mio.ErrReportNotFound) {
		// log but do not fail the request
	}
	return gen.GetRun200JSONResponse(detail), nil
}

func buildRunDetail(wf *wfv1.Workflow) gen.RunDetail {
	base := model.RunFromWorkflow(wf)
	d := gen.RunDetail{Run: base}
	if wf.Status.Nodes != nil {
		steps := make([]struct {
			Name       string `json:"name"`
			Phase      string `json:"phase"`
			StartedAt  *string `json:"startedAt,omitempty"`
			FinishedAt *string `json:"finishedAt,omitempty"`
			Message    *string `json:"message,omitempty"`
		}, 0, len(wf.Status.Nodes))
		for _, n := range wf.Status.Nodes {
			step := struct {
				Name       string `json:"name"`
				Phase      string `json:"phase"`
				StartedAt  *string `json:"startedAt,omitempty"`
				FinishedAt *string `json:"finishedAt,omitempty"`
				Message    *string `json:"message,omitempty"`
			}{Name: n.DisplayName, Phase: string(n.Phase)}
			if !n.StartedAt.IsZero() {
				s := n.StartedAt.Format("2006-01-02T15:04:05Z07:00")
				step.StartedAt = &s
			}
			if !n.FinishedAt.IsZero() {
				f := n.FinishedAt.Format("2006-01-02T15:04:05Z07:00")
				step.FinishedAt = &f
			}
			if n.Message != "" {
				m := n.Message
				step.Message = &m
			}
			steps = append(steps, step)
		}
		d.Steps = &steps
	}
	return d
}

// StreamRunEvents — GET /api/runs/{id}/events
// SSE is implemented in sse.go since it cannot live inside a strict-handler.
// The strict-handler implementation just returns "not implemented" — the
// real SSE handler is mounted directly on the chi router. See server.go.
func (h *Handlers) StreamRunEvents(_ context.Context, _ gen.StreamRunEventsRequestObject) (gen.StreamRunEventsResponseObject, error) {
	return gen.StreamRunEvents200TexteventStreamResponse{}, nil
}

// GetHealthz / GetReadyz exist in the OpenAPI but are wired directly on chi
// so they bypass auth. Provide trivial implementations so the strict-handler
// interface is fully satisfied.
func (h *Handlers) GetHealthz(_ context.Context, _ gen.GetHealthzRequestObject) (gen.GetHealthzResponseObject, error) {
	return gen.GetHealthz200Response{}, nil
}
func (h *Handlers) GetReadyz(_ context.Context, _ gen.GetReadyzRequestObject) (gen.GetReadyzResponseObject, error) {
	return gen.GetReadyz200Response{}, nil
}
```

**Note for the engineer:** the response type names (`ListScenarios200JSONResponse`, `GetScenario404Response`, etc.) are exactly what `oapi-codegen` with `strict-server: true` emits — they may not match this draft if the codegen output differs. After Task 3 builds, open `internal/api/gen/server.gen.go` and verify the exact type names; substitute in this file as needed. The interface contract (one method per operationId, request and response types) is stable.

The inline anonymous struct types for `Parameters` and `Steps` likely won't match what codegen produces either — codegen typically generates named types. Replace `struct { Name string; Default *string; ... }` with the actual `gen.ScenarioParameters` (or similar) named type that codegen emitted.

The simplest verification path: `go build ./...` after writing these files; the compiler tells you exactly which type names are wrong.

- [ ] **Step 4: Write controlplane/internal/api/handlers_test.go**

```go
package api

import (
	"context"
	"testing"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/dlh/dlh-test-fw/controlplane/internal/api/gen"
	"github.com/dlh/dlh-test-fw/controlplane/internal/k8s"
)

type fakeTemplates struct {
	items []wfv1.WorkflowTemplate
}

func (f *fakeTemplates) ListTemplates(_ context.Context) ([]wfv1.WorkflowTemplate, error) {
	return f.items, nil
}
func (f *fakeTemplates) GetTemplate(_ context.Context, name string) (*wfv1.WorkflowTemplate, error) {
	for i := range f.items {
		if f.items[i].Name == name {
			return &f.items[i], nil
		}
	}
	return nil, errNotFound
}

type fakeWorkflows struct {
	items []*wfv1.Workflow
}

func (f *fakeWorkflows) List(_ k8s.WorkflowFilter) ([]*wfv1.Workflow, error) { return f.items, nil }
func (f *fakeWorkflows) Get(name string) (*wfv1.Workflow, error) {
	for _, w := range f.items {
		if w.Name == name {
			return w, nil
		}
	}
	return nil, errNotFound
}
func (f *fakeWorkflows) Subscribe() (<-chan k8s.WorkflowEvent, func()) {
	ch := make(chan k8s.WorkflowEvent)
	return ch, func() { close(ch) }
}

type errNotFoundT struct{}

func (errNotFoundT) Error() string { return "not found" }

var errNotFound = errNotFoundT{}

type fakeReports struct{}

func (fakeReports) Read(_ context.Context, _ string) (map[string]any, error) { return nil, nil }

func TestListScenarios(t *testing.T) {
	deps := &RealDeps{
		Templates: &fakeTemplates{items: []wfv1.WorkflowTemplate{
			{ObjectMeta: metav1.ObjectMeta{Name: "mysql-pod-delete"}},
		}},
	}
	h := &Handlers{deps: deps}
	resp, err := h.ListScenarios(context.Background(), gen.ListScenariosRequestObject{})
	if err != nil {
		t.Fatalf("ListScenarios: %v", err)
	}
	out, ok := resp.(gen.ListScenarios200JSONResponse)
	if !ok {
		t.Fatalf("unexpected response type: %T", resp)
	}
	if len(out.Items) != 1 || out.Items[0].Id != "mysql-pod-delete" {
		t.Errorf("got %+v", out.Items)
	}
}
```

(Add similar tests for GetScenario / ListRuns / GetRun as you go — at least one happy-path test per handler. Tests for 404 paths can be added incrementally.)

- [ ] **Step 5: Run tests + fix codegen-related field name mismatches**

```bash
cd /Users/allen/repo/dlh-test-fw-plan15/controlplane
go build ./...
go test ./internal/api/...
```

If build fails: open `internal/api/gen/server.gen.go` and `internal/api/gen/types.gen.go`, find the actual type names, and fix `handlers.go` / `model/types.go` accordingly. Expect 1-2 iterations of "build, see compiler error, rename, build again."

Expected (after fixes): green build + passing tests.

- [ ] **Step 6: Commit**

```bash
git add controlplane/internal/api/ controlplane/internal/model/ controlplane/go.sum
git commit -m "feat(controlplane): API handlers for scenarios + runs

Strict-server implementation of GET /api/scenarios, GET /api/scenarios/{id},
GET /api/runs, GET /api/runs/{id}. SSE handler stub; real SSE in next task.
Health endpoints mounted directly on chi outside the strict handler."
```

---

## Task 9: SSE handler for /api/runs/{id}/events

**Files:**
- Create: `controlplane/internal/api/sse.go`
- Modify: `controlplane/internal/api/server.go` to mount SSE directly on chi
- Create: `controlplane/internal/api/sse_test.go`

- [ ] **Step 1: Write controlplane/internal/api/sse.go**

```go
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
```

- [ ] **Step 2: Modify controlplane/internal/api/server.go to mount SSE directly**

In `NewRouter`, after mounting the strict-handler-backed `/api` group, also mount the SSE route at the same prefix (chi allows multiple registrations as long as they don't overlap):

```go
sseH := &SSEHandler{Workflows: deps.Workflows}
apiGroup.Get("/runs/{id}/events", sseH.Handle)
```

The strict-handler's stub for `StreamRunEvents` is unreachable in practice because chi resolves the explicit route first. Add a comment noting this in handlers.go's StreamRunEvents stub.

- [ ] **Step 3: Write controlplane/internal/api/sse_test.go**

```go
package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/dlh/dlh-test-fw/controlplane/internal/k8s"
	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type controllableWorkflows struct {
	events chan k8s.WorkflowEvent
	wf     *wfv1.Workflow
}

func (c *controllableWorkflows) List(_ k8s.WorkflowFilter) ([]*wfv1.Workflow, error) {
	return nil, nil
}
func (c *controllableWorkflows) Get(name string) (*wfv1.Workflow, error) {
	if c.wf != nil && c.wf.Name == name {
		return c.wf, nil
	}
	return nil, errNotFound
}
func (c *controllableWorkflows) Subscribe() (<-chan k8s.WorkflowEvent, func()) {
	return c.events, func() {}
}

func TestSSE_EmitsSnapshotThenEvent(t *testing.T) {
	wf := &wfv1.Workflow{
		ObjectMeta: metav1.ObjectMeta{Name: "run-1"},
		Status:     wfv1.WorkflowStatus{Phase: "Running"},
	}
	src := &controllableWorkflows{events: make(chan k8s.WorkflowEvent, 1), wf: wf}
	sseH := &SSEHandler{Workflows: src}

	r := chi.NewRouter()
	r.Get("/api/runs/{id}/events", sseH.Handle)
	srv := httptest.NewServer(r)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/runs/run-1/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	go func() {
		time.Sleep(100 * time.Millisecond)
		newWf := wf.DeepCopy()
		newWf.Status.Phase = "Succeeded"
		src.events <- k8s.WorkflowEvent{Type: "MODIFIED", Workflow: newWf}
	}()

	buf := make([]byte, 2048)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])
	if !strings.Contains(body, "event: snapshot") {
		t.Errorf("expected snapshot event in %q", body)
	}
	// Read more for the MODIFIED event.
	n2, _ := resp.Body.Read(buf)
	body2 := string(buf[:n2])
	if !strings.Contains(body2, "event: MODIFIED") {
		t.Errorf("expected MODIFIED event in %q", body2)
	}
}
```

- [ ] **Step 4: Run tests**

```bash
cd /Users/allen/repo/dlh-test-fw-plan15/controlplane
go test ./internal/api/...
```

Expected: PASS. If the SSE test is flaky due to timing, increase the sleep to 200ms; the test is acceptably deterministic at that resolution.

- [ ] **Step 5: Commit**

```bash
git add controlplane/internal/api/sse.go controlplane/internal/api/sse_test.go controlplane/internal/api/server.go
git commit -m "feat(controlplane): SSE handler for /api/runs/{id}/events

Subscribes to the workflow informer's broadcast channel and filters by
run id. Initial snapshot read on connect; 15-second keepalive comments
prevent intermediary timeouts."
```

---

## Task 10: OIDC verifier + RBAC middleware

**Files:**
- Create: `controlplane/internal/auth/oidc.go`
- Create: `controlplane/internal/auth/fake.go`
- Create: `controlplane/internal/auth/rbac.go`
- Create: `controlplane/internal/auth/middleware.go`
- Create: `controlplane/internal/auth/oidc_test.go`
- Create: `controlplane/internal/auth/rbac_test.go`

- [ ] **Step 1: Write controlplane/internal/auth/oidc.go**

```go
package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
)

// Verifier wraps oidc.IDTokenVerifier with an audience check and groups
// claim extraction.
type Verifier struct {
	v             *oidc.IDTokenVerifier
	groupsClaim   string
	requiredAud   string
}

// NewVerifier builds a Verifier from the issuer URL + client ID + optional
// audience (defaults to client ID if empty).
func NewVerifier(ctx context.Context, issuer, clientID, audience, groupsClaim string) (*Verifier, error) {
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc provider: %w", err)
	}
	aud := audience
	if aud == "" {
		aud = clientID
	}
	if groupsClaim == "" {
		groupsClaim = "groups"
	}
	return &Verifier{
		v:           provider.Verifier(&oidc.Config{ClientID: clientID}),
		groupsClaim: groupsClaim,
		requiredAud: aud,
	}, nil
}

// Identity is the subset of token claims we care about.
type Identity struct {
	Subject string
	Email   string
	Groups  []string
}

// Verify validates the bearer token and returns the identity.
func (v *Verifier) Verify(ctx context.Context, rawToken string) (*Identity, error) {
	if v == nil {
		return nil, errors.New("verifier not configured")
	}
	tok, err := v.v.Verify(ctx, rawToken)
	if err != nil {
		return nil, fmt.Errorf("verify: %w", err)
	}
	var claims struct {
		Sub    string   `json:"sub"`
		Email  string   `json:"email"`
		Groups []string `json:"-"` // populated below by lookup of v.groupsClaim
	}
	if err := tok.Claims(&claims); err != nil {
		return nil, fmt.Errorf("claims: %w", err)
	}
	// Re-decode for groups under the configured key.
	rawClaims := map[string]any{}
	_ = tok.Claims(&rawClaims)
	if groups, ok := rawClaims[v.groupsClaim].([]any); ok {
		for _, g := range groups {
			if s, ok := g.(string); ok {
				claims.Groups = append(claims.Groups, s)
			}
		}
	}
	return &Identity{Subject: claims.Sub, Email: claims.Email, Groups: claims.Groups}, nil
}
```

- [ ] **Step 2: Write controlplane/internal/auth/fake.go**

A test-only fake that bypasses signature verification. NOT exported for production use — guarded by a build tag would be ideal; for v1 we live with naming convention.

```go
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// FakeVerifier accepts tokens of the form `fake:<sub>:<email>:<group1,group2>`.
// Only used in tests and when DLH_AUTH_DISABLED=true.
type FakeVerifier struct{}

func (FakeVerifier) Verify(_ context.Context, rawToken string) (*Identity, error) {
	if !strings.HasPrefix(rawToken, "fake:") {
		return nil, errors.New("not a fake token")
	}
	parts := strings.SplitN(strings.TrimPrefix(rawToken, "fake:"), ":", 3)
	if len(parts) < 2 {
		return nil, errors.New("malformed fake token")
	}
	id := &Identity{Subject: parts[0], Email: parts[1]}
	if len(parts) == 3 && parts[2] != "" {
		id.Groups = strings.Split(parts[2], ",")
	}
	return id, nil
}

// VerifierIface is the interface both Verifier and FakeVerifier satisfy.
type VerifierIface interface {
	Verify(ctx context.Context, rawToken string) (*Identity, error)
}

// Asserts both types satisfy the interface at compile time.
var _ VerifierIface = (*Verifier)(nil)
var _ VerifierIface = FakeVerifier{}

// Unused import guard so editors don't strip encoding/json.
var _ = json.RawMessage(nil)
```

- [ ] **Step 3: Write controlplane/internal/auth/rbac.go**

```go
package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Role is a coarse permission level.
type Role string

const (
	RoleViewer Role = "viewer"
	RoleRunner Role = "runner"
	RoleAdmin  Role = "admin"
)

// IsAtLeast returns true if r grants at least the privileges of want.
func (r Role) IsAtLeast(want Role) bool {
	order := map[Role]int{RoleViewer: 1, RoleRunner: 2, RoleAdmin: 3}
	return order[r] >= order[want]
}

// Roles maps OIDC groups to a Role. Loaded from a ConfigMap at startup
// and refreshed on a timer.
type Roles struct {
	mu       sync.RWMutex
	bindings map[string]Role // group -> role
}

// NewRoles fetches the configmap once. Caller may set up a goroutine to
// call Refresh periodically.
func NewRoles(ctx context.Context, client kubernetes.Interface, ns, name string) (*Roles, error) {
	r := &Roles{bindings: map[string]Role{}}
	if err := r.Refresh(ctx, client, ns, name); err != nil {
		return nil, err
	}
	return r, nil
}

// Refresh re-reads the ConfigMap. Data shape:
//
//	data:
//	  bindings.yaml: |
//	    viewer: ["dlh-viewers", "engineering"]
//	    runner: ["dlh-runners"]
//	    admin:  ["dlh-admins"]
func (r *Roles) Refresh(ctx context.Context, client kubernetes.Interface, ns, name string) error {
	cm, err := client.CoreV1().ConfigMaps(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get configmap %s/%s: %w", ns, name, err)
	}
	bindings, err := parseBindings(cm)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.bindings = bindings
	r.mu.Unlock()
	return nil
}

func parseBindings(cm *corev1.ConfigMap) (map[string]Role, error) {
	raw, ok := cm.Data["bindings.yaml"]
	if !ok {
		return nil, errors.New("configmap missing bindings.yaml key")
	}
	// Use a minimal hand-rolled parser to avoid pulling in yaml.v3 for
	// such a small file.
	out := map[string]Role{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// expect: role: ["a", "b"]
		colon := strings.Index(line, ":")
		if colon == -1 {
			continue
		}
		role := Role(strings.TrimSpace(line[:colon]))
		rest := strings.TrimSpace(line[colon+1:])
		rest = strings.TrimPrefix(rest, "[")
		rest = strings.TrimSuffix(rest, "]")
		for _, g := range strings.Split(rest, ",") {
			g = strings.TrimSpace(g)
			g = strings.Trim(g, "\"")
			if g == "" {
				continue
			}
			out[g] = role
		}
	}
	return out, nil
}

// Resolve returns the highest role across the identity's groups, or
// RoleViewer if none match. (Spec §9.1: unknown identities get viewer
// by default once authenticated.)
func (r *Roles) Resolve(id *Identity) Role {
	r.mu.RLock()
	defer r.mu.RUnlock()
	best := RoleViewer
	for _, g := range id.Groups {
		if role, ok := r.bindings[g]; ok {
			if role.IsAtLeast(best) {
				best = role
			}
		}
	}
	return best
}
```

- [ ] **Step 4: Write controlplane/internal/auth/middleware.go**

```go
package auth

import (
	"context"
	"net/http"
	"strings"
)

// ctxKey scopes context keys so they don't collide with other packages.
type ctxKey int

const (
	identityKey ctxKey = iota
	roleKey
)

// Middleware verifies bearer tokens and attaches the identity + role to
// the request context. Required-role checks are per-handler.
func Middleware(v VerifierIface, roles *Roles) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHdr := r.Header.Get("Authorization")
			if !strings.HasPrefix(authHdr, "Bearer ") {
				http.Error(w, "missing bearer token", http.StatusUnauthorized)
				return
			}
			token := strings.TrimPrefix(authHdr, "Bearer ")
			id, err := v.Verify(r.Context(), token)
			if err != nil {
				http.Error(w, "invalid token", http.StatusUnauthorized)
				return
			}
			role := roles.Resolve(id)
			ctx := context.WithValue(r.Context(), identityKey, id)
			ctx = context.WithValue(ctx, roleKey, role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireRole returns a middleware that enforces a minimum role.
func RequireRole(want Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role, _ := r.Context().Value(roleKey).(Role)
			if !role.IsAtLeast(want) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// IdentityFromContext extracts the verified identity, if present.
func IdentityFromContext(ctx context.Context) (*Identity, bool) {
	id, ok := ctx.Value(identityKey).(*Identity)
	return id, ok
}
```

- [ ] **Step 5: Write controlplane/internal/auth/rbac_test.go**

```go
package auth

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestParseBindings_Basic(t *testing.T) {
	cm := &corev1.ConfigMap{Data: map[string]string{
		"bindings.yaml": `
viewer: ["dlh-viewers"]
runner: ["dlh-runners"]
admin: ["dlh-admins"]
`,
	}}
	b, err := parseBindings(cm)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if b["dlh-runners"] != RoleRunner {
		t.Errorf("got %v", b)
	}
}

func TestResolve_HighestRoleWins(t *testing.T) {
	r := &Roles{bindings: map[string]Role{
		"a": RoleViewer,
		"b": RoleAdmin,
		"c": RoleRunner,
	}}
	id := &Identity{Groups: []string{"a", "b", "c"}}
	if got := r.Resolve(id); got != RoleAdmin {
		t.Errorf("got %v", got)
	}
}

func TestResolve_UnknownGroupsGetViewer(t *testing.T) {
	r := &Roles{bindings: map[string]Role{"a": RoleAdmin}}
	id := &Identity{Groups: []string{"unknown"}}
	if got := r.Resolve(id); got != RoleViewer {
		t.Errorf("got %v", got)
	}
}
```

- [ ] **Step 6: Write controlplane/internal/auth/oidc_test.go**

```go
package auth

import (
	"context"
	"testing"
)

func TestFakeVerifier_Roundtrip(t *testing.T) {
	v := FakeVerifier{}
	id, err := v.Verify(context.Background(), "fake:user-1:user@example.com:dlh-admins,dlh-runners")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if id.Subject != "user-1" {
		t.Errorf("subject: %q", id.Subject)
	}
	if id.Email != "user@example.com" {
		t.Errorf("email: %q", id.Email)
	}
	if len(id.Groups) != 2 || id.Groups[0] != "dlh-admins" {
		t.Errorf("groups: %v", id.Groups)
	}
}

func TestFakeVerifier_Malformed(t *testing.T) {
	v := FakeVerifier{}
	if _, err := v.Verify(context.Background(), "not-a-fake"); err == nil {
		t.Fatal("expected error")
	}
}
```

- [ ] **Step 7: Run tests**

```bash
cd /Users/allen/repo/dlh-test-fw-plan15/controlplane
go test ./internal/auth/...
```

Expected: 4 tests PASS.

- [ ] **Step 8: Commit**

```bash
git add controlplane/internal/auth/ controlplane/go.sum
git commit -m "feat(controlplane): OIDC verifier + RBAC ConfigMap loader + chi middleware

FakeVerifier for tests + DLH_AUTH_DISABLED local-dev mode. Role
ConfigMap shape uses bindings.yaml with role->groups arrays.
RequireRole middleware enforces minimums per route."
```

---

## Task 11: Wire everything in main.go

**Files:**
- Modify: `controlplane/cmd/dlh-controlplane/main.go`

- [ ] **Step 1: Rewrite main.go to wire all the pieces**

```go
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
	"github.com/dlh/dlh-test-fw/controlplane/internal/minio"
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

	mc, err := minio.New(cfg.MinIOEndpoint, cfg.MinIOAccessKey, cfg.MinIOSecretKey, cfg.MinIOSecure)
	if err != nil {
		logger.Error("minio client", "err", err)
		os.Exit(1)
	}
	reports := minio.NewReportReader(mc, cfg.MinIOBucket)

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

	deps := &api.RealDeps{Templates: tmplLister, Workflows: wfLister, Reports: reports}
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
```

- [ ] **Step 2: Build**

```bash
cd /Users/allen/repo/dlh-test-fw-plan15/controlplane
go build ./...
go test ./...
```

Expected: clean build, all tests pass.

- [ ] **Step 3: Commit**

```bash
git add controlplane/cmd/dlh-controlplane/main.go
git commit -m "feat(controlplane): wire k8s + minio + auth + handlers in main"
```

---

## Task 12: React UI scaffold + generated client

**Files:**
- Create: `controlplane/web/package.json`, `vite.config.ts`, `tsconfig.json`, `tailwind.config.js`, `postcss.config.js`, `index.html`
- Create: `controlplane/web/src/main.tsx`, `controlplane/web/src/App.tsx`, `controlplane/web/src/api/client.ts`, `controlplane/web/src/index.css`

- [ ] **Step 1: Initialize the Vite project**

```bash
cd /Users/allen/repo/dlh-test-fw-plan15/controlplane
mkdir -p web/src/api web/src/pages web/src/components
cd web
```

- [ ] **Step 2: Write web/package.json**

```json
{
  "name": "dlh-controlplane-web",
  "private": true,
  "version": "0.1.0",
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "tsc -b && vite build",
    "preview": "vite preview",
    "openapi-typescript": "openapi-typescript"
  },
  "dependencies": {
    "react": "^18.3.1",
    "react-dom": "^18.3.1",
    "react-router-dom": "^6.26.2",
    "openapi-fetch": "^0.13.0"
  },
  "devDependencies": {
    "@types/react": "^18.3.5",
    "@types/react-dom": "^18.3.0",
    "@vitejs/plugin-react": "^4.3.1",
    "autoprefixer": "^10.4.20",
    "openapi-typescript": "^7.4.0",
    "postcss": "^8.4.45",
    "tailwindcss": "^3.4.10",
    "typescript": "^5.5.4",
    "vite": "^5.4.6"
  }
}
```

- [ ] **Step 3: Write web/vite.config.ts**

```ts
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
  server: {
    proxy: {
      "/api": "http://localhost:8080",
      "/healthz": "http://localhost:8080",
    },
  },
});
```

- [ ] **Step 4: Write web/tsconfig.json**

```json
{
  "compilerOptions": {
    "target": "ES2022",
    "lib": ["ES2022", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "moduleResolution": "Bundler",
    "jsx": "react-jsx",
    "strict": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "noFallthroughCasesInSwitch": true,
    "esModuleInterop": true,
    "skipLibCheck": true,
    "resolveJsonModule": true,
    "isolatedModules": true,
    "useDefineForClassFields": true
  },
  "include": ["src"]
}
```

- [ ] **Step 5: Write web/tailwind.config.js + postcss.config.js**

```js
/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: { extend: {} },
  plugins: [],
};
```

```js
export default {
  plugins: { tailwindcss: {}, autoprefixer: {} },
};
```

- [ ] **Step 6: Write web/index.html**

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>dlh-controlplane</title>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
```

- [ ] **Step 7: Write web/src/index.css**

```css
@tailwind base;
@tailwind components;
@tailwind utilities;
```

- [ ] **Step 8: Write web/src/main.tsx**

```tsx
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import "./index.css";
import App from "./App";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <BrowserRouter>
      <App />
    </BrowserRouter>
  </StrictMode>
);
```

- [ ] **Step 9: Write web/src/App.tsx** (stub — pages added in Task 13)

```tsx
import { Routes, Route, Link } from "react-router-dom";

export default function App() {
  return (
    <div className="min-h-screen bg-slate-50 text-slate-900">
      <header className="border-b border-slate-200 bg-white">
        <nav className="mx-auto flex max-w-6xl gap-4 px-6 py-3 text-sm">
          <Link to="/" className="font-semibold">dlh-controlplane</Link>
          <Link to="/scenarios" className="text-slate-600 hover:text-slate-900">Scenarios</Link>
          <Link to="/runs" className="text-slate-600 hover:text-slate-900">Runs</Link>
        </nav>
      </header>
      <main className="mx-auto max-w-6xl px-6 py-8">
        <Routes>
          <Route path="/" element={<Home />} />
          <Route path="/scenarios" element={<Placeholder name="Scenarios" />} />
          <Route path="/runs" element={<Placeholder name="Runs" />} />
          <Route path="/runs/:id" element={<Placeholder name="Run detail" />} />
        </Routes>
      </main>
    </div>
  );
}

function Home() {
  return <p>Phase B viewer. Pick Scenarios or Runs above.</p>;
}

function Placeholder({ name }: { name: string }) {
  return <p>{name} — pending Task 13 implementation.</p>;
}
```

- [ ] **Step 10: Write web/src/api/client.ts** (will use generated types from Task 12 Step 12)

```ts
import createClient from "openapi-fetch";
import type { paths } from "./gen";

// In dev, Vite proxies /api; in prod (embedded), origin is the same as the page.
export const api = createClient<paths>({ baseUrl: "" });
```

- [ ] **Step 11: Install dependencies + generate types**

```bash
cd /Users/allen/repo/dlh-test-fw-plan15/controlplane/web
# Use pnpm if available; otherwise npm.
pnpm install || npm install
pnpm openapi-typescript ../api/openapi.yaml -o src/api/gen.ts || npx openapi-typescript ../api/openapi.yaml -o src/api/gen.ts
```

Expected: `web/node_modules/` populated; `web/src/api/gen.ts` created.

- [ ] **Step 12: Verify the UI builds**

```bash
pnpm build || npm run build
```

Expected: `web/dist/index.html` + assets created.

- [ ] **Step 13: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw-plan15
# Lockfile name depends on package manager — include whichever exists.
git add controlplane/web/package.json \
        controlplane/web/pnpm-lock.yaml controlplane/web/package-lock.json 2>/dev/null || true
git add controlplane/web/vite.config.ts controlplane/web/tsconfig.json \
        controlplane/web/tailwind.config.js controlplane/web/postcss.config.js \
        controlplane/web/index.html controlplane/web/src/
git commit -m "feat(controlplane): Vite + React UI scaffold with generated OpenAPI client"
```

---

## Task 13: UI pages — Scenarios, Runs, Run detail

**Files:**
- Create: `controlplane/web/src/pages/ScenariosPage.tsx`
- Create: `controlplane/web/src/pages/RunsPage.tsx`
- Create: `controlplane/web/src/pages/RunDetailPage.tsx`
- Create: `controlplane/web/src/components/StatusBadge.tsx`
- Modify: `controlplane/web/src/App.tsx`

- [ ] **Step 1: Write web/src/components/StatusBadge.tsx**

```tsx
const colors: Record<string, string> = {
  Pending: "bg-slate-200 text-slate-800",
  Running: "bg-blue-100 text-blue-800",
  Succeeded: "bg-emerald-100 text-emerald-800",
  Failed: "bg-rose-100 text-rose-800",
  Error: "bg-rose-100 text-rose-800",
  Unknown: "bg-slate-100 text-slate-700",
};

export function StatusBadge({ status }: { status: string }) {
  return (
    <span className={`inline-block rounded px-2 py-0.5 text-xs font-medium ${colors[status] ?? colors.Unknown}`}>
      {status}
    </span>
  );
}
```

- [ ] **Step 2: Write web/src/pages/ScenariosPage.tsx**

```tsx
import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/gen";

type Scenario = components["schemas"]["Scenario"];

export function ScenariosPage() {
  const [items, setItems] = useState<Scenario[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api.GET("/api/scenarios", {}).then(({ data, error }) => {
      if (error) setError(JSON.stringify(error));
      else setItems(data?.items ?? []);
    });
  }, []);

  if (error) return <p className="text-rose-700">Error: {error}</p>;
  if (!items) return <p>Loading…</p>;
  return (
    <section>
      <h1 className="mb-4 text-xl font-semibold">Scenarios</h1>
      <ul className="grid grid-cols-1 gap-3 md:grid-cols-2 lg:grid-cols-3">
        {items.map((s) => (
          <li key={s.id} className="rounded border border-slate-200 bg-white p-4">
            <div className="font-medium">{s.displayName}</div>
            {s.targetType && <div className="text-xs text-slate-500">{s.targetType}</div>}
            {s.description && <p className="mt-2 text-sm text-slate-700">{s.description}</p>}
          </li>
        ))}
      </ul>
    </section>
  );
}
```

- [ ] **Step 3: Write web/src/pages/RunsPage.tsx**

```tsx
import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../api/client";
import type { components } from "../api/gen";
import { StatusBadge } from "../components/StatusBadge";

type Run = components["schemas"]["Run"];

export function RunsPage() {
  const [items, setItems] = useState<Run[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api.GET("/api/runs", { params: { query: { limit: 100 } } }).then(({ data, error }) => {
      if (error) setError(JSON.stringify(error));
      else setItems(data?.items ?? []);
    });
  }, []);

  if (error) return <p className="text-rose-700">Error: {error}</p>;
  if (!items) return <p>Loading…</p>;
  return (
    <section>
      <h1 className="mb-4 text-xl font-semibold">Runs</h1>
      <table className="w-full border-collapse text-sm">
        <thead>
          <tr className="border-b border-slate-200 text-left text-slate-600">
            <th className="py-2">Scenario</th>
            <th>Status</th>
            <th>Started</th>
            <th>Score</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {items.map((r) => (
            <tr key={r.id} className="border-b border-slate-100">
              <td className="py-2">{r.scenario}</td>
              <td><StatusBadge status={r.status} /></td>
              <td className="text-slate-600">{new Date(r.startedAt).toLocaleString()}</td>
              <td>{r.score?.toFixed(2) ?? "—"}</td>
              <td><Link className="text-blue-600 hover:underline" to={`/runs/${r.id}`}>view</Link></td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}
```

- [ ] **Step 4: Write web/src/pages/RunDetailPage.tsx**

```tsx
import { useEffect, useState } from "react";
import { useParams } from "react-router-dom";
import { api } from "../api/client";
import type { components } from "../api/gen";
import { StatusBadge } from "../components/StatusBadge";

type RunDetail = components["schemas"]["RunDetail"];

export function RunDetailPage() {
  const { id } = useParams<{ id: string }>();
  const [run, setRun] = useState<RunDetail | null>(null);
  const [liveStatus, setLiveStatus] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!id) return;
    api.GET("/api/runs/{id}", { params: { path: { id } } }).then(({ data, error }) => {
      if (error) setError(JSON.stringify(error));
      else setRun(data!);
    });

    // SSE
    const es = new EventSource(`/api/runs/${id}/events`);
    const onEvent = (e: MessageEvent) => {
      try {
        const data = JSON.parse(e.data);
        if (data.phase) setLiveStatus(data.phase);
      } catch {}
    };
    es.addEventListener("snapshot", onEvent);
    es.addEventListener("MODIFIED", onEvent);
    es.addEventListener("ADDED", onEvent);
    es.addEventListener("DELETED", onEvent);
    return () => es.close();
  }, [id]);

  if (error) return <p className="text-rose-700">Error: {error}</p>;
  if (!run) return <p>Loading…</p>;
  const status = liveStatus ?? run.status;
  return (
    <section className="space-y-6">
      <header className="flex items-baseline gap-3">
        <h1 className="text-xl font-semibold">{run.id}</h1>
        <StatusBadge status={status} />
      </header>
      <div>
        <h2 className="mb-2 font-medium">Scenario</h2>
        <p className="text-sm text-slate-700">{run.scenario}</p>
      </div>
      {run.steps && (
        <div>
          <h2 className="mb-2 font-medium">Steps</h2>
          <ul className="space-y-1 text-sm">
            {run.steps.map((s, i) => (
              <li key={i} className="flex justify-between border-b border-slate-100 py-1">
                <span>{s.name}</span>
                <span className="text-slate-600">{s.phase}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
      {run.verdict && (
        <div>
          <h2 className="mb-2 font-medium">Verdict</h2>
          <pre className="overflow-auto rounded border border-slate-200 bg-slate-50 p-3 text-xs">
            {JSON.stringify(run.verdict, null, 2)}
          </pre>
        </div>
      )}
    </section>
  );
}
```

- [ ] **Step 5: Update web/src/App.tsx to wire the pages**

```tsx
import { Routes, Route, Link } from "react-router-dom";
import { ScenariosPage } from "./pages/ScenariosPage";
import { RunsPage } from "./pages/RunsPage";
import { RunDetailPage } from "./pages/RunDetailPage";

export default function App() {
  return (
    <div className="min-h-screen bg-slate-50 text-slate-900">
      <header className="border-b border-slate-200 bg-white">
        <nav className="mx-auto flex max-w-6xl gap-4 px-6 py-3 text-sm">
          <Link to="/" className="font-semibold">dlh-controlplane</Link>
          <Link to="/scenarios" className="text-slate-600 hover:text-slate-900">Scenarios</Link>
          <Link to="/runs" className="text-slate-600 hover:text-slate-900">Runs</Link>
        </nav>
      </header>
      <main className="mx-auto max-w-6xl px-6 py-8">
        <Routes>
          <Route path="/" element={<RunsPage />} />
          <Route path="/scenarios" element={<ScenariosPage />} />
          <Route path="/runs" element={<RunsPage />} />
          <Route path="/runs/:id" element={<RunDetailPage />} />
        </Routes>
      </main>
    </div>
  );
}
```

- [ ] **Step 6: Build the UI**

```bash
cd /Users/allen/repo/dlh-test-fw-plan15/controlplane/web
pnpm build || npm run build
```

Expected: clean build; `dist/` populated.

- [ ] **Step 7: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw-plan15
git add controlplane/web/src/
git commit -m "feat(controlplane/web): Scenarios + Runs + Run detail pages with SSE"
```

---

## Task 14: Embed UI in the Go binary

**Files:**
- Create: `controlplane/internal/api/ui.go`
- Modify: `controlplane/internal/api/server.go`

- [ ] **Step 1: Write controlplane/internal/api/ui.go**

```go
package api

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// Built by `make ui-build` before `go build`. The embed directive includes
// the entire dist/ tree.
//
//go:embed all:dist
var uiFS embed.FS

// UIHandler returns a handler that serves the embedded SPA, falling back
// to index.html for any unknown path (client-side routing).
func UIHandler() http.Handler {
	sub, err := fs.Sub(uiFS, "dist")
	if err != nil {
		// dist may be absent during initial development; serve a 404 stub.
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "UI bundle not built — run `make ui-build`", http.StatusNotFound)
		})
	}
	staticFS := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Don't fall back for /api or /healthz; those are handled elsewhere.
		if strings.HasPrefix(r.URL.Path, "/api") || strings.HasPrefix(r.URL.Path, "/healthz") {
			http.NotFound(w, r)
			return
		}
		// If the file exists in dist, serve it; otherwise serve index.html.
		clean := strings.TrimPrefix(r.URL.Path, "/")
		if clean == "" {
			clean = "index.html"
		}
		if _, err := fs.Stat(sub, clean); err == nil {
			staticFS.ServeHTTP(w, r)
			return
		}
		// SPA fallback.
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		staticFS.ServeHTTP(w, r2)
	})
}
```

- [ ] **Step 2: Create the dist placeholder so `go build` doesn't fail before `make ui-build`**

```bash
cd /Users/allen/repo/dlh-test-fw-plan15/controlplane/internal/api
mkdir -p dist
echo '<!doctype html><title>dlh</title><p>UI not built yet.</p>' > dist/index.html
```

Actually, the embed target should reference the real build output. Use a different approach: don't embed `internal/api/dist`; embed `web/dist` via a symlink or copy step. Better: have the Makefile copy `web/dist` into `internal/api/dist` before `go build`.

Update controlplane/Makefile (`ui-build` target) to also copy:

```makefile
ui-build: ui-install
	cd web && pnpm build
	rm -rf internal/api/dist
	cp -R web/dist internal/api/dist
```

`.gitignore` exclusion: add `internal/api/dist/` to `controlplane/.gitignore` so the embedded copy never lands in git.

- [ ] **Step 3: Update controlplane/.gitignore**

```
bin/
web/node_modules/
web/dist/
internal/api/dist/
*.tmp
```

- [ ] **Step 4: Modify controlplane/internal/api/server.go to mount the UI handler at /**

In `NewRouter`, after mounting `/api` and the SSE route, add:

```go
r.Handle("/*", UIHandler())
```

This catch-all serves SPA assets for any non-/api, non-/healthz path.

- [ ] **Step 5: Build everything and smoke-test**

```bash
cd /Users/allen/repo/dlh-test-fw-plan15/controlplane
make ui-build
go build ./...
```

Expected: `bin/dlh-controlplane` builds; `internal/api/dist/index.html` exists locally but is git-ignored.

- [ ] **Step 6: Commit**

```bash
git add controlplane/internal/api/ui.go controlplane/internal/api/server.go \
        controlplane/Makefile controlplane/.gitignore
git commit -m "feat(controlplane): embed web/dist as SPA assets via go:embed

ui-build target copies web/dist into internal/api/dist before go build;
the copy is gitignored. UIHandler falls back to index.html for SPA
client-side routing while preserving /api and /healthz."
```

---

## Task 15: Dockerfile

**Files:**
- Create: `controlplane/Dockerfile`

- [ ] **Step 1: Write controlplane/Dockerfile**

```dockerfile
# syntax=docker/dockerfile:1.7

# --- UI build stage ---------------------------------------------------------
FROM node:20-alpine AS ui
WORKDIR /src
COPY web/package.json web/pnpm-lock.yaml* web/package-lock.json* ./web/
RUN cd web && \
    if [ -f pnpm-lock.yaml ]; then npm install -g pnpm && pnpm install --frozen-lockfile; \
    else npm ci; fi
COPY api ./api
COPY web ./web
RUN cd web && \
    if [ -f pnpm-lock.yaml ]; then pnpm openapi-typescript ../api/openapi.yaml -o src/api/gen.ts && pnpm build; \
    else npx openapi-typescript ../api/openapi.yaml -o src/api/gen.ts && npm run build; fi

# --- Go build stage --------------------------------------------------------
FROM golang:1.26-alpine AS gobuild
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=ui /src/web/dist ./internal/api/dist
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/dlh-controlplane ./cmd/dlh-controlplane

# --- Runtime stage ---------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot
USER nonroot
COPY --from=gobuild /out/dlh-controlplane /dlh-controlplane
EXPOSE 8080
ENTRYPOINT ["/dlh-controlplane"]
```

- [ ] **Step 2: Verify the image builds**

```bash
cd /Users/allen/repo/dlh-test-fw-plan15/controlplane
docker build -t ghcr.io/dlh/dlh-controlplane:0.1.0 .
```

Expected: image builds; final stage is distroless-static.

- [ ] **Step 3: Commit**

```bash
git add controlplane/Dockerfile
git commit -m "feat(controlplane): multi-stage Dockerfile (UI + Go + distroless)"
```

---

## Task 16: Kubernetes manifests in controlplane/deploy/

**Files:**
- Modify: `controlplane/deploy/.gitkeep` (delete after manifests exist)
- Create: `controlplane/deploy/serviceaccount.yaml`
- Create: `controlplane/deploy/role.yaml`
- Create: `controlplane/deploy/rolebinding.yaml`
- Create: `controlplane/deploy/roles-configmap.yaml`
- Create: `controlplane/deploy/deployment.yaml`
- Create: `controlplane/deploy/service.yaml`
- Create: `controlplane/deploy/ingress.yaml`

- [ ] **Step 1: Write serviceaccount.yaml**

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: dlh-controlplane
  namespace: dlh-test-fw
  labels:
    app.kubernetes.io/name: dlh-controlplane
    app.kubernetes.io/part-of: dlh-test-fw
```

- [ ] **Step 2: Write role.yaml** (scoped per spec §9.2)

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: dlh-controlplane
  namespace: dlh-test-fw
  labels:
    app.kubernetes.io/name: dlh-controlplane
    app.kubernetes.io/part-of: dlh-test-fw
rules:
  - apiGroups: ["argoproj.io"]
    resources: ["workflows"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["argoproj.io"]
    resources: ["workflowtemplates"]
    verbs: ["get", "list"]
  - apiGroups: [""]
    resources: ["configmaps"]
    verbs: ["get", "list", "watch"]
    resourceNames: ["dlh-roles"]
```

- [ ] **Step 3: Write rolebinding.yaml**

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: dlh-controlplane
  namespace: dlh-test-fw
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: dlh-controlplane
subjects:
  - kind: ServiceAccount
    name: dlh-controlplane
    namespace: dlh-test-fw
```

- [ ] **Step 4: Write roles-configmap.yaml**

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: dlh-roles
  namespace: dlh-test-fw
  labels:
    app.kubernetes.io/part-of: dlh-test-fw
data:
  bindings.yaml: |
    # Map OIDC groups to roles. Replace placeholder group names with your
    # IdP's actual groups before deploying. See spec §9.1.
    viewer: ["REPLACE-VIEWER-GROUP"]
    runner: ["REPLACE-RUNNER-GROUP"]
    admin: ["REPLACE-ADMIN-GROUP"]
```

- [ ] **Step 5: Write deployment.yaml**

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: dlh-controlplane
  namespace: dlh-test-fw
  labels:
    app.kubernetes.io/name: dlh-controlplane
    app.kubernetes.io/part-of: dlh-test-fw
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: dlh-controlplane
  template:
    metadata:
      labels:
        app.kubernetes.io/name: dlh-controlplane
        app.kubernetes.io/part-of: dlh-test-fw
    spec:
      serviceAccountName: dlh-controlplane
      securityContext:
        runAsNonRoot: true
        seccompProfile:
          type: RuntimeDefault
      containers:
        - name: dlh-controlplane
          image: ghcr.io/dlh/dlh-controlplane:0.1.0
          imagePullPolicy: Never  # local minikube convention; flip for prod
          ports:
            - name: http
              containerPort: 8080
          env:
            - name: DLH_LISTEN_ADDR
              value: ":8080"
            - name: DLH_K8S_NAMESPACE
              value: "dlh-test-fw"
            - name: DLH_ROLES_NAMESPACE
              value: "dlh-test-fw"
            - name: DLH_ROLES_CONFIGMAP
              value: "dlh-roles"
            - name: DLH_MINIO_ENDPOINT
              value: "dlh-minio.dlh-test-fw.svc.cluster.local:9000"
            - name: DLH_MINIO_BUCKET
              value: "artifacts"
            - name: DLH_MINIO_ACCESS_KEY
              valueFrom:
                secretKeyRef:
                  name: dlh-minio-credentials
                  key: rootUser
            - name: DLH_MINIO_SECRET_KEY
              valueFrom:
                secretKeyRef:
                  name: dlh-minio-credentials
                  key: rootPassword
            - name: DLH_OIDC_ISSUER_URL
              value: "REPLACE-OIDC-ISSUER"
            - name: DLH_OIDC_CLIENT_ID
              value: "REPLACE-OIDC-CLIENT-ID"
          resources:
            requests: { cpu: 50m, memory: 64Mi }
            limits:   { cpu: 500m, memory: 256Mi }
          readinessProbe:
            httpGet: { path: /readyz, port: http }
            initialDelaySeconds: 2
            periodSeconds: 5
          livenessProbe:
            httpGet: { path: /healthz, port: http }
            initialDelaySeconds: 10
            periodSeconds: 15
```

- [ ] **Step 6: Write service.yaml**

```yaml
apiVersion: v1
kind: Service
metadata:
  name: dlh-controlplane
  namespace: dlh-test-fw
  labels:
    app.kubernetes.io/name: dlh-controlplane
spec:
  selector:
    app.kubernetes.io/name: dlh-controlplane
  ports:
    - name: http
      port: 80
      targetPort: http
```

- [ ] **Step 7: Write ingress.yaml**

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: dlh-controlplane
  namespace: dlh-test-fw
  annotations:
    # Allow long-lived SSE connections.
    nginx.ingress.kubernetes.io/proxy-read-timeout: "3600"
    nginx.ingress.kubernetes.io/proxy-send-timeout: "3600"
spec:
  ingressClassName: nginx
  rules:
    - host: dlh.REPLACE-DOMAIN
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: dlh-controlplane
                port:
                  number: 80
```

- [ ] **Step 8: Remove the placeholder + validate**

```bash
cd /Users/allen/repo/dlh-test-fw-plan15
git rm controlplane/deploy/.gitkeep
kubeconform -skip CustomResourceDefinition -strict -summary \
  -schema-location default \
  -schema-location 'https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json' \
  controlplane/deploy/*.yaml
```

Expected: 7 resources valid.

- [ ] **Step 9: Commit**

```bash
git add controlplane/deploy/
git commit -m "feat(controlplane): k8s manifests for ServiceAccount + Role + Deployment + Service + Ingress + roles ConfigMap

Scoped per spec §9.2 — Workflow + WorkflowTemplate read only; ConfigMap
get restricted to dlh-roles by resourceNames. SSE-friendly ingress
timeouts. REPLACE-* placeholders for OIDC issuer/client + viewer/runner/
admin groups + ingress domain."
```

---

## Task 17: Activate the dlh-controlplane Argo CD Application

Plan 14 left auto-sync OFF on the placeholder. Now that real manifests exist, enable auto-sync + add the resources-finalizer.

**Files:**
- Modify: `argocd/apps/dlh-controlplane.yaml`
- Modify: `argocd/appset/dlh-platform.yaml`

- [ ] **Step 1: Edit argocd/apps/dlh-controlplane.yaml**

Replace the entire file with:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: dlh-controlplane
  namespace: argocd
  labels:
    app.kubernetes.io/part-of: dlh-test-fw
  finalizers:
    - resources-finalizer.argocd.argoproj.io
spec:
  project: dlh-test-fw
  source:
    repoURL: https://github.com/REPLACE-OWNER/dlh-test-fw.git
    targetRevision: main
    path: controlplane/deploy
    directory:
      recurse: false
  destination:
    server: https://kubernetes.default.svc
    namespace: dlh-test-fw
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=false
      - ServerSideApply=true
      - ApplyOutOfSyncOnly=true
    retry:
      limit: 5
      backoff:
        duration: 5s
        factor: 2
        maxDuration: 3m
  revisionHistoryLimit: 10
```

- [ ] **Step 2: Edit argocd/appset/dlh-platform.yaml**

Find the list element with `appName: dlh-controlplane` and change `autoSync: "false"` to `autoSync: "true"`. The template body has no conditional on autoSync today, so this is documentation-only — but update for consistency.

- [ ] **Step 3: Validate**

```bash
kubeconform -skip CustomResourceDefinition -strict -summary \
  -schema-location default \
  -schema-location 'https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json' \
  argocd/apps/dlh-controlplane.yaml argocd/appset/dlh-platform.yaml
```

Expected: 2 valid.

- [ ] **Step 4: Commit**

```bash
git add argocd/apps/dlh-controlplane.yaml argocd/appset/dlh-platform.yaml
git commit -m "feat(argocd): activate dlh-controlplane Application — auto-sync + finalizer

Now that controlplane/deploy/ has real manifests (Task 16), enable
automated sync + selfHeal + prune + finalizer. ApplicationSet entry
updated for documentation parity."
```

---

## Task 18: CI extension for controlplane

**Files:**
- Modify: `.github/workflows/ci.yml`

- [ ] **Step 1: Add a controlplane job**

Append a new top-level job to `.github/workflows/ci.yml`:

```yaml
  controlplane:
    runs-on: ubuntu-latest
    timeout-minutes: 15
    defaults:
      run:
        working-directory: controlplane
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: controlplane/go.mod
          cache: true
          cache-dependency-path: controlplane/go.sum
      - name: go vet
        run: go vet ./...
      - name: go test
        run: go test ./...
      - uses: pnpm/action-setup@v4
        with:
          version: 9
      - uses: actions/setup-node@v4
        with:
          node-version: "20"
          cache: pnpm
          cache-dependency-path: controlplane/web/pnpm-lock.yaml
      - name: ui install
        run: pnpm install --frozen-lockfile
        working-directory: controlplane/web
      - name: ui build
        run: pnpm openapi-typescript ../api/openapi.yaml -o src/api/gen.ts && pnpm build
        working-directory: controlplane/web
```

- [ ] **Step 2: Lint the workflow**

```bash
cd /Users/allen/repo/dlh-test-fw-plan15
python3 -c "import sys, yaml; list(yaml.safe_load_all(open('.github/workflows/ci.yml')))" && echo "valid YAML"
```

Expected: `valid YAML`.

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: add controlplane job (go vet/test + ui build)"
```

---

## Task 19: Smoke-test against minikube

This task confirms the new binary actually runs in the existing minikube cluster. It does NOT push to main yet — issues found here get fixed before merge.

**Files:** None modified by tasks; commits only happen if smoke surfaces bugs to fix.

- [ ] **Step 1: Confirm minikube is up**

```bash
minikube status
```

Expected: cluster Ready. If not, run `scripts/minikube-up.sh && scripts/platform-up.sh` from main worktree.

- [ ] **Step 2: Build + reload image into minikube**

```bash
cd /Users/allen/repo/dlh-test-fw-plan15/controlplane
make reload-minikube
```

Expected: image rebuilt and loaded; old containers killed.

- [ ] **Step 3: Apply the new manifests + roles ConfigMap (manual, since Argo CD isn't installed in local minikube)**

```bash
cd /Users/allen/repo/dlh-test-fw-plan15
# Patch the OIDC env vars to use auth-disabled mode for local smoke.
# In a real cluster, Argo CD would apply these from controlplane/deploy/.
kubectl -n dlh-test-fw apply -f controlplane/deploy/
kubectl -n dlh-test-fw patch deployment dlh-controlplane --type=json -p='[
  {"op": "add", "path": "/spec/template/spec/containers/0/env/-", "value": {"name": "DLH_AUTH_DISABLED", "value": "true"}}
]'
kubectl -n dlh-test-fw rollout status deployment/dlh-controlplane --timeout=120s
```

Expected: rollout succeeds.

- [ ] **Step 4: Smoke the HTTP endpoints**

```bash
# Get a fresh ephemeral port-forward
kubectl -n dlh-test-fw port-forward svc/dlh-controlplane 18080:80 &
PF=$!
sleep 2
curl -fsS localhost:18080/healthz
curl -fsS localhost:18080/readyz
curl -fsS -H "Authorization: Bearer fake:tester:tester@example.com:dlh-admins" localhost:18080/api/scenarios | head -c 400
curl -fsS -H "Authorization: Bearer fake:tester:tester@example.com:dlh-admins" localhost:18080/api/runs | head -c 400
kill $PF
```

Expected: `ok` from health endpoints; JSON arrays (possibly empty `{"items":[]}`) from the API endpoints.

- [ ] **Step 5: Browser smoke (optional but recommended)**

Open `http://localhost:18080/` after a fresh port-forward and confirm the Scenarios and Runs pages render without console errors.

- [ ] **Step 6: If any bugs surface, fix them, commit, and re-smoke.**

Each bugfix is its own commit (`fix(controlplane): <one-line>`). Do not batch.

- [ ] **Step 7: Tear down the local-smoke patches**

```bash
kubectl -n dlh-test-fw delete deployment dlh-controlplane
kubectl -n dlh-test-fw delete -f controlplane/deploy/
```

(These will be re-applied in the real cluster by Argo CD; local-minikube doesn't need them persisting.)

---

## Task 20: FINDINGS + CLAUDE.md + README

**Files:**
- Modify: `docs/FINDINGS.md`
- Modify: `CLAUDE.md`
- Modify: `README.md`

- [ ] **Step 1: Append Plan 15 section to docs/FINDINGS.md**

Use Edit to append after the existing Plan 14 carry-forward paragraph:

```markdown

---

## Plan 15 — controlplane Phase B (read-only) (2026-05-21)

### What landed

- `controlplane/` Go module: chi-routed HTTP server with embedded React UI (`go:embed`), OIDC auth + role ConfigMap RBAC, in-cluster k8s informer for Workflows, Workflow + WorkflowTemplate lister, MinIO report.json reader, SSE event stream.
- `controlplane/deploy/`: ServiceAccount + scoped Role + RoleBinding + Deployment + Service + Ingress + roles ConfigMap.
- `argocd/apps/dlh-controlplane.yaml`: auto-sync enabled (Plan 14 left it manual).
- CI extended: new `controlplane` job (go vet + go test + UI build).

### Operational pitfalls discovered

1. **oapi-codegen response/request type names depend on output settings.** The `strict-server: true` configuration generates names like `ListScenarios200JSONResponse` and `GetScenario404Response`. If you tweak codegen options, the field names of inline anonymous structs (Parameters, Steps) can also change. The handler code MUST be reconciled with the actual generated names after every codegen regen; trust the compiler, not the docs.

2. **`go:embed all:dist` requires `dist/` to exist at compile time.** The Makefile copies `web/dist` into `internal/api/dist` before `go build`. CI must build the UI before the Go test step or `go vet` fails. Local dev: run `make ui-build` once when you change `web/`.

3. **SSE through nginx-ingress needs longer read/send timeouts.** Default 60s closes idle connections mid-stream. The Ingress manifest pins `proxy-read-timeout: 3600` and `proxy-send-timeout: 3600` plus `X-Accel-Buffering: no` from the handler.

4. **DLH_AUTH_DISABLED is a footgun.** It bypasses OIDC and lets any caller pass `fake:...` tokens. The flag is intended for `kubectl port-forward`-style local smoke only. Never wire it into a deployment manifest that ships to a shared cluster. The roles ConfigMap still applies — a fake token's group claim still resolves through the same Resolve() path.

5. **MinIO ReportReader path lookup is best-effort.** Phase B walks all objects under `<workflowName>/` and matches `/verdict/report.json`. If the artifact-repository convention changes (e.g., a new sub-key from Argo's artifact path templating), the reader returns ErrReportNotFound silently. Phase C's manifest.json indirection makes this deterministic.

### Carry-forward for Phase C

- Add `POST /api/runs` + manifest writes + index objects.
- Add `/internal/chaos` endpoint (called by Workflow steps).
- Add watchdog reconciler.
- Modify the 10 existing WorkflowTemplates to call `/internal/chaos` instead of inlining chaos CRs.
- Deprecate `run-scenario.sh` as a shim around `dlh run`.
- Decide on IdP for the first real environment that consumes this controlplane.
```

- [ ] **Step 2: Append CLAUDE.md section about the controlplane**

Use Edit to insert before the existing `## Image build + minikube reload` section (which is now at line 141 after Plan 14):

```markdown
## dlh-controlplane (Phase B onwards)

After Plan 15, `controlplane/` is a Go service that exposes the framework
cluster's runtime state via REST + an embedded React UI. Phase B is
read-only — `run-scenario.sh` still submits scenarios. Phase C will
add submission.

### Layout

- `cmd/dlh-controlplane/main.go` — entry
- `api/openapi.yaml` — single source of truth (do not hand-edit handlers' request/response types; regenerate)
- `internal/{api,auth,config,k8s,minio,model}/` — backend packages
- `web/` — Vite + React + Tailwind SPA, generated client from openapi-typescript
- `deploy/` — k8s manifests (plain YAML — Argo CD `directory:` source)
- `Makefile` — `codegen`, `ui-build`, `build`, `image`, `reload-minikube`

### Local dev

```
make codegen        # regenerate from OpenAPI
make ui-build       # build the React app and copy into internal/api/dist
DLH_AUTH_DISABLED=true go run ./cmd/dlh-controlplane
```

Fake tokens for `DLH_AUTH_DISABLED=true` mode:
`Authorization: Bearer fake:<sub>:<email>:<group1,group2>`

### Auth model

- OIDC bearer tokens verified against `DLH_OIDC_ISSUER_URL`.
- Groups claim (default `groups`) drives role binding via the
  `dlh-roles` ConfigMap (`bindings.yaml` data key).
- Roles: viewer < runner < admin.
- Phase B only requires viewer for all read endpoints.
```

- [ ] **Step 3: Append Plan 15 row to README.md plan table**

```markdown
| Plan 14 | `<hash>` | Argo CD platform lifecycle ... |
| Plan 15 | `XXXXXXX` | dlh-controlplane Phase B (read-only) — Go service + embedded React UI + OIDC auth + scoped RBAC + Workflow informer + MinIO report.json reader + SSE event stream; `controlplane/deploy/` manifests; `dlh-controlplane` Argo CD Application activated |
```

(The `XXXXXXX` placeholder is backfilled at merge time, same pattern as Plan 14.)

- [ ] **Step 4: Commit**

```bash
git add docs/FINDINGS.md CLAUDE.md README.md
git commit -m "docs: Plan 15 — FINDINGS + CLAUDE.md + README updates"
```

---

## Task 21: Render verification + merge to main

**Files:** None modified by edits.

- [ ] **Step 1: Push feature branch + verify CI**

```bash
cd /Users/allen/repo/dlh-test-fw-plan15
git push -u origin feat/plan15-controlplane-skeleton
gh run watch
```

Expected: all CI jobs (`helm`, `go`, `shellcheck`, `kubeconform`, `controlplane`) pass. If `controlplane` fails, fix and re-push before merging.

- [ ] **Step 2: Confirm placeholder count in new manifests**

```bash
grep -rn "REPLACE-" controlplane/deploy/ argocd/apps/dlh-controlplane.yaml
```

Expected: at least `REPLACE-VIEWER-GROUP`, `REPLACE-RUNNER-GROUP`, `REPLACE-ADMIN-GROUP`, `REPLACE-OIDC-ISSUER`, `REPLACE-OIDC-CLIENT-ID`, `REPLACE-DOMAIN`, `REPLACE-OWNER`. Document the new placeholders in the bootstrap doc (next step) or in a follow-up.

- [ ] **Step 3: Append new REPLACE-* tokens to docs/operations/bootstrap-via-argocd.md**

Use Edit. In the `sed` block under "Step 2 — Substitute placeholders", add lines for the new tokens:

```bash
# Controlplane manifests
sed -i "s|REPLACE-VIEWER-GROUP|dlh-viewers|g" controlplane/deploy/roles-configmap.yaml
sed -i "s|REPLACE-RUNNER-GROUP|dlh-runners|g" controlplane/deploy/roles-configmap.yaml
sed -i "s|REPLACE-ADMIN-GROUP|dlh-admins|g" controlplane/deploy/roles-configmap.yaml
sed -i "s|REPLACE-OIDC-ISSUER|https://your-idp.example.com|g" controlplane/deploy/deployment.yaml
sed -i "s|REPLACE-OIDC-CLIENT-ID|dlh-controlplane|g" controlplane/deploy/deployment.yaml
sed -i "s|dlh.REPLACE-DOMAIN|dlh.$DOMAIN|g" controlplane/deploy/ingress.yaml
```

Commit:

```bash
git add docs/operations/bootstrap-via-argocd.md
git commit -m "docs(bootstrap): substitute controlplane REPLACE-* placeholders"
```

- [ ] **Step 4: Merge to main**

```bash
cd /Users/allen/repo/dlh-test-fw
git checkout main
git pull --ff-only origin main
git merge --no-ff feat/plan15-controlplane-skeleton -m "Merge feat/plan15-controlplane-skeleton: dlh-controlplane Phase B (read-only)

Introduces controlplane/ — a Go service with an embedded React UI that
exposes the framework cluster's WorkflowTemplates + Workflows + MinIO
verdict reports through an OIDC-authenticated REST API plus SSE event
stream. Read-only — scenario submission stays on run-scenario.sh until
Phase C. Activates the dlh-controlplane Argo CD Application reserved by
Plan 14.

Plan 15 of dlh-test-fw. See:
- docs/superpowers/specs/2026-05-21-dlh-controlplane-design.md
- docs/superpowers/plans/2026-05-21-02-controlplane-skeleton.md"
```

- [ ] **Step 5: Backfill README plan hash**

```bash
MERGE_HASH=$(git log --first-parent --format=%h -1)
sed -i "" "s|| Plan 15 | \`XXXXXXX\`|| Plan 15 | \`$MERGE_HASH\`|" README.md
git add README.md
git commit -m "docs(readme): backfill Plan 15 merge hash"
```

- [ ] **Step 6: Push main + verify CI**

```bash
git push origin main
gh run list --branch main --limit 1
```

Wait up to 3 minutes; expect `success`.

- [ ] **Step 7: Worktree cleanup**

```bash
git worktree remove ../dlh-test-fw-plan15
git branch -d feat/plan15-controlplane-skeleton
git push origin --delete feat/plan15-controlplane-skeleton
```

- [ ] **Step 8: Verify final state**

```bash
git log --first-parent --oneline -3
ls controlplane/
grep "^| Plan 15" README.md
```

Expected: merge commit is the most recent first-parent entry; `controlplane/` directory exists with go.mod + cmd/ + internal/ + web/ + deploy/ + Dockerfile + Makefile; README has the backfilled Plan 15 row.

---

## Done

Plan 15 lands the read-only controlplane skeleton. The framework cluster now has a GitOps-deployed service that any user with OIDC credentials can hit to view scenarios, runs, and verdicts — no `kubectl` required for reads. Phase C is next: submission, manifest writes, watchdog reconciler, WorkflowTemplate rewiring.
