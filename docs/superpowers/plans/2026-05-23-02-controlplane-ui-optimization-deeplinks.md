# Controlplane UI Optimization — Plan 2 (Run-detail deep-linking) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver Workstream 2 of `docs/superpowers/specs/2026-05-23-controlplane-ui-optimization-design.md` — Run detail deep-links out to the Argo Workflows UI and Grafana, with the URLs assembled by the backend from configurable base URLs and rendered as header buttons (hidden when unset).

**Architecture:** A new pure `internal/links` Go package assembles the URLs (Argo workflow path; Grafana run + per-target-type dashboards with the run's time window and `dlh_scenario` var). Two new env knobs feed it. The `GetRun` handler enriches the existing `RunDetail` (`argoUrl` added to the OpenAPI contract, `grafanaUrls` already present) in both its workflow and manifest-fallback paths. The frontend renders one button per supplied URL. No new routes.

**Tech Stack:** Go 1.26 + oapi-codegen + chi (backend); React 18 + openapi-typescript (frontend); Helm/plain-YAML deploy. Tested with `go test` + `pnpm build` + Playwright (dev-time).

---

## Conventions for this plan

- **Depends on Plan 1** (`2026-05-23-01-controlplane-ui-optimization-web.md`) being merged to `main` first — Task 6 edits the redesigned `RunDetailPage.tsx` header from Plan 1.
- **Worktree:** create off `main` after Plan 1 merges — `git worktree add ../dlh-test-fw-deeplinks -b feat/controlplane-deeplinks main` (or the native worktree tool). Merge `--no-ff` at the end.
- Go commands run from `controlplane/`. Web commands from `controlplane/web/`.
- **OpenAPI is the source of truth.** After editing `api/openapi.yaml`, run `make codegen` — it regenerates `internal/api/gen/{types,server}.gen.go` **and** `web/src/api/gen.ts`. Never hand-edit generated files.
- **Per-task gate:** `go build ./... && go test ./...` for Go tasks; `pnpm build` for web tasks.

### Verified backend facts

- `GetRun` (`internal/api/handlers.go:79`) builds `detail := model.RunDetailFromWorkflow(wf)`, sets `detail.Verdict`, returns `gen.GetRun200JSONResponse(detail)`. A fallback path returns `runDetailFromManifest(*m)` when the Workflow CR is gone.
- `gen.RunDetail` fields (`internal/api/gen/types.gen.go`): `StartedAt time.Time`, `FinishedAt *time.Time`, `Scenario string`, `WorkflowName *string`, `GrafanaUrls *[]struct{ Label string \`json:"label"\`; Url string \`json:"url"\` }`. After Task 3 it also has `ArgoUrl *string`.
- `Deps` struct (`internal/api/server.go:21`) holds dependency configs (e.g. `AuthInfo AuthInfoConfig`); `cmd/dlh-controlplane/main.go` populates it from `cfg`.
- `cfg.K8sNamespace` (default `dlh-test-fw`) is the workflow namespace for Argo URLs.
- Dashboard UIDs in `dashboards/grafana/`: `dlh-run`, `dlh-mysql`, `dlh-kafka`, `dlh-doris`. The Grafana template var is `dlh_scenario` (FINDINGS #1).
- `config.Load` skips all required-field validation when `DLH_AUTH_DISABLED=true` — so config tests set that.

---

## Task 1: `internal/links` package — URL assembly (TDD, Go)

**Files:**
- Create: `controlplane/internal/links/links.go`
- Create: `controlplane/internal/links/links_test.go`

- [ ] **Step 1: Write the failing test**

```go
package links

import (
	"strconv"
	"testing"
	"time"
)

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestDeriveTargetType(t *testing.T) {
	cases := map[string]string{
		"mysql-pod-delete":         "mysql",
		"fixture-kafka-topic-seed": "kafka",
		"doris-be-network-loss":    "doris",
		"load-k6-run":              "generic",
	}
	for in, want := range cases {
		if got := DeriveTargetType(in); got != want {
			t.Errorf("DeriveTargetType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestArgoURL(t *testing.T) {
	if got := ArgoURL("", "ns", "wf"); got != "" {
		t.Errorf("empty base should yield empty, got %q", got)
	}
	if got := ArgoURL("https://argo.example.com", "ns", ""); got != "" {
		t.Errorf("empty workflow should yield empty, got %q", got)
	}
	got := ArgoURL("https://argo.example.com/", "dlh-test-fw", "mysql-pod-delete-20260523-130331")
	want := "https://argo.example.com/workflows/dlh-test-fw/mysql-pod-delete-20260523-130331"
	if got != want {
		t.Errorf("ArgoURL = %q, want %q", got, want)
	}
}

func TestGrafanaURLs(t *testing.T) {
	if got := GrafanaURLs("", "mysql-pod-delete", time.Now(), nil); got != nil {
		t.Errorf("empty base should yield nil, got %v", got)
	}

	start := time.Date(2026, 5, 23, 13, 3, 31, 0, time.UTC)
	end := time.Date(2026, 5, 23, 13, 7, 47, 0, time.UTC)
	fromMs := start.UnixMilli()
	toMs := end.UnixMilli()

	// mysql scenario, finished → run dashboard + mysql dashboard
	urls := GrafanaURLs("https://grafana.example.com/", "mysql-pod-delete", start, &end)
	if len(urls) != 2 {
		t.Fatalf("want 2 urls, got %d (%v)", len(urls), urls)
	}
	if urls[0].Label != "Run dashboard" {
		t.Errorf("first label = %q, want Run dashboard", urls[0].Label)
	}
	wantRun := "https://grafana.example.com/d/dlh-run/dlh-run?var-dlh_scenario=mysql-pod-delete&from=" +
		itoa(fromMs) + "&to=" + itoa(toMs)
	if urls[0].URL != wantRun {
		t.Errorf("run url = %q, want %q", urls[0].URL, wantRun)
	}
	if urls[1].Label != "MySQL dashboard" {
		t.Errorf("second label = %q, want MySQL dashboard", urls[1].Label)
	}
	if urls[1].URL != "https://grafana.example.com/d/dlh-mysql/dlh-mysql?var-dlh_scenario=mysql-pod-delete&from="+itoa(fromMs)+"&to="+itoa(toMs) {
		t.Errorf("mysql url wrong: %q", urls[1].URL)
	}

	// generic scenario, finished → only run dashboard
	gen := GrafanaURLs("https://grafana.example.com", "load-k6-run", start, &end)
	if len(gen) != 1 {
		t.Errorf("generic want 1 url, got %d", len(gen))
	}

	// running (no end) → to=now
	run := GrafanaURLs("https://grafana.example.com", "mysql-pod-delete", start, nil)
	if run[0].URL[len(run[0].URL)-7:] != "&to=now" {
		t.Errorf("running url should end with &to=now, got %q", run[0].URL)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd controlplane && go test ./internal/links/`
Expected: FAIL — package `links` has no `DeriveTargetType`/`ArgoURL`/`GrafanaURLs` (build error).

- [ ] **Step 3: Implement `internal/links/links.go`**

```go
// Package links assembles outbound deep-link URLs (Argo Workflows UI, Grafana
// dashboards) for a run. All functions are pure; empty base URLs yield no link
// so the feature degrades gracefully when unconfigured.
package links

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Config carries the per-environment base URLs + namespace used to build links.
type Config struct {
	ArgoBaseURL    string
	GrafanaBaseURL string
	Namespace      string
}

// NamedURL is a labeled outbound link.
type NamedURL struct {
	Label string
	URL   string
}

// These couple to dashboards/grafana/ — keep in sync (FINDINGS #1, #8).
const (
	runDashboardUID = "dlh-run"
	scenarioVar     = "dlh_scenario"
)

var targetDashboards = map[string]struct{ uid, label string }{
	"mysql": {"dlh-mysql", "MySQL dashboard"},
	"kafka": {"dlh-kafka", "Kafka dashboard"},
	"doris": {"dlh-doris", "Doris dashboard"},
}

// DeriveTargetType infers the engine from a scenario id (heuristic — mirrors the
// web-side deriveTargetType). Returns "generic" when nothing matches.
func DeriveTargetType(scenario string) string {
	switch {
	case strings.Contains(scenario, "mysql"):
		return "mysql"
	case strings.Contains(scenario, "kafka"):
		return "kafka"
	case strings.Contains(scenario, "doris"):
		return "doris"
	default:
		return "generic"
	}
}

// ArgoURL builds a link to the Argo Workflows UI for a workflow, or "" if base
// or workflowName is empty.
func ArgoURL(base, namespace, workflowName string) string {
	if base == "" || workflowName == "" {
		return ""
	}
	return fmt.Sprintf("%s/workflows/%s/%s", strings.TrimRight(base, "/"), namespace, workflowName)
}

// GrafanaURLs builds the run dashboard link plus a per-target-type dashboard link
// (when recognized), scoped to the run's time window. Returns nil if base is empty.
func GrafanaURLs(base, scenario string, start time.Time, end *time.Time) []NamedURL {
	if base == "" {
		return nil
	}
	b := strings.TrimRight(base, "/")
	fromMs := strconv.FormatInt(start.UnixMilli(), 10)
	toPart := "now"
	if end != nil {
		toPart = strconv.FormatInt(end.UnixMilli(), 10)
	}
	q := func(uid string) string {
		return fmt.Sprintf("%s/d/%s/%s?var-%s=%s&from=%s&to=%s", b, uid, uid, scenarioVar, scenario, fromMs, toPart)
	}
	urls := []NamedURL{{Label: "Run dashboard", URL: q(runDashboardUID)}}
	if d, ok := targetDashboards[DeriveTargetType(scenario)]; ok {
		urls = append(urls, NamedURL{Label: d.label, URL: q(d.uid)})
	}
	return urls
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd controlplane && go test ./internal/links/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add controlplane/internal/links/
git commit -m "feat(controlplane): links package — Argo + Grafana URL assembly"
```

---

## Task 2: Config — `DLH_ARGO_BASE_URL` + `DLH_GRAFANA_BASE_URL`

**Files:**
- Modify: `controlplane/internal/config/config.go`
- Modify: `controlplane/internal/config/config_test.go`

- [ ] **Step 1: Add the two fields to the `Config` struct**

In `controlplane/internal/config/config.go`, add to the `Config` struct (after `CIAudience string`):

```go
	// Optional deep-link base URLs. Empty = that link is omitted from RunDetail.
	ArgoBaseURL    string
	GrafanaBaseURL string
```

- [ ] **Step 2: Read them in `Load`**

In the `c := &Config{ ... }` literal (after the `CIAudience:` line), add:

```go
		ArgoBaseURL:    os.Getenv("DLH_ARGO_BASE_URL"),
		GrafanaBaseURL: os.Getenv("DLH_GRAFANA_BASE_URL"),
```

- [ ] **Step 3: Write the test (append to `config_test.go`)**

```go
func TestLoad_DeepLinkURLs(t *testing.T) {
	t.Setenv("DLH_AUTH_DISABLED", "true")
	t.Setenv("DLH_ARGO_BASE_URL", "https://argo.example.com")
	t.Setenv("DLH_GRAFANA_BASE_URL", "https://grafana.example.com")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if c.ArgoBaseURL != "https://argo.example.com" {
		t.Errorf("ArgoBaseURL = %q", c.ArgoBaseURL)
	}
	if c.GrafanaBaseURL != "https://grafana.example.com" {
		t.Errorf("GrafanaBaseURL = %q", c.GrafanaBaseURL)
	}
}

func TestLoad_DeepLinkURLs_DefaultEmpty(t *testing.T) {
	t.Setenv("DLH_AUTH_DISABLED", "true")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if c.ArgoBaseURL != "" || c.GrafanaBaseURL != "" {
		t.Errorf("expected empty deep-link URLs, got argo=%q grafana=%q", c.ArgoBaseURL, c.GrafanaBaseURL)
	}
}
```

- [ ] **Step 4: Run the test**

Run: `cd controlplane && go test ./internal/config/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add controlplane/internal/config/config.go controlplane/internal/config/config_test.go
git commit -m "feat(controlplane): DLH_ARGO_BASE_URL + DLH_GRAFANA_BASE_URL config"
```

---

## Task 3: OpenAPI — add `argoUrl` to `RunDetail` + regenerate

**Files:**
- Modify: `controlplane/api/openapi.yaml`
- Regenerate: `internal/api/gen/{types,server}.gen.go`, `web/src/api/gen.ts` (via `make codegen`)

- [ ] **Step 1: Add the `argoUrl` property**

In `controlplane/api/openapi.yaml`, inside the `RunDetail` schema's inline `properties:` block (the `allOf` → second object), add an `argoUrl` property next to `grafanaUrls`. Insert immediately before the `grafanaUrls:` key:

```yaml
            argoUrl:
              type: string
              description: "Deep link to the Argo Workflows UI for this run. Absent when DLH_ARGO_BASE_URL is unset."
```

- [ ] **Step 2: Regenerate Go + TS types**

Run: `cd controlplane && make codegen`
Expected: succeeds; `git status` shows modified `internal/api/gen/types.gen.go`, `internal/api/gen/server.gen.go` (possibly unchanged), and `web/src/api/gen.ts`.

- [ ] **Step 3: Verify the generated field exists**

Run: `cd controlplane && grep -n "ArgoUrl" internal/api/gen/types.gen.go`
Expected: a line `ArgoUrl *string \`json:"argoUrl,omitempty"\`` inside `RunDetail`.

Run: `grep -n "argoUrl" web/src/api/gen.ts`
Expected: `argoUrl?: string;` inside the RunDetail schema.

- [ ] **Step 4: Verify everything still builds**

```bash
cd controlplane
go build ./...
cd web && pnpm build
```
Expected: both PASS (no consumer of `ArgoUrl` yet — added in Task 4).

- [ ] **Step 5: Commit**

```bash
git add controlplane/api/openapi.yaml controlplane/internal/api/gen/ controlplane/web/src/api/gen.ts
git commit -m "feat(api): add RunDetail.argoUrl + regenerate types"
```

---

## Task 4: Wire `Links` into Deps + enrich `GetRun`

**Files:**
- Modify: `controlplane/internal/api/server.go`
- Modify: `controlplane/internal/api/handlers.go`
- Modify: `controlplane/cmd/dlh-controlplane/main.go`
- Test: `controlplane/internal/api/handlers_links_test.go`

- [ ] **Step 1: Add `Links` to the `Deps` struct**

In `controlplane/internal/api/server.go`, add the links import to the import block:

```go
	"github.com/dlh/dlh-test-fw/controlplane/internal/links"
```

(Match the existing module path used by the other `internal/...` imports in that file.) Then add a field to `type Deps struct`:

```go
	Links links.Config // Phase: deep-link base URLs (Argo/Grafana)
```

- [ ] **Step 2: Add the enrichment helper + call it in `GetRun` (both paths)**

In `controlplane/internal/api/handlers.go`, add the links import to the import block:

```go
	"github.com/dlh/dlh-test-fw/controlplane/internal/links"
```

Add this type alias + helper near `runDetailFromManifest` (the alias makes the anonymous `GrafanaUrls` element type addressable):

```go
// grafanaEntry aliases the anonymous element type of gen.RunDetail.GrafanaUrls
// so we can build the slice readably.
type grafanaEntry = struct {
	Label string `json:"label"`
	Url   string `json:"url"`
}

// addLinks enriches a RunDetail with Argo/Grafana deep links from configured
// base URLs. No-op for any link whose base URL is unset.
func (h *Handlers) addLinks(d *gen.RunDetail) {
	lc := h.deps.Links
	wfName := ""
	if d.WorkflowName != nil {
		wfName = *d.WorkflowName
	}
	if u := links.ArgoURL(lc.ArgoBaseURL, lc.Namespace, wfName); u != "" {
		d.ArgoUrl = &u
	}
	if urls := links.GrafanaURLs(lc.GrafanaBaseURL, d.Scenario, d.StartedAt, d.FinishedAt); len(urls) > 0 {
		arr := make([]grafanaEntry, 0, len(urls))
		for _, u := range urls {
			arr = append(arr, grafanaEntry{Label: u.Label, Url: u.URL})
		}
		d.GrafanaUrls = &arr
	}
}
```

Then update `GetRun` (`internal/api/handlers.go:79`). Change the manifest-fallback return:

```go
		if h.deps.Manifests != nil {
			if m, mErr := h.deps.Manifests.Read(ctx, req.Id); mErr == nil && m != nil {
				d := runDetailFromManifest(*m)
				h.addLinks(&d)
				return gen.GetRun200JSONResponse(d), nil
			}
		}
```

And the main return, replacing `return gen.GetRun200JSONResponse(detail), nil`:

```go
	h.addLinks(&detail)
	return gen.GetRun200JSONResponse(detail), nil
```

- [ ] **Step 3: Wire `Links` in `main.go`**

In `controlplane/cmd/dlh-controlplane/main.go`, add the links import (match existing `internal/...` import style):

```go
	"github.com/dlh/dlh-test-fw/controlplane/internal/links"
```

In the `deps := &api.Deps{ ... }` literal (alongside `AuthInfo: api.AuthInfoConfig{...}`), add:

```go
		Links: links.Config{
			ArgoBaseURL:    cfg.ArgoBaseURL,
			GrafanaBaseURL: cfg.GrafanaBaseURL,
			Namespace:      cfg.K8sNamespace,
		},
```

- [ ] **Step 4: Write the handler enrichment test**

Create `controlplane/internal/api/handlers_links_test.go`:

```go
package api

import (
	"testing"
	"time"

	"github.com/dlh/dlh-test-fw/controlplane/internal/api/gen"
	"github.com/dlh/dlh-test-fw/controlplane/internal/links"
)

func TestAddLinks_PopulatesArgoAndGrafana(t *testing.T) {
	h := &Handlers{deps: &Deps{Links: links.Config{
		ArgoBaseURL:    "https://argo.example.com",
		GrafanaBaseURL: "https://grafana.example.com",
		Namespace:      "dlh-test-fw",
	}}}
	wf := "mysql-pod-delete-20260523-130331"
	end := time.Date(2026, 5, 23, 13, 7, 47, 0, time.UTC)
	d := gen.RunDetail{
		Id:           wf,
		Scenario:     "mysql-pod-delete",
		WorkflowName: &wf,
		StartedAt:    time.Date(2026, 5, 23, 13, 3, 31, 0, time.UTC),
		FinishedAt:   &end,
	}
	h.addLinks(&d)

	if d.ArgoUrl == nil || *d.ArgoUrl != "https://argo.example.com/workflows/dlh-test-fw/"+wf {
		t.Errorf("ArgoUrl = %v", d.ArgoUrl)
	}
	if d.GrafanaUrls == nil || len(*d.GrafanaUrls) != 2 {
		t.Fatalf("want 2 grafana urls, got %v", d.GrafanaUrls)
	}
	if (*d.GrafanaUrls)[0].Label != "Run dashboard" || (*d.GrafanaUrls)[1].Label != "MySQL dashboard" {
		t.Errorf("labels = %v", *d.GrafanaUrls)
	}
}

func TestAddLinks_NoConfigNoLinks(t *testing.T) {
	h := &Handlers{deps: &Deps{Links: links.Config{Namespace: "dlh-test-fw"}}}
	wf := "x"
	d := gen.RunDetail{Id: "x", Scenario: "mysql-pod-delete", WorkflowName: &wf, StartedAt: time.Now()}
	h.addLinks(&d)
	if d.ArgoUrl != nil || d.GrafanaUrls != nil {
		t.Errorf("expected no links, got argo=%v grafana=%v", d.ArgoUrl, d.GrafanaUrls)
	}
}
```

- [ ] **Step 5: Run tests + build**

```bash
cd controlplane
go test ./internal/api/ ./internal/links/ ./internal/config/
go build ./...
```
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add controlplane/internal/api/server.go controlplane/internal/api/handlers.go controlplane/internal/api/handlers_links_test.go controlplane/cmd/dlh-controlplane/main.go
git commit -m "feat(controlplane): enrich RunDetail with Argo/Grafana deep links"
```

---

## Task 5: Deploy — expose the env knobs

**Files:**
- Modify: `controlplane/deploy/deployment.yaml`

- [ ] **Step 1: Add the two env vars to the controlplane Deployment**

In `controlplane/deploy/deployment.yaml`, inside the container `env:` list (after the `DLH_K8S_NAMESPACE` entry), add:

```yaml
            # Optional deep-link base URLs. Empty = Run detail hides the buttons.
            # Set per-environment (prod ingress hosts). Local minikube can leave
            # these empty or point them at a port-forward.
            - name: DLH_ARGO_BASE_URL
              value: ""
            - name: DLH_GRAFANA_BASE_URL
              value: ""
```

- [ ] **Step 2: Sanity-check the YAML parses**

Run: `kubectl apply --dry-run=client -f controlplane/deploy/deployment.yaml`
Expected: `deployment.apps/dlh-controlplane configured (dry run)` with no schema error. (If no cluster context is available, instead run `python3 -c "import yaml,sys; list(yaml.safe_load_all(open('controlplane/deploy/deployment.yaml')))"` and expect no error.)

- [ ] **Step 3: Commit**

```bash
git add controlplane/deploy/deployment.yaml
git commit -m "feat(deploy): expose DLH_ARGO_BASE_URL + DLH_GRAFANA_BASE_URL"
```

---

## Task 6: Frontend — deep-link buttons in the Run detail header

**Files:**
- Modify: `controlplane/web/src/pages/RunDetailPage.tsx` (the Plan 1 redesign)

- [ ] **Step 1: Add the `ExternalLink` icon to the lucide import**

In `controlplane/web/src/pages/RunDetailPage.tsx`, change the lucide import line to include `ExternalLink`:

```tsx
import { ArrowLeft, CheckCircle2, Circle, ExternalLink, Loader2, XCircle } from "lucide-react";
```

- [ ] **Step 2: Render the buttons in the header row**

Replace the header block (the `<div className="flex flex-wrap items-center gap-3">` containing the category icon, `<h1>`, and `<StatusBadge>`) with:

```tsx
      <div className="flex flex-wrap items-center gap-3">
        <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-muted">
          <CategoryIcon category={deriveCategory(run.scenario)} />
        </div>
        <h1 className="font-mono text-lg font-semibold">{run.id}</h1>
        <StatusBadge status={status} />
        {(run.argoUrl || (run.grafanaUrls && run.grafanaUrls.length > 0)) && (
          <div className="ml-auto flex flex-wrap items-center gap-2">
            {run.argoUrl && (
              <a href={run.argoUrl} target="_blank" rel="noreferrer"
                className="inline-flex h-8 items-center gap-1.5 rounded-md border border-input bg-background px-3 text-xs font-medium hover:bg-accent hover:text-accent-foreground">
                <ExternalLink className="h-3.5 w-3.5" /> Argo
              </a>
            )}
            {(run.grafanaUrls ?? []).map((g) => (
              <a key={g.url} href={g.url} target="_blank" rel="noreferrer"
                className="inline-flex h-8 items-center gap-1.5 rounded-md border border-input bg-background px-3 text-xs font-medium hover:bg-accent hover:text-accent-foreground">
                <ExternalLink className="h-3.5 w-3.5" /> {g.label}
              </a>
            ))}
          </div>
        )}
      </div>
```

- [ ] **Step 3: Verify build**

Run: `cd controlplane/web && pnpm build`
Expected: PASS (`run.argoUrl` and `run.grafanaUrls` exist on the regenerated `RunDetail` type from Task 3).

- [ ] **Step 4: Commit**

```bash
git add controlplane/web/src/pages/RunDetailPage.tsx
git commit -m "feat(web): Run detail Argo/Grafana deep-link buttons"
```

---

## Task 7: Full verification + docs + merge

**Files:**
- Modify: `CLAUDE.md` (repo root)

- [ ] **Step 1: Full backend + web build/test**

```bash
cd controlplane
go test ./...
make ui-build
make build
```
Expected: all PASS; binary embeds the updated UI.

- [ ] **Step 2: Reload into minikube with the knobs SET (to verify buttons render)**

```bash
cd controlplane && make reload-minikube
kubectl -n dlh-test-fw set env deployment/dlh-controlplane \
  DLH_ARGO_BASE_URL=https://argo.example.com \
  DLH_GRAFANA_BASE_URL=https://grafana.example.com
kubectl -n dlh-test-fw rollout status deployment/dlh-controlplane --timeout=60s
```

Note (known local-dev gotcha): if `make image`/`reload-minikube` fails pulling `gcr.io/distroless/static-debian12:nonroot` (DNS), apply the documented workaround — temporarily set the runtime stage in `controlplane/Dockerfile` to `FROM alpine:3.19` + `RUN adduser -D -u 65532 nonroot`, build/reload, then revert (do NOT commit it).

- [ ] **Step 3: Playwright dev-time verification (via MCP)**

```bash
kubectl -n dlh-test-fw port-forward svc/dlh-controlplane 18080:80
```
Open a finished mysql run (e.g. `/runs/mysql-pod-delete-20260523-130331`) and confirm:
- Header shows **Argo**, **Run dashboard**, and **MySQL dashboard** buttons.
- The Argo button's `href` is `https://argo.example.com/workflows/dlh-test-fw/<workflowName>`.
- The Run dashboard `href` contains `/d/dlh-run/dlh-run?var-dlh_scenario=mysql-pod-delete&from=<ms>&to=<ms>`.
- Open a **generic** scenario run (e.g. a `load-k6-run`/`verdict-slo-eval` run, if present) → only **Argo** + **Run dashboard** (no per-type dashboard).

Then unset to verify graceful hiding:
```bash
kubectl -n dlh-test-fw set env deployment/dlh-controlplane DLH_ARGO_BASE_URL- DLH_GRAFANA_BASE_URL-
kubectl -n dlh-test-fw rollout status deployment/dlh-controlplane --timeout=60s
```
Reload the run detail page → confirm **no** deep-link buttons render.

- [ ] **Step 4: Update `CLAUDE.md`**

Under the `### controlplane UI refresh` subsection in `/Users/allen/repo/dlh-test-fw/CLAUDE.md`, append:

```markdown
- Run detail deep-links (Plan `2026-05-23-02`): the backend assembles Argo +
  Grafana links into `RunDetail.argoUrl` / `grafanaUrls` from `DLH_ARGO_BASE_URL`
  and `DLH_GRAFANA_BASE_URL` (set in `controlplane/deploy/deployment.yaml`; empty
  = buttons hidden). URL logic + the `dlh_scenario` var + dashboard UIDs
  (`dlh-run`/`dlh-mysql`/`dlh-kafka`/`dlh-doris`) live in `internal/links`
  (Go-tested). The Grafana dashboard contract couples to `dashboards/grafana/`.
```

- [ ] **Step 5: Commit docs**

```bash
cd /Users/allen/repo/dlh-test-fw
git add CLAUDE.md
git commit -m "docs: note Run-detail deep-link env knobs in CLAUDE.md"
```

- [ ] **Step 6: Merge `--no-ff`**

```bash
cd /Users/allen/repo/dlh-test-fw
git checkout main
git merge --no-ff feat/controlplane-deeplinks -m "Merge feat/controlplane-deeplinks: Run-detail Argo/Grafana deep links (Plan 02)

Backend-assembled argoUrl + grafanaUrls (run + per-target-type dashboard)
from DLH_ARGO_BASE_URL / DLH_GRAFANA_BASE_URL, hidden when unset. New
internal/links package (Go-tested); RunDetail enriched in both GetRun paths."
git worktree remove ../dlh-test-fw-deeplinks
```

---

## Notes & deviations from the spec

- **Enrichment lives in the handler, not the model builder** — `model.RunDetailFromWorkflow` stays presentation-free; `Handlers.addLinks` (which has `Deps.Links`) enriches both the workflow and manifest-fallback paths so links work even after Argo TTL-collects the Workflow CR.
- **`grafanaEntry` type alias** (`= struct{...}`) is identical to the generated anonymous `GrafanaUrls` element type, so the slice assigns without conversion.
- **`deriveTargetType` is duplicated** in Go (`internal/links`) and TS (`web/src/lib/category.ts`, Plan 1). Keep the two rule lists identical — see the spec risk note.
- **Per-target-type label** uses a fixed map (`MySQL`/`Kafka`/`Doris` dashboard) rather than title-casing the type, to get correct casing.
- **Grafana `to=now`** for unfinished runs; finished runs use the exact epoch-ms window.
