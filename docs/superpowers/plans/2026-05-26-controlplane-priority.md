# Controlplane Priority — Visibility & Control — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the existing Argo workflow-priority mechanism visible and controllable in the controlplane across four phases — submit-time override + display, a read-only Queue view, editable per-scenario defaults, and (after a feasibility spike) live re-prioritization of pending runs.

**Architecture:** The priority mechanism already exists in-cluster (each scenario WorkflowTemplate bakes `spec.priority: 100`; a per-target-type semaphore `dlh-scenario-locks` serializes same-type runs; Argo releases blocked workflows in priority-desc, oldest-first order). This plan plumbs that through the controlplane: the OpenAPI spec gains `priority` on the run DTOs + three new endpoints (`/api/queue`, `/api/runs/{id}/priority`, `/api/scenario-priorities`); the submitter always resolves an *effective* priority (request → scenario-default ConfigMap → template's baked value) and stamps it on `wf.Spec.Priority` so every Workflow is self-describing; the React SPA gains a Queue page and a Default-priorities admin page. Pure logic (queue ordering, tier mapping, priority resolution) is unit-tested; the codegen-driven request/response types are regenerated from the OpenAPI spec.

**Tech Stack:** Go (oapi-codegen strict handlers, chi v5, client-go Argo informer), React + Vite + Tailwind + shadcn/ui (openapi-typescript generated client), Vitest, Helm. Spec: `docs/superpowers/specs/2026-05-25-controlplane-priority-design.md`.

**Important conventions (from CLAUDE.md / prior plans):**
- **NEVER `git add -A`.** `controlplane/internal/api/dist/` must NOT be committed — stage only the specific files each step names.
- After editing `controlplane/api/openapi.yaml`, run `cd controlplane && make codegen` to regenerate `internal/api/gen/*.go` AND `web/src/api/gen.ts`. Commit the regenerated files.
- `deriveTargetType` is intentionally duplicated in Go (`internal/links`) and TS (`web/src/lib/category.ts`) — keep the two rule lists in sync.
- Go tests: `cd controlplane && go test ./...`. Web tests: `cd controlplane/web && pnpm test`. Web build gate: `pnpm build`.
- Local-dev deploy after backend changes: `cd controlplane && make ui-build && make reload-minikube && kubectl -n dlh-test-fw rollout restart deployment/dlh-controlplane`.
- Fake local-dev token: `Authorization: Bearer fake:admin:admin@local:dlh-admin` (admin), `fake:runner:runner@local:dlh-runners` (runner). `DLH_AUTH_DISABLED=true` for vite dev.

---

## File Structure

**Phase 1 (Layer 1 — submit + display):**
- Modify `controlplane/api/openapi.yaml` — add `priority` to `CreateRunRequest` + `Run`.
- Modify `controlplane/internal/runs/submit.go` — `SubmitRequest.Priority`, effective-priority resolution, set `wf.Spec.Priority`.
- Modify `controlplane/internal/model/types.go` — `RunFromWorkflow` reads `wf.Spec.Priority` into `Run.Priority`.
- Modify `controlplane/internal/api/handlers.go` — `CreateRun` forwards `body.Priority`.
- Modify `controlplane/cmd/dlh/run.go` — `--priority` flag.
- Modify `controlplane/web/src/pages/ScenariosPage.tsx` — priority control on the Run card.
- Modify `controlplane/web/src/pages/RunsPage.tsx` — Priority column.

**Phase 2 (Queue view — read-only):**
- Create `controlplane/internal/queue/queue.go` + `queue_test.go` — pure grouping/ordering logic.
- Modify `controlplane/internal/config/config.go` — `LocksConfigMapName`.
- Modify `controlplane/api/openapi.yaml` — `GET /api/queue` + `Queue`/`QueueLane`/`QueueEntry` schemas.
- Modify `controlplane/internal/api/handlers.go` + `controlplane/internal/api/deps.go` — `GetQueue` handler + a locks reader.
- Create `controlplane/web/src/pages/QueuePage.tsx`; modify `controlplane/web/src/App.tsx` — nav item + route.
- Modify `controlplane/cmd/dlh/` — new `queue` command.
- Modify `controlplane/deploy/role.yaml` + chart — RBAC to read `dlh-scenario-locks`.

**Phase 3 (Layer 3 — editable defaults):**
- Create `controlplane/internal/priorities/priorities.go` + `priorities_test.go` — read/write `dlh-scenario-priorities` CM.
- Create `helm/dlh-test-fw/templates/dlh-scenario-priorities-configmap.yaml`.
- Modify `controlplane/internal/runs/submit.go` — insert scenario-default lookup into the resolution chain.
- Modify `controlplane/api/openapi.yaml` — `GET /api/scenario-priorities` + `PUT /api/scenario-priorities/{id}` + `ScenarioPriority` schema.
- Modify `controlplane/internal/api/handlers.go` — `GetScenarioPriorities` + `PutScenarioPriority` (admin).
- Create `controlplane/web/src/lib/tier.ts` + `tier.test.ts`.
- Create `controlplane/web/src/pages/DefaultPrioritiesPage.tsx`; modify `App.tsx` — route + link from Queue.
- Modify `controlplane/deploy/role.yaml` — RBAC update/patch on `dlh-scenario-priorities`.

**Phase 4 (Layer 2 — live re-prioritize, AFTER SPIKE):**
- Create `docs/FINDINGS.md` append (spike result) — patch-vs-resubmit decision.
- Modify `controlplane/internal/runs/` — reprioritize logic (pending-only guard).
- Modify `controlplane/api/openapi.yaml` — `POST /api/runs/{id}/priority`.
- Modify `controlplane/internal/api/handlers.go` — `Reprioritize` handler (runner, 409 if not Pending).
- Modify `controlplane/cmd/dlh/runs.go` — `reprioritize` subcommand.
- Modify `controlplane/web/src/pages/QueuePage.tsx` — reorder / to-front / cancel controls.

---
---

# PHASE 1 — Layer 1: Submit-time override + display

**Phase goal:** A run can be launched with an explicit priority via API/CLI/UI; every run displays its effective priority in the Runs list and Run detail. `RunDetail.priority` + RunDetailPage Priority cell already exist — this phase makes them (and the new list-view + submit path) actually carry a value.

### Task 1.1: Add `priority` to OpenAPI `CreateRunRequest` and `Run`

**Files:**
- Modify: `controlplane/api/openapi.yaml` (CreateRunRequest ~452-466; Run ~386-403)
- Regenerate: `controlplane/internal/api/gen/types.gen.go`, `controlplane/internal/api/gen/server.gen.go`, `controlplane/web/src/api/gen.ts`

- [ ] **Step 1: Add `priority` to `CreateRunRequest`**

In `controlplane/api/openapi.yaml`, find the `CreateRunRequest` schema and add a `priority` property after `targetId`:

```yaml
    CreateRunRequest:
      type: object
      required: [scenarioId]
      properties:
        scenarioId:
          type: string
          description: "WorkflowTemplate name (e.g. mysql-pod-delete)"
        targetId:
          type: string
          description: "Optional remote target ID. Empty = inject chaos in framework cluster."
        priority:
          type: integer
          description: "Optional priority override. Empty = scenario default (or baked WT value)."
        parameters:
          type: object
          description: "Optional parameter overrides. Keys are WT parameter names."
          additionalProperties:
            type: string
```

- [ ] **Step 2: Add `priority` to `Run`**

In the `Run` schema, add a `priority` property after `score`:

```yaml
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
        priority:   { type: integer, description: "Effective workflow priority. Absent for legacy runs with no spec.priority." }
        workflowName: { type: string }
        target:       { type: string, description: "Remote target ID (Run was injected into a remote cluster). Empty = local." }
        triggeredBy:
          type: object
          description: "Set when the run was fired by a Schedule (CronWorkflow)."
          properties:
            kind: { type: string, example: "Schedule" }
            id:   { type: string, description: "Schedule id (CronWorkflow name)" }
```

- [ ] **Step 3: Regenerate codegen**

Run: `cd controlplane && make codegen`
Expected: regenerates `internal/api/gen/types.gen.go` (adds `Priority *int` to `Run` and `Priority *int` to `CreateRunRequest`), `internal/api/gen/server.gen.go`, and `web/src/api/gen.ts`. No errors.

- [ ] **Step 4: Verify it compiles**

Run: `cd controlplane && go build ./...`
Expected: exit 0.

- [ ] **Step 5: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/api/openapi.yaml controlplane/internal/api/gen/types.gen.go controlplane/internal/api/gen/server.gen.go controlplane/web/src/api/gen.ts
git commit -m "feat(api): add priority to CreateRunRequest + Run schema

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 1.2: Submitter resolves + stamps effective priority

**Files:**
- Modify: `controlplane/internal/runs/submit.go:20-89`
- Test: `controlplane/internal/runs/submit_test.go`

**Design:** `Submit` already calls `WorkflowTemplates(...).Get(...)` to verify the template exists (submit.go:43). Capture that returned template and read its baked `Spec.Priority`. Resolve the effective priority: `req.Priority` (if non-nil) → else `tmpl.Spec.Priority` (baked) → else nil. Set `wf.Spec.Priority` (a `*int32`). (Phase 3 inserts a scenario-default lookup between request and baked.)

- [ ] **Step 1: Write the failing test**

Add to `controlplane/internal/runs/submit_test.go`:

```go
func TestSubmit_PriorityOverrideStampsWorkflow(t *testing.T) {
	ns := "dlh-test-fw"
	baked := int32(100)
	tmpl := &wfv1.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-pod-delete", Namespace: ns},
		Spec:       wfv1.WorkflowSpec{Priority: &baked},
	}
	argo := wfake.NewSimpleClientset(tmpl)
	s := &Submitter{Argo: argo, Namespace: ns}

	// explicit override wins
	p := 500
	res, err := s.Submit(context.Background(), SubmitRequest{ScenarioID: "mysql-pod-delete", Priority: &p})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	got, _ := argo.ArgoprojV1alpha1().Workflows(ns).Get(context.Background(), res.RunID, metav1.GetOptions{})
	if got.Spec.Priority == nil || *got.Spec.Priority != 500 {
		t.Errorf("override priority: got %v want 500", got.Spec.Priority)
	}
}

func TestSubmit_PriorityFallsBackToBaked(t *testing.T) {
	ns := "dlh-test-fw"
	baked := int32(100)
	tmpl := &wfv1.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-pod-delete", Namespace: ns},
		Spec:       wfv1.WorkflowSpec{Priority: &baked},
	}
	argo := wfake.NewSimpleClientset(tmpl)
	s := &Submitter{Argo: argo, Namespace: ns}

	res, err := s.Submit(context.Background(), SubmitRequest{ScenarioID: "mysql-pod-delete"}) // no override
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	got, _ := argo.ArgoprojV1alpha1().Workflows(ns).Get(context.Background(), res.RunID, metav1.GetOptions{})
	if got.Spec.Priority == nil || *got.Spec.Priority != 100 {
		t.Errorf("baked priority: got %v want 100", got.Spec.Priority)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd controlplane && go test ./internal/runs/ -run TestSubmit_Priority -v`
Expected: FAIL — `SubmitRequest` has no `Priority` field (compile error), and `wf.Spec.Priority` is never set.

- [ ] **Step 3: Add `Priority` to `SubmitRequest`**

In `controlplane/internal/runs/submit.go`, modify the struct (lines 20-26):

```go
// SubmitRequest is the inbound payload (one-step removed from the HTTP DTO).
type SubmitRequest struct {
	ScenarioID string
	TargetID   string
	Priority   *int // explicit override; nil = use scenario default / baked value
	Parameters map[string]string
	CreatedBy  string // OIDC subject
}
```

- [ ] **Step 4: Capture the template and stamp the effective priority**

In `submit.go`, change the template-existence check (lines 42-49) to capture the template, then set `wf.Spec.Priority` when building the Workflow. The existing block:

```go
	// Verify the template exists; this becomes 404 to the API caller.
	if _, err := s.Argo.ArgoprojV1alpha1().WorkflowTemplates(s.Namespace).Get(ctx, req.ScenarioID, metav1.GetOptions{}); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("scenario %q not found: %w", req.ScenarioID, err)
		}
		return nil, fmt.Errorf("get workflowtemplate %q: %w", req.ScenarioID, err)
	}
```

becomes:

```go
	// Verify the template exists; this becomes 404 to the API caller.
	tmpl, err := s.Argo.ArgoprojV1alpha1().WorkflowTemplates(s.Namespace).Get(ctx, req.ScenarioID, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("scenario %q not found: %w", req.ScenarioID, err)
		}
		return nil, fmt.Errorf("get workflowtemplate %q: %w", req.ScenarioID, err)
	}
```

Then, immediately before the `wf := &wfv1.Workflow{...}` literal (around line 70), add the resolution:

```go
	// Resolve the EFFECTIVE priority so the Workflow is self-describing in the
	// UI: explicit request override → (Phase 3: scenario default) → template's
	// baked spec.priority. nil leaves spec.priority unset (legacy behaviour).
	effPriority := s.resolvePriority(req, tmpl)
```

And in the `wf.Spec` literal, add the `Priority` field:

```go
		Spec: wfv1.WorkflowSpec{
			WorkflowTemplateRef: &wfv1.WorkflowTemplateRef{Name: req.ScenarioID},
			Arguments:           wfv1.Arguments{Parameters: params},
			Priority:            effPriority,
		},
```

- [ ] **Step 5: Add the `resolvePriority` helper**

At the end of `submit.go`, add:

```go
// resolvePriority returns the effective workflow priority pointer.
// Order: explicit request override → template's baked spec.priority.
// (Phase 3 inserts a scenario-default ConfigMap lookup between the two.)
func (s *Submitter) resolvePriority(req SubmitRequest, tmpl *wfv1.WorkflowTemplate) *int32 {
	if req.Priority != nil {
		v := int32(*req.Priority)
		return &v
	}
	if tmpl != nil && tmpl.Spec.Priority != nil {
		v := *tmpl.Spec.Priority
		return &v
	}
	return nil
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd controlplane && go test ./internal/runs/ -v`
Expected: PASS (all submit tests, including the two new ones and the pre-existing `TestSubmit_CreatesWorkflowWithTemplateRef`).

- [ ] **Step 7: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/internal/runs/submit.go controlplane/internal/runs/submit_test.go
git commit -m "feat(runs): submitter resolves + stamps effective priority

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 1.3: Map `wf.Spec.Priority` into the Run list DTO

**Files:**
- Modify: `controlplane/internal/model/types.go:84-117` (`RunFromWorkflow`)
- Test: `controlplane/internal/model/types_test.go` (create if absent)

**Note:** `RunDetailFromWorkflow` already populates `d.Priority` (types.go:157-161). Only the list-view `RunFromWorkflow` needs it.

- [ ] **Step 1: Write the failing test**

Append to `controlplane/internal/model/types_test.go` (create the file with this content if it does not exist — match the package name `model`):

```go
package model

import (
	"testing"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestRunFromWorkflow_Priority(t *testing.T) {
	p := int32(200)
	wf := &wfv1.Workflow{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-pod-delete-20260101-000000",
			Labels: map[string]string{"dlh.scenario": "mysql-pod-delete"}},
		Spec: wfv1.WorkflowSpec{Priority: &p},
	}
	r := RunFromWorkflow(wf)
	if r.Priority == nil || *r.Priority != 200 {
		t.Errorf("priority: got %v want 200", r.Priority)
	}

	// no priority → nil
	wf2 := &wfv1.Workflow{ObjectMeta: metav1.ObjectMeta{Name: "x"}}
	if RunFromWorkflow(wf2).Priority != nil {
		t.Error("expected nil priority for workflow with no spec.priority")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd controlplane && go test ./internal/model/ -run TestRunFromWorkflow_Priority -v`
Expected: FAIL — `RunFromWorkflow` never sets `r.Priority`.

- [ ] **Step 3: Populate `Priority` in `RunFromWorkflow`**

In `controlplane/internal/model/types.go`, inside `RunFromWorkflow`, add after the `r.Target` block (after line ~108, before the `for _, owner := range wf.OwnerReferences` loop):

```go
	if wf.Spec.Priority != nil {
		p := int(*wf.Spec.Priority)
		r.Priority = &p
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd controlplane && go test ./internal/model/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/internal/model/types.go controlplane/internal/model/types_test.go
git commit -m "feat(model): map wf.Spec.Priority into Run list DTO

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 1.4: `CreateRun` handler forwards priority

**Files:**
- Modify: `controlplane/internal/api/handlers.go:131-186` (`CreateRun`)
- Test: `controlplane/internal/api/handlers_test.go`

- [ ] **Step 1: Write the failing test**

Append to `controlplane/internal/api/handlers_test.go`:

```go
func TestCreateRun_ForwardsPriority(t *testing.T) {
	ns := "dlh-test-fw"
	baked := int32(100)
	tmpl := &wfv1.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-pod-delete", Namespace: ns},
		Spec:       wfv1.WorkflowSpec{Priority: &baked},
	}
	argo := wfake.NewSimpleClientset(tmpl)
	deps := &Deps{
		Templates: &fakeTemplates{items: []wfv1.WorkflowTemplate{*tmpl}},
		Submitter: &runs.Submitter{Argo: argo, Namespace: ns},
		Manifests: &runs.ManifestWriter{Client: nil, Bucket: "artifacts"},
	}
	h := &Handlers{deps: deps}

	prio := 500
	req := gen.CreateRunRequestObject{Body: &gen.CreateRunRequest{ScenarioId: "mysql-pod-delete", Priority: &prio}}
	resp, err := h.CreateRun(context.Background(), req)
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	out := resp.(gen.CreateRun202JSONResponse)
	got, _ := argo.ArgoprojV1alpha1().Workflows(ns).Get(context.Background(), out.Id, metav1.GetOptions{})
	if got.Spec.Priority == nil || *got.Spec.Priority != 500 {
		t.Errorf("workflow priority: got %v want 500", got.Spec.Priority)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd controlplane && go test ./internal/api/ -run TestCreateRun_ForwardsPriority -v`
Expected: FAIL — `CreateRun` does not read `body.Priority`, so the stamped priority is the baked 100, not 500.

- [ ] **Step 3: Forward `body.Priority` in `CreateRun`**

In `controlplane/internal/api/handlers.go`, inside `CreateRun`, after the `targetID` extraction block (after line ~157, before the `h.deps.Submitter.Submit` call), build the `SubmitRequest` with `Priority`:

```go
	targetID := ""
	if body.TargetId != nil {
		targetID = *body.TargetId
	}
	sr, err := h.deps.Submitter.Submit(ctx, runs.SubmitRequest{
		ScenarioID: body.ScenarioId,
		TargetID:   targetID,
		Priority:   body.Priority,
		Parameters: params,
		CreatedBy:  createdBy,
	})
```

(Only the `Priority: body.Priority,` line is new — `body.Priority` is `*int`, matching `SubmitRequest.Priority`.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd controlplane && go test ./internal/api/ -v`
Expected: PASS (new test + existing `TestCreateRun_Submits`, `TestListRuns`, etc.).

- [ ] **Step 5: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/internal/api/handlers.go controlplane/internal/api/handlers_test.go
git commit -m "feat(api): CreateRun forwards priority override to submitter

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 1.5: CLI `dlh run --priority N`

**Files:**
- Modify: `controlplane/cmd/dlh/run.go:13-61`

- [ ] **Step 1: Add the `--priority` flag and request field**

In `controlplane/cmd/dlh/run.go`, add a `priority` var to the flag block and wire it into the request body. The `var (...)` block becomes:

```go
	var (
		paramFlags []string
		wait       bool
		target     string
		priority   int
	)
```

Inside `RunE`, after the `if target != "" { body["targetId"] = target }` block, add:

```go
			if priority != 0 {
				body["priority"] = priority
			}
```

And register the flag alongside the others (after the `--target` flag registration):

```go
	c.Flags().IntVar(&priority, "priority", 0, "Workflow priority override (0 = scenario default)")
```

- [ ] **Step 2: Verify it compiles**

Run: `cd controlplane && go build ./cmd/dlh`
Expected: exit 0.

- [ ] **Step 3: Verify the flag is wired**

Run: `cd controlplane && go run ./cmd/dlh run --help`
Expected: output includes a `--priority int   Workflow priority override (0 = scenario default)` line.

- [ ] **Step 4: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/cmd/dlh/run.go
git commit -m "feat(cli): dlh run --priority N

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 1.6: UI — priority control on Scenarios + Priority column on Runs

**Files:**
- Modify: `controlplane/web/src/pages/ScenariosPage.tsx:36-49, 105-131`
- Modify: `controlplane/web/src/pages/RunsPage.tsx:154-194`
- (RunDetailPage already shows priority — no change.)

**Design:** A small numeric input next to the TargetPicker on each scenario card (placeholder shows the baked default; empty = use default). A Priority column between Status and Started on the Runs table.

- [ ] **Step 1: Add priority state + control to ScenariosPage**

In `controlplane/web/src/pages/ScenariosPage.tsx`, add a state map alongside `submitTarget` (near the top of the component, where `submitTarget`/`setSubmitTarget` are declared):

```tsx
  const [submitPriority, setSubmitPriority] = useState<Record<string, string>>({});
```

Update `handleRun` (lines 36-49) to include priority in the POST body:

```tsx
  const handleRun = async (s: Scenario) => {
    setSubmitting(s.id);
    try {
      const targetId = submitTarget[s.id] || undefined;
      const raw = submitPriority[s.id];
      const priority = raw && raw.trim() !== "" ? Number(raw) : undefined;
      const { data, error } = await api.POST("/api/runs", {
        body: { scenarioId: s.id, targetId, priority },
      });
      if (error) toast.error("Submit failed", { description: JSON.stringify(error) });
      else if (data?.id) {
        toast.success(`Run ${data.id} submitted`);
        navigate(`/runs/${data.id}`);
      }
    } finally {
      setSubmitting(null);
    }
  };
```

In the card's action row (lines 117-129), add an `Input` before the `TargetPicker`. Ensure `Input` is imported (`import { Input } from "@/components/ui/input";`):

```tsx
      <div className="mt-auto flex items-center gap-2">
        <Input
          type="number"
          value={submitPriority[s.id] ?? ""}
          onChange={(e) => setSubmitPriority((r) => ({ ...r, [s.id]: e.target.value }))}
          placeholder="prio"
          title="Priority override (blank = scenario default)"
          className="h-8 w-[72px]"
        />
        <TargetPicker
          value={submitTarget[s.id] ?? ""}
          onChange={(v) => setSubmitTarget((r) => ({ ...r, [s.id]: v }))}
          filterType={s.targetType ?? undefined}
        />
        <Button size="sm" disabled={submitting === s.id} onClick={() => handleRun(s)}>
          <Play className="h-3.5 w-3.5" />
          {submitting === s.id ? "Submitting…" : "Run"}
        </Button>
      </div>
```

(Note: this also fixes a pre-existing JSX typo at line 127 where `{submitting === s.id} ? "Submitting…" : "Run"}` was malformed — the corrected ternary is `{submitting === s.id ? "Submitting…" : "Run"}`.)

- [ ] **Step 2: Add the Priority column to RunsPage**

In `controlplane/web/src/pages/RunsPage.tsx`, add a header cell after the Status `<TableHead>` (line ~159):

```tsx
    <TableHead>Status</TableHead>
    <TableHead>Priority</TableHead>
```

And a matching body cell after the `<StatusBadge>` `<TableCell>` (line ~179):

```tsx
      <TableCell><StatusBadge status={String(r.status)} /></TableCell>
      <TableCell className="text-muted-foreground tabular-nums">{r.priority ?? "—"}</TableCell>
```

- [ ] **Step 3: Verify the build + tests pass**

Run: `cd controlplane/web && pnpm build && pnpm test`
Expected: `tsc -b && vite build` succeeds (exit 0); Vitest reports all existing tests passing.

- [ ] **Step 4: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/web/src/pages/ScenariosPage.tsx controlplane/web/src/pages/RunsPage.tsx
git commit -m "feat(web): priority control on Scenarios + Priority column on Runs

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 1.7: Phase 1 live verification

**Files:** none (deploy + verify)

- [ ] **Step 1: Build + deploy**

Run:
```bash
cd /Users/allen/repo/dlh-test-fw/controlplane && make ui-build && make reload-minikube
kubectl -n dlh-test-fw rollout restart deployment/dlh-controlplane
kubectl -n dlh-test-fw rollout status deployment/dlh-controlplane --timeout=120s
```
Expected: rollout completes.

- [ ] **Step 2: Submit a run with an explicit priority via CLI**

Run:
```bash
DLH_TOKEN="fake:runner:runner@local:dlh-runners" go run ./cmd/dlh run mysql-pod-delete --priority 500 --endpoint http://localhost:8080
```
(If port-forward is down: `kubectl -n dlh-test-fw port-forward svc/dlh-controlplane 8080:80 &`)
Expected: `submitted: mysql-pod-delete-<timestamp>`.

- [ ] **Step 3: Confirm the API reports priority 500**

Run:
```bash
curl -s -H "Authorization: Bearer fake:runner:runner@local:dlh-runners" http://localhost:8080/api/runs | python3 -c "import sys,json; d=json.load(sys.stdin); print([(r['id'], r.get('priority')) for r in d['items'][:3]])"
```
Expected: the new run shows priority `500`.

- [ ] **Step 4: Confirm in the UI (Playwright)**

Navigate to `http://localhost:8080/runs`, screenshot, and confirm the Priority column shows `500` for the new run; open the run detail and confirm the Priority meta cell shows `500`. Console: 0 errors.

---
---

# PHASE 2 — Queue view (read-only)

**Phase goal:** A new `Queue` nav page makes the per-target-type semaphore legible — one lane per target type (mysql/kafka/doris), each showing the running holder and the ordered pending runs, with a calm Idle empty state. Backed by a read-only `GET /api/queue`.

### Task 2.1: Pure queue grouping/ordering logic

**Files:**
- Create: `controlplane/internal/queue/queue.go`
- Create: `controlplane/internal/queue/queue_test.go`

**Design:** A pure function takes the non-terminal workflows + the lock keys/slot counts and returns per-key lanes. Within a lane: `Running` workflows are holders; `Pending` workflows are the queue, ordered by priority desc then creationTimestamp asc (mirroring Argo's release order). Target type is derived via `links.DeriveTargetType`.

- [ ] **Step 1: Write the failing test**

Create `controlplane/internal/queue/queue_test.go`:

```go
package queue

import (
	"testing"
	"time"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func wf(name, scenario, phase string, prio int32, created time.Time) *wfv1.Workflow {
	return &wfv1.Workflow{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			CreationTimestamp: metav1.NewTime(created),
			Labels:            map[string]string{"dlh.scenario": scenario},
		},
		Spec:   wfv1.WorkflowSpec{Priority: &prio},
		Status: wfv1.WorkflowStatus{Phase: wfv1.WorkflowPhase(phase)},
	}
}

func TestBuildLanes_GroupsRunningAndOrdersPending(t *testing.T) {
	t0 := time.Date(2026, 5, 26, 0, 0, 0, 0, time.UTC)
	wfs := []*wfv1.Workflow{
		wf("m-run", "mysql-pod-delete", "Running", 100, t0),
		wf("m-lowprio-old", "mysql-pod-delete", "Pending", 100, t0.Add(1*time.Minute)),
		wf("m-highprio-new", "mysql-pod-delete", "Pending", 500, t0.Add(2*time.Minute)),
		wf("k-run", "kafka-broker-partition", "Running", 100, t0),
	}
	keys := []LockKey{{Key: "mysql", Slots: 1}, {Key: "kafka", Slots: 1}, {Key: "doris", Slots: 1}}

	lanes := BuildLanes(wfs, keys)

	if len(lanes) != 3 {
		t.Fatalf("expected 3 lanes, got %d", len(lanes))
	}
	mysql := lanes[0]
	if mysql.Key != "mysql" || mysql.Slots != 1 {
		t.Fatalf("lane[0] = %+v", mysql)
	}
	if len(mysql.Running) != 1 || mysql.Running[0].ID != "m-run" {
		t.Errorf("mysql running: %+v", mysql.Running)
	}
	// higher priority first even though it was submitted later
	if len(mysql.Pending) != 2 || mysql.Pending[0].ID != "m-highprio-new" || mysql.Pending[1].ID != "m-lowprio-old" {
		t.Errorf("mysql pending order: %+v", mysql.Pending)
	}
	// doris lane is idle (present but empty)
	if lanes[2].Key != "doris" || len(lanes[2].Running) != 0 || len(lanes[2].Pending) != 0 {
		t.Errorf("doris lane should be idle: %+v", lanes[2])
	}
}

func TestBuildLanes_IgnoresTerminalWorkflows(t *testing.T) {
	t0 := time.Date(2026, 5, 26, 0, 0, 0, 0, time.UTC)
	wfs := []*wfv1.Workflow{
		wf("m-done", "mysql-pod-delete", "Succeeded", 100, t0),
		wf("m-failed", "mysql-pod-delete", "Failed", 100, t0),
	}
	lanes := BuildLanes(wfs, []LockKey{{Key: "mysql", Slots: 1}})
	if len(lanes[0].Running) != 0 || len(lanes[0].Pending) != 0 {
		t.Errorf("terminal workflows must not appear: %+v", lanes[0])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd controlplane && go test ./internal/queue/ -v`
Expected: FAIL — package `queue` does not exist.

- [ ] **Step 3: Implement the queue logic**

Create `controlplane/internal/queue/queue.go`:

```go
// Package queue builds the per-target-type semaphore view consumed by
// GET /api/queue. It is pure: given the current workflows + the lock keys,
// it groups Running holders and orders Pending runs the way Argo releases
// them (priority desc, then oldest first).
package queue

import (
	"sort"
	"time"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"

	"github.com/dlh/dlh-test-fw/controlplane/internal/links"
)

// LockKey is one semaphore key + its slot count (from dlh-scenario-locks).
type LockKey struct {
	Key   string
	Slots int
}

// Entry is one workflow in a lane.
type Entry struct {
	ID          string
	Scenario    string
	Priority    *int
	SubmittedAt time.Time
}

// Lane is the running holder(s) + ordered pending queue for one semaphore key.
type Lane struct {
	Key     string
	Slots   int
	Running []Entry
	Pending []Entry
}

func isTerminal(p wfv1.WorkflowPhase) bool {
	return p == wfv1.WorkflowSucceeded || p == wfv1.WorkflowFailed || p == wfv1.WorkflowError
}

func entryOf(w *wfv1.Workflow) Entry {
	e := Entry{
		ID:          w.Name,
		SubmittedAt: w.CreationTimestamp.Time,
	}
	if w.Spec.WorkflowTemplateRef != nil {
		e.Scenario = w.Spec.WorkflowTemplateRef.Name
	} else if v := w.Labels["dlh.scenario"]; v != "" {
		e.Scenario = v
	}
	if w.Spec.Priority != nil {
		p := int(*w.Spec.Priority)
		e.Priority = &p
	}
	return e
}

// prioVal returns the comparable priority (nil sorts as 0, matching Argo default).
func prioVal(e Entry) int {
	if e.Priority == nil {
		return 0
	}
	return *e.Priority
}

// BuildLanes groups workflows by derived target type into one lane per lock key,
// preserving the key order given. Running workflows are holders; Pending ones
// are ordered priority-desc then oldest-first.
func BuildLanes(wfs []*wfv1.Workflow, keys []LockKey) []Lane {
	running := map[string][]Entry{}
	pending := map[string][]Entry{}
	for _, w := range wfs {
		if w == nil || isTerminal(w.Status.Phase) {
			continue
		}
		scenario := ""
		if w.Spec.WorkflowTemplateRef != nil {
			scenario = w.Spec.WorkflowTemplateRef.Name
		} else {
			scenario = w.Labels["dlh.scenario"]
		}
		key := links.DeriveTargetType(scenario)
		e := entryOf(w)
		switch w.Status.Phase {
		case wfv1.WorkflowRunning:
			running[key] = append(running[key], e)
		case wfv1.WorkflowPending, "":
			pending[key] = append(pending[key], e)
		default:
			// Unknown / non-running, non-pending, non-terminal — treat as pending.
			pending[key] = append(pending[key], e)
		}
	}

	lanes := make([]Lane, 0, len(keys))
	for _, k := range keys {
		lane := Lane{Key: k.Key, Slots: k.Slots, Running: running[k.Key], Pending: pending[k.Key]}
		sort.SliceStable(lane.Running, func(i, j int) bool {
			return lane.Running[i].SubmittedAt.Before(lane.Running[j].SubmittedAt)
		})
		sort.SliceStable(lane.Pending, func(i, j int) bool {
			pi, pj := prioVal(lane.Pending[i]), prioVal(lane.Pending[j])
			if pi != pj {
				return pi > pj // higher priority first
			}
			return lane.Pending[i].SubmittedAt.Before(lane.Pending[j].SubmittedAt) // oldest first
		})
		lanes = append(lanes, lane)
	}
	return lanes
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd controlplane && go test ./internal/queue/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/internal/queue/queue.go controlplane/internal/queue/queue_test.go
git commit -m "feat(queue): pure lane grouping + Argo release-order sort

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 2.2: Config — lock ConfigMap name

**Files:**
- Modify: `controlplane/internal/config/config.go:13-39, 43-82`

- [ ] **Step 1: Add the config field**

In `controlplane/internal/config/config.go`, add to the `Config` struct (after `GrafanaBaseURL`):

```go
	// LocksConfigMapName is the dlh-scenario-locks ConfigMap (semaphore slot counts).
	LocksConfigMapName string
```

- [ ] **Step 2: Bind it in `Load`**

In `Load()`, add to the struct literal (after `GrafanaBaseURL: ...`):

```go
		LocksConfigMapName: getenv("DLH_LOCKS_CONFIGMAP", "dlh-scenario-locks"),
```

- [ ] **Step 3: Verify it compiles**

Run: `cd controlplane && go build ./...`
Expected: exit 0.

- [ ] **Step 4: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/internal/config/config.go
git commit -m "feat(config): DLH_LOCKS_CONFIGMAP (dlh-scenario-locks)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 2.3: OpenAPI — `GET /api/queue`

**Files:**
- Modify: `controlplane/api/openapi.yaml`
- Regenerate: gen files

- [ ] **Step 1: Add the path**

In `controlplane/api/openapi.yaml`, under `paths:`, add (after the `/api/runs/{id}` block):

```yaml
  /api/queue:
    get:
      operationId: getQueue
      responses:
        "200":
          description: per-target-type semaphore lanes
          content:
            application/json:
              schema: { $ref: "#/components/schemas/Queue" }
```

- [ ] **Step 2: Add the schemas**

In `components/schemas`, add:

```yaml
    Queue:
      type: object
      required: [lanes]
      properties:
        lanes:
          type: array
          items: { $ref: "#/components/schemas/QueueLane" }
    QueueLane:
      type: object
      required: [key, slots, running, pending]
      properties:
        key:    { type: string, description: "Semaphore key / target type (mysql/kafka/doris)." }
        slots:  { type: integer, description: "Concurrent slots for this key." }
        running:
          type: array
          items: { $ref: "#/components/schemas/QueueEntry" }
        pending:
          type: array
          description: "Ordered by release order (priority desc, then oldest first)."
          items: { $ref: "#/components/schemas/QueueEntry" }
    QueueEntry:
      type: object
      required: [id, scenario, submittedAt]
      properties:
        id:          { type: string }
        scenario:    { type: string }
        priority:    { type: integer }
        submittedAt: { type: string, format: date-time }
```

- [ ] **Step 3: Regenerate + compile**

Run: `cd controlplane && make codegen && go build ./...`
Expected: gen files updated; build fails only because `StrictServerInterface` now requires a `GetQueue` method (that's the next task). If `go build` fails with "missing method GetQueue", that is expected — proceed to Task 2.4. If it fails for any other reason, fix that first.

- [ ] **Step 4: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/api/openapi.yaml controlplane/internal/api/gen/types.gen.go controlplane/internal/api/gen/server.gen.go controlplane/web/src/api/gen.ts
git commit -m "feat(api): GET /api/queue schema + path

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 2.4: `GetQueue` handler + locks reader

**Files:**
- Modify: `controlplane/internal/api/handlers.go`
- Modify: `controlplane/internal/api/deps.go` (or wherever `Deps` is defined — find with `grep -rn "type Deps struct" controlplane/internal/api/`)
- Modify: `controlplane/cmd/dlh-controlplane/main.go` (wire the new dep)
- Test: `controlplane/internal/api/handlers_test.go`

**Design:** Add a `LocksReader` interface to `Deps` that returns `[]queue.LockKey` (read from the `dlh-scenario-locks` ConfigMap). The handler lists non-terminal workflows, calls `queue.BuildLanes`, and maps to the generated DTO. For testability the handler depends on the interface, not the k8s client directly.

- [ ] **Step 1: Write the failing test**

Append to `controlplane/internal/api/handlers_test.go`:

```go
// fakeLocks implements LocksReader.
type fakeLocks struct{ keys []queue.LockKey }

func (f *fakeLocks) Keys(_ context.Context) ([]queue.LockKey, error) { return f.keys, nil }

func TestGetQueue_GroupsAndOrders(t *testing.T) {
	t0 := metav1.Now()
	mk := func(name, scenario, phase string, prio int32) *wfv1.Workflow {
		return &wfv1.Workflow{
			ObjectMeta: metav1.ObjectMeta{Name: name, CreationTimestamp: t0,
				Labels: map[string]string{"dlh.scenario": scenario}},
			Spec:   wfv1.WorkflowSpec{Priority: &prio},
			Status: wfv1.WorkflowStatus{Phase: wfv1.WorkflowPhase(phase)},
		}
	}
	deps := &Deps{
		Workflows: &fakeWorkflows{items: []*wfv1.Workflow{
			mk("m-run", "mysql-pod-delete", "Running", 100),
			mk("m-pend", "mysql-pod-delete", "Pending", 500),
		}},
		Locks: &fakeLocks{keys: []queue.LockKey{{Key: "mysql", Slots: 1}, {Key: "kafka", Slots: 1}}},
	}
	h := &Handlers{deps: deps}
	resp, err := h.GetQueue(context.Background(), gen.GetQueueRequestObject{})
	if err != nil {
		t.Fatalf("GetQueue: %v", err)
	}
	out := resp.(gen.GetQueue200JSONResponse)
	if len(out.Lanes) != 2 {
		t.Fatalf("expected 2 lanes, got %d", len(out.Lanes))
	}
	if out.Lanes[0].Key != "mysql" || len(out.Lanes[0].Running) != 1 || len(out.Lanes[0].Pending) != 1 {
		t.Errorf("mysql lane: %+v", out.Lanes[0])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd controlplane && go test ./internal/api/ -run TestGetQueue -v`
Expected: FAIL — `Deps` has no `Locks` field and `Handlers` has no `GetQueue` method.

- [ ] **Step 3: Add the `LocksReader` interface + `Deps.Locks`**

First locate the `Deps` struct: `grep -rn "type Deps struct" controlplane/internal/api/`. In that file, add the field to the struct:

```go
	Locks LocksReader
```

And add the interface near the other dep interfaces in the same file:

```go
// LocksReader returns the semaphore keys + slot counts (dlh-scenario-locks).
type LocksReader interface {
	Keys(ctx context.Context) ([]queue.LockKey, error)
}
```

Add the import `"github.com/dlh/dlh-test-fw/controlplane/internal/queue"` to that file.

- [ ] **Step 4: Implement the `GetQueue` handler**

Add to `controlplane/internal/api/handlers.go`:

```go
// GetQueue — GET /api/queue
func (h *Handlers) GetQueue(ctx context.Context, _ gen.GetQueueRequestObject) (gen.GetQueueResponseObject, error) {
	keys, err := h.deps.Locks.Keys(ctx)
	if err != nil {
		return nil, err
	}
	wfs, err := h.deps.Workflows.List(k8s.WorkflowFilter{})
	if err != nil {
		return nil, err
	}
	lanes := queue.BuildLanes(wfs, keys)

	out := make([]gen.QueueLane, 0, len(lanes))
	for _, l := range lanes {
		gl := gen.QueueLane{Key: l.Key, Slots: l.Slots,
			Running: mapEntries(l.Running), Pending: mapEntries(l.Pending)}
		out = append(out, gl)
	}
	return gen.GetQueue200JSONResponse{Lanes: out}, nil
}

func mapEntries(es []queue.Entry) []gen.QueueEntry {
	out := make([]gen.QueueEntry, 0, len(es))
	for _, e := range es {
		ge := gen.QueueEntry{Id: e.ID, Scenario: e.Scenario, SubmittedAt: e.SubmittedAt}
		if e.Priority != nil {
			p := *e.Priority
			ge.Priority = &p
		}
		out = append(out, ge)
	}
	return out
}
```

Add the import `"github.com/dlh/dlh-test-fw/controlplane/internal/queue"` to `handlers.go` if not already present. (`k8s` is already imported — see `ListRuns`.)

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd controlplane && go test ./internal/api/ -run TestGetQueue -v`
Expected: PASS.

- [ ] **Step 6: Implement the real `LocksReader` + wire it in main**

Create `controlplane/internal/api/locks.go`:

```go
package api

import (
	"context"
	"sort"
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/dlh/dlh-test-fw/controlplane/internal/queue"
)

// ConfigMapLocks reads semaphore keys + slot counts from the dlh-scenario-locks
// ConfigMap. Keys are returned sorted for stable lane ordering.
type ConfigMapLocks struct {
	Client    kubernetes.Interface
	Namespace string
	Name      string
}

func (c *ConfigMapLocks) Keys(ctx context.Context) ([]queue.LockKey, error) {
	cm, err := c.Client.CoreV1().ConfigMaps(c.Namespace).Get(ctx, c.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	keys := make([]queue.LockKey, 0, len(cm.Data))
	for k, v := range cm.Data {
		slots, _ := strconv.Atoi(v)
		if slots <= 0 {
			slots = 1
		}
		keys = append(keys, queue.LockKey{Key: k, Slots: slots})
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].Key < keys[j].Key })
	return keys, nil
}
```

In `controlplane/cmd/dlh-controlplane/main.go`, where `Deps` is constructed (find with `grep -n "Deps{" controlplane/cmd/dlh-controlplane/main.go`), set the `Locks` field:

```go
		Locks: &api.ConfigMapLocks{Client: clients.Core, Namespace: cfg.K8sNamespace, Name: cfg.LocksConfigMapName},
```

- [ ] **Step 7: Verify the whole thing compiles + tests pass**

Run: `cd controlplane && go build ./... && go test ./internal/api/ ./internal/queue/`
Expected: exit 0, all PASS.

- [ ] **Step 8: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/internal/api/handlers.go controlplane/internal/api/handlers_test.go controlplane/internal/api/locks.go controlplane/cmd/dlh-controlplane/main.go
# also stage the Deps file you edited (e.g. deps.go or server.go)
git add controlplane/internal/api/*.go
git commit -m "feat(api): GET /api/queue handler + dlh-scenario-locks reader

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

> ⚠️ The `git add controlplane/internal/api/*.go` above stages only `.go` files in that dir — it does NOT touch `controlplane/internal/api/dist/`. Confirm with `git status` before committing that no `dist/` files are staged.

---

### Task 2.5: RBAC — read `dlh-scenario-locks`

**Files:**
- Modify: `controlplane/deploy/role.yaml:17-19`

- [ ] **Step 1: Add the ConfigMap name to the get/list/watch rule**

In `controlplane/deploy/role.yaml`, extend the existing configmaps rule's `resourceNames`:

```yaml
  - apiGroups: [""]
    resources: ["configmaps"]
    verbs: ["get", "list", "watch"]
    resourceNames: ["dlh-roles", "dlh-targets", "dlh-scenario-locks"]
```

- [ ] **Step 2: Apply (local-dev)**

Run: `kubectl apply -f controlplane/deploy/role.yaml`
Expected: `role.rbac.authorization.k8s.io/dlh-controlplane configured`.

- [ ] **Step 3: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/deploy/role.yaml
git commit -m "feat(rbac): controlplane reads dlh-scenario-locks ConfigMap

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 2.6: Queue page + nav item + route

**Files:**
- Create: `controlplane/web/src/pages/QueuePage.tsx`
- Modify: `controlplane/web/src/App.tsx:3, 18-23, 107-114`

- [ ] **Step 1: Create the Queue page**

Create `controlplane/web/src/pages/QueuePage.tsx`:

```tsx
import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { Settings } from "lucide-react";
import { api } from "../api/client";
import type { components } from "../api/gen";
import { ErrorState } from "@/components/ErrorState";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { relativeTime } from "@/lib/time";

type Queue = components["schemas"]["Queue"];
type Lane = components["schemas"]["QueueLane"];

const POLL_MS = 5000;

export function QueuePage() {
  const [queue, setQueue] = useState<Queue | null>(null);
  const [error, setError] = useState<unknown>(null);

  const reload = useCallback(() => {
    api.GET("/api/queue", {}).then(({ data, error: e }) => {
      if (e) setError(e);
      else { setQueue(data as Queue); setError(null); }
    });
  }, []);

  useEffect(() => {
    reload();
    const poll = setInterval(reload, POLL_MS);
    return () => clearInterval(poll);
  }, [reload]);

  if (error) return <ErrorState message="Failed to load queue" details={error} />;
  if (!queue) return <div className="space-y-4"><Skeleton className="h-8 w-48" /><Skeleton className="h-40 w-full" /></div>;

  return (
    <section className="space-y-5">
      <div className="flex items-center justify-between">
        <h1 className="text-lg font-semibold">Queue</h1>
        <Link to="/admin/priorities" className="inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground">
          <Settings className="h-4 w-4" /> Default priorities
        </Link>
      </div>
      <p className="rounded-md border bg-card px-4 py-2 text-xs text-muted-foreground">
        1 slot per target type · releases by priority (high→low, then oldest) · types run in parallel.
      </p>
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        {queue.lanes.map((lane) => <LaneCard key={lane.key} lane={lane} />)}
      </div>
    </section>
  );
}

function LaneCard({ lane }: { lane: Lane }) {
  const idle = lane.running.length === 0 && lane.pending.length === 0;
  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between">
        <CardTitle className="text-base capitalize">{lane.key}</CardTitle>
        <span className="text-xs text-muted-foreground">{lane.slots} slot{lane.slots === 1 ? "" : "s"}</span>
      </CardHeader>
      <CardContent className="space-y-3">
        {idle && <div className="rounded-md border border-dashed py-6 text-center text-sm text-muted-foreground">Idle</div>}
        {lane.running.length > 0 && (
          <div>
            <div className="mb-1 text-xs font-medium uppercase tracking-wide text-muted-foreground">Running</div>
            {lane.running.map((e) => (
              <div key={e.id} className="flex items-center justify-between rounded-md bg-status-running/10 px-2.5 py-1.5 text-sm">
                <span className="flex items-center gap-2">
                  <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-status-running" />
                  <Link to={`/runs/${e.id}`} className="hover:underline">{e.scenario}</Link>
                </span>
                <span className="font-mono text-xs text-muted-foreground">p{e.priority ?? "—"}</span>
              </div>
            ))}
          </div>
        )}
        {lane.pending.length > 0 && (
          <div>
            <div className="mb-1 text-xs font-medium uppercase tracking-wide text-muted-foreground">Queued · release order</div>
            <div className="space-y-1">
              {lane.pending.map((e, i) => (
                <div key={e.id} className="flex items-center justify-between rounded-md border px-2.5 py-1.5 text-sm">
                  <span className="flex items-center gap-2">
                    <span className="font-mono text-xs text-muted-foreground">#{i + 1}</span>
                    {i === 0 && <span className="rounded bg-primary/15 px-1.5 py-0.5 text-[10px] font-semibold text-primary">NEXT</span>}
                    <Link to={`/runs/${e.id}`} className="hover:underline">{e.scenario}</Link>
                  </span>
                  <span className="flex items-center gap-2 text-xs text-muted-foreground">
                    <span title={new Date(e.submittedAt).toLocaleString()}>{relativeTime(e.submittedAt)}</span>
                    <span className="font-mono">p{e.priority ?? "—"}</span>
                  </span>
                </div>
              ))}
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
```

- [ ] **Step 2: Add the nav item + route in App.tsx**

In `controlplane/web/src/App.tsx`, add `ListOrdered` to the lucide import (line 3):

```tsx
import { Activity, Clock, Crosshair, LayoutGrid, ListOrdered, Moon, Sun } from "lucide-react";
```

Add a Queue entry to the `NAV` array (after Schedules):

```tsx
const NAV = [
  { to: "/runs", label: "Runs", Icon: Activity },
  { to: "/scenarios", label: "Scenarios", Icon: LayoutGrid },
  { to: "/queue", label: "Queue", Icon: ListOrdered },
  { to: "/targets", label: "Targets", Icon: Crosshair },
  { to: "/schedules", label: "Schedules", Icon: Clock },
];
```

Import the page (with the other page imports at the top):

```tsx
import { QueuePage } from "./pages/QueuePage";
```

Add the routes (in the `<Routes>` block):

```tsx
  <Route path="/queue" element={<QueuePage />} />
```

(The `/admin/priorities` route is added in Phase 3 — the link will 404 until then; that is acceptable mid-phase.)

- [ ] **Step 3: Verify build**

Run: `cd controlplane/web && pnpm build`
Expected: exit 0.

- [ ] **Step 4: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/web/src/pages/QueuePage.tsx controlplane/web/src/App.tsx
git commit -m "feat(web): Queue page + nav item (per-target-type lanes)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 2.7: CLI `dlh queue`

**Files:**
- Create: `controlplane/cmd/dlh/queue.go`
- Modify: `controlplane/cmd/dlh/root.go:21`

- [ ] **Step 1: Create the queue command**

Create `controlplane/cmd/dlh/queue.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func queueCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "queue",
		Short: "Show the per-target-type run queue",
		RunE: func(_ *cobra.Command, _ []string) error {
			raw, _, err := newClient().do("GET", "/api/queue", nil, nil)
			if err != nil {
				return err
			}
			var resp struct {
				Lanes []struct {
					Key     string `json:"key"`
					Slots   int    `json:"slots"`
					Running []struct {
						ID, Scenario string `json:"-"`
					} `json:"running"`
					Pending []struct {
						ID       string `json:"id"`
						Scenario string `json:"scenario"`
						Priority *int   `json:"priority"`
					} `json:"pending"`
				} `json:"lanes"`
			}
			if err := json.Unmarshal(raw, &resp); err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "LANE\tSLOTS\tRUNNING\tQUEUED")
			for _, l := range resp.Lanes {
				fmt.Fprintf(tw, "%s\t%d\t%d\t%d\n", l.Key, l.Slots, len(l.Running), len(l.Pending))
			}
			return tw.Flush()
		},
	}
}
```

- [ ] **Step 2: Register it in root.go**

In `controlplane/cmd/dlh/root.go`, add `queueCmd()` to the `AddCommand` call (line 21):

```go
	root.AddCommand(runCmd(), runsCmd(), loginCmd(), scheduleCmd(), queueCmd())
```

- [ ] **Step 3: Verify it compiles**

Run: `cd controlplane && go build ./cmd/dlh && go run ./cmd/dlh queue --help`
Expected: exit 0; help shows "Show the per-target-type run queue".

- [ ] **Step 4: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/cmd/dlh/queue.go controlplane/cmd/dlh/root.go
git commit -m "feat(cli): dlh queue

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 2.8: Phase 2 live verification

- [ ] **Step 1: Build + deploy**

Run:
```bash
cd /Users/allen/repo/dlh-test-fw/controlplane && make ui-build && make reload-minikube
kubectl -n dlh-test-fw rollout restart deployment/dlh-controlplane
kubectl -n dlh-test-fw rollout status deployment/dlh-controlplane --timeout=120s
```

- [ ] **Step 2: Hit the endpoint**

Run: `curl -s -H "Authorization: Bearer fake:viewer:viewer@local:dlh-viewers" http://localhost:8080/api/queue | python3 -m json.tool`
Expected: JSON with a `lanes` array containing mysql/kafka/doris keys, each with `slots: 1`.

- [ ] **Step 3: Confirm the page (Playwright)**

Navigate to `http://localhost:8080/queue`, screenshot. Expected: three lanes (mysql/kafka/doris); idle lanes show the dashed "Idle" state; the rules strip is visible. Console: 0 errors.

---
---

# PHASE 3 — Layer 3: Editable per-scenario defaults

**Phase goal:** An admin page sets a baseline priority per scenario, stored in a `dlh-scenario-priorities` ConfigMap that the submitter consults (between request-override and baked-value). Named tiers (Low/Normal/High/Urgent = 10/100/200/500) are UI sugar over the raw int.

### Task 3.1: Scenario-priorities ConfigMap + RBAC + config

**Files:**
- Create: `helm/dlh-test-fw/templates/dlh-scenario-priorities-configmap.yaml`
- Modify: `controlplane/deploy/role.yaml`
- Modify: `controlplane/internal/config/config.go`

- [ ] **Step 1: Create the ConfigMap template**

Create `helm/dlh-test-fw/templates/dlh-scenario-priorities-configmap.yaml`:

```yaml
# Per-scenario default priority overrides, managed by the controlplane
# (admin "Default priorities" page) at runtime. Data keys are scenario ids;
# values are stringified ints. Empty by default — when a scenario has no
# entry, the submitter falls back to the WorkflowTemplate's baked spec.priority.
#
# helm.sh/resource-policy: keep prevents `helm upgrade`/uninstall from
# clobbering operator-set values (the controlplane owns this data at runtime).
apiVersion: v1
kind: ConfigMap
metadata:
  name: dlh-scenario-priorities
  namespace: {{ include "dlh.namespace" . }}
  labels:
    {{- include "dlh.labels" . | nindent 4 }}
  annotations:
    helm.sh/resource-policy: keep
data: {}
```

- [ ] **Step 2: Extend RBAC**

In `controlplane/deploy/role.yaml`, add `dlh-scenario-priorities` to the get/list/watch resourceNames, and add a new rule for update/patch. The configmaps rules become:

```yaml
  - apiGroups: [""]
    resources: ["configmaps"]
    verbs: ["get", "list", "watch"]
    resourceNames: ["dlh-roles", "dlh-targets", "dlh-scenario-locks", "dlh-scenario-priorities"]
  - apiGroups: [""]
    resources: ["configmaps"]
    verbs: ["update", "patch"]
    resourceNames: ["dlh-scenario-priorities"]
```

- [ ] **Step 3: Add the config field**

In `controlplane/internal/config/config.go`, add to the struct:

```go
	// PrioritiesConfigMapName is the dlh-scenario-priorities ConfigMap (per-scenario default overrides).
	PrioritiesConfigMapName string
```

And in `Load()`:

```go
		PrioritiesConfigMapName: getenv("DLH_PRIORITIES_CONFIGMAP", "dlh-scenario-priorities"),
```

- [ ] **Step 4: Apply RBAC + verify build**

Run:
```bash
kubectl apply -f controlplane/deploy/role.yaml
cd /Users/allen/repo/dlh-test-fw/controlplane && go build ./...
```
Expected: role configured; build exit 0.

- [ ] **Step 5: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add helm/dlh-test-fw/templates/dlh-scenario-priorities-configmap.yaml controlplane/deploy/role.yaml controlplane/internal/config/config.go
git commit -m "feat(chart): dlh-scenario-priorities ConfigMap + RBAC + config

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 3.2: `priorities` package — read/write defaults

**Files:**
- Create: `controlplane/internal/priorities/priorities.go`
- Create: `controlplane/internal/priorities/priorities_test.go`

**Design:** A `Store` backed by `kubernetes.Interface`. `Get(scenario)` returns the override int (and whether present). `Set(scenario, priority)` writes the CM data key. `All()` returns the whole map. Uses a fake clientset for tests.

- [ ] **Step 1: Write the failing test**

Create `controlplane/internal/priorities/priorities_test.go`:

```go
package priorities

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func newStore(data map[string]string) *Store {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "dlh-scenario-priorities", Namespace: "dlh-test-fw"},
		Data:       data,
	}
	return &Store{Client: fake.NewSimpleClientset(cm), Namespace: "dlh-test-fw", Name: "dlh-scenario-priorities"}
}

func TestStore_GetSet(t *testing.T) {
	s := newStore(map[string]string{"mysql-pod-delete": "200"})
	ctx := context.Background()

	if v, ok, _ := s.Get(ctx, "mysql-pod-delete"); !ok || v != 200 {
		t.Fatalf("Get existing: got %d ok=%v want 200 true", v, ok)
	}
	if _, ok, _ := s.Get(ctx, "kafka-broker-partition"); ok {
		t.Fatal("Get missing: expected ok=false")
	}

	if err := s.Set(ctx, "kafka-broker-partition", 500); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if v, ok, _ := s.Get(ctx, "kafka-broker-partition"); !ok || v != 500 {
		t.Errorf("Get after Set: got %d ok=%v want 500 true", v, ok)
	}
}

func TestStore_All(t *testing.T) {
	s := newStore(map[string]string{"a": "10", "b": "not-an-int"})
	all, err := s.All(context.Background())
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if all["a"] != 10 {
		t.Errorf("a: got %d want 10", all["a"])
	}
	if _, ok := all["b"]; ok {
		t.Error("non-int values must be skipped")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd controlplane && go test ./internal/priorities/ -v`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Implement the store**

Create `controlplane/internal/priorities/priorities.go`:

```go
// Package priorities reads + writes the dlh-scenario-priorities ConfigMap:
// per-scenario default priority overrides consulted by the submitter.
package priorities

import (
	"context"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Store is a thin accessor over the dlh-scenario-priorities ConfigMap.
type Store struct {
	Client    kubernetes.Interface
	Namespace string
	Name      string
}

// Get returns the override priority for a scenario and whether one is set.
func (s *Store) Get(ctx context.Context, scenario string) (int, bool, error) {
	all, err := s.All(ctx)
	if err != nil {
		return 0, false, err
	}
	v, ok := all[scenario]
	return v, ok, nil
}

// All returns every parseable override (non-integer values are skipped).
func (s *Store) All(ctx context.Context) (map[string]int, error) {
	cm, err := s.Client.CoreV1().ConfigMaps(s.Namespace).Get(ctx, s.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return map[string]int{}, nil // absent CM = no overrides
		}
		return nil, err
	}
	out := make(map[string]int, len(cm.Data))
	for k, v := range cm.Data {
		if n, err := strconv.Atoi(v); err == nil {
			out[k] = n
		}
	}
	return out, nil
}

// Set writes (creating the CM if absent) the override for a scenario.
func (s *Store) Set(ctx context.Context, scenario string, priority int) error {
	cms := s.Client.CoreV1().ConfigMaps(s.Namespace)
	cm, err := cms.Get(ctx, s.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		cm = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: s.Name, Namespace: s.Namespace},
			Data:       map[string]string{},
		}
		cm.Data[scenario] = strconv.Itoa(priority)
		_, cErr := cms.Create(ctx, cm, metav1.CreateOptions{})
		return cErr
	}
	if err != nil {
		return err
	}
	if cm.Data == nil {
		cm.Data = map[string]string{}
	}
	cm.Data[scenario] = strconv.Itoa(priority)
	_, uErr := cms.Update(ctx, cm, metav1.UpdateOptions{})
	return uErr
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd controlplane && go test ./internal/priorities/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/internal/priorities/priorities.go controlplane/internal/priorities/priorities_test.go
git commit -m "feat(priorities): Store over dlh-scenario-priorities ConfigMap

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 3.3: Submitter consults scenario defaults

**Files:**
- Modify: `controlplane/internal/runs/submit.go` (`Submitter` struct + `resolvePriority`)
- Test: `controlplane/internal/runs/submit_test.go`

**Design:** Add an optional `Defaults` lookup to `Submitter` — an interface so tests can fake it and so `internal/runs` doesn't import `internal/priorities` (avoid a cycle; define the interface in `runs`). Insert it into `resolvePriority` between request-override and baked-value.

- [ ] **Step 1: Write the failing test**

Append to `controlplane/internal/runs/submit_test.go`:

```go
type fakeDefaults struct{ m map[string]int }

func (f fakeDefaults) Get(_ context.Context, scenario string) (int, bool, error) {
	v, ok := f.m[scenario]
	return v, ok, nil
}

func TestSubmit_PriorityUsesScenarioDefault(t *testing.T) {
	ns := "dlh-test-fw"
	baked := int32(100)
	tmpl := &wfv1.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-pod-delete", Namespace: ns},
		Spec:       wfv1.WorkflowSpec{Priority: &baked},
	}
	argo := wfake.NewSimpleClientset(tmpl)
	s := &Submitter{Argo: argo, Namespace: ns, Defaults: fakeDefaults{m: map[string]int{"mysql-pod-delete": 300}}}

	// no explicit override → scenario default (300) wins over baked (100)
	res, err := s.Submit(context.Background(), SubmitRequest{ScenarioID: "mysql-pod-delete"})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	got, _ := argo.ArgoprojV1alpha1().Workflows(ns).Get(context.Background(), res.RunID, metav1.GetOptions{})
	if got.Spec.Priority == nil || *got.Spec.Priority != 300 {
		t.Errorf("scenario-default priority: got %v want 300", got.Spec.Priority)
	}

	// explicit override still wins over the scenario default
	p := 500
	res2, _ := s.Submit(context.Background(), SubmitRequest{ScenarioID: "mysql-pod-delete", Priority: &p})
	got2, _ := argo.ArgoprojV1alpha1().Workflows(ns).Get(context.Background(), res2.RunID, metav1.GetOptions{})
	if got2.Spec.Priority == nil || *got2.Spec.Priority != 500 {
		t.Errorf("override over default: got %v want 500", got2.Spec.Priority)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd controlplane && go test ./internal/runs/ -run TestSubmit_PriorityUsesScenarioDefault -v`
Expected: FAIL — `Submitter` has no `Defaults` field.

- [ ] **Step 3: Add the `Defaults` interface + field**

In `controlplane/internal/runs/submit.go`, find the `Submitter` struct (search `type Submitter struct`) and add a field + the interface. Add near the top:

```go
// ScenarioDefaults looks up a per-scenario default priority override.
type ScenarioDefaults interface {
	Get(ctx context.Context, scenario string) (int, bool, error)
}
```

And add to the `Submitter` struct:

```go
	Defaults ScenarioDefaults // optional; nil = no per-scenario defaults
```

- [ ] **Step 4: Thread the lookup into `resolvePriority`**

Change the `resolvePriority` signature to take a context and consult `Defaults`. Replace the helper added in Task 1.2 with:

```go
// resolvePriority returns the effective workflow priority pointer.
// Order: explicit request override → per-scenario default (dlh-scenario-priorities)
// → template's baked spec.priority.
func (s *Submitter) resolvePriority(ctx context.Context, req SubmitRequest, tmpl *wfv1.WorkflowTemplate) *int32 {
	if req.Priority != nil {
		v := int32(*req.Priority)
		return &v
	}
	if s.Defaults != nil {
		if d, ok, err := s.Defaults.Get(ctx, req.ScenarioID); err == nil && ok {
			v := int32(d)
			return &v
		}
	}
	if tmpl != nil && tmpl.Spec.Priority != nil {
		v := *tmpl.Spec.Priority
		return &v
	}
	return nil
}
```

And update the call site (added in Task 1.2) to pass `ctx`:

```go
	effPriority := s.resolvePriority(ctx, req, tmpl)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd controlplane && go test ./internal/runs/ -v`
Expected: PASS (new test + all prior submit tests; the Task 1.2 tests still pass because `Defaults` is nil there).

- [ ] **Step 6: Wire the real Store into the submitter in main**

In `controlplane/cmd/dlh-controlplane/main.go`, where the `Submitter` is constructed (find with `grep -n "Submitter{" controlplane/cmd/dlh-controlplane/main.go`), set `Defaults`:

```go
		Defaults: &priorities.Store{Client: clients.Core, Namespace: cfg.K8sNamespace, Name: cfg.PrioritiesConfigMapName},
```

Add the import `"github.com/dlh/dlh-test-fw/controlplane/internal/priorities"`.

- [ ] **Step 7: Verify build + tests**

Run: `cd controlplane && go build ./... && go test ./internal/runs/`
Expected: exit 0, PASS.

- [ ] **Step 8: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/internal/runs/submit.go controlplane/internal/runs/submit_test.go controlplane/cmd/dlh-controlplane/main.go
git commit -m "feat(runs): submitter consults per-scenario default priority

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 3.4: OpenAPI — scenario-priorities endpoints

**Files:**
- Modify: `controlplane/api/openapi.yaml`
- Regenerate: gen files

- [ ] **Step 1: Add the paths**

In `controlplane/api/openapi.yaml`, under `paths:`, add:

```yaml
  /api/scenario-priorities:
    get:
      operationId: getScenarioPriorities
      responses:
        "200":
          description: per-scenario baked default + current override
          content:
            application/json:
              schema:
                type: object
                required: [items]
                properties:
                  items:
                    type: array
                    items: { $ref: "#/components/schemas/ScenarioPriority" }
  /api/scenario-priorities/{id}:
    put:
      operationId: putScenarioPriority
      parameters:
        - in: path
          name: id
          required: true
          schema: { type: string }
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [priority]
              properties:
                priority: { type: integer }
      responses:
        "200":
          description: updated
          content:
            application/json:
              schema: { $ref: "#/components/schemas/ScenarioPriority" }
        "400":
          description: invalid request
        "403":
          description: forbidden (admin role required)
        "404":
          description: scenario not found
```

- [ ] **Step 2: Add the schema**

In `components/schemas`:

```yaml
    ScenarioPriority:
      type: object
      required: [scenario, baked]
      properties:
        scenario: { type: string }
        baked:    { type: integer, description: "WorkflowTemplate's baked spec.priority." }
        override: { type: integer, nullable: true, description: "Current override from dlh-scenario-priorities; null = none (uses baked)." }
        effective: { type: integer, description: "override ?? baked." }
```

- [ ] **Step 3: Regenerate + compile**

Run: `cd controlplane && make codegen && go build ./...`
Expected: gen files updated; `go build` fails only with "missing methods GetScenarioPriorities / PutScenarioPriority" (expected — next task). Any other error must be fixed first.

- [ ] **Step 4: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/api/openapi.yaml controlplane/internal/api/gen/types.gen.go controlplane/internal/api/gen/server.gen.go controlplane/web/src/api/gen.ts
git commit -m "feat(api): scenario-priorities GET + PUT schema

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 3.5: Scenario-priorities handlers (admin) + RBAC wiring

**Files:**
- Modify: `controlplane/internal/api/handlers.go`
- Modify: the `Deps` file — add a `Priorities` interface + a `Templates` baked-priority reader (Templates already exists)
- Modify: `controlplane/internal/api/server.go` — `RequireRole(admin)` on the PUT route
- Test: `controlplane/internal/api/handlers_test.go`

**Design:** `GetScenarioPriorities` lists templates (for baked values) + overrides (from the priorities Store) and joins them. `PutScenarioPriority` validates the scenario exists, then `Set`s the override. The Store interface in `Deps`:

```go
type PrioritiesStore interface {
	All(ctx context.Context) (map[string]int, error)
	Get(ctx context.Context, scenario string) (int, bool, error)
	Set(ctx context.Context, scenario string, priority int) error
}
```

(`*priorities.Store` already satisfies this.)

- [ ] **Step 1: Write the failing test**

Append to `controlplane/internal/api/handlers_test.go`:

```go
type fakePriorities struct{ m map[string]int }

func (f *fakePriorities) All(_ context.Context) (map[string]int, error) { return f.m, nil }
func (f *fakePriorities) Get(_ context.Context, s string) (int, bool, error) {
	v, ok := f.m[s]
	return v, ok, nil
}
func (f *fakePriorities) Set(_ context.Context, s string, p int) error {
	if f.m == nil {
		f.m = map[string]int{}
	}
	f.m[s] = p
	return nil
}

func TestPutAndGetScenarioPriorities(t *testing.T) {
	ns := "dlh-test-fw"
	baked := int32(100)
	tmpl := wfv1.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-pod-delete", Namespace: ns},
		Spec:       wfv1.WorkflowSpec{Priority: &baked},
	}
	fp := &fakePriorities{m: map[string]int{}}
	deps := &Deps{
		Templates:  &fakeTemplates{items: []wfv1.WorkflowTemplate{tmpl}},
		Priorities: fp,
	}
	h := &Handlers{deps: deps}

	// PUT override 500
	prio := 500
	putResp, err := h.PutScenarioPriority(context.Background(), gen.PutScenarioPriorityRequestObject{
		Id:   "mysql-pod-delete",
		Body: &gen.PutScenarioPriorityJSONRequestBody{Priority: prio},
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	po := putResp.(gen.PutScenarioPriority200JSONResponse)
	if po.Override == nil || *po.Override != 500 || po.Effective != 500 || po.Baked != 100 {
		t.Errorf("put result: %+v", po)
	}

	// GET reflects it
	getResp, _ := h.GetScenarioPriorities(context.Background(), gen.GetScenarioPrioritiesRequestObject{})
	go200 := getResp.(gen.GetScenarioPriorities200JSONResponse)
	if len(go200.Items) != 1 || go200.Items[0].Scenario != "mysql-pod-delete" || go200.Items[0].Effective != 500 {
		t.Errorf("get items: %+v", go200.Items)
	}

	// PUT unknown scenario → 404
	r404, _ := h.PutScenarioPriority(context.Background(), gen.PutScenarioPriorityRequestObject{
		Id: "nope", Body: &gen.PutScenarioPriorityJSONRequestBody{Priority: 1},
	})
	if _, ok := r404.(gen.PutScenarioPriority404Response); !ok {
		t.Errorf("expected 404 for unknown scenario, got %T", r404)
	}
}
```

> Note: the exact generated field/type names (`PutScenarioPriorityJSONRequestBody`, `PutScenarioPriority200JSONResponse`, `GetScenarioPriorities200JSONResponse`) come from codegen in Task 3.4 — verify them against `internal/api/gen/server.gen.go` and adjust the test to match if oapi-codegen named them slightly differently.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd controlplane && go test ./internal/api/ -run TestPutAndGetScenarioPriorities -v`
Expected: FAIL — `Deps` has no `Priorities` and handlers don't exist.

- [ ] **Step 3: Add `Priorities` to `Deps`**

In the `Deps` file, add the interface (shown in the Design block above) and the field:

```go
	Priorities PrioritiesStore
```

- [ ] **Step 4: Implement the handlers**

Add to `controlplane/internal/api/handlers.go`:

```go
// GetScenarioPriorities — GET /api/scenario-priorities
func (h *Handlers) GetScenarioPriorities(ctx context.Context, _ gen.GetScenarioPrioritiesRequestObject) (gen.GetScenarioPrioritiesResponseObject, error) {
	tmpls, err := h.deps.Templates.ListTemplates(ctx)
	if err != nil {
		return nil, err
	}
	overrides, err := h.deps.Priorities.All(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]gen.ScenarioPriority, 0, len(tmpls))
	for _, t := range tmpls {
		baked := 0
		if t.Spec.Priority != nil {
			baked = int(*t.Spec.Priority)
		}
		sp := gen.ScenarioPriority{Scenario: t.Name, Baked: baked, Effective: baked}
		if ov, ok := overrides[t.Name]; ok {
			o := ov
			sp.Override = &o
			sp.Effective = ov
		}
		items = append(items, sp)
	}
	return gen.GetScenarioPriorities200JSONResponse{Items: items}, nil
}

// PutScenarioPriority — PUT /api/scenario-priorities/{id}
func (h *Handlers) PutScenarioPriority(ctx context.Context, req gen.PutScenarioPriorityRequestObject) (gen.PutScenarioPriorityResponseObject, error) {
	if req.Body == nil {
		return gen.PutScenarioPriority400Response{}, nil
	}
	tmpl, err := h.deps.Templates.GetTemplate(ctx, req.Id)
	if err != nil || tmpl == nil {
		return gen.PutScenarioPriority404Response{}, nil
	}
	if err := h.deps.Priorities.Set(ctx, req.Id, req.Body.Priority); err != nil {
		return nil, err
	}
	baked := 0
	if tmpl.Spec.Priority != nil {
		baked = int(*tmpl.Spec.Priority)
	}
	o := req.Body.Priority
	return gen.PutScenarioPriority200JSONResponse{
		Scenario: req.Id, Baked: baked, Override: &o, Effective: req.Body.Priority,
	}, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd controlplane && go test ./internal/api/ -run TestPutAndGetScenarioPriorities -v`
Expected: PASS (adjust generated type names if codegen differs, per the Step 1 note).

- [ ] **Step 6: Enforce admin role on PUT + wire the Store in main**

In `controlplane/internal/api/server.go`, after `gen.HandlerFromMux(strictSI, r)`, add an explicit role-gated route for the PUT (mirrors the SSE explicit-route pattern; last-registration-wins):

```go
	// Admin-only: editing per-scenario default priorities.
	if authMW != nil {
		r.With(authMW, auth.RequireRole(auth.RoleAdmin)).
			Put("/api/scenario-priorities/{id}", func(w http.ResponseWriter, req *http.Request) {
				strictSI.(http.Handler).ServeHTTP(w, req)
			})
	}
```

> If `strictSI` is not directly an `http.Handler` in this codebase, instead wrap the existing chi route: re-register the generated handler for that one path behind the middleware. Check how the SSE route re-registration is done in `server.go` (the `// Explicit SSE route` block) and mirror that exact mechanism — the goal is only to add `RequireRole(admin)` in front of the already-generated PUT handler. Viewer suffices for the GET (no extra wiring needed — Phase B default).

In `controlplane/cmd/dlh-controlplane/main.go`, set `Priorities` on `Deps` (reuse the same Store instance created in Task 3.3, or create one):

```go
		Priorities: &priorities.Store{Client: clients.Core, Namespace: cfg.K8sNamespace, Name: cfg.PrioritiesConfigMapName},
```

- [ ] **Step 7: Verify build + tests**

Run: `cd controlplane && go build ./... && go test ./internal/api/`
Expected: exit 0, PASS.

- [ ] **Step 8: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/internal/api/handlers.go controlplane/internal/api/handlers_test.go controlplane/internal/api/server.go controlplane/cmd/dlh-controlplane/main.go
git add controlplane/internal/api/*.go   # stages the edited Deps file; confirm no dist/ staged
git commit -m "feat(api): scenario-priorities GET (viewer) + PUT (admin) handlers

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 3.6: Tier mapping helper (TS) + tests

**Files:**
- Create: `controlplane/web/src/lib/tier.ts`
- Create: `controlplane/web/src/lib/tier.test.ts`

- [ ] **Step 1: Write the failing test**

Create `controlplane/web/src/lib/tier.test.ts`:

```ts
import { describe, it, expect } from "vitest";
import { TIERS, tierForPriority, priorityForTier } from "@/lib/tier";

describe("tier mapping", () => {
  it("exposes the four named tiers", () => {
    expect(TIERS.map((t) => t.label)).toEqual(["Low", "Normal", "High", "Urgent"]);
    expect(TIERS.map((t) => t.value)).toEqual([10, 100, 200, 500]);
  });
  it("maps a priority to its exact tier label, else null", () => {
    expect(tierForPriority(100)).toBe("Normal");
    expect(tierForPriority(500)).toBe("Urgent");
    expect(tierForPriority(150)).toBeNull(); // custom value, no exact tier
  });
  it("maps a tier label to its priority value", () => {
    expect(priorityForTier("High")).toBe(200);
    expect(priorityForTier("nope")).toBeNull();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd controlplane/web && pnpm test -- tier`
Expected: FAIL — `@/lib/tier` does not exist.

- [ ] **Step 3: Implement the helper**

Create `controlplane/web/src/lib/tier.ts`:

```ts
// Named priority tiers — pure UI sugar over the underlying integer.
// The raw int is always authoritative; tiers are exact-match labels.
export const TIERS = [
  { label: "Low", value: 10 },
  { label: "Normal", value: 100 },
  { label: "High", value: 200 },
  { label: "Urgent", value: 500 },
] as const;

export type TierLabel = (typeof TIERS)[number]["label"];

/** Returns the tier label whose value exactly equals priority, else null. */
export function tierForPriority(priority: number): TierLabel | null {
  const t = TIERS.find((t) => t.value === priority);
  return t ? t.label : null;
}

/** Returns the priority value for a tier label, else null. */
export function priorityForTier(label: string): number | null {
  const t = TIERS.find((t) => t.label === label);
  return t ? t.value : null;
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd controlplane/web && pnpm test -- tier`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/web/src/lib/tier.ts controlplane/web/src/lib/tier.test.ts
git commit -m "feat(web): priority tier mapping helper + tests

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 3.7: Default-priorities admin page

**Files:**
- Create: `controlplane/web/src/pages/DefaultPrioritiesPage.tsx`
- Modify: `controlplane/web/src/App.tsx` (import + route)

**Design:** A table of scenarios; each row shows baked + a tier-chip row + a numeric input + a status (`= baked default` / `overridden`). Editing auto-saves via PUT with a toast.

- [ ] **Step 1: Create the page**

Create `controlplane/web/src/pages/DefaultPrioritiesPage.tsx`:

```tsx
import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { ArrowLeft } from "lucide-react";
import { toast } from "sonner";
import { api } from "../api/client";
import type { components } from "../api/gen";
import { ErrorState } from "@/components/ErrorState";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { TIERS, tierForPriority } from "@/lib/tier";

type SP = components["schemas"]["ScenarioPriority"];

export function DefaultPrioritiesPage() {
  const [items, setItems] = useState<SP[] | null>(null);
  const [error, setError] = useState<unknown>(null);

  const reload = useCallback(() => {
    api.GET("/api/scenario-priorities", {}).then(({ data, error: e }) => {
      if (e) setError(e);
      else { setItems((data?.items ?? []) as SP[]); setError(null); }
    });
  }, []);

  useEffect(() => { reload(); }, [reload]);

  const save = async (scenario: string, priority: number) => {
    const { error: e } = await api.PUT("/api/scenario-priorities/{id}", {
      params: { path: { id: scenario } },
      body: { priority },
    });
    if (e) toast.error("Save failed", { description: JSON.stringify(e) });
    else { toast.success(`${scenario} → priority ${priority}`); reload(); }
  };

  if (error) return <ErrorState message="Failed to load priorities" details={error} />;
  if (!items) return <div className="space-y-4"><Skeleton className="h-8 w-64" /><Skeleton className="h-40 w-full" /></div>;

  return (
    <section className="space-y-5">
      <Link to="/queue" className="inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground">
        <ArrowLeft className="h-4 w-4" /> Queue
      </Link>
      <h1 className="text-lg font-semibold">Default priorities</h1>
      <Card>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Scenario</TableHead>
                <TableHead>Tiers</TableHead>
                <TableHead>Effective</TableHead>
                <TableHead>Status</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((sp) => (
                <PriorityRow key={sp.scenario} sp={sp} onSave={save} />
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </section>
  );
}

function PriorityRow({ sp, onSave }: { sp: SP; onSave: (s: string, p: number) => void }) {
  const [raw, setRaw] = useState(String(sp.effective));
  const overridden = sp.override != null;
  const currentTier = tierForPriority(Number(raw));
  return (
    <TableRow>
      <TableCell className="font-medium">{sp.scenario}</TableCell>
      <TableCell>
        <div className="flex gap-1">
          {TIERS.map((t) => (
            <Button
              key={t.label}
              size="sm"
              variant={currentTier === t.label ? "default" : "outline"}
              onClick={() => { setRaw(String(t.value)); onSave(sp.scenario, t.value); }}
            >
              {t.label}
            </Button>
          ))}
        </div>
      </TableCell>
      <TableCell>
        <div className="flex items-center gap-2">
          <Input
            type="number"
            value={raw}
            onChange={(e) => setRaw(e.target.value)}
            className="h-8 w-[88px] tabular-nums"
          />
          <Button size="sm" variant="ghost" onClick={() => onSave(sp.scenario, Number(raw))}>Save</Button>
        </div>
      </TableCell>
      <TableCell className="text-xs text-muted-foreground">
        {overridden
          ? <span>overridden · baked {sp.baked}</span>
          : <span>= baked default ({sp.baked})</span>}
      </TableCell>
    </TableRow>
  );
}
```

- [ ] **Step 2: Add the route in App.tsx**

In `controlplane/web/src/App.tsx`, import the page:

```tsx
import { DefaultPrioritiesPage } from "./pages/DefaultPrioritiesPage";
```

Add the route (inside `<Routes>`):

```tsx
  <Route path="/admin/priorities" element={<DefaultPrioritiesPage />} />
```

- [ ] **Step 3: Verify build + tests**

Run: `cd controlplane/web && pnpm build && pnpm test`
Expected: exit 0; all tests pass.

- [ ] **Step 4: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/web/src/pages/DefaultPrioritiesPage.tsx controlplane/web/src/App.tsx
git commit -m "feat(web): Default priorities admin page (tiers + auto-save)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 3.8: Phase 3 live verification

- [ ] **Step 1: Build + deploy**

Run:
```bash
cd /Users/allen/repo/dlh-test-fw/controlplane && make ui-build && make reload-minikube
kubectl -n dlh-test-fw rollout restart deployment/dlh-controlplane
kubectl -n dlh-test-fw rollout status deployment/dlh-controlplane --timeout=120s
```
(Also `kubectl apply` the chart's new ConfigMap if not present: `kubectl -n dlh-test-fw get cm dlh-scenario-priorities || kubectl -n dlh-test-fw create cm dlh-scenario-priorities`)

- [ ] **Step 2: Set a default via API (admin token)**

Run:
```bash
curl -s -X PUT -H "Authorization: Bearer fake:admin:admin@local:dlh-admins" \
  -H "Content-Type: application/json" -d '{"priority":300}' \
  http://localhost:8080/api/scenario-priorities/mysql-pod-delete | python3 -m json.tool
```
Expected: `{"scenario":"mysql-pod-delete","baked":100,"override":300,"effective":300}`.

- [ ] **Step 3: Confirm a new run picks up the default**

Run a run WITHOUT `--priority`, then check its priority is 300:
```bash
DLH_TOKEN="fake:runner:runner@local:dlh-runners" go run ./cmd/dlh run mysql-pod-delete --endpoint http://localhost:8080
curl -s -H "Authorization: Bearer fake:viewer:viewer@local:dlh-viewers" http://localhost:8080/api/runs | python3 -c "import sys,json;d=json.load(sys.stdin);print([(r['id'],r.get('priority')) for r in d['items'][:2]])"
```
Expected: newest run shows priority `300`.

- [ ] **Step 4: Confirm forbidden for non-admin**

Run:
```bash
curl -s -o /dev/null -w "%{http_code}\n" -X PUT -H "Authorization: Bearer fake:runner:runner@local:dlh-runners" \
  -H "Content-Type: application/json" -d '{"priority":1}' http://localhost:8080/api/scenario-priorities/mysql-pod-delete
```
Expected: `403`.

- [ ] **Step 5: Confirm the admin page (Playwright)**

Navigate to `http://localhost:8080/admin/priorities` (and via the Queue page's "Default priorities" link). Screenshot. Click a tier chip; confirm a success toast and the status flips to "overridden". Console: 0 errors.

---
---

# PHASE 4 — Layer 2: Live re-prioritize pending runs (SPIKE FIRST)

**Phase goal:** Re-prioritize (or move-to-front) a *pending* run from the Queue page / CLI. **The first task is a feasibility spike** — Argo fixes semaphore order at admission, so whether patching a pending workflow's `spec.priority` actually re-orders the release queue must be proven against the pinned Argo Workflows chart (`0.45.20`) before building the endpoint + UI. If it does not hold, the fallback is cancel + resubmit.

### Task 4.1: SPIKE — does patching a pending workflow's priority re-order the semaphore queue?

**Files:**
- Append findings to: `docs/FINDINGS.md`

**This is an experiment, not a TDD task.** Execute it against the live minikube cluster.

- [ ] **Step 1: Set the semaphore to 1 slot (already the default) and saturate it**

Submit one run that will hold the mysql slot for a while, then two more that will queue:
```bash
EP=http://localhost:8080
TOK="fake:runner:runner@local:dlh-runners"
DLH_TOKEN=$TOK go run ./cmd/dlh run mysql-pod-delete --endpoint $EP            # holder
DLH_TOKEN=$TOK go run ./cmd/dlh run mysql-pod-delete --priority 100 --endpoint $EP  # queued A (low)
DLH_TOKEN=$TOK go run ./cmd/dlh run mysql-pod-delete --priority 100 --endpoint $EP  # queued B (low)
```

- [ ] **Step 2: Confirm A and B are Pending (waiting on the semaphore)**

Run: `kubectl -n dlh-test-fw get wf -l dlh.scenario=mysql-pod-delete --sort-by=.metadata.creationTimestamp`
Expected: one Running, two Pending. Note the two pending names (A older than B).

- [ ] **Step 3: Patch the NEWER pending workflow (B) to a higher priority**

```bash
kubectl -n dlh-test-fw patch wf <B-name> --type merge -p '{"spec":{"priority":500}}'
```

- [ ] **Step 4: When the holder finishes, observe which pending run is admitted next**

Watch: `kubectl -n dlh-test-fw get wf -l dlh.scenario=mysql-pod-delete -w`
Expected outcome to record:
- **If B (the patched, newer, higher-priority) starts before A** → patching works; Layer 2 uses the priority patch.
- **If A (older) starts first regardless** → Argo ignores post-admission priority changes for semaphore ordering; Layer 2 must use **cancel + resubmit**.

- [ ] **Step 5: Record the decision in FINDINGS.md**

Append a numbered finding to `docs/FINDINGS.md` documenting: the Argo chart version (`0.45.20`), the experiment, the observed result, and the chosen mechanism (PATCH vs CANCEL+RESUBMIT). This decision drives Task 4.2.

- [ ] **Step 6: Clean up the spike workflows**

```bash
kubectl -n dlh-test-fw delete wf <A-name> <B-name> <holder-name>
```

- [ ] **Step 7: Commit the finding**

```bash
cd /Users/allen/repo/dlh-test-fw
git add docs/FINDINGS.md
git commit -m "docs(findings): Argo pending-priority-patch reorder spike result

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 4.2: Reprioritize logic (pending-only guard)

**Files:**
- Modify: `controlplane/internal/runs/` (add `reprioritize.go`)
- Test: `controlplane/internal/runs/reprioritize_test.go`

**Design (PATCH branch — use if the spike succeeded):** A `Reprioritize(ctx, runID, priority)` method that (1) Gets the workflow, (2) returns a typed `ErrNotPending` if the phase is not Pending, (3) patches `spec.priority`. **If the spike chose CANCEL+RESUBMIT**, implement instead: terminate the pending workflow and resubmit with the new priority; adjust the test accordingly. The interface the handler calls stays the same either way.

- [ ] **Step 1: Write the failing test (PATCH branch)**

Create `controlplane/internal/runs/reprioritize_test.go`:

```go
package runs

import (
	"context"
	"errors"
	"testing"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	wfake "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned/fake"
)

func TestReprioritize_PendingOnly(t *testing.T) {
	ns := "dlh-test-fw"
	pending := &wfv1.Workflow{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: ns},
		Status:     wfv1.WorkflowStatus{Phase: wfv1.WorkflowPending},
	}
	running := &wfv1.Workflow{
		ObjectMeta: metav1.ObjectMeta{Name: "r", Namespace: ns},
		Status:     wfv1.WorkflowStatus{Phase: wfv1.WorkflowRunning},
	}
	argo := wfake.NewSimpleClientset(pending, running)
	s := &Submitter{Argo: argo, Namespace: ns}

	// running → ErrNotPending
	if err := s.Reprioritize(context.Background(), "r", 500); !errors.Is(err, ErrNotPending) {
		t.Errorf("running reprioritize: got %v want ErrNotPending", err)
	}

	// pending → patches spec.priority
	if err := s.Reprioritize(context.Background(), "p", 500); err != nil {
		t.Fatalf("pending reprioritize: %v", err)
	}
	got, _ := argo.ArgoprojV1alpha1().Workflows(ns).Get(context.Background(), "p", metav1.GetOptions{})
	if got.Spec.Priority == nil || *got.Spec.Priority != 500 {
		t.Errorf("patched priority: got %v want 500", got.Spec.Priority)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd controlplane && go test ./internal/runs/ -run TestReprioritize -v`
Expected: FAIL — `Reprioritize` / `ErrNotPending` don't exist.

- [ ] **Step 3: Implement (PATCH branch)**

Create `controlplane/internal/runs/reprioritize.go`:

```go
package runs

import (
	"context"
	"errors"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
)

// ErrNotPending is returned when a reprioritize targets a run that is not
// queued (only Pending runs can be re-ordered; Argo fixes order at admission).
var ErrNotPending = errors.New("run is not pending")

// Reprioritize patches a pending workflow's spec.priority so Argo releases it
// in the new order. Returns ErrNotPending if the run is not Pending.
func (s *Submitter) Reprioritize(ctx context.Context, runID string, priority int) error {
	wf, err := s.Argo.ArgoprojV1alpha1().Workflows(s.Namespace).Get(ctx, runID, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get workflow %q: %w", runID, err)
	}
	if wf.Status.Phase != wfv1.WorkflowPending && wf.Status.Phase != "" {
		return ErrNotPending
	}
	patch := []byte(fmt.Sprintf(`{"spec":{"priority":%d}}`, int32(priority)))
	_, err = s.Argo.ArgoprojV1alpha1().Workflows(s.Namespace).Patch(
		ctx, runID, types.MergePatchType, patch, metav1.PatchOptions{})
	return err
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd controlplane && go test ./internal/runs/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/internal/runs/reprioritize.go controlplane/internal/runs/reprioritize_test.go
git commit -m "feat(runs): Reprioritize pending workflow (priority patch)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 4.3: OpenAPI — `POST /api/runs/{id}/priority`

**Files:**
- Modify: `controlplane/api/openapi.yaml`
- Regenerate: gen files

- [ ] **Step 1: Add the path**

In `controlplane/api/openapi.yaml`, under `paths:`, add:

```yaml
  /api/runs/{id}/priority:
    post:
      operationId: reprioritizeRun
      parameters:
        - in: path
          name: id
          required: true
          schema: { type: string }
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [priority]
              properties:
                priority: { type: integer }
      responses:
        "202":
          description: re-prioritized
        "400":
          description: invalid request
        "404":
          description: run not found
        "409":
          description: run is not pending (cannot reorder a running/finished run)
```

- [ ] **Step 2: Regenerate + compile**

Run: `cd controlplane && make codegen && go build ./...`
Expected: gen files updated; build fails only with "missing method ReprioritizeRun" (expected — next task).

- [ ] **Step 3: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/api/openapi.yaml controlplane/internal/api/gen/types.gen.go controlplane/internal/api/gen/server.gen.go controlplane/web/src/api/gen.ts
git commit -m "feat(api): POST /api/runs/{id}/priority schema

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 4.4: `ReprioritizeRun` handler (runner, 409 if not pending)

**Files:**
- Modify: `controlplane/internal/api/handlers.go`
- Modify: `controlplane/internal/api/server.go` (RequireRole(runner) on the route)
- Test: `controlplane/internal/api/handlers_test.go`

- [ ] **Step 1: Write the failing test**

Append to `controlplane/internal/api/handlers_test.go`:

```go
func TestReprioritizeRun_Statuses(t *testing.T) {
	ns := "dlh-test-fw"
	pending := &wfv1.Workflow{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: ns},
		Status: wfv1.WorkflowStatus{Phase: wfv1.WorkflowPending}}
	running := &wfv1.Workflow{ObjectMeta: metav1.ObjectMeta{Name: "r", Namespace: ns},
		Status: wfv1.WorkflowStatus{Phase: wfv1.WorkflowRunning}}
	argo := wfake.NewSimpleClientset(pending, running)
	deps := &Deps{
		Submitter: &runs.Submitter{Argo: argo, Namespace: ns},
		Workflows: &fakeWorkflows{items: []*wfv1.Workflow{pending, running}},
	}
	h := &Handlers{deps: deps}

	// pending → 202
	r202, err := h.ReprioritizeRun(context.Background(), gen.ReprioritizeRunRequestObject{
		Id: "p", Body: &gen.ReprioritizeRunJSONRequestBody{Priority: 500}})
	if err != nil {
		t.Fatalf("Reprioritize pending: %v", err)
	}
	if _, ok := r202.(gen.ReprioritizeRun202Response); !ok {
		t.Errorf("pending: got %T want 202", r202)
	}

	// running → 409
	r409, _ := h.ReprioritizeRun(context.Background(), gen.ReprioritizeRunRequestObject{
		Id: "r", Body: &gen.ReprioritizeRunJSONRequestBody{Priority: 500}})
	if _, ok := r409.(gen.ReprioritizeRun409Response); !ok {
		t.Errorf("running: got %T want 409", r409)
	}

	// unknown → 404
	r404, _ := h.ReprioritizeRun(context.Background(), gen.ReprioritizeRunRequestObject{
		Id: "nope", Body: &gen.ReprioritizeRunJSONRequestBody{Priority: 500}})
	if _, ok := r404.(gen.ReprioritizeRun404Response); !ok {
		t.Errorf("unknown: got %T want 404", r404)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd controlplane && go test ./internal/api/ -run TestReprioritizeRun -v`
Expected: FAIL — handler missing.

- [ ] **Step 3: Implement the handler**

Add to `controlplane/internal/api/handlers.go`:

```go
// ReprioritizeRun — POST /api/runs/{id}/priority
func (h *Handlers) ReprioritizeRun(ctx context.Context, req gen.ReprioritizeRunRequestObject) (gen.ReprioritizeRunResponseObject, error) {
	if req.Body == nil {
		return gen.ReprioritizeRun400Response{}, nil
	}
	if _, err := h.deps.Workflows.Get(req.Id); err != nil {
		return gen.ReprioritizeRun404Response{}, nil
	}
	err := h.deps.Submitter.Reprioritize(ctx, req.Id, req.Body.Priority)
	switch {
	case errors.Is(err, runs.ErrNotPending):
		return gen.ReprioritizeRun409Response{}, nil
	case err != nil:
		return nil, err
	}
	return gen.ReprioritizeRun202Response{}, nil
}
```

(`errors` and `runs` are already imported in handlers.go — confirm with the import block; `GetRun`/`CreateRun` already use them.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd controlplane && go test ./internal/api/ -run TestReprioritizeRun -v`
Expected: PASS.

- [ ] **Step 5: Enforce runner role on the route**

In `controlplane/internal/api/server.go`, after `gen.HandlerFromMux`, add (mirroring the admin PUT wiring from Task 3.5):

```go
	if authMW != nil {
		r.With(authMW, auth.RequireRole(auth.RoleRunner)).
			Post("/api/runs/{id}/priority", func(w http.ResponseWriter, req *http.Request) {
				strictSI.(http.Handler).ServeHTTP(w, req)
			})
	}
```

(Use the same mechanism you used for the admin PUT route — keep them consistent.)

- [ ] **Step 6: Verify build + tests**

Run: `cd controlplane && go build ./... && go test ./internal/api/`
Expected: exit 0, PASS.

- [ ] **Step 7: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/internal/api/handlers.go controlplane/internal/api/handlers_test.go controlplane/internal/api/server.go
git commit -m "feat(api): ReprioritizeRun handler (runner; 409 if not pending)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 4.5: CLI `dlh runs reprioritize`

**Files:**
- Modify: `controlplane/cmd/dlh/runs.go:15-22` (register) + add the subcommand

- [ ] **Step 1: Add the subcommand**

In `controlplane/cmd/dlh/runs.go`, register it in `runsCmd()` (line 20):

```go
	c.AddCommand(runsLsCmd(), runsShowCmd(), runsLogsCmd(), runsCancelCmd(), runsReprioritizeCmd())
```

And add the command (a `--to-front` flag sets a very high priority as a convenience):

```go
func runsReprioritizeCmd() *cobra.Command {
	var (
		priority int
		toFront  bool
	)
	c := &cobra.Command{
		Use:   "reprioritize <run-id>",
		Short: "Change a pending run's priority (only works while queued)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			p := priority
			if toFront {
				p = 1000 // above the Urgent tier (500)
			}
			if p == 0 {
				return fmt.Errorf("provide --priority N or --to-front")
			}
			_, _, err := newClient().do("POST", "/api/runs/"+args[0]+"/priority", map[string]any{"priority": p}, nil)
			if err != nil {
				return err
			}
			fmt.Printf("reprioritized %s → %d\n", args[0], p)
			return nil
		},
	}
	c.Flags().IntVar(&priority, "priority", 0, "New priority")
	c.Flags().BoolVar(&toFront, "to-front", false, "Move to front (priority 1000)")
	return c
}
```

(`fmt` is already imported in runs.go.)

- [ ] **Step 2: Verify it compiles**

Run: `cd controlplane && go build ./cmd/dlh && go run ./cmd/dlh runs reprioritize --help`
Expected: exit 0; help shows `--priority` and `--to-front`.

- [ ] **Step 3: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/cmd/dlh/runs.go
git commit -m "feat(cli): dlh runs reprioritize <id> --priority/--to-front

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 4.6: Queue page reorder / to-front / cancel controls

**Files:**
- Modify: `controlplane/web/src/pages/QueuePage.tsx`

**Design:** On each pending entry, add a "To front" button and a "Cancel" button. "To front" sets priority above the current lane max; Cancel calls `DELETE /api/runs/{id}`. Running entries get no controls. Refresh via the existing 5s poll (also call `reload()` after an action).

- [ ] **Step 1: Add the action handlers + buttons**

In `controlplane/web/src/pages/QueuePage.tsx`, pass `reload` down to `LaneCard` and add per-entry actions. Add imports:

```tsx
import { Button } from "@/components/ui/button";
import { toast } from "sonner";
import { ArrowUpToLine, X } from "lucide-react";
```

In the `QueuePage` component, define the actions and pass them down:

```tsx
  const toFront = async (lane: Lane, id: string) => {
    const max = Math.max(0, ...lane.pending.map((e) => e.priority ?? 0), ...lane.running.map((e) => e.priority ?? 0));
    const { error: e } = await api.POST("/api/runs/{id}/priority", {
      params: { path: { id } }, body: { priority: max + 100 },
    });
    if (e) toast.error("Reprioritize failed", { description: JSON.stringify(e) });
    else { toast.success("Moved to front"); reload(); }
  };
  const cancel = async (id: string) => {
    const { error: e } = await api.DELETE("/api/runs/{id}", { params: { path: { id } } });
    if (e) toast.error("Cancel failed", { description: JSON.stringify(e) });
    else { toast.success("Cancelled"); reload(); }
  };
```

Change the lane render to pass the callbacks:

```tsx
        {queue.lanes.map((lane) => (
          <LaneCard key={lane.key} lane={lane} onToFront={(id) => toFront(lane, id)} onCancel={cancel} />
        ))}
```

Update the `LaneCard` signature and the pending-entry markup to add buttons (only on pending rows, and only on rows that are not already #1):

```tsx
function LaneCard({ lane, onToFront, onCancel }: { lane: Lane; onToFront: (id: string) => void; onCancel: (id: string) => void }) {
```

Inside the pending `.map`, replace the right-hand `<span>` with controls:

```tsx
                  <span className="flex items-center gap-2 text-xs text-muted-foreground">
                    <span title={new Date(e.submittedAt).toLocaleString()}>{relativeTime(e.submittedAt)}</span>
                    <span className="font-mono">p{e.priority ?? "—"}</span>
                    {i > 0 && (
                      <Button size="sm" variant="ghost" title="Move to front" onClick={() => onToFront(e.id)}>
                        <ArrowUpToLine className="h-3.5 w-3.5" />
                      </Button>
                    )}
                    <Button size="sm" variant="ghost" title="Cancel" onClick={() => onCancel(e.id)}>
                      <X className="h-3.5 w-3.5" />
                    </Button>
                  </span>
```

- [ ] **Step 2: Verify build**

Run: `cd controlplane/web && pnpm build`
Expected: exit 0.

- [ ] **Step 3: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/web/src/pages/QueuePage.tsx
git commit -m "feat(web): Queue page to-front + cancel controls

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 4.7: Phase 4 live verification

- [ ] **Step 1: Build + deploy**

Run:
```bash
cd /Users/allen/repo/dlh-test-fw/controlplane && make ui-build && make reload-minikube
kubectl -n dlh-test-fw rollout restart deployment/dlh-controlplane
kubectl -n dlh-test-fw rollout status deployment/dlh-controlplane --timeout=120s
```

- [ ] **Step 2: Saturate the mysql lane and reorder via UI**

Submit a holder + two queued runs (as in Task 4.1 Step 1). On `http://localhost:8080/queue`, click "Move to front" on the #2 pending run. Screenshot before/after. Expected: it jumps to #1 (NEXT badge). Console: 0 errors.

- [ ] **Step 3: Confirm 409 for a running run**

Run:
```bash
curl -s -o /dev/null -w "%{http_code}\n" -X POST -H "Authorization: Bearer fake:runner:runner@local:dlh-runners" \
  -H "Content-Type: application/json" -d '{"priority":999}' http://localhost:8080/api/runs/<running-run-id>/priority
```
Expected: `409`.

- [ ] **Step 4: Cancel a queued run via UI**

Click "Cancel" on a pending run. Expected: it disappears from the lane; toast confirms. Console: 0 errors.

- [ ] **Step 5: Clean up**

Delete the spike workflows: `kubectl -n dlh-test-fw delete wf -l dlh.scenario=mysql-pod-delete --field-selector status.phase!=Running` (or by name).

---
---

## Final verification (after all phases)

- [ ] **Run the full Go suite:** `cd controlplane && go test ./...` → all PASS.
- [ ] **Run the full web suite + build:** `cd controlplane/web && pnpm test && pnpm build` → all PASS, build exit 0.
- [ ] **Confirm no `dist/` was committed:** `git log --stat | grep "internal/api/dist" && echo "LEAK" || echo "clean"` → "clean".
- [ ] **Update CLAUDE.md** "Phase F additions" area with a short "Priority (Plan 2026-05-26)" note: the three layers, the `dlh-scenario-priorities` ConfigMap, the `/api/queue` + `/api/scenario-priorities` + `/api/runs/{id}/priority` endpoints, and the spike outcome. Commit.
- [ ] **Append to `docs/FINDINGS.md`** any drift discovered during execution.
- [ ] **Finish the branch** via `superpowers:finishing-a-development-branch`.

---

## Self-Review (plan author)

**Spec coverage:**
- Layer 1 (submit override + display) → Tasks 1.1–1.7. ✓ (API/submitter/model/CLI/UI all covered; RunDetail display pre-existing.)
- Layer 2 (live re-prioritize) → Tasks 4.1–4.7, spike-gated. ✓ (409 Pending-only guard, CLI, UI controls, cancel+resubmit fallback documented.)
- Layer 3 (editable defaults) → Tasks 3.1–3.8. ✓ (ConfigMap, store, submitter integration, GET/PUT, admin UI, tiers.)
- Queue view → Tasks 2.1–2.8. ✓ (`/api/queue`, lanes, rules strip, idle state, CLI, polling.)
- RBAC (runner submit/reprioritize, admin defaults, viewer queue) → Tasks 2.5, 3.1, 3.5, 4.4. ✓
- Tiers (10/100/200/500) → Task 3.6. ✓
- Testing (Vitest tier + queue order; Go submit resolution, queue grouping, pending guard, CRUD+RBAC; spike; Playwright live) → covered across tasks + live-verification tasks. ✓

**Known gaps / risks flagged for the implementer:**
- The exact oapi-codegen-generated type names (e.g. `PutScenarioPriorityJSONRequestBody`, `ReprioritizeRun202Response`) must be verified against `internal/api/gen/server.gen.go` after each `make codegen` and the test code adjusted to match if the generator named them differently.
- The role-gated explicit-route wiring in `server.go` (Tasks 3.5, 4.4) must mirror the existing SSE explicit-route mechanism — the pseudo-code `strictSI.(http.Handler)` is indicative; use whatever the SSE block uses to re-register a single path behind middleware.
- Task 4.x is contingent on the Task 4.1 spike. If the spike says "patching doesn't reorder", swap the PATCH implementation in Task 4.2 for cancel+resubmit (the handler/CLI/UI interface is unchanged).
- `deriveTargetType` duplication (Go `internal/links` ↔ TS `web/src/lib/category.ts`) is reused, not modified — no sync risk introduced.
