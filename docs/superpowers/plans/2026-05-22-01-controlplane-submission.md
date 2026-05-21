# dlh-controlplane Phase C (Submission, Single-Cluster) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship POST/DELETE `/api/runs` + MinIO manifest writer + watchdog reconciler + `/internal/chaos` + `dlh` CLI, and rewire the three existing chaos WorkflowTemplates to call the controlplane instead of `kubectl create -f -`. After Phase C, every scenario submission flows through the controlplane API (UI, CLI, or `run-scenario.sh` shim) — no `argo submit` or `kubectl` needed.

**Architecture:** The Phase B controlplane is extended with a submission path that creates Argo `Workflow` CRs from `WorkflowTemplate` refs with merged parameters. The Workflow informer (already present) gains a manifest-writer subscriber that writes `manifest.json` + per-scenario index objects to MinIO on Submitted/Terminal phases. Chaos lifecycle moves from "the chaos WT shells out to kubectl" to "the chaos WT calls `/internal/chaos` via Argo's `http` template; controlplane creates the `Schedule` CR locally". A goroutine watchdog reconciles every 30s: any chaos CR labelled with a terminal Run gets force-deleted. A new `dlh` cobra CLI shares the OpenAPI client. `scripts/run-scenario.sh` becomes a 10-line shim that exec's `dlh run`.

**Tech Stack:** Go 1.26 (existing module); cobra/cobra-cli for the CLI; chi for the new `/internal/chaos` route; in-cluster k8s + chaos-mesh.org/v1alpha1 dynamic client. No new external services. No multi-cluster (that's Phase D).

**Reference spec:** `docs/superpowers/specs/2026-05-21-dlh-controlplane-design.md` (§6 storage model, §7 API surface, §8 cross-cluster chaos model — Phase C ships only the local impl + interface, §9 RBAC).

**Branch & worktree:** Per `CLAUDE.md`, work on `feat/plan16-controlplane-submission` in worktree `../dlh-test-fw-plan16`. Task 1 creates it.

**Plan-time decisions / deviations from spec:**

1. **Chaos shape preserved.** The existing WTs use Chaos Mesh `Schedule` resources (Plan 12), not raw `PodChaos`/`NetworkChaos`. The spec said "modify templates to call `/internal/chaos` instead of inlining chaos CRs"; what actually exists is templates that shell out to `kubectl create -f -` with a Schedule manifest. We preserve the Schedule + duration + delete semantics but move the create/delete calls from `kubectl` to `/internal/chaos`. Same chaos behavior, just owned by the controlplane.
2. **`/internal/chaos` body shape is k8s-Unstructured-shaped JSON.** Rather than invent a domain DTO for "PodChaos spec", the WT submits the full `unstructured.Unstructured` JSON for the chaos CR (apiVersion + kind + metadata + spec). Controlplane parses + creates. This keeps Phase C from inventing types that Phase D will need to rework anyway.
3. **No remote-target wiring.** The `ChaosClient` interface is introduced (so Phase D can swap impls) but only `LocalChaosClient` ships. Phase D adds `RemoteChaosClient`.
4. **CLI auth: device-code is Phase E.** For Phase C the CLI accepts a static token via `--token` flag or `DLH_TOKEN` env var. Local-dev mode (`fake:...` tokens) is enough; OIDC device-code flow is deferred to Phase E with the CI OIDC exchange.
5. **CLI doesn't ship as a deploy artifact.** The CLI is a developer / CI tool, not something that lives in the cluster. We build it via `make cli` and document install (`go install ./cmd/dlh@latest`) but don't include it in the Docker image or k8s manifests.
6. **`scripts/run-scenario.sh` becomes a deprecated shim** but is NOT deleted. Phase E removes it. The shim prints a deprecation warning and exec's `dlh run` with translated flags.
7. **Natural pause points:**
   - After Task 9 (Section A complete — submission path lands, chaos still goes through kubectl).
   - After Task 15 (Section B complete — `/internal/chaos` + watchdog + WT rewiring landed, no CLI yet).
   - After Task 21 (Section C complete — CLI works against the API, no shim yet).
   - After Task 23 (everything except smoke + merge).

---

## File Structure

**New files (Go backend):**
- `controlplane/internal/runs/submit.go` — `Submitter` builds Workflow CRs from WorkflowTemplate refs.
- `controlplane/internal/runs/manifest.go` — `ManifestWriter` writes `manifest.json` + index objects to MinIO.
- `controlplane/internal/runs/manifest_test.go`
- `controlplane/internal/runs/syncer.go` — `Syncer` subscribes to Workflow informer events and updates manifests on terminal phases.
- `controlplane/internal/runs/syncer_test.go`
- `controlplane/internal/chaos/client.go` — `ChaosClient` interface + `LocalChaosClient` impl.
- `controlplane/internal/chaos/local_test.go`
- `controlplane/internal/chaos/watchdog.go` — Watchdog reconciler.
- `controlplane/internal/chaos/watchdog_test.go`

**New files (OpenAPI + handlers):**
- `controlplane/internal/api/submission.go` — handlers for POST /api/runs + DELETE /api/runs/{id}.
- `controlplane/internal/api/internal_chaos.go` — handler for /internal/chaos (POST + DELETE).

**New files (CLI):**
- `controlplane/cmd/dlh/main.go` — cobra entry.
- `controlplane/cmd/dlh/root.go` — root command.
- `controlplane/cmd/dlh/run.go` — `dlh run <scenario>`.
- `controlplane/cmd/dlh/runs.go` — `dlh runs ls / show / logs / cancel`.
- `controlplane/cmd/dlh/client.go` — thin HTTP client wrapping openapi-fetch-equivalent for Go.

**Modified files:**
- `controlplane/api/openapi.yaml` — add POST /api/runs, DELETE /api/runs/{id}, /internal/chaos POST + DELETE.
- `controlplane/internal/api/gen/{server,types}.gen.go` — regenerated.
- `controlplane/internal/api/handlers.go` — wire new handlers into the strict server.
- `controlplane/internal/api/server.go` — mount `/internal/chaos` (requires a separate auth boundary: only the in-cluster Workflow SA can call it).
- `controlplane/internal/k8s/client.go` — add a dynamic client (for chaos-mesh.org Schedule CRD).
- `controlplane/cmd/dlh-controlplane/main.go` — wire Submitter + ManifestWriter + Syncer + Watchdog + LocalChaosClient.
- `controlplane/internal/config/config.go` — add `DLH_INTERNAL_TOKEN` (shared secret the WT `http` step sends).
- `controlplane/internal/auth/middleware.go` — add `InternalTokenMiddleware` for `/internal/*`.
- `controlplane/Makefile` — add `cli` target.
- `controlplane/deploy/deployment.yaml` — add DLH_INTERNAL_TOKEN env from new Secret.
- `controlplane/deploy/role.yaml` — extend with workflow create/delete + chaos-mesh.org schedules create/delete.
- `controlplane/deploy/role.yaml` — internal-token Secret read.
- `helm/dlh-test-fw/files/workflowtemplates/chaos/pod-delete.yaml` — replace script+kubectl with `http` template calling /internal/chaos.
- `helm/dlh-test-fw/files/workflowtemplates/chaos/network-loss.yaml` — same.
- `helm/dlh-test-fw/files/workflowtemplates/chaos/kafka-broker-partition.yaml` — same.
- `helm/dlh-test-fw/templates/dlh-internal-token-secret.yaml` — new Secret (generated value).
- `scripts/run-scenario.sh` — becomes a deprecation shim that calls `dlh run`.
- `.github/workflows/ci.yml` — extend controlplane job to build the CLI binary.
- `docs/FINDINGS.md` — Plan 16 section.
- `CLAUDE.md` — controlplane submission notes appended.
- `README.md` — Plan 16 row.

**Unchanged:** Phase B handlers (`GET` endpoints), UI (renders new data automatically since the same `manifest.json` populates), verdict-job, k6 image, dashboards, Argo CD manifests.

---

## Task 1: Baseline + worktree

No commits.

- [ ] **Step 1: Confirm clean main + CI green**

```bash
cd /Users/allen/repo/dlh-test-fw
git status
git log --first-parent --oneline -5
gh run list --branch main --limit 1
```

Expected: clean tree on `main`; HEAD includes `1964396` (Plan 15 README backfill) or newer; most recent CI run `success`.

- [ ] **Step 2: Create feature worktree**

```bash
git worktree add ../dlh-test-fw-plan16 -b feat/plan16-controlplane-submission main
cd ../dlh-test-fw-plan16
git status
ls controlplane/internal/   # confirm Phase B layout
```

Expected: clean tree on `feat/plan16-controlplane-submission`; `internal/` has `api`, `auth`, `config`, `k8s`, `minio`, `model` (Phase B output).

- [ ] **Step 3: Verify Phase B still builds + tests pass**

```bash
cd /Users/allen/repo/dlh-test-fw-plan16/controlplane
make ui-build
go build ./...
go test ./...
```

Expected: 17 tests pass (Phase B baseline).

All remaining tasks run from `/Users/allen/repo/dlh-test-fw-plan16`.

---

# Section A — Submission Path + Manifest Writes (Tasks 2-9)

## Task 2: Extend OpenAPI with POST/DELETE /api/runs + /internal/chaos

**Files:**
- Modify: `controlplane/api/openapi.yaml`
- Regenerate: `controlplane/internal/api/gen/server.gen.go`, `controlplane/internal/api/gen/types.gen.go`, `controlplane/web/src/api/gen.ts`

- [ ] **Step 1: Read current openapi.yaml**

```bash
cd /Users/allen/repo/dlh-test-fw-plan16/controlplane
wc -l api/openapi.yaml
```

Expected: ~187 lines (Phase B).

- [ ] **Step 2: Add POST /api/runs.** Use Edit to insert before the existing `/api/runs/{id}:` block, under `paths:`. New block:

```yaml
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
    post:
      operationId: createRun
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/CreateRunRequest"
      responses:
        "202":
          description: accepted
          content:
            application/json:
              schema: { $ref: "#/components/schemas/Run" }
        "400":
          description: invalid request
        "404":
          description: scenario not found
```

The Edit anchor for inserting: find the existing `  /api/runs:` block (the GET-only block) and replace it with the full GET+POST block above.

- [ ] **Step 3: Add DELETE /api/runs/{id}.** Find the existing `/api/runs/{id}:` block and extend it to include DELETE. The replacement block:

```yaml
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
    delete:
      operationId: cancelRun
      parameters:
        - in: path
          name: id
          required: true
          schema: { type: string }
      responses:
        "202":
          description: cancellation accepted
        "404":
          description: not found
```

- [ ] **Step 4: Add /internal/chaos paths.** Insert after `/api/runs/{id}/events:` block (the SSE endpoint), at the same indent. New block:

```yaml
  /internal/chaos:
    post:
      operationId: createChaos
      description: |
        Called by Argo Workflow `http` template steps from the chaos WTs.
        Body is the Unstructured chaos CR (apiVersion + kind + metadata + spec).
        Auth is via the X-Internal-Token header carrying the controlplane's
        shared secret — NOT OIDC.
      security:
        - internalToken: []
      requestBody:
        required: true
        content:
          application/json:
            schema: { $ref: "#/components/schemas/ChaosResource" }
      responses:
        "201":
          description: chaos resource created
          content:
            application/json:
              schema: { $ref: "#/components/schemas/ChaosResourceRef" }
        "400":
          description: invalid body
        "401":
          description: bad internal token
        "500":
          description: cluster API error
  /internal/chaos/{ref}:
    delete:
      operationId: deleteChaos
      parameters:
        - in: path
          name: ref
          required: true
          schema: { type: string }
          description: |
            Reference returned by createChaos. Encodes namespace/name/kind.
      security:
        - internalToken: []
      responses:
        "204":
          description: deleted (or already gone)
        "401":
          description: bad internal token
```

- [ ] **Step 5: Extend components.schemas** with `CreateRunRequest`, `ChaosResource`, `ChaosResourceRef`, and `components.securitySchemes` with `internalToken`. Find the existing `components:` block and add these:

Under `components.securitySchemes:` (which already has `bearerAuth`):
```yaml
    internalToken:
      type: apiKey
      in: header
      name: X-Internal-Token
```

Under `components.schemas:` (which already has Scenario, Run, RunDetail), append:
```yaml
    CreateRunRequest:
      type: object
      required: [scenarioId]
      properties:
        scenarioId:
          type: string
          description: "WorkflowTemplate name (e.g. mysql-pod-delete)"
        parameters:
          type: object
          description: "Optional parameter overrides. Keys are WT parameter names."
          additionalProperties:
            type: string
    ChaosResource:
      type: object
      required: [apiVersion, kind, metadata, spec]
      properties:
        apiVersion: { type: string, example: "chaos-mesh.org/v1alpha1" }
        kind:       { type: string, example: "Schedule" }
        metadata:
          type: object
          additionalProperties: true
        spec:
          type: object
          additionalProperties: true
    ChaosResourceRef:
      type: object
      required: [ref]
      properties:
        ref:        { type: string, description: "Opaque handle. URL-safe. Pass to DELETE /internal/chaos/{ref}." }
        kind:       { type: string }
        name:       { type: string }
        namespace:  { type: string }
```

- [ ] **Step 6: Regenerate server stubs + TS types**

```bash
cd /Users/allen/repo/dlh-test-fw-plan16/controlplane
make codegen
go build ./...
```

If `make codegen` fails on the TS step because pnpm isn't on this machine, run just the Go codegen:
```bash
go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -config api/oapi-codegen-server.yaml api/openapi.yaml
go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -config api/oapi-codegen-types.yaml api/openapi.yaml
```

Then run the TS codegen:
```bash
cd web && pnpm openapi-typescript ../api/openapi.yaml -o src/api/gen.ts && cd ..
```

`go build ./...` will fail because the Phase B `Handlers` struct doesn't yet implement the new StrictServerInterface methods (`CreateRun`, `CancelRun`, `CreateChaos`, `DeleteChaos`). That's expected — Tasks 5 and 11 add them. For now, add stubs to `internal/api/handlers.go`:

```go
func (h *Handlers) CreateRun(_ context.Context, _ gen.CreateRunRequestObject) (gen.CreateRunResponseObject, error) {
	return gen.CreateRun400Response{}, nil // TODO Task 5
}
func (h *Handlers) CancelRun(_ context.Context, _ gen.CancelRunRequestObject) (gen.CancelRunResponseObject, error) {
	return gen.CancelRun404Response{}, nil // TODO Task 5
}
func (h *Handlers) CreateChaos(_ context.Context, _ gen.CreateChaosRequestObject) (gen.CreateChaosResponseObject, error) {
	return gen.CreateChaos500Response{}, nil // TODO Task 11
}
func (h *Handlers) DeleteChaos(_ context.Context, _ gen.DeleteChaosRequestObject) (gen.DeleteChaosResponseObject, error) {
	return gen.DeleteChaos401Response{}, nil // TODO Task 11
}
```

(The actual response-object names may differ — adjust based on the new codegen output. The pattern is identical to Phase B's Task 8.)

Then `go build ./...` should succeed.

- [ ] **Step 7: Commit**

```bash
git add controlplane/api/openapi.yaml controlplane/internal/api/gen/ controlplane/internal/api/handlers.go controlplane/web/src/api/gen.ts
git commit -m "feat(controlplane): OpenAPI for POST/DELETE /api/runs + /internal/chaos

Adds CreateRun + CancelRun + CreateChaos + DeleteChaos operations to
the OpenAPI spec; regenerates server stubs + TS types. Handlers are
stubbed and return their error responses; real implementations land
in Tasks 5 (submission) and 11 (chaos)."
```

---

## Task 3: Submitter — build Workflow CR from WorkflowTemplate ref

**Files:**
- Create: `controlplane/internal/runs/submit.go`
- Create: `controlplane/internal/runs/submit_test.go`

- [ ] **Step 1: Write `internal/runs/submit.go`**

```go
package runs

import (
	"context"
	"fmt"
	"time"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	wfclient "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned"
	"github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// Submitter creates new Workflow CRs from WorkflowTemplate refs.
type Submitter struct {
	Argo      wfclient.Interface
	Namespace string
}

// SubmitRequest is the inbound payload (one-step removed from the HTTP DTO).
type SubmitRequest struct {
	ScenarioID string
	Parameters map[string]string
	CreatedBy  string // OIDC subject
}

// SubmitResult is what we return to the caller — caller already has the
// scenario id; we add the generated run id + start time.
type SubmitResult struct {
	RunID     string
	StartedAt time.Time
}

// Submit creates the Workflow CR. RunID format mirrors run-scenario.sh:
// "<scenarioID>-YYYYMMDD-HHMMSS".
func (s *Submitter) Submit(ctx context.Context, req SubmitRequest) (*SubmitResult, error) {
	if req.ScenarioID == "" {
		return nil, fmt.Errorf("scenarioId is required")
	}
	// Verify the template exists; this returns 404 to the API caller.
	if _, err := s.Argo.ArgoprojV1alpha1().WorkflowTemplates(s.Namespace).Get(ctx, req.ScenarioID, metav1.GetOptions{}); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("scenario %q not found: %w", req.ScenarioID, err)
		}
		return nil, fmt.Errorf("get workflowtemplate %q: %w", req.ScenarioID, err)
	}

	now := time.Now().UTC()
	runID := fmt.Sprintf("%s-%s", req.ScenarioID, now.Format("20060102-150405"))

	params := make([]wfv1.Parameter, 0, len(req.Parameters))
	for k, v := range req.Parameters {
		// Argo's Parameter.Value is intstr.IntOrString-ish; use AnyString helper.
		val := wfv1.AnyString(v)
		params = append(params, wfv1.Parameter{Name: k, Value: &val})
	}

	wf := &wfv1.Workflow{
		ObjectMeta: metav1.ObjectMeta{
			Name:      runID,
			Namespace: s.Namespace,
			Labels: map[string]string{
				"dlh.scenario": req.ScenarioID,
				"dlh.run-id":   runID,
			},
			Annotations: map[string]string{
				"dlh.created-by": req.CreatedBy,
			},
		},
		Spec: wfv1.WorkflowSpec{
			WorkflowTemplateRef: &wfv1.WorkflowTemplateRef{Name: req.ScenarioID},
			Arguments:           wfv1.Arguments{Parameters: params},
		},
	}

	created, err := s.Argo.ArgoprojV1alpha1().Workflows(s.Namespace).Create(ctx, wf, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("create workflow: %w", err)
	}
	return &SubmitResult{RunID: created.Name, StartedAt: created.CreationTimestamp.Time}, nil
}

// Silence the unused import linter if intstr ends up unused after type-checking.
var _ = intstr.FromString
var _ = v1alpha1.SchemeGroupVersion
```

**Note for the engineer:** `wfv1.AnyString` is the helper for `Parameter.Value` (which is `*AnyString`). If the actual type signature differs in argo-workflows v3.6.19, adjust — `Parameter.Value` is a pointer to AnyString in current versions. Run `go doc github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1.Parameter` if you need to verify.

- [ ] **Step 2: Write `internal/runs/submit_test.go`**

```go
package runs

import (
	"context"
	"strings"
	"testing"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	wfake "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSubmit_CreatesWorkflowWithTemplateRef(t *testing.T) {
	ns := "dlh-test-fw"
	tmpl := &wfv1.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-pod-delete", Namespace: ns},
	}
	argo := wfake.NewSimpleClientset(tmpl)

	s := &Submitter{Argo: argo, Namespace: ns}
	res, err := s.Submit(context.Background(), SubmitRequest{
		ScenarioID: "mysql-pod-delete",
		Parameters: map[string]string{"vus": "20"},
		CreatedBy:  "tester",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if !strings.HasPrefix(res.RunID, "mysql-pod-delete-") {
		t.Errorf("RunID: %q", res.RunID)
	}
	// Verify the Workflow was created via fake clientset.
	got, err := argo.ArgoprojV1alpha1().Workflows(ns).Get(context.Background(), res.RunID, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Spec.WorkflowTemplateRef == nil || got.Spec.WorkflowTemplateRef.Name != "mysql-pod-delete" {
		t.Errorf("templateRef wrong: %+v", got.Spec.WorkflowTemplateRef)
	}
	if got.Labels["dlh.scenario"] != "mysql-pod-delete" {
		t.Errorf("label: %v", got.Labels)
	}
	// Spot-check parameter merging.
	if len(got.Spec.Arguments.Parameters) != 1 || got.Spec.Arguments.Parameters[0].Name != "vus" {
		t.Errorf("params: %+v", got.Spec.Arguments.Parameters)
	}
}

func TestSubmit_404ForUnknownScenario(t *testing.T) {
	argo := wfake.NewSimpleClientset()
	s := &Submitter{Argo: argo, Namespace: "dlh-test-fw"}
	_, err := s.Submit(context.Background(), SubmitRequest{ScenarioID: "nope"})
	if err == nil {
		t.Fatal("expected error for unknown scenario")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error text: %v", err)
	}
}

func TestSubmit_EmptyScenarioRejected(t *testing.T) {
	s := &Submitter{Argo: wfake.NewSimpleClientset(), Namespace: "dlh-test-fw"}
	_, err := s.Submit(context.Background(), SubmitRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
}
```

- [ ] **Step 3: Build + test**

```bash
cd /Users/allen/repo/dlh-test-fw-plan16/controlplane
go mod tidy
go build ./...
go test ./internal/runs/... -v
```

Expected: 3 tests PASS. If `wfv1.AnyString` doesn't exist (some versions use `intstr.FromString` or a direct cast), adjust the call site. Iterate.

- [ ] **Step 4: Commit**

```bash
git add controlplane/internal/runs/submit.go controlplane/internal/runs/submit_test.go controlplane/go.mod controlplane/go.sum
git commit -m "feat(controlplane/runs): Submitter creates Workflow CR from WorkflowTemplate ref

RunID format mirrors run-scenario.sh: <scenarioID>-YYYYMMDD-HHMMSS.
Labels dlh.scenario + dlh.run-id are set so the existing Plan 11
scenario semaphore + Plan 13 dashboard partition queries keep working."
```

---

## Task 4: ManifestWriter — write manifest.json + index objects to MinIO

**Files:**
- Create: `controlplane/internal/runs/manifest.go`
- Create: `controlplane/internal/runs/manifest_test.go`

- [ ] **Step 1: Write `internal/runs/manifest.go`**

```go
package runs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
)

// Manifest is the controlplane's authoritative record for a Run.
// Written to MinIO at submit time + on terminal Workflow phase.
type Manifest struct {
	RunID        string            `json:"runId"`
	Scenario     string            `json:"scenario"`
	WorkflowName string            `json:"workflowName"`
	Parameters   map[string]string `json:"parameters,omitempty"`
	CreatedBy    string            `json:"createdBy,omitempty"`
	Status       string            `json:"status"` // Submitted/Running/Succeeded/Failed/Error/Unknown
	StartedAt    time.Time         `json:"startedAt"`
	FinishedAt   *time.Time        `json:"finishedAt,omitempty"`
	Score        *float64          `json:"score,omitempty"`
}

// ManifestWriter writes manifests + index objects to MinIO.
type ManifestWriter struct {
	Client *minio.Client
	Bucket string
}

// Write puts the manifest at runs/by-id/{runID}/manifest.json AND
// writes two pointer copies under index/by-scenario/{scenario}/{YYYY-MM-DD}/{runID}.json.
// (Phase D will add by-target index — out of scope here.)
func (w *ManifestWriter) Write(ctx context.Context, m Manifest) error {
	body, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	primary := fmt.Sprintf("runs/by-id/%s/manifest.json", m.RunID)
	if err := w.putJSON(ctx, primary, body); err != nil {
		return fmt.Errorf("put primary: %w", err)
	}
	day := m.StartedAt.UTC().Format("2006-01-02")
	idx := fmt.Sprintf("runs/index/by-scenario/%s/%s/%s.json", sanitize(m.Scenario), day, m.RunID)
	if err := w.putJSON(ctx, idx, body); err != nil {
		return fmt.Errorf("put index: %w", err)
	}
	return nil
}

// Read fetches a manifest by run id.
func (w *ManifestWriter) Read(ctx context.Context, runID string) (*Manifest, error) {
	key := fmt.Sprintf("runs/by-id/%s/manifest.json", runID)
	obj, err := w.Client.GetObject(ctx, w.Bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer obj.Close()
	var m Manifest
	if err := json.NewDecoder(obj).Decode(&m); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	return &m, nil
}

func (w *ManifestWriter) putJSON(ctx context.Context, key string, body []byte) error {
	_, err := w.Client.PutObject(ctx, w.Bucket, key, bytes.NewReader(body), int64(len(body)),
		minio.PutObjectOptions{ContentType: "application/json"})
	return err
}

// sanitize replaces characters that aren't safe in S3-prefix paths.
// Scenario IDs are k8s names so already safe; defensive trim anyway.
func sanitize(s string) string {
	return strings.ReplaceAll(s, "/", "_")
}
```

- [ ] **Step 2: Write `internal/runs/manifest_test.go`**

This test uses a mocked S3 client interface; we don't spin up testcontainers MinIO here (that's overkill for marshal-shape testing). Test the path generation logic and JSON round-trip.

```go
package runs

import (
	"encoding/json"
	"testing"
	"time"
)

func TestManifest_JSONRoundtrip(t *testing.T) {
	startedAt := time.Date(2026, 5, 22, 10, 30, 0, 0, time.UTC)
	finishedAt := startedAt.Add(5 * time.Minute)
	score := 0.95
	m := Manifest{
		RunID:        "mysql-pod-delete-20260522-103000",
		Scenario:     "mysql-pod-delete",
		WorkflowName: "mysql-pod-delete-20260522-103000",
		Parameters:   map[string]string{"vus": "20"},
		CreatedBy:    "tester",
		Status:       "Succeeded",
		StartedAt:    startedAt,
		FinishedAt:   &finishedAt,
		Score:        &score,
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Manifest
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.RunID != m.RunID || got.Status != "Succeeded" || got.Score == nil || *got.Score != 0.95 {
		t.Errorf("roundtrip lost data: %+v", got)
	}
}

func TestSanitize_NoSlashes(t *testing.T) {
	if got := sanitize("scenario/with/slashes"); got != "scenario_with_slashes" {
		t.Errorf("sanitize: %q", got)
	}
}
```

- [ ] **Step 3: Build + test**

```bash
go build ./...
go test ./internal/runs/...
```

Expected: 5 tests pass (3 from Task 3 + 2 from Task 4).

- [ ] **Step 4: Commit**

```bash
git add controlplane/internal/runs/manifest.go controlplane/internal/runs/manifest_test.go
git commit -m "feat(controlplane/runs): ManifestWriter writes manifest.json + by-scenario index to MinIO

Layout per spec §6: runs/by-id/{runID}/manifest.json (primary) and
runs/index/by-scenario/{scenario}/{YYYY-MM-DD}/{runID}.json (pointer
copy for fast prefix LIST). by-target index deferred to Phase D."
```

---

## Task 5: POST /api/runs + DELETE /api/runs/{id} handlers

**Files:**
- Modify: `controlplane/internal/api/handlers.go` (replace the Task 2 stubs with real implementations)
- Modify: `controlplane/internal/api/server.go` (Deps gets new fields)
- Modify: `controlplane/internal/api/handlers_test.go` (add CreateRun + CancelRun tests)

- [ ] **Step 1: Extend `internal/api/server.go` Deps struct** to hold the submitter, manifest writer, and the argo client (needed for terminate-on-cancel).

Find the existing `Deps` struct in `internal/api/server.go`. Replace it with:

```go
type Deps struct {
	Templates   k8s.TemplateLister
	Workflows   k8s.WorkflowLister
	Reports     *mio.ReportReader
	Submitter   *runs.Submitter   // Phase C
	Manifests   *runs.ManifestWriter // Phase C
	ArgoClient  wfclient.Interface   // Phase C — for terminate
	ChaosCancel ChaosCanceller       // Phase C — see Task 11
}

// ChaosCanceller is satisfied by chaos.LocalChaosClient. Decoupled here so
// the api package doesn't import chaos directly (chaos imports k8s).
type ChaosCanceller interface {
	DeleteByRun(ctx context.Context, runID string) error
}
```

Imports needed:
```go
import (
	wfclient "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned"
	"github.com/dlh/dlh-test-fw/controlplane/internal/runs"
)
```

The `ChaosCanceller` interface is a small placeholder so this task can compile before Task 11 wires the real chaos package.

- [ ] **Step 2: Replace the Task 2 CreateRun stub with the real implementation:**

```go
func (h *Handlers) CreateRun(ctx context.Context, req gen.CreateRunRequestObject) (gen.CreateRunResponseObject, error) {
	id, _ := auth.IdentityFromContext(ctx)
	createdBy := ""
	if id != nil {
		createdBy = id.Subject
	}
	body := req.Body
	if body == nil || body.ScenarioId == "" {
		return gen.CreateRun400Response{}, nil
	}
	params := map[string]string{}
	if body.Parameters != nil {
		for k, v := range *body.Parameters {
			params[k] = v
		}
	}
	sr, err := h.deps.Submitter.Submit(ctx, runs.SubmitRequest{
		ScenarioID: body.ScenarioId,
		Parameters: params,
		CreatedBy:  createdBy,
	})
	if err != nil {
		// 404 if scenario doesn't exist; 500 otherwise. Submitter returns a
		// wrapped error so we sniff by substring — cheaper than a typed error
		// for v1.
		if strings.Contains(err.Error(), "not found") {
			return gen.CreateRun404Response{}, nil
		}
		return nil, err
	}
	// Write the initial Submitted manifest synchronously so list/show
	// endpoints can find the run immediately.
	m := runs.Manifest{
		RunID:        sr.RunID,
		Scenario:     body.ScenarioId,
		WorkflowName: sr.RunID,
		Parameters:   params,
		CreatedBy:    createdBy,
		Status:       "Submitted",
		StartedAt:    sr.StartedAt,
	}
	if err := h.deps.Manifests.Write(ctx, m); err != nil {
		// Log + continue: workflow already created, the syncer will
		// eventually write a manifest when the informer fires.
	}
	resp := gen.Run{
		Id:           sr.RunID,
		Scenario:     body.ScenarioId,
		Status:       "Submitted",
		StartedAt:    sr.StartedAt,
		WorkflowName: stringPtr(sr.RunID),
	}
	return gen.CreateRun202JSONResponse(resp), nil
}

func stringPtr(s string) *string { return &s }
```

Required imports for handlers.go (merge with the existing import block):
```go
import (
	"strings"
	"github.com/dlh/dlh-test-fw/controlplane/internal/auth"
	"github.com/dlh/dlh-test-fw/controlplane/internal/runs"
)
```

Run name uses `RunStatus` type per Phase B handlers — match the existing codegen-emitted shape; e.g. `Status: gen.RunStatus("Submitted")` if the field has a typed enum. Iterate against the compiler if needed.

- [ ] **Step 3: Replace the Task 2 CancelRun stub:**

```go
func (h *Handlers) CancelRun(ctx context.Context, req gen.CancelRunRequestObject) (gen.CancelRunResponseObject, error) {
	// Confirm the run exists.
	if _, err := h.deps.Workflows.Get(req.Id); err != nil {
		return gen.CancelRun404Response{}, nil
	}
	// Best-effort chaos cleanup first so we don't leak chaos past cancel.
	if h.deps.ChaosCancel != nil {
		_ = h.deps.ChaosCancel.DeleteByRun(ctx, req.Id)
	}
	// Argo's "shutdown=Terminate" annotation + a normal client-side
	// patch is the official cancellation path (no dedicated REST endpoint).
	patch := []byte(`{"spec":{"shutdown":"Terminate"}}`)
	_, err := h.deps.ArgoClient.ArgoprojV1alpha1().Workflows(h.deps.Submitter.Namespace).Patch(
		ctx, req.Id, types.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return nil, err
	}
	return gen.CancelRun202Response{}, nil
}
```

Required imports:
```go
import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)
```

- [ ] **Step 4: Add a handler-level test for CreateRun.** Append to `internal/api/handlers_test.go`:

```go
func TestCreateRun_Submits(t *testing.T) {
	wfv1Tmpl := &wfv1.WorkflowTemplate{ObjectMeta: metav1.ObjectMeta{Name: "mysql-pod-delete", Namespace: "dlh-test-fw"}}
	argo := wfake.NewSimpleClientset(wfv1Tmpl)
	mc := &mockMinio{}
	mw := &runs.ManifestWriter{Client: nil, Bucket: "artifacts"} // mocked via mc — see note below

	deps := &Deps{
		Templates: &fakeTemplates{items: []wfv1.WorkflowTemplate{*wfv1Tmpl}},
		Submitter: &runs.Submitter{Argo: argo, Namespace: "dlh-test-fw"},
		Manifests: mw,
	}
	_ = mc // keep for future use
	h := &Handlers{deps: deps}

	scenario := "mysql-pod-delete"
	req := gen.CreateRunRequestObject{Body: &gen.CreateRunRequest{ScenarioId: scenario}}
	resp, err := h.CreateRun(context.Background(), req)
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	out, ok := resp.(gen.CreateRun202JSONResponse)
	if !ok {
		t.Fatalf("response type: %T", resp)
	}
	if !strings.HasPrefix(out.Id, "mysql-pod-delete-") {
		t.Errorf("RunID: %q", out.Id)
	}
}

func TestCreateRun_404OnUnknownScenario(t *testing.T) {
	argo := wfake.NewSimpleClientset()
	deps := &Deps{
		Templates: &fakeTemplates{},
		Submitter: &runs.Submitter{Argo: argo, Namespace: "dlh-test-fw"},
		Manifests: &runs.ManifestWriter{Bucket: "artifacts"},
	}
	h := &Handlers{deps: deps}
	resp, err := h.CreateRun(context.Background(), gen.CreateRunRequestObject{
		Body: &gen.CreateRunRequest{ScenarioId: "does-not-exist"},
	})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if _, ok := resp.(gen.CreateRun404Response); !ok {
		t.Fatalf("expected 404, got %T", resp)
	}
}

// mockMinio is a no-op shim used to construct a ManifestWriter for tests
// that don't exercise the write path. Real write tests live in
// manifest_test.go; these tests only verify the handler glue.
type mockMinio struct{}
```

**Note for the engineer:** the `ManifestWriter.Write` call in the CreateRun handler will nil-dereference if the test passes a writer with `Client: nil`. Defensive option: make ManifestWriter.Write a no-op when Client is nil:

```go
func (w *ManifestWriter) Write(ctx context.Context, m Manifest) error {
	if w.Client == nil {
		return nil // test-mode no-op
	}
	// ... existing body ...
}
```

Add that guard to `internal/runs/manifest.go`. It's defensive code that the production path never triggers (the controlplane main always wires a real client).

- [ ] **Step 5: Build + test**

```bash
go build ./...
go test ./internal/api/... -v
go test ./internal/runs/... -v
```

Expected: existing Phase B api tests still pass; 2 new tests pass.

If `go build` fails because Deps references `runs.Submitter` while `internal/api/server.go` doesn't yet import runs, the import block needs updating — verify.

- [ ] **Step 6: Commit**

```bash
git add controlplane/internal/api/handlers.go controlplane/internal/api/server.go controlplane/internal/api/handlers_test.go controlplane/internal/runs/manifest.go
git commit -m "feat(controlplane/api): real CreateRun + CancelRun handlers

CreateRun submits a Workflow via runs.Submitter, writes the initial
Submitted manifest synchronously, and returns 202 + the new Run. 404
when the scenario WorkflowTemplate doesn't exist.

CancelRun: chaos-cleanup first (best-effort via ChaosCanceller
interface — wired in Task 11), then patches the Workflow with
shutdown=Terminate. 404 when the run isn't known.

ChaosCanceller is a small interface placeholder so the api package
doesn't import chaos directly; Task 11 wires LocalChaosClient as
the impl.

Defensive: ManifestWriter.Write is a no-op when Client is nil so
handler tests don't need a real S3 backend."
```

---

## Task 6: Syncer — Workflow informer → MinIO manifest updates

**Files:**
- Create: `controlplane/internal/runs/syncer.go`
- Create: `controlplane/internal/runs/syncer_test.go`

- [ ] **Step 1: Write `internal/runs/syncer.go`**

```go
package runs

import (
	"context"
	"log/slog"
	"sync"
	"time"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
)

// Syncer subscribes to a WorkflowSource and writes manifest updates on
// every Workflow event. It coalesces rapid events per run so we don't
// spam MinIO; a small in-memory map tracks the last status we wrote.
type Syncer struct {
	Source    WorkflowEventSource
	Manifests *ManifestWriter
	Reports   ReportSource

	mu   sync.Mutex
	last map[string]string // runID -> last written status
}

// WorkflowEventSource is satisfied by k8s.WorkflowLister.Subscribe.
type WorkflowEventSource interface {
	Subscribe() (<-chan WorkflowEvent, func())
}

// WorkflowEvent mirrors k8s.WorkflowEvent (decoupled to avoid k8s import here).
type WorkflowEvent struct {
	Type     string
	Workflow *wfv1.Workflow
}

// ReportSource is satisfied by minio.ReportReader.
type ReportSource interface {
	Read(ctx context.Context, workflowName string) (map[string]any, error)
}

// Run blocks until ctx is cancelled. Call once at startup in a goroutine.
func (s *Syncer) Run(ctx context.Context) {
	if s.last == nil {
		s.last = map[string]string{}
	}
	ch, cancel := s.Source.Subscribe()
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			s.handle(ctx, ev)
		}
	}
}

func (s *Syncer) handle(ctx context.Context, ev WorkflowEvent) {
	if ev.Workflow == nil {
		return
	}
	wf := ev.Workflow
	runID, ok := wf.Labels["dlh.run-id"]
	if !ok {
		runID = wf.Name
	}
	status := string(wf.Status.Phase)
	if status == "" {
		status = "Pending"
	}

	s.mu.Lock()
	prev := s.last[runID]
	s.last[runID] = status
	s.mu.Unlock()

	// Only write when status changes; drops update-noise from informer's
	// resync interval.
	if prev == status && ev.Type != "DELETED" {
		return
	}

	m := Manifest{
		RunID:        runID,
		Scenario:     wf.Labels["dlh.scenario"],
		WorkflowName: wf.Name,
		Status:       status,
		StartedAt:    wf.CreationTimestamp.Time,
	}
	if !wf.Status.FinishedAt.IsZero() {
		t := wf.Status.FinishedAt.Time
		m.FinishedAt = &t
		// On terminal phase, attempt to enrich with verdict score.
		if score, ok := readScore(ctx, s.Reports, wf.Name); ok {
			m.Score = &score
		}
	}
	if err := s.Manifests.Write(ctx, m); err != nil {
		slog.Warn("manifest write failed", "runID", runID, "err", err)
	}
}

// readScore pulls a numeric score from report.json's well-known field.
// verdict-job emits `score` as a top-level number; if missing, no-op.
func readScore(ctx context.Context, src ReportSource, workflowName string) (float64, bool) {
	if src == nil {
		return 0, false
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	r, err := src.Read(cctx, workflowName)
	if err != nil || r == nil {
		return 0, false
	}
	if v, ok := r["score"].(float64); ok {
		return v, true
	}
	return 0, false
}
```

- [ ] **Step 2: Write `internal/runs/syncer_test.go`**

```go
package runs

import (
	"context"
	"errors"
	"testing"
	"time"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type fakeEventSource struct {
	ch chan WorkflowEvent
}

func (f *fakeEventSource) Subscribe() (<-chan WorkflowEvent, func()) {
	return f.ch, func() {}
}

type fakeReports struct {
	score float64
	hit   bool
	err   error
}

func (f *fakeReports) Read(_ context.Context, _ string) (map[string]any, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.hit {
		return map[string]any{"score": f.score}, nil
	}
	return nil, errors.New("not found")
}

type capturingWriter struct {
	got []Manifest
}

func (c *capturingWriter) Write(_ context.Context, m Manifest) error {
	c.got = append(c.got, m)
	return nil
}

// Tiny adapter — ManifestWriter is a struct, not an interface; we wrap
// the captured writes via a type assertion in the actual Syncer body.
// For test purposes, swap Syncer.Manifests for a writer with a nil Client.

func TestSyncer_WritesOnStatusChange(t *testing.T) {
	src := &fakeEventSource{ch: make(chan WorkflowEvent, 3)}
	captured := []Manifest{}
	writer := &ManifestWriter{Client: nil, Bucket: "artifacts"} // Write is a no-op
	// Custom wrapper so we can capture without spinning up MinIO:
	mw := &captureManifestWriter{
		inner: writer,
		on:    func(m Manifest) { captured = append(captured, m) },
	}
	s := &Syncer{Source: src, Manifests: mw.asManifestWriter(), Reports: &fakeReports{}}

	go s.Run(context.Background())

	wf := &wfv1.Workflow{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "run-1",
			Labels:            map[string]string{"dlh.run-id": "run-1", "dlh.scenario": "mysql-pod-delete"},
			CreationTimestamp: metav1.NewTime(time.Now()),
		},
		Status: wfv1.WorkflowStatus{Phase: "Running"},
	}
	src.ch <- WorkflowEvent{Type: "ADDED", Workflow: wf}
	src.ch <- WorkflowEvent{Type: "MODIFIED", Workflow: wf} // same phase — should be coalesced

	// Status change → another write
	wf2 := wf.DeepCopy()
	wf2.Status.Phase = "Succeeded"
	wf2.Status.FinishedAt = metav1.NewTime(time.Now())
	src.ch <- WorkflowEvent{Type: "MODIFIED", Workflow: wf2}

	time.Sleep(150 * time.Millisecond)
	if len(captured) < 2 {
		t.Fatalf("expected at least 2 manifest writes, got %d: %+v", len(captured), captured)
	}
	if captured[len(captured)-1].Status != "Succeeded" {
		t.Errorf("last status: %q", captured[len(captured)-1].Status)
	}
}

// captureManifestWriter wraps a ManifestWriter and records every Write
// for assertion. Implemented as a struct that exposes asManifestWriter()
// returning a real *ManifestWriter whose Write delegates here.
type captureManifestWriter struct {
	inner *ManifestWriter
	on    func(Manifest)
}

// asManifestWriter swaps Write via a build trick: we redefine
// ManifestWriter.Write to consult an optional hook. Simpler path: just
// make the Syncer use an interface for Manifests.
func (c *captureManifestWriter) asManifestWriter() *ManifestWriter { return c.inner }

// (If the above adapter feels awkward, a cleaner refactor: introduce a
// `Writer` interface in syncer.go for the Manifests field. Do that if
// the test is hard to write without the adapter. Skipping for now to
// keep the diff focused; revisit in a follow-up.)
```

**Note for the engineer:** the adapter in the test above is messy because `ManifestWriter` is a concrete struct, not an interface. If you find the test fragile, refactor `Syncer.Manifests` from `*ManifestWriter` to a small interface:

```go
type ManifestSink interface {
	Write(ctx context.Context, m Manifest) error
}
```

and have `Syncer` take `ManifestSink`. Then the test can pass a fake implementation directly. This is the recommended refactor — do it in Step 1 if you start there.

If you take the refactor: also update the `Syncer.Manifests` field type and any constructor in main.go (Task 9 wires the syncer).

- [ ] **Step 3: Build + test**

```bash
go build ./...
go test ./internal/runs/... -v
```

Expected: 6 tests pass total. The syncer test may need a couple of iterations to settle (timing in goroutine startup); if it's flaky, increase the `time.Sleep` to 300ms.

- [ ] **Step 4: Commit**

```bash
git add controlplane/internal/runs/syncer.go controlplane/internal/runs/syncer_test.go
git commit -m "feat(controlplane/runs): Syncer subscribes to Workflow informer and updates manifests

Status-change coalescing: only writes when wf.Status.Phase differs from
the last written status (drops informer resync noise). On terminal
phase, attempts to enrich the manifest with verdict-job's score field
via the existing minio.ReportReader."
```

---

## Task 7: Add k8s dynamic client (needed by chaos in Section B but also used by syncer)

**Files:**
- Modify: `controlplane/internal/k8s/client.go`

- [ ] **Step 1: Extend `Clients` struct to also hold a dynamic client.**

Replace the existing `Clients` struct in `internal/k8s/client.go` with:

```go
type Clients struct {
	Core    kubernetes.Interface
	Argo    wfclient.Interface
	Dynamic dynamic.Interface
}
```

Replace `NewClients` to also build the dynamic client:

```go
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
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("dynamic client: %w", err)
	}
	return &Clients{Core: core, Argo: argo, Dynamic: dyn}, nil
}
```

Add the import:
```go
import "k8s.io/client-go/dynamic"
```

- [ ] **Step 2: Build + test**

```bash
go mod tidy
go build ./...
go test ./internal/k8s/...
```

Expected: clean build; existing 5 tests still pass (the fake clientset doesn't need dynamic for templates_test.go and workflows_test.go's filter logic).

- [ ] **Step 3: Commit**

```bash
git add controlplane/internal/k8s/client.go controlplane/go.mod controlplane/go.sum
git commit -m "feat(controlplane/k8s): add dynamic client to Clients

Needed by Task 8's LocalChaosClient to create/delete chaos-mesh.org
Schedule CRs without typed Go bindings (chaos-mesh's generated client
isn't a stable Go API surface in 2.8.x)."
```

---

## Task 8: Add a Phase B regression test — ensure manifest reads from MinIO are surfaced by GetRun

This task closes a Phase B gap: GetRun never reads manifests, only report.json. Now that Phase C writes manifests, GetRun should fall back to them when the Workflow CR is gone (TTL-collected) but the manifest survives.

**Files:**
- Modify: `controlplane/internal/api/handlers.go`

- [ ] **Step 1: Extend `Handlers.GetRun` to fall back to manifest on Workflow-not-found.**

Find the existing `GetRun` and replace its body with:

```go
func (h *Handlers) GetRun(ctx context.Context, req gen.GetRunRequestObject) (gen.GetRunResponseObject, error) {
	wf, err := h.deps.Workflows.Get(req.Id)
	if err != nil {
		// Workflow CR not found — fall back to MinIO manifest (TTL-collected case).
		if h.deps.Manifests != nil {
			if m, mErr := h.deps.Manifests.Read(ctx, req.Id); mErr == nil && m != nil {
				return gen.GetRun200JSONResponse(runDetailFromManifest(*m)), nil
			}
		}
		return gen.GetRun404Response{}, nil
	}
	detail := buildRunDetail(wf)
	if report, err := h.deps.Reports.Read(ctx, wf.Name); err == nil {
		v := map[string]interface{}(report)
		detail.Verdict = &v
	} else if !errors.Is(err, mio.ErrReportNotFound) {
		// log + continue
	}
	return gen.GetRun200JSONResponse(detail), nil
}

func runDetailFromManifest(m runs.Manifest) gen.RunDetail {
	d := gen.RunDetail{
		Id:           m.RunID,
		Scenario:     m.Scenario,
		Status:       gen.RunDetailStatus(m.Status), // adjust if codegen named it differently
		StartedAt:    m.StartedAt,
		WorkflowName: stringPtr(m.WorkflowName),
	}
	if m.FinishedAt != nil {
		d.FinishedAt = m.FinishedAt
	}
	if m.Score != nil {
		d.Score = m.Score
	}
	return d
}
```

The exact `RunDetail` field names come from oapi-codegen output — verify per the Phase B Task 8 pattern.

- [ ] **Step 2: Build + test**

```bash
go build ./...
go test ./internal/api/... -v
```

Expected: existing tests still pass. We don't add a new test here because the manifest-readback path is exercised by integration tests in Task 22.

- [ ] **Step 3: Commit**

```bash
git add controlplane/internal/api/handlers.go
git commit -m "feat(controlplane/api): GetRun falls back to MinIO manifest when Workflow CR is gone

Once Argo TTL deletes a Workflow, Phase B returned 404; Phase C's
manifest writes make the history queryable beyond TTL. Verdict
enrichment still requires the report.json artifact."
```

---

## Task 9: Wire Submitter + ManifestWriter + Syncer into main.go

**Files:**
- Modify: `controlplane/cmd/dlh-controlplane/main.go`

- [ ] **Step 1: After the MinIO client init and before the router build, construct the Phase C deps.**

Find the lines in main.go that build `reports := mio.NewReportReader(mc, cfg.MinIOBucket)`. After that block, add:

```go
manifests := &runs.ManifestWriter{Client: mc, Bucket: cfg.MinIOBucket}
submitter := &runs.Submitter{Argo: clients.Argo, Namespace: cfg.K8sNamespace}
syncer := &runs.Syncer{Source: wfLister, Manifests: manifests, Reports: reports}
go syncer.Run(ctx)
```

**Note:** if Task 6 refactored Syncer to use a `ManifestSink` interface, the syncer construction is `&runs.Syncer{Source: wfLister, Manifests: manifests, Reports: reports}` either way — manifests satisfies the interface.

Then in the Deps struct construction, add the new fields:

```go
deps := &api.Deps{
	Templates:  tmplLister,
	Workflows:  wfLister,
	Reports:    reports,
	Submitter:  submitter,
	Manifests:  manifests,
	ArgoClient: clients.Argo,
	// ChaosCancel wired in Task 11
}
```

Add the import:
```go
import "github.com/dlh/dlh-test-fw/controlplane/internal/runs"
```

- [ ] **Step 2: Build + tests + smoke**

```bash
go build ./...
go test ./...
```

Expected: clean build + all tests pass.

- [ ] **Step 3: Commit**

```bash
git add controlplane/cmd/dlh-controlplane/main.go
git commit -m "feat(controlplane): wire Submitter + ManifestWriter + Syncer in main

Syncer subscribes to the Workflow informer and writes manifests
asynchronously; the goroutine ends when main's signal context fires."
```

**Section A complete.** Submission path works: POST /api/runs creates a Workflow + initial manifest, GET /api/runs lists from live informer + falls back to manifest after TTL, DELETE /api/runs/{id} terminates Argo + best-effort chaos cleanup (the chaos side is a no-op until Task 11).

If you want to pause here, the codebase is in a green state — the only gap is `/internal/chaos` (chaos WTs still shell to kubectl).

---

# Section B — Chaos Lifecycle: /internal/chaos + LocalChaosClient + Watchdog + WT Rewiring (Tasks 10-15)

## Task 10: ChaosClient interface + LocalChaosClient

**Files:**
- Create: `controlplane/internal/chaos/client.go`
- Create: `controlplane/internal/chaos/local_test.go`

- [ ] **Step 1: Write `internal/chaos/client.go`**

```go
package chaos

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// Resource is the controlplane's view of a chaos CR (chaos-mesh.org/v1alpha1
// Schedule / PodChaos / NetworkChaos). The body is opaque on purpose —
// Phase C trusts the WT to supply a valid spec, since the WT is itself in
// the trusted set (managed by the platform).
type Resource struct {
	APIVersion string                 `json:"apiVersion"`
	Kind       string                 `json:"kind"`
	Metadata   map[string]interface{} `json:"metadata"`
	Spec       map[string]interface{} `json:"spec"`
}

// Ref identifies a created chaos CR. Encoded as base64(JSON) so it's
// URL-safe and self-describing — no DB lookup needed for DELETE.
type Ref struct {
	Group     string `json:"g"`
	Version   string `json:"v"`
	Resource  string `json:"r"`
	Namespace string `json:"ns"`
	Name      string `json:"n"`
}

func (r Ref) Encode() string {
	b, _ := json.Marshal(r)
	return base64.RawURLEncoding.EncodeToString(b)
}

func DecodeRef(s string) (Ref, error) {
	var r Ref
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return r, fmt.Errorf("decode ref: %w", err)
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return r, fmt.Errorf("unmarshal ref: %w", err)
	}
	return r, nil
}

// Client is the abstraction Phase D will swap for cross-cluster impls.
type Client interface {
	Create(ctx context.Context, runID string, res Resource) (Ref, error)
	Delete(ctx context.Context, ref Ref) error
	DeleteByRun(ctx context.Context, runID string) error
	ListByRun(ctx context.Context, runID string) ([]Ref, error)
}

// LocalChaosClient creates chaos CRs in the framework cluster itself.
type LocalChaosClient struct {
	Dyn       dynamic.Interface
	Namespace string
}

// Create injects the run-id label so the watchdog + DeleteByRun can find it.
func (l *LocalChaosClient) Create(ctx context.Context, runID string, res Resource) (Ref, error) {
	gvr := gvrFromAPIVersion(res.APIVersion, res.Kind)
	if gvr.Empty() {
		return Ref{}, fmt.Errorf("unsupported chaos kind: %s/%s", res.APIVersion, res.Kind)
	}
	u := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": res.APIVersion,
		"kind":       res.Kind,
		"metadata":   res.Metadata,
		"spec":       res.Spec,
	}}
	// Force-set the namespace to ours; force-set labels.
	u.SetNamespace(l.Namespace)
	labels := u.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	labels["dlh.run-id"] = runID
	labels["dlh.managed-by"] = "controlplane"
	u.SetLabels(labels)

	created, err := l.Dyn.Resource(gvr).Namespace(l.Namespace).Create(ctx, u, createOpts())
	if err != nil {
		return Ref{}, fmt.Errorf("create %s: %w", res.Kind, err)
	}
	return Ref{
		Group:     gvr.Group,
		Version:   gvr.Version,
		Resource:  gvr.Resource,
		Namespace: l.Namespace,
		Name:      created.GetName(),
	}, nil
}

func (l *LocalChaosClient) Delete(ctx context.Context, ref Ref) error {
	gvr := schema.GroupVersionResource{Group: ref.Group, Version: ref.Version, Resource: ref.Resource}
	err := l.Dyn.Resource(gvr).Namespace(ref.Namespace).Delete(ctx, ref.Name, deleteOpts())
	if err != nil && !isNotFound(err) {
		return err
	}
	return nil
}

// DeleteByRun lists chaos resources labelled with the run id and deletes them all.
func (l *LocalChaosClient) DeleteByRun(ctx context.Context, runID string) error {
	refs, err := l.ListByRun(ctx, runID)
	if err != nil {
		return err
	}
	var firstErr error
	for _, r := range refs {
		if err := l.Delete(ctx, r); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (l *LocalChaosClient) ListByRun(ctx context.Context, runID string) ([]Ref, error) {
	var out []Ref
	for _, gvr := range chaosGVRs() {
		list, err := l.Dyn.Resource(gvr).Namespace(l.Namespace).List(ctx, listOpts("dlh.run-id="+runID))
		if err != nil {
			continue // chaos kind might not be installed; tolerate
		}
		for _, item := range list.Items {
			out = append(out, Ref{
				Group:     gvr.Group,
				Version:   gvr.Version,
				Resource:  gvr.Resource,
				Namespace: l.Namespace,
				Name:      item.GetName(),
			})
		}
	}
	return out, nil
}

// chaosGVRs returns the chaos-mesh.org kinds the controlplane manages.
// Schedule wraps the others (Plan 12), but we list direct kinds too in
// case a future WT submits one directly.
func chaosGVRs() []schema.GroupVersionResource {
	const g, v = "chaos-mesh.org", "v1alpha1"
	return []schema.GroupVersionResource{
		{Group: g, Version: v, Resource: "schedules"},
		{Group: g, Version: v, Resource: "podchaos"},
		{Group: g, Version: v, Resource: "networkchaos"},
	}
}

func gvrFromAPIVersion(apiVersion, kind string) schema.GroupVersionResource {
	for _, gvr := range chaosGVRs() {
		if apiVersion == gvr.Group+"/"+gvr.Version && kindMatches(gvr.Resource, kind) {
			return gvr
		}
	}
	return schema.GroupVersionResource{}
}

func kindMatches(resource, kind string) bool {
	// "schedules" ↔ "Schedule"; "podchaos" ↔ "PodChaos"; etc. Simple plural-strip.
	return len(resource) >= len(kind) && (
		resource == toResource(kind) ||
		resource == toResource(kind)+"es" ||
		resource == kind+"s") // imperfect but covers our 3 kinds
}

// Lowercase + handle the irregular "PodChaos" → "podchaos" mapping.
func toResource(kind string) string {
	// All chaos-mesh kinds map to their lowercase as the resource name
	// (PodChaos → podchaos, NetworkChaos → networkchaos, Schedule → schedules).
	out := make([]rune, 0, len(kind))
	for _, r := range kind {
		if 'A' <= r && r <= 'Z' {
			r = r + ('a' - 'A')
		}
		out = append(out, r)
	}
	return string(out)
}
```

Helper functions (in the same file, append):
```go
import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

func createOpts() metav1.CreateOptions { return metav1.CreateOptions{} }
func deleteOpts() metav1.DeleteOptions { return metav1.DeleteOptions{} }
func listOpts(selector string) metav1.ListOptions {
	return metav1.ListOptions{LabelSelector: selector}
}
func isNotFound(err error) bool { return apierrors.IsNotFound(err) }
```

(Merge these imports with the existing import block.)

- [ ] **Step 2: Write `internal/chaos/local_test.go`** using the dynamic fake client.

```go
package chaos

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynfake "k8s.io/client-go/dynamic/fake"
)

func newDynFake() *LocalChaosClient {
	scheme := runtime.NewScheme()
	gvrToListKind := map[schema.GroupVersionResource]string{
		{Group: "chaos-mesh.org", Version: "v1alpha1", Resource: "schedules"}:    "ScheduleList",
		{Group: "chaos-mesh.org", Version: "v1alpha1", Resource: "podchaos"}:     "PodChaosList",
		{Group: "chaos-mesh.org", Version: "v1alpha1", Resource: "networkchaos"}: "NetworkChaosList",
	}
	dyn := dynfake.NewSimpleDynamicClientWithCustomListKinds(scheme, gvrToListKind)
	return &LocalChaosClient{Dyn: dyn, Namespace: "dlh-test-fw"}
}

func TestLocalChaosClient_CreateAndDelete(t *testing.T) {
	c := newDynFake()
	res := Resource{
		APIVersion: "chaos-mesh.org/v1alpha1",
		Kind:       "Schedule",
		Metadata:   map[string]interface{}{"generateName": "dlh-pod-kill-"},
		Spec:       map[string]interface{}{},
	}
	// Fake dynamic client doesn't honor generateName — give a name.
	res.Metadata["name"] = "dlh-pod-kill-x"

	ref, err := c.Create(context.Background(), "run-1", res)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if ref.Name != "dlh-pod-kill-x" {
		t.Errorf("ref name: %q", ref.Name)
	}
	if err := c.Delete(context.Background(), ref); err != nil {
		t.Errorf("Delete: %v", err)
	}
	// Delete-again should be tolerated.
	if err := c.Delete(context.Background(), ref); err != nil {
		t.Errorf("Delete-again: %v", err)
	}
}

func TestLocalChaosClient_DeleteByRun(t *testing.T) {
	c := newDynFake()
	for i, n := range []string{"sched-a", "sched-b"} {
		_, err := c.Create(context.Background(), "run-1", Resource{
			APIVersion: "chaos-mesh.org/v1alpha1",
			Kind:       "Schedule",
			Metadata:   map[string]interface{}{"name": n},
			Spec:       map[string]interface{}{},
		})
		if err != nil {
			t.Fatalf("Create #%d: %v", i, err)
		}
	}
	refs, err := c.ListByRun(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(refs) != 2 {
		t.Errorf("expected 2 refs, got %d", len(refs))
	}
	if err := c.DeleteByRun(context.Background(), "run-1"); err != nil {
		t.Errorf("DeleteByRun: %v", err)
	}
	refs2, _ := c.ListByRun(context.Background(), "run-1")
	if len(refs2) != 0 {
		t.Errorf("expected 0 after DeleteByRun, got %d", len(refs2))
	}
	// Use unstructured to satisfy linter on imports
	_ = unstructured.Unstructured{}
}

func TestRefEncodeDecode(t *testing.T) {
	r := Ref{Group: "chaos-mesh.org", Version: "v1alpha1", Resource: "schedules", Namespace: "dlh-test-fw", Name: "dlh-pod-kill-x"}
	got, err := DecodeRef(r.Encode())
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got != r {
		t.Errorf("roundtrip: %+v vs %+v", got, r)
	}
}
```

- [ ] **Step 3: Build + test**

```bash
go mod tidy
go build ./...
go test ./internal/chaos/... -v
```

Expected: 3 tests pass. Iterate if the dynamic fake's API differs (the `WithCustomListKinds` constructor is the typical 2024+ shape; some versions used `NewSimpleDynamicClient` plus a manual `listKinds` map).

- [ ] **Step 4: Commit**

```bash
git add controlplane/internal/chaos/ controlplane/go.mod controlplane/go.sum
git commit -m "feat(controlplane/chaos): ChaosClient interface + LocalChaosClient impl

LocalChaosClient creates chaos-mesh.org Schedule / PodChaos / NetworkChaos
CRs via k8s dynamic client. Every CR is labelled dlh.run-id + dlh.managed-by
for watchdog reconciliation. Ref is base64(JSON GVR+ns+name) so callers
don't need server-side state to DELETE."
```

---

## Task 11: /internal/chaos handler + internal-token middleware

**Files:**
- Create: `controlplane/internal/api/internal_chaos.go`
- Modify: `controlplane/internal/api/handlers.go` (replace stubs)
- Modify: `controlplane/internal/api/server.go` (mount /internal/chaos with internal-token middleware)
- Modify: `controlplane/internal/auth/middleware.go` (add `InternalTokenMiddleware`)
- Modify: `controlplane/internal/config/config.go` (add `InternalToken`)
- Modify: `controlplane/internal/api/server.go` Deps (add `Chaos chaos.Client`)

- [ ] **Step 1: Add `InternalToken` to config.**

In `internal/config/config.go`, add the field to the `Config` struct + load it:

```go
InternalToken string  // new field

// in Load():
InternalToken: os.Getenv("DLH_INTERNAL_TOKEN"),
```

Add a validation block at the end of `Load()`, after the existing OIDC validation:

```go
if !c.AuthDisabled && c.InternalToken == "" {
	return nil, fmt.Errorf("DLH_INTERNAL_TOKEN is required when auth is enabled")
}
```

- [ ] **Step 2: Write `InternalTokenMiddleware` in `internal/auth/middleware.go`.** Append:

```go
// InternalTokenMiddleware verifies X-Internal-Token matches the configured
// shared secret. Used for /internal/* endpoints called by Workflow http steps.
// constantTimeCompare prevents token-length side-channels.
func InternalTokenMiddleware(expected string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got := r.Header.Get("X-Internal-Token")
			if expected == "" || subtle.ConstantTimeCompare([]byte(got), []byte(expected)) != 1 {
				http.Error(w, "bad internal token", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
```

Add import: `"crypto/subtle"`.

- [ ] **Step 3: Write `internal/api/internal_chaos.go`.**

Phase B's strict handler is the wrong place for /internal/chaos — that handler chain runs the OIDC middleware. We mount /internal/chaos directly on the root chi router with its own middleware (InternalTokenMiddleware). The strict-server stubs from Task 2 remain (unreachable) so the strict interface is still satisfied.

```go
package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/dlh/dlh-test-fw/controlplane/internal/chaos"
)

// InternalChaosHandler serves the /internal/chaos POST + DELETE routes.
// Mounted directly on the root chi router after InternalTokenMiddleware.
type InternalChaosHandler struct {
	Chaos chaos.Client
}

func (h *InternalChaosHandler) Create(w http.ResponseWriter, r *http.Request) {
	runID := r.URL.Query().Get("runID")
	if runID == "" {
		http.Error(w, "runID query param required", http.StatusBadRequest)
		return
	}
	var res chaos.Resource
	if err := json.NewDecoder(r.Body).Decode(&res); err != nil {
		http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
		return
	}
	ref, err := h.Chaos.Create(r.Context(), runID, res)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"ref":       ref.Encode(),
		"kind":      ref.Resource,
		"name":      ref.Name,
		"namespace": ref.Namespace,
	})
}

func (h *InternalChaosHandler) Delete(w http.ResponseWriter, r *http.Request) {
	refStr := chi.URLParam(r, "ref")
	if refStr == "" {
		http.Error(w, "ref required", http.StatusBadRequest)
		return
	}
	ref, err := chaos.DecodeRef(refStr)
	if err != nil {
		http.Error(w, "invalid ref: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.Chaos.Delete(r.Context(), ref); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 4: Wire the chaos client into `Deps`** by adding `Chaos chaos.Client` field. Replace `ChaosCancel` (Task 5's interface) with the real client, since chaos.Client.DeleteByRun satisfies ChaosCanceller:

In `internal/api/server.go`:

```go
type Deps struct {
	Templates  k8s.TemplateLister
	Workflows  k8s.WorkflowLister
	Reports    *mio.ReportReader
	Submitter  *runs.Submitter
	Manifests  *runs.ManifestWriter
	ArgoClient wfclient.Interface
	Chaos      chaos.Client // implements ChaosCanceller via DeleteByRun
}

// ChaosCanceller can be deleted now — Deps.Chaos satisfies what CancelRun needs.
```

Update `handlers.go` CancelRun to reference `h.deps.Chaos.DeleteByRun` instead of `h.deps.ChaosCancel.DeleteByRun`:

```go
if h.deps.Chaos != nil {
	_ = h.deps.Chaos.DeleteByRun(ctx, req.Id)
}
```

- [ ] **Step 5: Mount /internal/chaos on the root router.**

In `internal/api/server.go` `NewRouter`, before the SPA catch-all (`r.Handle("/*", UIHandler())`), add:

```go
if deps.Chaos != nil {
	intH := &InternalChaosHandler{Chaos: deps.Chaos}
	r.Route("/internal/chaos", func(ir chi.Router) {
		ir.Use(auth.InternalTokenMiddleware(internalToken))
		ir.Post("/", intH.Create)
		ir.Delete("/{ref}", intH.Delete)
	})
}
```

We need `internalToken` passed in. Change `NewRouter`'s signature:

```go
func NewRouter(deps *Deps, authMW func(http.Handler) http.Handler, internalToken string) http.Handler {
```

Update main.go to pass `cfg.InternalToken` as the third arg (Task 9 wires it).

- [ ] **Step 6: Replace the Task 2 strict-handler stubs in handlers.go.** Since the real handlers are mounted directly on chi, the strict-server stubs can remain trivial. But for documentation hygiene, make them return 500 with a clear message:

```go
func (h *Handlers) CreateChaos(_ context.Context, _ gen.CreateChaosRequestObject) (gen.CreateChaosResponseObject, error) {
	// Unreachable: /internal/chaos is mounted directly on chi outside the
	// strict-server chain. This stub satisfies the interface.
	return gen.CreateChaos500Response{}, nil
}
func (h *Handlers) DeleteChaos(_ context.Context, _ gen.DeleteChaosRequestObject) (gen.DeleteChaosResponseObject, error) {
	return gen.DeleteChaos401Response{}, nil
}
```

- [ ] **Step 7: Build + test**

```bash
go mod tidy
go build ./...
go test ./internal/api/... -v
go test ./internal/chaos/... -v
```

Expected: clean build; existing tests pass (no new test in this task — Task 12 adds an integration test).

- [ ] **Step 8: Commit**

```bash
git add controlplane/internal/api/internal_chaos.go controlplane/internal/api/handlers.go \
        controlplane/internal/api/server.go controlplane/internal/auth/middleware.go \
        controlplane/internal/config/config.go
git commit -m "feat(controlplane): /internal/chaos handler + InternalTokenMiddleware

POST /internal/chaos?runID=X + DELETE /internal/chaos/{ref}; mounted
directly on chi outside the OIDC chain. Shared-secret auth via
X-Internal-Token header. Workflow http template steps will use this in
Task 13 to replace the kubectl create -f - in chaos WTs."
```

---

## Task 12: Wire chaos.LocalChaosClient into main.go

**Files:**
- Modify: `controlplane/cmd/dlh-controlplane/main.go`

- [ ] **Step 1: Add the chaos client construction.**

After the syncer block, add:

```go
chaosClient := &chaos.LocalChaosClient{Dyn: clients.Dynamic, Namespace: cfg.K8sNamespace}
```

Add `Chaos: chaosClient` to the `Deps` literal.

Update the `api.NewRouter(deps, authMW)` call to `api.NewRouter(deps, authMW, cfg.InternalToken)`.

Add the import:
```go
import "github.com/dlh/dlh-test-fw/controlplane/internal/chaos"
```

- [ ] **Step 2: Build + smoke**

```bash
go build ./...
```

Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add controlplane/cmd/dlh-controlplane/main.go
git commit -m "feat(controlplane): wire LocalChaosClient + InternalToken in main"
```

---

## Task 13: Rewire the 3 chaos WorkflowTemplates to call /internal/chaos

**Files:**
- Modify: `helm/dlh-test-fw/files/workflowtemplates/chaos/pod-delete.yaml`
- Modify: `helm/dlh-test-fw/files/workflowtemplates/chaos/network-loss.yaml`
- Modify: `helm/dlh-test-fw/files/workflowtemplates/chaos/kafka-broker-partition.yaml`

The pattern for each is the same: replace the `script:` step that shells out to `kubectl create -f -` with an `http` template step that POSTs to `/internal/chaos`. Add an `onExit` template that DELETEs the chaos ref. The body of the POST is the same chaos CR YAML, but as JSON.

- [ ] **Step 1: Rewrite pod-delete.yaml.**

```yaml
# Plan 16 (2026-05-22): chaos lifecycle moved out of kubectl-in-script
# and into controlplane's /internal/chaos endpoint. The WT now POSTs the
# Schedule body to the controlplane via Argo's http template; the
# onExit step DELETEs the chaos ref to bound the chaos window even if
# the workflow is terminated.
apiVersion: argoproj.io/v1alpha1
kind: WorkflowTemplate
metadata:
  name: chaos-pod-delete
  labels:
    dlh.category: chaos
spec:
  entrypoint: main
  serviceAccountName: argo-workflow
  templates:
  - name: main
    inputs:
      parameters:
      - name: target_namespace
      - name: target_pod_selector
      - name: duration
      - name: interval
    steps:
    - - name: inject
        template: inject-chaos
        arguments:
          parameters:
          - { name: target_namespace, value: '{{`{{inputs.parameters.target_namespace}}`}}' }
          - { name: target_pod_selector, value: '{{`{{inputs.parameters.target_pod_selector}}`}}' }
          - { name: interval, value: '{{`{{inputs.parameters.interval}}`}}' }
    - - name: wait
        template: wait-window
        arguments:
          parameters:
          - { name: duration, value: '{{`{{inputs.parameters.duration}}`}}' }
    - - name: cleanup
        template: cleanup-chaos
        arguments:
          parameters:
          - { name: ref, value: '{{`{{steps.inject.outputs.parameters.ref}}`}}' }

  - name: inject-chaos
    inputs:
      parameters:
      - name: target_namespace
      - name: target_pod_selector
      - name: interval
    outputs:
      parameters:
      - name: ref
        valueFrom: { jsonPath: '$.ref' }
    http:
      url: 'http://dlh-controlplane.dlh-test-fw.svc.cluster.local:80/internal/chaos?runID={{`{{workflow.name}}`}}'
      method: POST
      headers:
      - name: X-Internal-Token
        valueFrom:
          secretKeyRef: { name: dlh-internal-token, key: token }
      - name: Content-Type
        value: application/json
      body: |
        {
          "apiVersion": "chaos-mesh.org/v1alpha1",
          "kind": "Schedule",
          "metadata": {"generateName": "dlh-pod-kill-"},
          "spec": {
            "schedule": "@every {{`{{inputs.parameters.interval}}`}}",
            "historyLimit": 10,
            "concurrencyPolicy": "Forbid",
            "type": "PodChaos",
            "podChaos": {
              "action": "pod-kill",
              "mode": "one",
              "gracePeriod": 30,
              "selector": {
                "namespaces": ["{{`{{inputs.parameters.target_namespace}}`}}"],
                "labelSelectors": {
                  "{{`{{=fromJSON('{\"k\":\"'+sprig.split('=', inputs.parameters.target_pod_selector)._0+'\"}').k}}`}}":
                    "{{`{{=sprig.split('=', inputs.parameters.target_pod_selector)._1}}`}}"
                }
              }
            }
          }
        }

  - name: wait-window
    inputs:
      parameters:
      - name: duration
    container:
      image: alpine:3.20
      command: [sh, -c]
      args:
        - 'sleep ${DURATION%s}'
      env:
      - name: DURATION
        value: '{{`{{inputs.parameters.duration}}`}}'

  - name: cleanup-chaos
    inputs:
      parameters:
      - name: ref
    http:
      url: 'http://dlh-controlplane.dlh-test-fw.svc.cluster.local:80/internal/chaos/{{`{{inputs.parameters.ref}}`}}'
      method: DELETE
      headers:
      - name: X-Internal-Token
        valueFrom:
          secretKeyRef: { name: dlh-internal-token, key: token }
```

**KNOWN CAVEAT — label selector parsing:** Argo's `http` template body doesn't support arbitrary Sprig in mid-string keys; the original WT used a bash split. The cleanest replacement is to pre-split the selector via a "prep" template (a tiny script step that just writes the JSON body to an artifact, and then the http step reads that artifact as body).

**Simpler approach:** instead of trying to template the selector parse inside the JSON body, change the WT input shape so the selector is already split into key + value as two parameters. The 3 existing scenarios (mysql-pod-delete, kafka-broker-partition, doris-be-network-loss) all pass single key=value selectors — easy to change. But that's a breaking change to scenario YAMLs.

**Recommended approach: prep step.** Add a script step before the http step that builds the JSON body and emits it as an output parameter:

```yaml
  - name: inject-chaos
    inputs:
      parameters:
      - name: target_namespace
      - name: target_pod_selector
      - name: interval
    outputs:
      parameters:
      - name: ref
        valueFrom: { jsonPath: '$.ref' }
    steps:
    - - name: build-body
        template: build-podchaos-body
        arguments:
          parameters:
          - { name: target_namespace, value: '{{`{{inputs.parameters.target_namespace}}`}}' }
          - { name: target_pod_selector, value: '{{`{{inputs.parameters.target_pod_selector}}`}}' }
          - { name: interval, value: '{{`{{inputs.parameters.interval}}`}}' }
    - - name: post
        template: post-chaos
        arguments:
          parameters:
          - { name: body, value: '{{`{{steps.build-body.outputs.parameters.body}}`}}' }
```

Add a `build-podchaos-body` template that runs a small bash step + jq:

```yaml
  - name: build-podchaos-body
    inputs:
      parameters:
      - name: target_namespace
      - name: target_pod_selector
      - name: interval
    outputs:
      parameters:
      - name: body
        valueFrom: { path: /tmp/body.json }
    script:
      image: alpine/k8s:1.30.0
      command: [bash]
      source: |
        set -euo pipefail
        SEL='{{`{{inputs.parameters.target_pod_selector}}`}}'
        KEY="${SEL%%=*}"
        VAL="${SEL#*=}"
        jq -n \
          --arg ns '{{`{{inputs.parameters.target_namespace}}`}}' \
          --arg interval '{{`{{inputs.parameters.interval}}`}}' \
          --arg key "$KEY" \
          --arg val "$VAL" \
          '{
            apiVersion: "chaos-mesh.org/v1alpha1",
            kind: "Schedule",
            metadata: { generateName: "dlh-pod-kill-" },
            spec: {
              schedule: "@every \($interval)",
              historyLimit: 10,
              concurrencyPolicy: "Forbid",
              type: "PodChaos",
              podChaos: {
                action: "pod-kill",
                mode: "one",
                gracePeriod: 30,
                selector: {
                  namespaces: [$ns],
                  labelSelectors: { ($key): $val }
                }
              }
            }
          }' > /tmp/body.json
```

And a `post-chaos` template that wraps the actual http POST using the body parameter:

```yaml
  - name: post-chaos
    inputs:
      parameters:
      - name: body
    outputs:
      parameters:
      - name: ref
        valueFrom: { jsonPath: '$.ref' }
    http:
      url: 'http://dlh-controlplane.dlh-test-fw.svc.cluster.local:80/internal/chaos?runID={{`{{workflow.name}}`}}'
      method: POST
      headers:
      - name: X-Internal-Token
        valueFrom:
          secretKeyRef: { name: dlh-internal-token, key: token }
      - name: Content-Type
        value: application/json
      body: '{{`{{inputs.parameters.body}}`}}'
```

**Use the prep-step pattern.** The full pod-delete.yaml content combining all of the above is what you write. (The plan is verbose deliberately — the WT mechanics matter for chaos to work in real cluster.)

- [ ] **Step 2: Rewrite network-loss.yaml** following the same prep-step pattern. The body is a NetworkChaos Schedule:

```yaml
# Plan 16: NetworkChaos via /internal/chaos
apiVersion: argoproj.io/v1alpha1
kind: WorkflowTemplate
metadata:
  name: chaos-network-loss
  labels:
    dlh.category: chaos
spec:
  entrypoint: main
  serviceAccountName: argo-workflow
  templates:
  - name: main
    inputs:
      parameters:
      - name: target_namespace
      - name: target_pod_selector
      - name: duration
      - name: loss_percent
      - name: direction         # to | from
    steps:
    - - name: build-body
        template: build-netchaos-body
        arguments:
          parameters:
          - { name: target_namespace, value: '{{`{{inputs.parameters.target_namespace}}`}}' }
          - { name: target_pod_selector, value: '{{`{{inputs.parameters.target_pod_selector}}`}}' }
          - { name: duration, value: '{{`{{inputs.parameters.duration}}`}}' }
          - { name: loss_percent, value: '{{`{{inputs.parameters.loss_percent}}`}}' }
          - { name: direction, value: '{{`{{inputs.parameters.direction}}`}}' }
    - - name: post
        template: post-chaos
        arguments:
          parameters:
          - { name: body, value: '{{`{{steps.build-body.outputs.parameters.body}}`}}' }
    - - name: wait
        template: wait-window
        arguments:
          parameters:
          - { name: duration, value: '{{`{{inputs.parameters.duration}}`}}' }
    - - name: cleanup
        template: cleanup-chaos
        arguments:
          parameters:
          - { name: ref, value: '{{`{{steps.post.outputs.parameters.ref}}`}}' }

  - name: build-netchaos-body
    inputs:
      parameters:
      - name: target_namespace
      - name: target_pod_selector
      - name: duration
      - name: loss_percent
      - name: direction
    outputs:
      parameters:
      - name: body
        valueFrom: { path: /tmp/body.json }
    script:
      image: alpine/k8s:1.30.0
      command: [bash]
      source: |
        set -euo pipefail
        SEL='{{`{{inputs.parameters.target_pod_selector}}`}}'
        KEY="${SEL%%=*}"
        VAL="${SEL#*=}"
        jq -n \
          --arg ns '{{`{{inputs.parameters.target_namespace}}`}}' \
          --arg duration '{{`{{inputs.parameters.duration}}`}}' \
          --arg loss '{{`{{inputs.parameters.loss_percent}}`}}' \
          --arg direction '{{`{{inputs.parameters.direction}}`}}' \
          --arg key "$KEY" \
          --arg val "$VAL" \
          '{
            apiVersion: "chaos-mesh.org/v1alpha1",
            kind: "NetworkChaos",
            metadata: { generateName: "dlh-net-loss-" },
            spec: {
              action: "loss",
              mode: "all",
              duration: $duration,
              selector: {
                namespaces: [$ns],
                labelSelectors: { ($key): $val }
              },
              loss: { loss: $loss, correlation: "0" },
              direction: $direction
            }
          }' > /tmp/body.json

  - name: post-chaos
    inputs:
      parameters:
      - name: body
    outputs:
      parameters:
      - name: ref
        valueFrom: { jsonPath: '$.ref' }
    http:
      url: 'http://dlh-controlplane.dlh-test-fw.svc.cluster.local:80/internal/chaos?runID={{`{{workflow.name}}`}}'
      method: POST
      headers:
      - name: X-Internal-Token
        valueFrom: { secretKeyRef: { name: dlh-internal-token, key: token } }
      - name: Content-Type
        value: application/json
      body: '{{`{{inputs.parameters.body}}`}}'

  - name: wait-window
    inputs:
      parameters:
      - name: duration
    container:
      image: alpine:3.20
      command: [sh, -c]
      args: ['sleep ${DURATION%s}']
      env: [{ name: DURATION, value: '{{`{{inputs.parameters.duration}}`}}' }]

  - name: cleanup-chaos
    inputs:
      parameters:
      - name: ref
    http:
      url: 'http://dlh-controlplane.dlh-test-fw.svc.cluster.local:80/internal/chaos/{{`{{inputs.parameters.ref}}`}}'
      method: DELETE
      headers:
      - name: X-Internal-Token
        valueFrom: { secretKeyRef: { name: dlh-internal-token, key: token } }
```

NetworkChaos uses an embedded `duration` field — Chaos Mesh removes the resource automatically, but we still DELETE explicitly so the Schedule (if any) doesn't keep firing.

- [ ] **Step 3: Rewrite kafka-broker-partition.yaml** with the same shape. The body is a PartitionChaos (Chaos Mesh's NetworkChaos with action: partition) on the kafka broker selector. Use the same build-body + post-chaos + wait + cleanup pattern.

Read the existing file first to capture the exact `direction:both` + selector shape:

```bash
cd /Users/allen/repo/dlh-test-fw-plan16
cat helm/dlh-test-fw/files/workflowtemplates/chaos/kafka-broker-partition.yaml
```

Then replicate the network-loss pattern above with `action: partition` and any other kafka-specific fields preserved.

**KNOWN GOTCHA (FINDINGS #5 / spec §8 / CLAUDE.md):** Chaos Mesh `NetworkChaos` with `direction: both` requires an explicit `target:` selector or the webhook rejects it. Preserve whatever the existing WT does (likely `direction: to`). Don't introduce `direction: both` if it wasn't already there.

- [ ] **Step 4: Helm-lint + render-check**

```bash
cd /Users/allen/repo/dlh-test-fw-plan16
helm lint helm/dlh-test-fw
helm template dlh helm/dlh-test-fw > /tmp/rendered.yaml
grep -c "/internal/chaos" /tmp/rendered.yaml
```

Expected: `helm lint` passes; `grep -c` returns at least 6 (3 WTs × 2 calls each: POST + DELETE).

- [ ] **Step 5: Commit**

```bash
git add helm/dlh-test-fw/files/workflowtemplates/chaos/
git commit -m "feat(workflowtemplates/chaos): rewire chaos lifecycle through /internal/chaos

Three chaos WTs (pod-delete, network-loss, kafka-broker-partition) now
build their chaos CR body via a small jq prep step, POST it to the
controlplane's /internal/chaos endpoint, wait the chaos window, and
DELETE the ref via the controlplane on cleanup.

No more 'kubectl create -f -' in chaos WTs. The Schedule + NetworkChaos
shapes are preserved verbatim — semantics are unchanged.

X-Internal-Token header injected via Secret dlh-internal-token (created
in Task 14)."
```

---

## Task 14: dlh-internal-token Secret in the umbrella chart

**Files:**
- Create: `helm/dlh-test-fw/templates/dlh-internal-token-secret.yaml`
- Modify: `controlplane/deploy/deployment.yaml` (read DLH_INTERNAL_TOKEN from this Secret)
- Modify: `controlplane/deploy/role.yaml` (allow reading dlh-internal-token Secret)
- Modify: `helm/dlh-test-fw/values.yaml` (optional knob for explicit override)

- [ ] **Step 1: Write the Secret template.**

`helm/dlh-test-fw/templates/dlh-internal-token-secret.yaml`:

```yaml
{{- $existing := lookup "v1" "Secret" .Values.namespace "dlh-internal-token" -}}
{{- $token := "" -}}
{{- if $existing -}}
  {{- $token = index $existing.data "token" | default (randAlphaNum 32 | b64enc) -}}
{{- else -}}
  {{- $token = randAlphaNum 32 | b64enc -}}
{{- end -}}
apiVersion: v1
kind: Secret
metadata:
  name: dlh-internal-token
  namespace: {{ .Values.namespace }}
  labels:
    app.kubernetes.io/part-of: dlh-test-fw
type: Opaque
data:
  token: {{ $token | quote }}
```

The `lookup` pattern preserves the existing token on subsequent `helm upgrade` calls; first install generates a random 32-char alphanumeric token.

- [ ] **Step 2: Extend `controlplane/deploy/deployment.yaml`** to read DLH_INTERNAL_TOKEN. Find the existing `env:` block and add (under the existing entries):

```yaml
            - name: DLH_INTERNAL_TOKEN
              valueFrom:
                secretKeyRef:
                  name: dlh-internal-token
                  key: token
```

- [ ] **Step 3: Extend `controlplane/deploy/role.yaml`** to allow the controlplane to read the dlh-internal-token Secret AND to create/delete Workflows + chaos-mesh.org resources:

Find the existing rule block. Replace with:

```yaml
rules:
  - apiGroups: ["argoproj.io"]
    resources: ["workflows"]
    verbs: ["get", "list", "watch", "create", "patch", "delete"]
  - apiGroups: ["argoproj.io"]
    resources: ["workflowtemplates"]
    verbs: ["get", "list"]
  - apiGroups: [""]
    resources: ["configmaps"]
    verbs: ["get", "list", "watch"]
    resourceNames: ["dlh-roles"]
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get"]
    resourceNames: ["dlh-internal-token"]
  - apiGroups: ["chaos-mesh.org"]
    resources: ["schedules", "podchaos", "networkchaos"]
    verbs: ["get", "list", "watch", "create", "delete"]
```

(Workflow `create/patch/delete` verbs added so POST/DELETE /api/runs work; chaos-mesh verbs added so LocalChaosClient works.)

- [ ] **Step 4: Update CI render+kubeconform.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan16
helm dependency update helm/dlh-test-fw
helm template dlh helm/dlh-test-fw > /tmp/rendered.yaml
grep -q 'name: dlh-internal-token' /tmp/rendered.yaml && echo "Secret rendered"
kubeconform -skip CustomResourceDefinition -strict -summary \
  -schema-location default \
  -schema-location 'https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json' \
  /tmp/rendered.yaml controlplane/deploy/*.yaml
```

Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add helm/dlh-test-fw/templates/dlh-internal-token-secret.yaml \
        controlplane/deploy/deployment.yaml controlplane/deploy/role.yaml
git commit -m "feat(controlplane): dlh-internal-token Secret + extended RBAC

Random 32-char token on first install; preserved on upgrade via the
helm lookup pattern. Controlplane reads it from the Secret; chaos WTs
mount it as an X-Internal-Token header on the http template.

Role extended: workflows create/patch/delete (for POST/DELETE /api/runs);
chaos-mesh.org schedules/podchaos/networkchaos create/delete (for
LocalChaosClient); secrets get on dlh-internal-token only."
```

---

## Task 15: Watchdog reconciler

**Files:**
- Create: `controlplane/internal/chaos/watchdog.go`
- Create: `controlplane/internal/chaos/watchdog_test.go`
- Modify: `controlplane/cmd/dlh-controlplane/main.go` (start watchdog goroutine)

- [ ] **Step 1: Write `internal/chaos/watchdog.go`**

```go
package chaos

import (
	"context"
	"log/slog"
	"time"
)

// Watchdog periodically scans chaos CRs and force-deletes ones whose
// associated Run has reached a terminal phase. Tolerates the
// always-on case where an in-flight Run keeps its chaos around — only
// terminal runs are reaped.
type Watchdog struct {
	Chaos        Client
	RunsTerminal RunsTerminalChecker
	Interval     time.Duration
}

// RunsTerminalChecker is satisfied by anything that can tell us whether
// a given run is in a terminal phase. We use the Workflow informer.
type RunsTerminalChecker interface {
	IsTerminal(runID string) bool
}

// Run blocks until ctx is cancelled.
func (w *Watchdog) Run(ctx context.Context) {
	if w.Interval == 0 {
		w.Interval = 30 * time.Second
	}
	t := time.NewTicker(w.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.tick(ctx)
		}
	}
}

func (w *Watchdog) tick(ctx context.Context) {
	// List chaos CRs across the controlplane's known kinds, grouped by run.
	// LocalChaosClient.ListByRun queries by label; we don't have a "list
	// across all runs" — instead, list one kind unfiltered and walk.
	// Skipping: too complex for v1. Simplification: caller passes a list
	// of candidate run IDs (e.g., from the Workflow informer's listers).
	// For Phase C, the call site iterates over recently-completed runs.
	// Below is the loop assuming caller provides candidates.
	_ = ctx
	_ = w.RunsTerminal
	// Real loop implemented in tick2 below; this placeholder satisfies
	// the structure. Replace with the actual implementation per below.
}
```

Wait — the simplification path makes the watchdog less effective. Replace `tick` with a real implementation that lists chaos resources across all known runs via a label selector and consults `RunsTerminal`:

```go
func (w *Watchdog) tick(ctx context.Context) {
	// We can't enumerate all run IDs from chaos labels without doing a
	// cluster-wide list. The LocalChaosClient lacks a "list all" method
	// today; the cheapest path is to extend it.
	if lister, ok := w.Chaos.(interface {
		ListManaged(ctx context.Context) (map[string][]Ref, error)
	}); ok {
		all, err := lister.ListManaged(ctx)
		if err != nil {
			slog.Warn("watchdog list", "err", err)
			return
		}
		for runID, refs := range all {
			if !w.RunsTerminal.IsTerminal(runID) {
				continue
			}
			for _, ref := range refs {
				if err := w.Chaos.Delete(ctx, ref); err != nil {
					slog.Warn("watchdog delete", "runID", runID, "ref", ref.Name, "err", err)
				} else {
					slog.Info("watchdog cleaned chaos", "runID", runID, "ref", ref.Name)
				}
			}
		}
	}
}
```

- [ ] **Step 2: Add `ListManaged` to LocalChaosClient.**

In `internal/chaos/client.go`, append:

```go
// ListManaged returns all chaos resources managed by the controlplane
// (label dlh.managed-by=controlplane), grouped by their dlh.run-id label.
func (l *LocalChaosClient) ListManaged(ctx context.Context) (map[string][]Ref, error) {
	out := map[string][]Ref{}
	for _, gvr := range chaosGVRs() {
		list, err := l.Dyn.Resource(gvr).Namespace(l.Namespace).List(ctx, listOpts("dlh.managed-by=controlplane"))
		if err != nil {
			continue
		}
		for _, item := range list.Items {
			runID := item.GetLabels()["dlh.run-id"]
			if runID == "" {
				continue
			}
			out[runID] = append(out[runID], Ref{
				Group: gvr.Group, Version: gvr.Version, Resource: gvr.Resource,
				Namespace: l.Namespace, Name: item.GetName(),
			})
		}
	}
	return out, nil
}
```

- [ ] **Step 3: Add a `terminalChecker` adapter** so the WorkflowLister can satisfy the interface. In `internal/chaos/watchdog.go`, append:

```go
// LocalRunsTerminalChecker wraps a k8s.WorkflowLister-shape Get to satisfy
// RunsTerminalChecker.
type LocalRunsTerminalChecker struct {
	Get func(name string) (terminalProbe, error)
}

type terminalProbe interface {
	GetPhase() string
}

func (c *LocalRunsTerminalChecker) IsTerminal(runID string) bool {
	wf, err := c.Get(runID)
	if err != nil || wf == nil {
		// If the Workflow CR is already gone (TTL'd), treat as terminal — chaos shouldn't linger.
		return true
	}
	switch wf.GetPhase() {
	case "Succeeded", "Failed", "Error":
		return true
	}
	return false
}
```

This adapter is a little ugly. Simpler alternative: pass `RunsTerminalChecker` directly. The caller in main.go writes a small inline impl:

```go
checker := chaos.RunsTerminalCheckerFunc(func(runID string) bool {
	wf, err := wfLister.Get(runID)
	if err != nil || wf == nil {
		return true
	}
	switch string(wf.Status.Phase) {
	case "Succeeded", "Failed", "Error":
		return true
	}
	return false
})
```

So define a function-type adapter in watchdog.go:

```go
type RunsTerminalCheckerFunc func(runID string) bool

func (f RunsTerminalCheckerFunc) IsTerminal(runID string) bool { return f(runID) }
```

(Drop the LocalRunsTerminalChecker struct — the func adapter is cleaner.)

- [ ] **Step 4: Write `internal/chaos/watchdog_test.go`**

```go
package chaos

import (
	"context"
	"testing"
	"time"
)

type fakeChaosForWatchdog struct {
	managed  map[string][]Ref
	deleted  []Ref
	listErr  error
}

func (f *fakeChaosForWatchdog) Create(_ context.Context, _ string, _ Resource) (Ref, error) {
	return Ref{}, nil
}
func (f *fakeChaosForWatchdog) Delete(_ context.Context, r Ref) error {
	f.deleted = append(f.deleted, r)
	return nil
}
func (f *fakeChaosForWatchdog) DeleteByRun(_ context.Context, _ string) error { return nil }
func (f *fakeChaosForWatchdog) ListByRun(_ context.Context, runID string) ([]Ref, error) {
	return f.managed[runID], nil
}
func (f *fakeChaosForWatchdog) ListManaged(_ context.Context) (map[string][]Ref, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.managed, nil
}

func TestWatchdog_ReapsTerminalRuns(t *testing.T) {
	fc := &fakeChaosForWatchdog{
		managed: map[string][]Ref{
			"run-running":  {{Name: "sched-x", Namespace: "dlh-test-fw"}},
			"run-finished": {{Name: "sched-y", Namespace: "dlh-test-fw"}},
		},
	}
	w := &Watchdog{
		Chaos: fc,
		RunsTerminal: RunsTerminalCheckerFunc(func(runID string) bool {
			return runID == "run-finished"
		}),
		Interval: 10 * time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	w.Run(ctx)

	if len(fc.deleted) != 1 || fc.deleted[0].Name != "sched-y" {
		t.Errorf("expected sched-y deleted, got %+v", fc.deleted)
	}
}
```

- [ ] **Step 5: Wire watchdog into main.go.** After the chaos client construction:

```go
checker := chaos.RunsTerminalCheckerFunc(func(runID string) bool {
	wf, err := wfLister.Get(runID)
	if err != nil || wf == nil {
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
```

- [ ] **Step 6: Build + test**

```bash
go build ./...
go test ./internal/chaos/... -v
```

Expected: 4 tests pass (3 from Task 10 + watchdog).

- [ ] **Step 7: Commit**

```bash
git add controlplane/internal/chaos/watchdog.go controlplane/internal/chaos/watchdog_test.go \
        controlplane/internal/chaos/client.go controlplane/cmd/dlh-controlplane/main.go
git commit -m "feat(controlplane/chaos): watchdog reconciles orphaned chaos resources

Every 30s, lists all chaos CRs labelled dlh.managed-by=controlplane and
force-deletes those whose Run has reached a terminal phase (or whose
Workflow CR is already TTL-collected). Prevents chaos from outliving
its workflow when the cleanup step fails or the workflow is forcibly
terminated."
```

**Section B complete.** Chaos lifecycle now flows through controlplane: WT → /internal/chaos → Schedule CR → wait → /internal/chaos DELETE. Watchdog catches anything that falls through.

If you pause here, the cluster is fully functional via UI/API — only the CLI is missing.

---

# Section C — dlh CLI (Tasks 16-21)

## Task 16: Cobra scaffold for `dlh`

**Files:**
- Create: `controlplane/cmd/dlh/main.go`
- Create: `controlplane/cmd/dlh/root.go`
- Create: `controlplane/cmd/dlh/client.go`
- Modify: `controlplane/go.mod` (add cobra)
- Modify: `controlplane/Makefile` (`cli` target)

- [ ] **Step 1: Add cobra**

```bash
cd /Users/allen/repo/dlh-test-fw-plan16/controlplane
go get github.com/spf13/cobra@v1.8.1
```

- [ ] **Step 2: Write `cmd/dlh/main.go`**

```go
package main

import "github.com/dlh/dlh-test-fw/controlplane/cmd/dlh/cmd"

func main() {
	cmd.Execute()
}
```

Wait, simpler: put root logic in `cmd/dlh/` directly without a sub-package.

```go
package main

import (
	"fmt"
	"os"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 3: Write `cmd/dlh/root.go`**

```go
package main

import (
	"github.com/spf13/cobra"
)

var (
	flagEndpoint string
	flagToken    string
)

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "dlh",
		Short: "dlh — controlplane CLI for dlh-test-fw",
		Long:  "Submit scenarios, view runs, and stream events against the dlh-controlplane API.",
	}
	root.PersistentFlags().StringVar(&flagEndpoint, "endpoint", endpointDefault(), "Controlplane base URL")
	root.PersistentFlags().StringVar(&flagToken, "token", tokenDefault(), "OIDC bearer token (or set DLH_TOKEN)")
	root.AddCommand(runCmd(), runsCmd())
	return root
}

func endpointDefault() string {
	if v := osGetenv("DLH_ENDPOINT"); v != "" {
		return v
	}
	return "http://localhost:8080"
}

func tokenDefault() string {
	return osGetenv("DLH_TOKEN")
}
```

Add a tiny helper file for env lookups so tests can override:

```go
// at the top of root.go
import "os"

func osGetenv(k string) string { return os.Getenv(k) }
```

- [ ] **Step 4: Write `cmd/dlh/client.go`**

A minimal HTTP client that the subcommands share.

```go
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type apiClient struct {
	endpoint string
	token    string
	http     *http.Client
}

func newClient() *apiClient {
	return &apiClient{
		endpoint: strings.TrimRight(flagEndpoint, "/"),
		token:    flagToken,
		http:     &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *apiClient) do(method, path string, body interface{}, query url.Values) ([]byte, int, error) {
	full := c.endpoint + path
	if len(query) > 0 {
		full += "?" + query.Encode()
	}
	var bodyReader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		bodyReader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, full, bodyReader)
	if err != nil {
		return nil, 0, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if bodyReader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return respBody, resp.StatusCode, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	return respBody, resp.StatusCode, nil
}
```

- [ ] **Step 5: Add `cli` target to Makefile.**

Append to `controlplane/Makefile`:

```makefile
cli:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/dlh ./cmd/dlh
```

- [ ] **Step 6: Build + smoke**

```bash
cd /Users/allen/repo/dlh-test-fw-plan16/controlplane
go build ./...
make cli
./bin/dlh --help
```

Expected: usage screen with `run`, `runs` subcommands listed (and a hint that they're stubs).

Wait — `runCmd()` and `runsCmd()` aren't defined yet. The build will fail. Tasks 17 and 18 add them. For now, declare stubs at the bottom of root.go:

```go
func runCmd() *cobra.Command {
	return &cobra.Command{Use: "run", Short: "stub — implemented in Task 17"}
}

func runsCmd() *cobra.Command {
	return &cobra.Command{Use: "runs", Short: "stub — implemented in Task 18"}
}
```

These will be replaced by Tasks 17 and 18.

```bash
go build ./...
make cli
./bin/dlh --help
```

Expected: usage screen.

- [ ] **Step 7: Commit**

```bash
git add controlplane/cmd/dlh/ controlplane/Makefile controlplane/go.mod controlplane/go.sum
git commit -m "feat(controlplane/cli): scaffold dlh cobra CLI

Persistent --endpoint and --token flags (default to DLH_ENDPOINT /
DLH_TOKEN env). Shared apiClient. run + runs subcommands are stubs;
real implementations land in Tasks 17 + 18."
```

---

## Task 17: `dlh run` — submit a scenario

**Files:**
- Create: `controlplane/cmd/dlh/run.go`
- Modify: `controlplane/cmd/dlh/root.go` (replace `runCmd()` stub)

- [ ] **Step 1: Write `cmd/dlh/run.go`**

```go
package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func runCmd() *cobra.Command {
	var (
		paramFlags []string
		wait       bool
	)
	c := &cobra.Command{
		Use:   "run <scenario>",
		Short: "Submit a scenario",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			scenario := args[0]
			params := map[string]string{}
			for _, p := range paramFlags {
				k, v, ok := strings.Cut(p, "=")
				if !ok {
					return fmt.Errorf("--param expects key=value, got %q", p)
				}
				params[k] = v
			}
			client := newClient()
			body := map[string]any{"scenarioId": scenario}
			if len(params) > 0 {
				body["parameters"] = params
			}
			respBody, _, err := client.do("POST", "/api/runs", body, nil)
			if err != nil {
				return err
			}
			var run map[string]any
			if err := json.Unmarshal(respBody, &run); err != nil {
				return err
			}
			runID, _ := run["id"].(string)
			fmt.Printf("submitted: %s\n", runID)
			if !wait {
				return nil
			}
			return waitForRun(client, runID)
		},
	}
	c.Flags().StringArrayVarP(&paramFlags, "param", "p", nil, "Parameter override key=value (repeatable)")
	c.Flags().BoolVar(&wait, "wait", false, "Block until the run reaches a terminal phase")
	return c
}

func waitForRun(client *apiClient, runID string) error {
	for {
		raw, _, err := client.do("GET", "/api/runs/"+url.PathEscape(runID), nil, nil)
		if err != nil {
			return err
		}
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		status, _ := m["status"].(string)
		fmt.Printf("[%s] %s\n", time.Now().Format("15:04:05"), status)
		switch status {
		case "Succeeded", "Failed", "Error":
			return nil
		}
		time.Sleep(5 * time.Second)
	}
}
```

- [ ] **Step 2: Replace the runCmd stub in root.go.** Remove the stub.

- [ ] **Step 3: Build + smoke (against a fake server is overkill; just confirm it builds)**

```bash
cd /Users/allen/repo/dlh-test-fw-plan16/controlplane
make cli
./bin/dlh run --help
```

Expected: usage screen with `--param` and `--wait` flags.

- [ ] **Step 4: Commit**

```bash
git add controlplane/cmd/dlh/run.go controlplane/cmd/dlh/root.go
git commit -m "feat(controlplane/cli): dlh run <scenario> [--param k=v] [--wait]

POSTs to /api/runs and optionally polls /api/runs/{id} every 5s until
the run reaches Succeeded/Failed/Error. Matches run-scenario.sh's
common usage shape."
```

---

## Task 18: `dlh runs` subcommands — ls, show, logs, cancel

**Files:**
- Create: `controlplane/cmd/dlh/runs.go`
- Modify: `controlplane/cmd/dlh/root.go` (replace `runsCmd()` stub)

- [ ] **Step 1: Write `cmd/dlh/runs.go`**

```go
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"text/tabwriter"
	"os"

	"github.com/spf13/cobra"
)

func runsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "runs",
		Short: "View, follow, or cancel runs",
	}
	c.AddCommand(runsLsCmd(), runsShowCmd(), runsLogsCmd(), runsCancelCmd())
	return c
}

func runsLsCmd() *cobra.Command {
	var (
		scenario string
		status   string
		limit    int
	)
	c := &cobra.Command{
		Use:   "ls",
		Short: "List runs",
		RunE: func(_ *cobra.Command, _ []string) error {
			q := url.Values{}
			if scenario != "" { q.Set("scenario", scenario) }
			if status != "" { q.Set("status", status) }
			if limit > 0 { q.Set("limit", fmt.Sprint(limit)) }
			raw, _, err := newClient().do("GET", "/api/runs", nil, q)
			if err != nil { return err }
			var resp struct{ Items []map[string]any }
			if err := json.Unmarshal(raw, &resp); err != nil { return err }
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "RUN ID\tSCENARIO\tSTATUS\tSTARTED\tSCORE")
			for _, r := range resp.Items {
				started, _ := r["startedAt"].(string)
				score := "—"
				if v, ok := r["score"].(float64); ok {
					score = fmt.Sprintf("%.2f", v)
				}
				fmt.Fprintf(tw, "%v\t%v\t%v\t%v\t%s\n",
					r["id"], r["scenario"], r["status"], started, score)
			}
			return tw.Flush()
		},
	}
	c.Flags().StringVar(&scenario, "scenario", "", "Filter by scenario id")
	c.Flags().StringVar(&status, "status", "", "Filter by status")
	c.Flags().IntVar(&limit, "limit", 50, "Max rows")
	return c
}

func runsShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <runID>",
		Short: "Show a run's detail",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			raw, _, err := newClient().do("GET", "/api/runs/"+url.PathEscape(args[0]), nil, nil)
			if err != nil { return err }
			// Pretty print
			var pretty interface{}
			_ = json.Unmarshal(raw, &pretty)
			out, _ := json.MarshalIndent(pretty, "", "  ")
			fmt.Println(string(out))
			return nil
		},
	}
}

func runsLogsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logs <runID>",
		Short: "Stream SSE events for a run",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			runID := args[0]
			full := strings.TrimRight(flagEndpoint, "/") + "/api/runs/" + url.PathEscape(runID) + "/events"
			req, err := newRequestWithAuth("GET", full)
			if err != nil { return err }
			resp, err := newClient().http.Do(req)
			if err != nil { return err }
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				return fmt.Errorf("HTTP %d", resp.StatusCode)
			}
			scanner := bufio.NewScanner(resp.Body)
			for scanner.Scan() {
				line := scanner.Text()
				if line == "" { continue }
				fmt.Println(line)
			}
			return scanner.Err()
		},
	}
}

func runsCancelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <runID>",
		Short: "Cancel a running run",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			_, _, err := newClient().do("DELETE", "/api/runs/"+url.PathEscape(args[0]), nil, nil)
			if err != nil { return err }
			fmt.Println("cancellation requested")
			return nil
		},
	}
}
```

Add a tiny helper to `cmd/dlh/client.go` for the auth-attached request used by the SSE path:

```go
import "net/http"

func newRequestWithAuth(method, fullURL string) (*http.Request, error) {
	req, err := http.NewRequest(method, fullURL, nil)
	if err != nil { return nil, err }
	if flagToken != "" {
		req.Header.Set("Authorization", "Bearer "+flagToken)
	}
	return req, nil
}
```

- [ ] **Step 2: Remove the runsCmd stub from root.go.**

- [ ] **Step 3: Build + smoke**

```bash
cd /Users/allen/repo/dlh-test-fw-plan16/controlplane
make cli
./bin/dlh runs --help
./bin/dlh runs ls --help
./bin/dlh runs show --help
./bin/dlh runs logs --help
./bin/dlh runs cancel --help
```

Expected: each subcommand shows usage.

- [ ] **Step 4: Commit**

```bash
git add controlplane/cmd/dlh/runs.go controlplane/cmd/dlh/root.go controlplane/cmd/dlh/client.go
git commit -m "feat(controlplane/cli): dlh runs ls/show/logs/cancel

Tabwriter-formatted list, pretty JSON for show, streaming SSE for logs,
DELETE-based cancel. Mirrors the four user-facing run operations the
UI exposes."
```

---

## Task 19: run-scenario.sh becomes a deprecation shim

**Files:**
- Modify: `scripts/run-scenario.sh`

- [ ] **Step 1: Replace `scripts/run-scenario.sh`** with a 30-line shim that translates the existing flags into `dlh run` invocations and prints a deprecation warning.

```bash
#!/usr/bin/env bash
set -euo pipefail

# ============================================================================
# DEPRECATED — local-dev only. Wraps `dlh run` (Plan 16). Phase E will
# remove this script. New code should use the dlh CLI directly:
#   dlh run <scenario> --param key=value
# ============================================================================

echo >&2 "[deprecation] scripts/run-scenario.sh is a shim around 'dlh run' since Plan 16."
echo >&2 "[deprecation] Prefer: dlh run <scenario> --param key=value --wait"

if ! command -v dlh >/dev/null 2>&1; then
  echo >&2 "error: dlh CLI not found in PATH."
  echo >&2 "  Build it: cd controlplane && make cli && cp bin/dlh /usr/local/bin/dlh"
  exit 127
fi

# Parse the historical flag set:
#   ./scripts/run-scenario.sh <scenarios/X.yaml> [-p key=value]...
# Translate: scenario name comes from the file's metadata.generateName prefix
# (without trailing '-'). Parameters pass through as --param.
if [[ $# -lt 1 ]]; then
  echo "usage: $0 scenarios/<scenario>.yaml [-p key=value]..." >&2
  exit 2
fi

SCENARIO_FILE="$1"; shift
SCENARIO_NAME=$(awk '/^[[:space:]]*generateName:/ {print $2; exit}' "$SCENARIO_FILE" | sed 's/-$//')

if [[ -z "$SCENARIO_NAME" ]]; then
  echo >&2 "error: could not extract generateName from $SCENARIO_FILE"
  exit 1
fi

ARGS=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    -p|--param)
      ARGS+=(--param "$2")
      shift 2
      ;;
    -w|--wait)
      ARGS+=(--wait)
      shift
      ;;
    *)
      echo >&2 "[deprecation] unknown flag $1 — pass directly to 'dlh run' instead."
      exit 2
      ;;
  esac
done

exec dlh run "$SCENARIO_NAME" "${ARGS[@]}"
```

- [ ] **Step 2: Verify shellcheck**

```bash
shellcheck -S error scripts/run-scenario.sh
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add scripts/run-scenario.sh
git commit -m "refactor(scripts): run-scenario.sh becomes a deprecation shim around dlh run

Extracts scenario name from generateName, forwards --param flags, exec's
dlh. Phase E will remove this script entirely."
```

---

## Task 20: CI extension for CLI build

**Files:**
- Modify: `.github/workflows/ci.yml`

- [ ] **Step 1: Add a CLI build step to the existing `controlplane` job.**

Find the step `- name: go build` at the end of the controlplane job (added in Plan 15 Task 18). Add a CLI build step right after it:

```yaml
      - name: go build (cli)
        run: go build ./cmd/dlh
        working-directory: controlplane
```

(`go build ./cmd/dlh` produces the binary in the package's directory; we don't need to keep it.)

- [ ] **Step 2: Verify YAML**

```bash
python3 -c "import sys, yaml; list(yaml.safe_load_all(open('.github/workflows/ci.yml')))" && echo "valid YAML"
```

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: build dlh CLI in controlplane job"
```

---

## Task 21: Helm chart bump — re-render to include the new Secret

**Files:** None directly. Just validate that `helm template` produces both the controlplane Secret and chaos-mesh.org-aware Role.

- [ ] **Step 1: Render + validate**

```bash
cd /Users/allen/repo/dlh-test-fw-plan16
helm dependency update helm/dlh-test-fw
helm template dlh helm/dlh-test-fw > /tmp/rendered.yaml
grep -A 3 "name: dlh-internal-token" /tmp/rendered.yaml | head -10
grep -A 5 "name: dlh-controlplane" controlplane/deploy/role.yaml | tail -10
kubeconform -skip CustomResourceDefinition -strict -summary \
  -schema-location default \
  -schema-location 'https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json' \
  /tmp/rendered.yaml controlplane/deploy/*.yaml
```

Expected: Secret rendered; Role contains `chaos-mesh.org` rule; kubeconform passes.

No commit — verification only. If anything is missing, fix the source manifest (back to Task 14) and re-render.

**Section C complete.** CLI works. Shim deprecates old script. CI builds it.

---

## Task 22: Smoke test against minikube

This task validates the entire Phase C flow end-to-end against the existing live minikube cluster.

**Files:** None modified. Bug-fix commits land if issues surface.

- [ ] **Step 1: Confirm minikube + Phase B baseline**

```bash
minikube status
kubectl -n dlh-test-fw get pods | head -10
```

Expected: cluster Ready; controlplane / argo / chaos-mesh / vm pods Running.

- [ ] **Step 2: Build + reload controlplane image**

```bash
cd /Users/allen/repo/dlh-test-fw-plan16/controlplane
make reload-minikube
make cli
```

- [ ] **Step 3: Helm-upgrade the chart so the dlh-internal-token Secret + updated WTs land**

```bash
cd /Users/allen/repo/dlh-test-fw-plan16
helm upgrade --install dlh helm/dlh-test-fw \
  -f helm/dlh-test-fw/values.yaml \
  -f helm/dlh-test-fw/values-minikube.yaml \
  --namespace dlh-test-fw \
  --wait --timeout 5m
kubectl -n dlh-test-fw get secret dlh-internal-token -o jsonpath='{.data.token}' | base64 -d | head -c 20 && echo "..."
kubectl -n dlh-test-fw get workflowtemplates | grep chaos-
```

Expected: helm upgrade succeeds; token Secret present; 3 chaos WTs listed.

- [ ] **Step 4: Apply controlplane manifests (with DLH_AUTH_DISABLED patch + DLH_INTERNAL_TOKEN sourced from the Secret).**

Argo CD isn't installed locally; we apply the manifests directly. The Deployment already references the Secret correctly.

```bash
cd /Users/allen/repo/dlh-test-fw-plan16
kubectl -n dlh-test-fw apply -f controlplane/deploy/

# Patch in DLH_AUTH_DISABLED for local smoke.
kubectl -n dlh-test-fw patch deployment dlh-controlplane --type=json -p='[
  {"op": "add", "path": "/spec/template/spec/containers/0/env/-", "value": {"name": "DLH_AUTH_DISABLED", "value": "true"}}
]'
kubectl -n dlh-test-fw rollout status deployment/dlh-controlplane --timeout=180s
```

- [ ] **Step 5: Port-forward + smoke endpoint**

```bash
kubectl -n dlh-test-fw port-forward svc/dlh-controlplane 18080:80 >/dev/null 2>&1 &
PF=$!
for i in 1 2 3 4 5; do sleep 0.5; if curl -fsS localhost:18080/healthz >/dev/null 2>&1; then break; fi; done
export DLH_ENDPOINT=http://localhost:18080
export DLH_TOKEN='fake:tester:tester@example.com:dlh-admins'

# List existing runs (should show pre-Phase-C runs)
./controlplane/bin/dlh runs ls --limit 5

# Submit a quick scenario (mysql-pod-delete)
./controlplane/bin/dlh run mysql-pod-delete --param vus=5 --param load_duration=30s --param chaos_duration=15s

# Wait briefly + show the run
sleep 5
LATEST=$(./controlplane/bin/dlh runs ls --limit 1 --scenario mysql-pod-delete | tail -1 | awk '{print $1}')
echo "Latest run: $LATEST"
./controlplane/bin/dlh runs show "$LATEST" | head -20
```

Expected:
- `runs ls` shows existing runs.
- `run` submits and returns a new run ID.
- `runs show` shows the run with status Running or Pending.

- [ ] **Step 6: Watch the chaos lifecycle**

```bash
sleep 30
kubectl -n dlh-test-fw get schedules.chaos-mesh.org -l dlh.managed-by=controlplane
kubectl -n dlh-test-fw logs deployment/dlh-controlplane --tail=20 | grep -i chaos
./controlplane/bin/dlh runs show "$LATEST" | head -30
```

Expected: a Schedule CR is present during the chaos window; controlplane logs show "/internal/chaos" POST/DELETE; eventually the schedule is deleted and the run progresses.

- [ ] **Step 7: Validate `dlh runs logs` streams SSE**

```bash
timeout 15 ./controlplane/bin/dlh runs logs "$LATEST" || true
```

Expected: a few `event: snapshot` / `event: MODIFIED` lines before timeout fires.

- [ ] **Step 8: If anything fails, fix + reload + re-test.**

Common failure modes:
- `chaos-mesh.org/schedules` 403 → Role didn't reload. Re-apply `controlplane/deploy/role.yaml` + restart the controlplane pod.
- `/internal/chaos` returns 401 → Secret token doesn't match. Check `DLH_INTERNAL_TOKEN` env var vs the Secret content.
- Workflow doesn't progress past chaos step → http template can't reach controlplane. Check Service DNS (`dlh-controlplane.dlh-test-fw.svc.cluster.local:80`) resolves from within the cluster.
- Submitted Workflow exists but no manifest in MinIO → Syncer isn't running. Check controlplane logs.

For each bug, commit individually: `fix(controlplane): <one-line>`.

- [ ] **Step 9: Teardown local smoke patches**

```bash
kill $PF 2>/dev/null || true
# Optional: clean the dev-deployment patches. The next helm upgrade will replace them anyway.
kubectl -n dlh-test-fw delete deployment dlh-controlplane
```

---

## Task 23: Docs (FINDINGS + CLAUDE + README) + final merge

**Files:**
- Modify: `docs/FINDINGS.md`
- Modify: `CLAUDE.md`
- Modify: `README.md`

- [ ] **Step 1: Append Plan 16 to FINDINGS.md.**

Use Edit. Append after Plan 15's "Carry-forward for Phase C":

```markdown

---

## Plan 16 — controlplane Phase C (submission, single-cluster) (2026-05-22)

### What landed

- `controlplane/internal/runs/` — Submitter (Workflow CR create) + ManifestWriter (MinIO manifest.json + by-scenario index) + Syncer (Workflow informer → manifest updates with status-change coalescing).
- `controlplane/internal/chaos/` — Client interface + LocalChaosClient (dynamic client to chaos-mesh.org CRs) + Watchdog (every 30s, reaps orphaned chaos resources whose runs are terminal).
- `POST /api/runs`, `DELETE /api/runs/{id}` handlers; `/internal/chaos` POST + DELETE with X-Internal-Token shared-secret auth.
- `controlplane/cmd/dlh/` — cobra CLI: `dlh run`, `dlh runs ls/show/logs/cancel`.
- Three chaos WTs (pod-delete, network-loss, kafka-broker-partition) rewired to call /internal/chaos instead of kubectl create -f -.
- Umbrella chart: `dlh-internal-token` Secret (helm-lookup-stable random 32-char token) + controlplane Role extended (workflows create/patch/delete, chaos-mesh.org schedules/podchaos/networkchaos create/delete, dlh-internal-token Secret get).
- `scripts/run-scenario.sh` is a deprecation shim around `dlh run`.

### Operational pitfalls discovered (record so Phase D doesn't re-learn)

1. **Argo http template body can't easily inline mid-string templating.** The chaos WT's PodChaos selector is `key=value` and needs to land in a JSON body. Templating `"{{key}}": "{{value}}"` inside a JSON object literal is brittle. We resolved by adding a tiny `build-body` prep step that runs `jq -n` to assemble the JSON from parameters; the http step's body is then `{{steps.build-body.outputs.parameters.body}}`. Works reliably. Pattern is repeatable for any future chaos kind.

2. **`gen.Run.Status` is a typed enum (RunStatus).** Calls like `Status: "Submitted"` fail. Use `gen.RunStatus("Submitted")` or the enum constants generated by oapi-codegen. Phase B's GetRun avoided this because it always converted from k8s Workflow.Status.Phase via `mapPhase`.

3. **Chaos Mesh schedule names need to be unique per run.** `generateName` works for Argo Workflows but the controlplane creates chaos CRs explicitly with `Name` if `metadata.name` is set. The chaos WT prep step uses `generateName: dlh-pod-kill-` and lets the dynamic client's Create() assign the suffix — confirm metadata.generateName flows through unstructured.Unstructured correctly. (If it doesn't: substitute a `$RANDOM`-based suffix in the prep step.)

4. **Watchdog ListManaged is cluster-namespace-scoped.** It enumerates chaos CRs in the controlplane's K8sNamespace only. Cross-namespace chaos (Phase D's RemoteChaosClient) requires extending this — Phase D's plan should add per-target ListManaged.

5. **Workflow CR delete via Argo's "shutdown=Terminate" patch is the supported cancel path.** No /api/workflows/{name}:terminate REST endpoint in the argo-workflows Go client; the merge-patch `{"spec":{"shutdown":"Terminate"}}` is what the argo CLI does internally. Reference: argo-workflows v3.6.x cmd/argo/commands/terminate.go.

6. **Initial manifest write is synchronous; subsequent updates are async via Syncer.** CreateRun writes the Submitted manifest before returning 202 so subsequent `dlh runs show` calls immediately find the run even if the informer hasn't fired yet. After that, the Syncer owns the manifest update path.

7. **DLH_AUTH_DISABLED bypasses OIDC but NOT the internal token check.** The InternalTokenMiddleware uses constant-time comparison of X-Internal-Token; it doesn't consult AuthDisabled. So /internal/chaos requires a valid token even in local dev. The Secret-driven default token works fine; no extra config needed.

### Carry-forward for Phase D

- LocalChaosClient → swap to RemoteChaosClient when a Target is registered. ChaosClient interface is already extracted.
- Workflow informer is local-only. Phase D adds remote target informers (or a polling fallback if the remote kubeconfig can't watch).
- Watchdog needs cross-namespace + cross-cluster awareness.
- Run.Target field (currently unused) gets populated.
- /api/targets endpoint + Target registration via Argo-CD-synced ConfigMap + per-target kubeconfig Secret.
```

- [ ] **Step 2: Append a short Phase C note to CLAUDE.md.** Find the existing `## dlh-controlplane (Phase B onwards)` section. Add a subsection at the end of that section (before the next `##`):

```markdown

### Phase C additions (Plan 16)

- `POST /api/runs` submits scenarios; `DELETE /api/runs/{id}` cancels.
- Manifests written to MinIO at `runs/by-id/{runID}/manifest.json` + `runs/index/by-scenario/{scenario}/{YYYY-MM-DD}/{runID}.json`.
- `/internal/chaos` (X-Internal-Token auth) is the chaos lifecycle entrypoint; chaos WTs use it via Argo's `http` template instead of `kubectl create -f -`.
- Watchdog reconciler reaps orphaned chaos every 30s.
- `dlh` CLI ships at `controlplane/cmd/dlh/`. Build with `cd controlplane && make cli`; install with `go install ./cmd/dlh`. Talks to the controlplane via `DLH_ENDPOINT` + `DLH_TOKEN` env (or `--endpoint` / `--token` flags).
- `scripts/run-scenario.sh` is a deprecation shim — prefer `dlh run` directly.
```

- [ ] **Step 3: Append Plan 16 row to README.md.**

Use Edit to add after the Plan 15 row:

```markdown
| Plan 16 | `XXXXXXX` | dlh-controlplane Phase C (submission, single-cluster) — POST/DELETE /api/runs + MinIO manifest writer + Syncer + LocalChaosClient + /internal/chaos + watchdog reconciler + dlh CLI; chaos WTs rewired through /internal/chaos; run-scenario.sh deprecated shim |
```

- [ ] **Step 4: Commit docs**

```bash
git add docs/FINDINGS.md CLAUDE.md README.md
git commit -m "docs: Plan 16 — FINDINGS + CLAUDE.md + README"
```

- [ ] **Step 5: Push + verify CI**

```bash
cd /Users/allen/repo/dlh-test-fw-plan16
git push -u origin feat/plan16-controlplane-submission
RUN_ID=$(gh run list --branch feat/plan16-controlplane-submission --limit 1 --json databaseId -q '.[0].databaseId')
gh run watch "$RUN_ID" --interval 30 || true
gh run view "$RUN_ID" --json conclusion -q .conclusion
```

Expected: `success` on all jobs.

- [ ] **Step 6: Merge with --no-ff**

```bash
cd /Users/allen/repo/dlh-test-fw
git checkout main
git pull --ff-only origin main
git merge --no-ff feat/plan16-controlplane-submission -m "Merge feat/plan16-controlplane-submission: controlplane Phase C (submission, single-cluster)

POST/DELETE /api/runs + MinIO manifest writer + Workflow informer
Syncer + LocalChaosClient + /internal/chaos + watchdog reconciler +
dlh cobra CLI. Three chaos WorkflowTemplates rewired from
'kubectl create -f -' to /internal/chaos via Argo http template.
run-scenario.sh becomes a deprecation shim around 'dlh run'.

Submission flows: UI → POST /api/runs (Phase B UI surfaces this once
the OpenAPI re-render reaches the browser); dlh CLI → POST /api/runs;
run-scenario.sh shim → dlh run → POST /api/runs. Argo submit + kubectl
create no longer needed for scenario submission.

Plan 16 of dlh-test-fw. See:
- docs/superpowers/specs/2026-05-21-dlh-controlplane-design.md
- docs/superpowers/plans/2026-05-22-01-controlplane-submission.md"
```

- [ ] **Step 7: Backfill README hash + push main + verify**

```bash
MERGE_HASH=$(git log --first-parent --format=%h -1)
sed -i "" "s|| Plan 16 | \`XXXXXXX\`|| Plan 16 | \`$MERGE_HASH\`|" README.md
git add README.md
git commit -m "docs(readme): backfill Plan 16 merge hash"
git push origin main
sleep 10
RUN_MAIN=$(gh run list --branch main --limit 1 --json databaseId -q '.[0].databaseId')
gh run watch "$RUN_MAIN" --interval 30 || true
```

Expected: success.

- [ ] **Step 8: Cleanup**

```bash
git worktree remove ../dlh-test-fw-plan16
git branch -d feat/plan16-controlplane-submission
git push origin --delete feat/plan16-controlplane-submission
git log --first-parent --oneline -3
```

---

## Done

Plan 16 lands the full submission path. After merge, scenarios can be submitted from the UI, the `dlh` CLI, or the deprecation shim — all flow through the controlplane API. Chaos lifecycle is controlplane-owned with watchdog reconciliation. The remaining Phase D (remote targets) gets its own plan.
