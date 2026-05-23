# dlh-controlplane Phase F (Schedules) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Surface Argo's `CronWorkflow` CRD through the controlplane API + UI + CLI as a first-class "Schedule" resource. Engineers can create, list, pause/resume, and delete schedules; the existing Workflow informer + Syncer pick up firing Workflows automatically and surface them under `/api/runs` with no extra wiring.

**Architecture:** A new `internal/schedules/` package mirrors `runs.Submitter` but builds `argoproj.io/v1alpha1/CronWorkflow` CRs instead of `Workflow` CRs. The `CronWorkflow.spec.workflowSpec` uses `WorkflowTemplateRef` + merged params (identical shape to Submitter's output) so firing Workflows inherit the scenario + target labels needed by Phase D's Run path. Pause/resume is a JSON-merge patch on `spec.suspend`. The controlplane never creates a separate "schedule_id → run_id" mapping — Argo stamps firing Workflows with owner references back to the CronWorkflow, and clients that need that linkage walk it in k8s. For v1 we surface `lastScheduledTime` + active children counts via the Schedule detail response.

**Tech Stack:** Go 1.26 (existing module); existing `argoproj/argo-workflows/v3 v3.6.19` provides `wfclient.ArgoprojV1alpha1().CronWorkflows(ns)`; chi router; oapi-codegen; existing OIDC + session JWT middleware; existing UI stack (Vite + React + Tailwind); existing cobra CLI.

**Reference spec:** `docs/superpowers/specs/2026-05-21-dlh-controlplane-design.md` (§12 Phase F + §14 open question #5 — Schedule and Run independent until firing time).

**Branch & worktree:** Per `CLAUDE.md`, work on `feat/plan19-controlplane-schedules` in worktree `/Users/allen/repo/dlh-test-fw-plan19`. Task 1 creates it.

**Plan-time decisions / deviations from spec:**

1. **No schedule_id label on Workflows.** Spec §14 #5 left this open. v1 decision: Argo's CronWorkflow already sets `ownerReferences` on every child Workflow pointing at the CronWorkflow that fired it. We surface child workflows via the existing Workflow informer + a small UI link (`runs.cronWorkflow == scheduleId`) sourced from `wf.ownerReferences[0].name`. No new label needed; no Submitter changes.
2. **Pause/resume via `spec.suspend` JSON-merge patch.** Standard Argo pattern. The dedicated `/pause` + `/resume` POST endpoints are sugar on top.
3. **`schedule` (singular) string, not `schedules` (plural).** Argo v3.6 added a `schedules: []string` field but the spec.schedule single-string form is older and universally supported. v1 ships single-cron-only; multi-cron defers.
4. **RBAC: runner role minimum.** Schedules trigger Runs; treating them like submission. Spec §9 lists runner as the submit-capable role.
5. **Empty target_id default.** Schedules without `targetId` default to local chaos (matching Plan 17's CreateRun behavior).
6. **No schedule edit endpoint.** v1 ships create + delete; "edit" is `delete + create`. Adding PATCH is small but expanding the OpenAPI surface for v1 isn't worth it; if users complain, add later.
7. **Timezone field exposed.** Argo's `spec.timezone` is straightforward to surface; otherwise users have to deal with UTC vs local mental math.
8. **No Workflow-history rollup in the API.** `/api/schedules/{id}` returns `lastScheduledTime` + `active` count + `succeeded`/`failed` counts from `cronwf.Status` — that's all v1 needs. Per-firing history is the existing `/api/runs?scenario=...` view, filterable client-side.
9. **Natural pause points:**
   - After Task 7 (Section A — backend POST/GET/DELETE/pause/resume + ScheduleManager; no UI/CLI yet).
   - After Task 11 (Section B — CLI subcommands lands).
   - After Task 14 (Section C — UI Schedules page lands).
   - After Task 18 (everything except smoke + merge).

---

## File Structure

**New files (Go backend):**
- `controlplane/internal/schedules/manager.go` — `Manager` builds CronWorkflow CRs from scenario + target + params + cron + timezone; exposes List/Get/Delete/Pause/Resume.
- `controlplane/internal/schedules/manager_test.go`
- `controlplane/internal/api/schedules.go` — chi-routed handlers (real impl; OpenAPI strict stubs forward to it).
- `controlplane/internal/api/schedules_test.go`

**Modified files (Go backend):**
- `controlplane/api/openapi.yaml` — add `/api/schedules` paths + Schedule schemas; regenerate.
- `controlplane/internal/api/gen/*.gen.go` — regenerated.
- `controlplane/internal/api/handlers.go` — wire 6 new handler methods to schedules helpers.
- `controlplane/internal/api/server.go` — Deps gains `*schedules.Manager`.
- `controlplane/cmd/dlh-controlplane/main.go` — construct Manager; populate Deps.
- `controlplane/deploy/role.yaml` — add `argoproj.io/cronworkflows` get/list/watch/create/patch/delete.

**New files (CLI):**
- `controlplane/cmd/dlh/schedule.go` — cobra `schedule` parent + 6 subcommands.

**Modified files (CLI):**
- `controlplane/cmd/dlh/root.go` — register `scheduleCmd()`.

**New files (UI):**
- `controlplane/web/src/pages/SchedulesPage.tsx` — list + pause/resume + delete + inline create form.

**Modified files (UI):**
- `controlplane/web/src/App.tsx` — add `/schedules` route + nav link.
- `controlplane/web/src/pages/RunDetailPage.tsx` — if the Run has `ownerReferences[0].kind == CronWorkflow`, surface a "Triggered by schedule: <name>" link.

**Documentation:**
- `docs/FINDINGS.md` — Plan 19 section.
- `CLAUDE.md` — Phase F additions subsection.
- `README.md` — Plan 19 row.

**Unchanged:** verdict-job, k6 image, dashboards, Argo CD manifests, all existing controlplane code except the listed modifications.

---

## Task 1: Baseline + worktree

No commits.

- [ ] **Step 1: Verify clean main + CI green + Phase E present.**

```bash
cd /Users/allen/repo/dlh-test-fw
git status
git log --first-parent --oneline -5
gh run list --branch main --limit 1
ls controlplane/internal/runs/
ls controlplane/internal/auth/
```

Expected: clean tree on `main`; HEAD includes `0119234` (Plan 18 README backfill) and `a402cbb` (Plan 18 merge); CI `success`; runs/ + auth/ packages present.

- [ ] **Step 2: Confirm Argo CronWorkflow types are usable.**

```bash
grep -A 3 "^type CronWorkflow struct" /Users/allen/go/pkg/mod/github.com/argoproj/argo-workflows/v3@v3.6.19/pkg/apis/workflow/v1alpha1/cron_workflow_types.go 2>/dev/null || find /Users/allen/go/pkg/mod/github.com/argoproj/argo-workflows -name 'cron_workflow_types.go' | head -1
```

Expected: the `CronWorkflow` Go type exists in the argo-workflows module already in go.sum.

- [ ] **Step 3: Create the feature worktree using ABSOLUTE path.**

```bash
cd /Users/allen/repo/dlh-test-fw
git worktree add /Users/allen/repo/dlh-test-fw-plan19 -b feat/plan19-controlplane-schedules main
cd /Users/allen/repo/dlh-test-fw-plan19
git worktree list
git status
```

Expected: clean tree on `feat/plan19-controlplane-schedules`; worktree path `/Users/allen/repo/dlh-test-fw-plan19` as sibling.

- [ ] **Step 4: Verify Phase E baseline:**

```bash
cd /Users/allen/repo/dlh-test-fw-plan19/controlplane
make ui-build 2>&1 | tail -3
go build ./...
go test ./...
```

Expected: ui-build succeeds; go build clean; all Phase E tests pass.

All remaining tasks run from `/Users/allen/repo/dlh-test-fw-plan19`.

---

# Section A — Backend ScheduleManager + handlers (Tasks 2-7)

## Task 2: ScheduleManager skeleton (types + Create)

**Files:**
- Create: `controlplane/internal/schedules/manager.go`
- Create: `controlplane/internal/schedules/manager_test.go`

- [ ] **Step 1: Write `internal/schedules/manager.go`** with types + Create:

```go
// Package schedules wraps Argo's CronWorkflow CRD as a controlplane
// "Schedule" resource. Mirrors runs.Submitter's shape so firing
// Workflows inherit the dlh.scenario + dlh.target labels that the
// existing Workflow informer + Syncer already understand.
package schedules

import (
	"context"
	"fmt"
	"strings"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	wfclient "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Manager owns CronWorkflow lifecycle: create, list, get, delete, pause/resume.
type Manager struct {
	Argo      wfclient.Interface
	Namespace string
}

// CreateRequest is the inbound payload (one-step removed from the HTTP DTO).
type CreateRequest struct {
	Name       string            // user-supplied schedule id; must be a valid k8s name
	ScenarioID string            // WorkflowTemplate name
	TargetID   string            // empty = local
	Cron       string            // e.g. "*/15 * * * *"
	Timezone   string            // e.g. "Asia/Tokyo"; empty = UTC
	Parameters map[string]string // optional WT param overrides
	CreatedBy  string            // OIDC subject (annotation only)
}

// Create builds + applies a CronWorkflow CR. Returns the created object.
func (m *Manager) Create(ctx context.Context, req CreateRequest) (*wfv1.CronWorkflow, error) {
	if err := validateName(req.Name); err != nil {
		return nil, err
	}
	if req.ScenarioID == "" {
		return nil, fmt.Errorf("scenarioId is required")
	}
	if req.Cron == "" {
		return nil, fmt.Errorf("cron is required")
	}
	// Verify the scenario WorkflowTemplate exists.
	if _, err := m.Argo.ArgoprojV1alpha1().WorkflowTemplates(m.Namespace).Get(ctx, req.ScenarioID, metav1.GetOptions{}); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("scenario %q not found: %w", req.ScenarioID, err)
		}
		return nil, fmt.Errorf("get workflowtemplate: %w", err)
	}

	// Build workflow params: user overrides + target_id (always present).
	params := make([]wfv1.Parameter, 0, len(req.Parameters)+1)
	for k, v := range req.Parameters {
		val := wfv1.AnyString(v)
		params = append(params, wfv1.Parameter{Name: k, Value: &val})
	}
	tidVal := wfv1.AnyString(req.TargetID)
	params = append(params, wfv1.Parameter{Name: "target_id", Value: &tidVal})

	// Labels propagate to child Workflows via workflowMetadata.
	wfLabels := map[string]string{
		"dlh.scenario": req.ScenarioID,
	}
	if req.TargetID != "" {
		wfLabels["dlh.target"] = req.TargetID
	}

	cron := &wfv1.CronWorkflow{
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.Name,
			Namespace: m.Namespace,
			Labels: map[string]string{
				"dlh.scenario": req.ScenarioID,
				"dlh.schedule": req.Name,
			},
			Annotations: map[string]string{
				"dlh.created-by": req.CreatedBy,
			},
		},
		Spec: wfv1.CronWorkflowSpec{
			Schedule:          req.Cron,
			Timezone:          req.Timezone,
			ConcurrencyPolicy: wfv1.ForbidConcurrent,
			WorkflowSpec: wfv1.WorkflowSpec{
				WorkflowTemplateRef: &wfv1.WorkflowTemplateRef{Name: req.ScenarioID},
				Arguments:           wfv1.Arguments{Parameters: params},
				ServiceAccountName:  "argo-workflow",
			},
			WorkflowMetadata: &metav1.ObjectMeta{
				Labels: wfLabels,
			},
		},
	}
	created, err := m.Argo.ArgoprojV1alpha1().CronWorkflows(m.Namespace).Create(ctx, cron, metav1.CreateOptions{})
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil, fmt.Errorf("schedule %q already exists", req.Name)
		}
		return nil, fmt.Errorf("create cronworkflow: %w", err)
	}
	return created, nil
}

// validateName rejects anything that wouldn't be a valid k8s resource name.
// Cheap defense; the k8s API would reject anyway, but the error is clearer here.
func validateName(name string) error {
	if name == "" {
		return fmt.Errorf("schedule name is required")
	}
	if len(name) > 253 {
		return fmt.Errorf("schedule name too long (>253 chars)")
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '.':
		default:
			return fmt.Errorf("schedule name must be lowercase alphanumeric + '-' + '.'")
		}
	}
	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		return fmt.Errorf("schedule name cannot start or end with '-'")
	}
	return nil
}
```

- [ ] **Step 2: Write `internal/schedules/manager_test.go`** with create tests:

```go
package schedules

import (
	"context"
	"strings"
	"testing"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	wfake "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newManager(t *testing.T, objs ...interface{}) *Manager {
	t.Helper()
	// argo fake takes runtime.Object — wrap each.
	runtimeObjs := make([]wfake.Object, 0, len(objs)) // adjust per actual signature below
	_ = runtimeObjs
	// Simpler: call NewSimpleClientset on a slice of typed pointers — the
	// fake accepts them.
	switch len(objs) {
	case 0:
		return &Manager{Argo: wfake.NewSimpleClientset(), Namespace: "dlh-test-fw"}
	}
	// Variadic interface to runtime.Object cast doesn't work without copy;
	// the fake's NewSimpleClientset signature is variadic runtime.Object so
	// we collect typed objects directly here.
	// (Tests below pass typed pointers — adjust if you switch to runtime.Object)
	return nil
}
```

The fake-client variadic gymnastics is fiddly. Simplify — just call `wfake.NewSimpleClientset` inline at each test site:

```go
package schedules

import (
	"context"
	"strings"
	"testing"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	wfake "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCreate_HappyPath(t *testing.T) {
	tmpl := &wfv1.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-pod-delete", Namespace: "dlh-test-fw"},
	}
	argo := wfake.NewSimpleClientset(tmpl)
	m := &Manager{Argo: argo, Namespace: "dlh-test-fw"}
	got, err := m.Create(context.Background(), CreateRequest{
		Name:       "nightly-mysql",
		ScenarioID: "mysql-pod-delete",
		Cron:       "0 2 * * *",
		CreatedBy:  "tester",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.Name != "nightly-mysql" {
		t.Errorf("name: %q", got.Name)
	}
	if got.Spec.Schedule != "0 2 * * *" {
		t.Errorf("schedule: %q", got.Spec.Schedule)
	}
	if got.Spec.WorkflowSpec.WorkflowTemplateRef == nil || got.Spec.WorkflowSpec.WorkflowTemplateRef.Name != "mysql-pod-delete" {
		t.Errorf("templateRef: %+v", got.Spec.WorkflowSpec.WorkflowTemplateRef)
	}
	// Confirm target_id parameter was added with empty value.
	foundTargetID := false
	for _, p := range got.Spec.WorkflowSpec.Arguments.Parameters {
		if p.Name == "target_id" {
			foundTargetID = true
		}
	}
	if !foundTargetID {
		t.Errorf("target_id parameter not appended: %+v", got.Spec.WorkflowSpec.Arguments.Parameters)
	}
	// Confirm workflowMetadata labels are set for downstream Syncer.
	if got.Spec.WorkflowMetadata == nil || got.Spec.WorkflowMetadata.Labels["dlh.scenario"] != "mysql-pod-delete" {
		t.Errorf("workflowMetadata.labels: %+v", got.Spec.WorkflowMetadata)
	}
}

func TestCreate_WithTarget(t *testing.T) {
	tmpl := &wfv1.WorkflowTemplate{ObjectMeta: metav1.ObjectMeta{Name: "mysql-pod-delete", Namespace: "dlh-test-fw"}}
	argo := wfake.NewSimpleClientset(tmpl)
	m := &Manager{Argo: argo, Namespace: "dlh-test-fw"}
	got, _ := m.Create(context.Background(), CreateRequest{
		Name:       "nightly-mysql-staging",
		ScenarioID: "mysql-pod-delete",
		TargetID:   "staging-mysql",
		Cron:       "0 2 * * *",
	})
	if got.Spec.WorkflowMetadata.Labels["dlh.target"] != "staging-mysql" {
		t.Errorf("dlh.target label missing: %+v", got.Spec.WorkflowMetadata.Labels)
	}
}

func TestCreate_UnknownScenario(t *testing.T) {
	argo := wfake.NewSimpleClientset()
	m := &Manager{Argo: argo, Namespace: "dlh-test-fw"}
	_, err := m.Create(context.Background(), CreateRequest{
		Name: "x", ScenarioID: "nope", Cron: "0 * * * *",
	})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not found, got %v", err)
	}
}

func TestCreate_RejectsEmpty(t *testing.T) {
	m := &Manager{Argo: wfake.NewSimpleClientset(), Namespace: "dlh-test-fw"}
	cases := []CreateRequest{
		{Name: "", ScenarioID: "x", Cron: "0 * * * *"},
		{Name: "x", ScenarioID: "", Cron: "0 * * * *"},
		{Name: "x", ScenarioID: "y", Cron: ""},
	}
	for i, c := range cases {
		if _, err := m.Create(context.Background(), c); err == nil {
			t.Errorf("case %d: expected error", i)
		}
	}
}

func TestValidateName(t *testing.T) {
	cases := []struct {
		in   string
		wantOK bool
	}{
		{"foo", true},
		{"foo-bar", true},
		{"a.b", true},
		{"", false},
		{"-foo", false},
		{"foo-", false},
		{"Foo", false},        // uppercase rejected
		{"under_score", false}, // underscore rejected
	}
	for _, c := range cases {
		err := validateName(c.in)
		if (err == nil) != c.wantOK {
			t.Errorf("validateName(%q): err=%v wantOK=%v", c.in, err, c.wantOK)
		}
	}
}
```

- [ ] **Step 3: Build + test:**

```bash
cd /Users/allen/repo/dlh-test-fw-plan19/controlplane
go mod tidy
go build ./...
go test ./internal/schedules/... -v
```

Expected: clean build; 5 tests pass.

- [ ] **Step 4: Commit:**

```bash
cd /Users/allen/repo/dlh-test-fw-plan19
git add controlplane/internal/schedules/
git status
git commit -m "feat(controlplane/schedules): Manager.Create wraps Argo CronWorkflow

Mirrors runs.Submitter shape — verifies scenario WT exists, appends
target_id parameter, sets workflowMetadata.labels so firing Workflows
inherit dlh.scenario + dlh.target. Single-cron form (spec.schedule);
v3.6 spec.schedules deferred."
```

---

## Task 3: ScheduleManager List / Get / Delete

**Files:**
- Modify: `controlplane/internal/schedules/manager.go`
- Modify: `controlplane/internal/schedules/manager_test.go`

- [ ] **Step 1: Append List + Get + Delete to manager.go.**

```go
import (
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// List returns all schedules in the namespace.
func (m *Manager) List(ctx context.Context) ([]wfv1.CronWorkflow, error) {
	list, err := m.Argo.ArgoprojV1alpha1().CronWorkflows(m.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list cronworkflows: %w", err)
	}
	return list.Items, nil
}

// Get returns the CronWorkflow or ErrNotFound.
func (m *Manager) Get(ctx context.Context, name string) (*wfv1.CronWorkflow, error) {
	got, err := m.Argo.ArgoprojV1alpha1().CronWorkflows(m.Namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return got, nil
}

// Delete removes the schedule. Idempotent — returns nil if already gone.
func (m *Manager) Delete(ctx context.Context, name string) error {
	err := m.Argo.ArgoprojV1alpha1().CronWorkflows(m.Namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

// ErrNotFound is returned by Get when the schedule doesn't exist.
var ErrNotFound = fmt.Errorf("schedule not found")
```

(Reuse the existing `apierrors` import from Create; this Step's `import` block is additive — merge if needed. Use Edit to append the new methods + the ErrNotFound declaration after the existing Create body.)

- [ ] **Step 2: Append tests to manager_test.go:**

```go
func TestList_EmptyNamespace(t *testing.T) {
	m := &Manager{Argo: wfake.NewSimpleClientset(), Namespace: "dlh-test-fw"}
	got, err := m.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %d", len(got))
	}
}

func TestGet_NotFound(t *testing.T) {
	m := &Manager{Argo: wfake.NewSimpleClientset(), Namespace: "dlh-test-fw"}
	_, err := m.Get(context.Background(), "nope")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestDelete_IdempotentOnMissing(t *testing.T) {
	m := &Manager{Argo: wfake.NewSimpleClientset(), Namespace: "dlh-test-fw"}
	if err := m.Delete(context.Background(), "nope"); err != nil {
		t.Errorf("expected nil for missing, got %v", err)
	}
}

func TestCreate_ListGetDelete_Roundtrip(t *testing.T) {
	tmpl := &wfv1.WorkflowTemplate{ObjectMeta: metav1.ObjectMeta{Name: "mysql-pod-delete", Namespace: "dlh-test-fw"}}
	argo := wfake.NewSimpleClientset(tmpl)
	m := &Manager{Argo: argo, Namespace: "dlh-test-fw"}
	_, err := m.Create(context.Background(), CreateRequest{
		Name: "nightly", ScenarioID: "mysql-pod-delete", Cron: "0 2 * * *",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	list, _ := m.List(context.Background())
	if len(list) != 1 || list[0].Name != "nightly" {
		t.Errorf("list: %+v", list)
	}
	got, _ := m.Get(context.Background(), "nightly")
	if got == nil || got.Name != "nightly" {
		t.Errorf("get: %+v", got)
	}
	if err := m.Delete(context.Background(), "nightly"); err != nil {
		t.Errorf("delete: %v", err)
	}
	if _, err := m.Get(context.Background(), "nightly"); err != ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}
```

- [ ] **Step 3: Build + test:**

```bash
go build ./...
go test ./internal/schedules/... -v
```

Expected: 9 tests pass (5 from Task 2 + 4 new).

- [ ] **Step 4: Commit:**

```bash
git add controlplane/internal/schedules/
git commit -m "feat(controlplane/schedules): List + Get + Delete (idempotent on missing)"
```

---

## Task 4: Manager Pause + Resume

**Files:**
- Modify: `controlplane/internal/schedules/manager.go`
- Modify: `controlplane/internal/schedules/manager_test.go`

- [ ] **Step 1: Append Pause + Resume to manager.go.**

```go
import (
	"k8s.io/apimachinery/pkg/types"
)

// Pause toggles spec.suspend=true via JSON-merge patch. Idempotent.
func (m *Manager) Pause(ctx context.Context, name string) error {
	return m.setSuspend(ctx, name, true)
}

// Resume toggles spec.suspend=false. Idempotent.
func (m *Manager) Resume(ctx context.Context, name string) error {
	return m.setSuspend(ctx, name, false)
}

func (m *Manager) setSuspend(ctx context.Context, name string, suspend bool) error {
	patch := []byte(fmt.Sprintf(`{"spec":{"suspend":%t}}`, suspend))
	_, err := m.Argo.ArgoprojV1alpha1().CronWorkflows(m.Namespace).Patch(
		ctx, name, types.MergePatchType, patch, metav1.PatchOptions{},
	)
	if apierrors.IsNotFound(err) {
		return ErrNotFound
	}
	return err
}
```

(Merge the new `types` import into the existing import block via Edit.)

- [ ] **Step 2: Append tests:**

```go
func TestPause_Resume_Roundtrip(t *testing.T) {
	tmpl := &wfv1.WorkflowTemplate{ObjectMeta: metav1.ObjectMeta{Name: "mysql-pod-delete", Namespace: "dlh-test-fw"}}
	argo := wfake.NewSimpleClientset(tmpl)
	m := &Manager{Argo: argo, Namespace: "dlh-test-fw"}
	_, err := m.Create(context.Background(), CreateRequest{
		Name: "nightly", ScenarioID: "mysql-pod-delete", Cron: "0 2 * * *",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := m.Pause(context.Background(), "nightly"); err != nil {
		t.Errorf("Pause: %v", err)
	}
	got, _ := m.Get(context.Background(), "nightly")
	if !got.Spec.Suspend {
		t.Errorf("expected suspend=true after Pause, got false")
	}
	if err := m.Resume(context.Background(), "nightly"); err != nil {
		t.Errorf("Resume: %v", err)
	}
	got, _ = m.Get(context.Background(), "nightly")
	if got.Spec.Suspend {
		t.Errorf("expected suspend=false after Resume, got true")
	}
}

func TestPause_NotFound(t *testing.T) {
	m := &Manager{Argo: wfake.NewSimpleClientset(), Namespace: "dlh-test-fw"}
	if err := m.Pause(context.Background(), "nope"); err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}
```

- [ ] **Step 3: Build + test:**

```bash
go build ./...
go test ./internal/schedules/... -v
```

Expected: 11 tests pass.

- [ ] **Step 4: Commit:**

```bash
git add controlplane/internal/schedules/
git commit -m "feat(controlplane/schedules): Pause + Resume via spec.suspend JSON-merge patch"
```

---

## Task 5: OpenAPI for /api/schedules

**Files:**
- Modify: `controlplane/api/openapi.yaml`
- Regenerate: `controlplane/internal/api/gen/*.gen.go`, `controlplane/web/src/api/gen.ts`
- Modify: `controlplane/internal/api/handlers.go` — stubs

- [ ] **Step 1: Inspect existing spec to find insertion points:**

```bash
cd /Users/allen/repo/dlh-test-fw-plan19/controlplane
grep -n "^  /\|^components:" api/openapi.yaml | head -20
```

- [ ] **Step 2: Add Schedule path blocks** before `components:`. Use the `Edit` tool. old_string = `^components:` (unique). new_string = the new paths + the same `components:` heading:

```
  /api/schedules:
    get:
      operationId: listSchedules
      responses:
        "200":
          description: list of schedules
          content:
            application/json:
              schema:
                type: object
                required: [items]
                properties:
                  items:
                    type: array
                    items: { $ref: "#/components/schemas/Schedule" }
    post:
      operationId: createSchedule
      requestBody:
        required: true
        content:
          application/json:
            schema: { $ref: "#/components/schemas/CreateScheduleRequest" }
      responses:
        "201":
          description: created
          content:
            application/json:
              schema: { $ref: "#/components/schemas/Schedule" }
        "400":
          description: invalid request
        "404":
          description: scenario not found
        "409":
          description: schedule already exists
  /api/schedules/{id}:
    get:
      operationId: getSchedule
      parameters:
        - in: path
          name: id
          required: true
          schema: { type: string }
      responses:
        "200":
          description: schedule detail
          content:
            application/json:
              schema: { $ref: "#/components/schemas/Schedule" }
        "404":
          description: not found
    delete:
      operationId: deleteSchedule
      parameters:
        - in: path
          name: id
          required: true
          schema: { type: string }
      responses:
        "204":
          description: deleted (or already gone)
  /api/schedules/{id}/pause:
    post:
      operationId: pauseSchedule
      parameters:
        - in: path
          name: id
          required: true
          schema: { type: string }
      responses:
        "204":
          description: paused
        "404":
          description: not found
  /api/schedules/{id}/resume:
    post:
      operationId: resumeSchedule
      parameters:
        - in: path
          name: id
          required: true
          schema: { type: string }
      responses:
        "204":
          description: resumed
        "404":
          description: not found
components:
```

- [ ] **Step 3: Add Schedule + CreateScheduleRequest schemas.** Find the last existing schema in `components.schemas:` (likely `AuthInfo` from Plan 18). Use Edit to append:

```yaml
    Schedule:
      type: object
      required: [id, scenario, cron]
      properties:
        id:                { type: string }
        scenario:          { type: string }
        target:            { type: string }
        cron:              { type: string }
        timezone:          { type: string }
        suspended:         { type: boolean }
        createdBy:         { type: string }
        lastScheduledAt:   { type: string, format: date-time }
        activeCount:       { type: integer, format: int32 }
        successfulCount:   { type: integer, format: int64 }
        failedCount:       { type: integer, format: int64 }
        parameters:
          type: object
          additionalProperties: { type: string }
    CreateScheduleRequest:
      type: object
      required: [id, scenarioId, cron]
      properties:
        id:
          type: string
          description: "Schedule identifier. Lowercase alphanumeric + '-' + '.' (k8s name rules)."
        scenarioId:
          type: string
          description: "WorkflowTemplate name (e.g. mysql-pod-delete)"
        targetId:
          type: string
          description: "Optional remote target ID. Empty = framework cluster."
        cron:
          type: string
          description: "Standard 5-field cron expression."
        timezone:
          type: string
          description: "IANA tz name (e.g. Asia/Tokyo). Empty = UTC."
        parameters:
          type: object
          description: "Optional WorkflowTemplate parameter overrides."
          additionalProperties: { type: string }
```

(Anchor on the existing `AuthInfo` block's last line — likely the `authDisabled:` field declaration.)

- [ ] **Step 4: Validate YAML + regenerate:**

```bash
cd /Users/allen/repo/dlh-test-fw-plan19/controlplane
python3 -c "import yaml; list(yaml.safe_load_all(open('api/openapi.yaml')))" && echo "valid YAML"
go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -config api/oapi-codegen-server.yaml api/openapi.yaml
go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -config api/oapi-codegen-types.yaml api/openapi.yaml
cd web && pnpm openapi-typescript ../api/openapi.yaml -o src/api/gen.ts && cd ..
```

- [ ] **Step 5: Lookup response type names** (so the stubs in Step 6 compile):

```bash
grep -nE "^type (ListSchedules|CreateSchedule|GetSchedule|DeleteSchedule|PauseSchedule|ResumeSchedule)[A-Za-z0-9]*Response" internal/api/gen/server.gen.go
```

Take note of the actual emitted names — typically `ListSchedules200JSONResponse`, `CreateSchedule201JSONResponse`, `CreateSchedule400Response`, `CreateSchedule404Response`, `CreateSchedule409Response`, `GetSchedule200JSONResponse`, `GetSchedule404Response`, `DeleteSchedule204Response`, `PauseSchedule204Response`, `PauseSchedule404Response`, `ResumeSchedule204Response`, `ResumeSchedule404Response`.

- [ ] **Step 6: Append 6 stubs to `internal/api/handlers.go`.** At the bottom of the file:

```go
// Phase F stubs. Real implementations in Task 6.
func (h *Handlers) ListSchedules(_ context.Context, _ gen.ListSchedulesRequestObject) (gen.ListSchedulesResponseObject, error) {
	return gen.ListSchedules200JSONResponse{Items: []gen.Schedule{}}, nil
}
func (h *Handlers) CreateSchedule(_ context.Context, _ gen.CreateScheduleRequestObject) (gen.CreateScheduleResponseObject, error) {
	return gen.CreateSchedule400Response{}, nil
}
func (h *Handlers) GetSchedule(_ context.Context, _ gen.GetScheduleRequestObject) (gen.GetScheduleResponseObject, error) {
	return gen.GetSchedule404Response{}, nil
}
func (h *Handlers) DeleteSchedule(_ context.Context, _ gen.DeleteScheduleRequestObject) (gen.DeleteScheduleResponseObject, error) {
	return gen.DeleteSchedule204Response{}, nil
}
func (h *Handlers) PauseSchedule(_ context.Context, _ gen.PauseScheduleRequestObject) (gen.PauseScheduleResponseObject, error) {
	return gen.PauseSchedule404Response{}, nil
}
func (h *Handlers) ResumeSchedule(_ context.Context, _ gen.ResumeScheduleRequestObject) (gen.ResumeScheduleResponseObject, error) {
	return gen.ResumeSchedule404Response{}, nil
}
```

(Substitute actual response-type names if they differ.)

- [ ] **Step 7: Build + test:**

```bash
cd /Users/allen/repo/dlh-test-fw-plan19/controlplane
go build ./...
go test ./...
```

Expected: clean build; existing tests still pass.

- [ ] **Step 8: Commit:**

```bash
cd /Users/allen/repo/dlh-test-fw-plan19
git add controlplane/api/openapi.yaml controlplane/internal/api/gen/ \
        controlplane/internal/api/handlers.go controlplane/web/src/api/gen.ts
git commit -m "feat(controlplane): OpenAPI for /api/schedules + Schedule schemas

Six operations: list, create, get, delete, pause, resume. Schedule DTO
mirrors the Argo CronWorkflow status surface (lastScheduledAt + active
/ successful / failed counts). Stub handlers; real impl in Task 6."
```

---

## Task 6: Wire ScheduleManager + real handlers

**Files:**
- Create: `controlplane/internal/api/schedules.go`
- Create: `controlplane/internal/api/schedules_test.go`
- Modify: `controlplane/internal/api/server.go`
- Modify: `controlplane/internal/api/handlers.go` (replace stubs)
- Modify: `controlplane/cmd/dlh-controlplane/main.go`

- [ ] **Step 1: Add `Schedules *schedules.Manager` to Deps in `internal/api/server.go`.**

```bash
grep -A 12 "^type Deps struct" internal/api/server.go
```

Use Edit. Append `Schedules *schedules.Manager` after the existing fields. Add import `"github.com/dlh/dlh-test-fw/controlplane/internal/schedules"`.

- [ ] **Step 2: Write `internal/api/schedules.go`** with DTO conversion + handler helpers.

Inspect the codegen-emitted Schedule field shapes first:

```bash
grep -A 14 "^type Schedule struct" internal/api/gen/types.gen.go
grep -A 6 "^type CreateScheduleRequest struct" internal/api/gen/types.gen.go
```

The codegen will produce something like:

```go
type Schedule struct {
	ActiveCount     *int32             `json:"activeCount,omitempty"`
	CreatedBy       *string            `json:"createdBy,omitempty"`
	Cron            string             `json:"cron"`
	FailedCount     *int64             `json:"failedCount,omitempty"`
	Id              string             `json:"id"`
	LastScheduledAt *time.Time         `json:"lastScheduledAt,omitempty"`
	Parameters      *map[string]string `json:"parameters,omitempty"`
	Scenario        string             `json:"scenario"`
	SuccessfulCount *int64             `json:"successfulCount,omitempty"`
	Suspended       *bool              `json:"suspended,omitempty"`
	Target          *string            `json:"target,omitempty"`
	Timezone        *string            `json:"timezone,omitempty"`
}
```

Write `internal/api/schedules.go`:

```go
package api

import (
	"context"
	"errors"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"

	"github.com/dlh/dlh-test-fw/controlplane/internal/api/gen"
	"github.com/dlh/dlh-test-fw/controlplane/internal/schedules"
)

func scheduleDTO(c *wfv1.CronWorkflow) gen.Schedule {
	id := c.Name
	scenario := ""
	if c.Spec.WorkflowSpec.WorkflowTemplateRef != nil {
		scenario = c.Spec.WorkflowSpec.WorkflowTemplateRef.Name
	} else if v := c.Labels["dlh.scenario"]; v != "" {
		scenario = v
	}
	target := ""
	if c.Spec.WorkflowMetadata != nil {
		if v := c.Spec.WorkflowMetadata.Labels["dlh.target"]; v != "" {
			target = v
		}
	}
	suspended := c.Spec.Suspend
	tz := c.Spec.Timezone
	createdBy := ""
	if v, ok := c.Annotations["dlh.created-by"]; ok {
		createdBy = v
	}
	params := map[string]string{}
	for _, p := range c.Spec.WorkflowSpec.Arguments.Parameters {
		if p.Value != nil {
			params[p.Name] = p.Value.String()
		}
	}
	dto := gen.Schedule{
		Id:        id,
		Scenario:  scenario,
		Cron:      c.Spec.Schedule,
		Suspended: &suspended,
	}
	if target != "" {
		dto.Target = &target
	}
	if tz != "" {
		dto.Timezone = &tz
	}
	if createdBy != "" {
		dto.CreatedBy = &createdBy
	}
	if len(params) > 0 {
		dto.Parameters = &params
	}
	if c.Status.LastScheduledTime != nil {
		t := c.Status.LastScheduledTime.Time
		dto.LastScheduledAt = &t
	}
	active := int32(len(c.Status.Active))
	dto.ActiveCount = &active
	succ := c.Status.Succeeded
	dto.SuccessfulCount = &succ
	fail := c.Status.Failed
	dto.FailedCount = &fail
	return dto
}

// handleListSchedules retrieves all schedules + maps to DTO.
func (h *Handlers) handleListSchedules(ctx context.Context) (gen.ListSchedulesResponseObject, error) {
	if h.deps.Schedules == nil {
		return gen.ListSchedules200JSONResponse{Items: []gen.Schedule{}}, nil
	}
	list, err := h.deps.Schedules.List(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]gen.Schedule, 0, len(list))
	for i := range list {
		items = append(items, scheduleDTO(&list[i]))
	}
	return gen.ListSchedules200JSONResponse{Items: items}, nil
}

// handleCreateSchedule builds a Manager.CreateRequest and returns the DTO.
func (h *Handlers) handleCreateSchedule(ctx context.Context, req gen.CreateScheduleRequestObject) (gen.CreateScheduleResponseObject, error) {
	if h.deps.Schedules == nil {
		return gen.CreateSchedule400Response{}, nil
	}
	body := req.Body
	if body == nil {
		return gen.CreateSchedule400Response{}, nil
	}
	id, _ := auth.IdentityFromContext(ctx)
	createdBy := ""
	if id != nil {
		createdBy = id.Subject
	}
	mr := schedules.CreateRequest{
		Name:       body.Id,
		ScenarioID: body.ScenarioId,
		Cron:       body.Cron,
		CreatedBy:  createdBy,
	}
	if body.TargetId != nil {
		mr.TargetID = *body.TargetId
	}
	if body.Timezone != nil {
		mr.Timezone = *body.Timezone
	}
	if body.Parameters != nil {
		mr.Parameters = *body.Parameters
	}
	got, err := h.deps.Schedules.Create(ctx, mr)
	if err != nil {
		// Map errors to status codes.
		s := err.Error()
		switch {
		case contains(s, "not found"):
			return gen.CreateSchedule404Response{}, nil
		case contains(s, "already exists"):
			return gen.CreateSchedule409Response{}, nil
		default:
			return gen.CreateSchedule400Response{}, nil
		}
	}
	return gen.CreateSchedule201JSONResponse(scheduleDTO(got)), nil
}

// handleGetSchedule
func (h *Handlers) handleGetSchedule(ctx context.Context, req gen.GetScheduleRequestObject) (gen.GetScheduleResponseObject, error) {
	if h.deps.Schedules == nil {
		return gen.GetSchedule404Response{}, nil
	}
	got, err := h.deps.Schedules.Get(ctx, req.Id)
	if errors.Is(err, schedules.ErrNotFound) {
		return gen.GetSchedule404Response{}, nil
	}
	if err != nil {
		return nil, err
	}
	return gen.GetSchedule200JSONResponse(scheduleDTO(got)), nil
}

// handleDeleteSchedule
func (h *Handlers) handleDeleteSchedule(ctx context.Context, req gen.DeleteScheduleRequestObject) (gen.DeleteScheduleResponseObject, error) {
	if h.deps.Schedules == nil {
		return gen.DeleteSchedule204Response{}, nil
	}
	if err := h.deps.Schedules.Delete(ctx, req.Id); err != nil {
		return nil, err
	}
	return gen.DeleteSchedule204Response{}, nil
}

// handlePauseSchedule
func (h *Handlers) handlePauseSchedule(ctx context.Context, req gen.PauseScheduleRequestObject) (gen.PauseScheduleResponseObject, error) {
	if h.deps.Schedules == nil {
		return gen.PauseSchedule404Response{}, nil
	}
	if err := h.deps.Schedules.Pause(ctx, req.Id); err != nil {
		if errors.Is(err, schedules.ErrNotFound) {
			return gen.PauseSchedule404Response{}, nil
		}
		return nil, err
	}
	return gen.PauseSchedule204Response{}, nil
}

// handleResumeSchedule
func (h *Handlers) handleResumeSchedule(ctx context.Context, req gen.ResumeScheduleRequestObject) (gen.ResumeScheduleResponseObject, error) {
	if h.deps.Schedules == nil {
		return gen.ResumeSchedule404Response{}, nil
	}
	if err := h.deps.Schedules.Resume(ctx, req.Id); err != nil {
		if errors.Is(err, schedules.ErrNotFound) {
			return gen.ResumeSchedule404Response{}, nil
		}
		return nil, err
	}
	return gen.ResumeSchedule204Response{}, nil
}

// contains is a tiny strings.Contains alias to avoid importing strings just
// for this — but actually let's just import strings:
func contains(s, substr string) bool { return stringsContains(s, substr) }
```

Wait — defining `contains` like that and routing through `stringsContains` is silly. Just import `"strings"` and inline:

Use `"strings"` import + `strings.Contains(s, "not found")` directly. Drop the `contains` helper at the bottom.

Re-imports for `schedules.go`:

```go
import (
	"context"
	"errors"
	"strings"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"

	"github.com/dlh/dlh-test-fw/controlplane/internal/api/gen"
	"github.com/dlh/dlh-test-fw/controlplane/internal/auth"
	"github.com/dlh/dlh-test-fw/controlplane/internal/schedules"
)
```

And replace `contains(s, "not found")` with `strings.Contains(s, "not found")` etc.

(If the actual codegen-emitted field name for `CreateScheduleRequest.ScenarioId` differs — e.g., `ScenarioID` — adjust assignment.)

- [ ] **Step 3: Replace the 6 stubs in `internal/api/handlers.go`** with thin redirects to the schedules.go helpers:

```go
func (h *Handlers) ListSchedules(ctx context.Context, _ gen.ListSchedulesRequestObject) (gen.ListSchedulesResponseObject, error) {
	return h.handleListSchedules(ctx)
}
func (h *Handlers) CreateSchedule(ctx context.Context, req gen.CreateScheduleRequestObject) (gen.CreateScheduleResponseObject, error) {
	return h.handleCreateSchedule(ctx, req)
}
func (h *Handlers) GetSchedule(ctx context.Context, req gen.GetScheduleRequestObject) (gen.GetScheduleResponseObject, error) {
	return h.handleGetSchedule(ctx, req)
}
func (h *Handlers) DeleteSchedule(ctx context.Context, req gen.DeleteScheduleRequestObject) (gen.DeleteScheduleResponseObject, error) {
	return h.handleDeleteSchedule(ctx, req)
}
func (h *Handlers) PauseSchedule(ctx context.Context, req gen.PauseScheduleRequestObject) (gen.PauseScheduleResponseObject, error) {
	return h.handlePauseSchedule(ctx, req)
}
func (h *Handlers) ResumeSchedule(ctx context.Context, req gen.ResumeScheduleRequestObject) (gen.ResumeScheduleResponseObject, error) {
	return h.handleResumeSchedule(ctx, req)
}
```

- [ ] **Step 4: Write `internal/api/schedules_test.go`** for the happy + 404 paths:

```go
package api

import (
	"context"
	"testing"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	wfake "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/dlh/dlh-test-fw/controlplane/internal/api/gen"
	"github.com/dlh/dlh-test-fw/controlplane/internal/schedules"
)

func TestCreateSchedule_HappyPath(t *testing.T) {
	tmpl := &wfv1.WorkflowTemplate{ObjectMeta: metav1.ObjectMeta{Name: "mysql-pod-delete", Namespace: "dlh-test-fw"}}
	argo := wfake.NewSimpleClientset(tmpl)
	deps := &Deps{Schedules: &schedules.Manager{Argo: argo, Namespace: "dlh-test-fw"}}
	h := &Handlers{deps: deps}
	resp, err := h.CreateSchedule(context.Background(), gen.CreateScheduleRequestObject{
		Body: &gen.CreateScheduleRequest{
			Id:         "nightly",
			ScenarioId: "mysql-pod-delete",
			Cron:       "0 2 * * *",
		},
	})
	if err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}
	out, ok := resp.(gen.CreateSchedule201JSONResponse)
	if !ok {
		t.Fatalf("response type: %T", resp)
	}
	if out.Id != "nightly" {
		t.Errorf("id: %q", out.Id)
	}
}

func TestCreateSchedule_404OnUnknownScenario(t *testing.T) {
	argo := wfake.NewSimpleClientset()
	deps := &Deps{Schedules: &schedules.Manager{Argo: argo, Namespace: "dlh-test-fw"}}
	h := &Handlers{deps: deps}
	resp, err := h.CreateSchedule(context.Background(), gen.CreateScheduleRequestObject{
		Body: &gen.CreateScheduleRequest{Id: "x", ScenarioId: "nope", Cron: "0 * * * *"},
	})
	if err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}
	if _, ok := resp.(gen.CreateSchedule404Response); !ok {
		t.Errorf("expected 404, got %T", resp)
	}
}

func TestPauseSchedule_404OnUnknown(t *testing.T) {
	deps := &Deps{Schedules: &schedules.Manager{Argo: wfake.NewSimpleClientset(), Namespace: "dlh-test-fw"}}
	h := &Handlers{deps: deps}
	resp, _ := h.PauseSchedule(context.Background(), gen.PauseScheduleRequestObject{Id: "nope"})
	if _, ok := resp.(gen.PauseSchedule404Response); !ok {
		t.Errorf("expected 404, got %T", resp)
	}
}
```

- [ ] **Step 5: Wire Manager into main.go.**

In `cmd/dlh-controlplane/main.go`, find where the existing managers (Submitter, ManifestWriter, etc.) are constructed. Append:

```go
scheduleMgr := &schedules.Manager{Argo: clients.Argo, Namespace: cfg.K8sNamespace}
```

Then add to the Deps literal:

```go
Schedules: scheduleMgr,
```

Add the import `"github.com/dlh/dlh-test-fw/controlplane/internal/schedules"`.

- [ ] **Step 6: Build + test.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan19/controlplane
go mod tidy
go build ./...
go test ./...
```

Expected: clean build; new handler tests pass; all other tests still pass.

If the codegen-emitted `gen.CreateScheduleRequest.ScenarioId` is actually `ScenarioID` or similar, the compiler will tell you — adjust the assignment in schedules.go.

- [ ] **Step 7: Commit:**

```bash
git add controlplane/internal/api/schedules.go controlplane/internal/api/schedules_test.go \
        controlplane/internal/api/handlers.go controlplane/internal/api/server.go \
        controlplane/cmd/dlh-controlplane/main.go controlplane/go.sum
git commit -m "feat(controlplane): wire schedules.Manager + /api/schedules handlers

scheduleDTO maps Argo CronWorkflow → OpenAPI Schedule (surfaces
lastScheduledTime + Active/Succeeded/Failed counts). Errors map to
404 / 409 / 400. Handler tests cover happy path + 404 on unknown
scenario + 404 on missing pause target."
```

---

## Task 7: Extend Role for CronWorkflow access

**Files:**
- Modify: `controlplane/deploy/role.yaml`

- [ ] **Step 1: Read current Role.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan19
cat controlplane/deploy/role.yaml
```

- [ ] **Step 2: Update the `argoproj.io/workflows` rule to also include `cronworkflows`.** Find the existing block:

```yaml
  - apiGroups: ["argoproj.io"]
    resources: ["workflows"]
    verbs: ["get", "list", "watch", "create", "patch", "delete"]
```

Replace with:

```yaml
  - apiGroups: ["argoproj.io"]
    resources: ["workflows", "cronworkflows"]
    verbs: ["get", "list", "watch", "create", "patch", "delete"]
```

(Use Edit; the resources list expansion is the only change.)

- [ ] **Step 3: Validate.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan19
kubeconform -skip CustomResourceDefinition -strict -summary \
  -schema-location default \
  -schema-location 'https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json' \
  controlplane/deploy/*.yaml 2>&1 | tail -3
```

Expected: Invalid=0.

- [ ] **Step 4: Commit:**

```bash
git add controlplane/deploy/role.yaml
git commit -m "feat(controlplane): Role gets cronworkflows access alongside workflows

Same verbs (get/list/watch/create/patch/delete) so schedules.Manager
can manage CronWorkflow CRs in the framework namespace."
```

**Section A complete.** Backend can CRUD schedules; CronWorkflow integration ready.

---

# Section B — dlh schedule CLI subcommands (Tasks 8-11)

## Task 8: schedule create + list + show

**Files:**
- Create: `controlplane/cmd/dlh/schedule.go`
- Modify: `controlplane/cmd/dlh/root.go`

- [ ] **Step 1: Write `cmd/dlh/schedule.go`** with parent + first three subcommands:

```go
package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func scheduleCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "schedule",
		Short: "Manage CronWorkflow-backed schedules",
	}
	c.AddCommand(scheduleCreateCmd(), scheduleLsCmd(), scheduleShowCmd(),
		schedulePauseCmd(), scheduleResumeCmd(), scheduleDeleteCmd())
	return c
}

func scheduleCreateCmd() *cobra.Command {
	var (
		scenario   string
		target     string
		cron       string
		timezone   string
		paramFlags []string
	)
	c := &cobra.Command{
		Use:   "create <id>",
		Short: "Create a new schedule",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if scenario == "" || cron == "" {
				return fmt.Errorf("--scenario and --cron are required")
			}
			params := map[string]string{}
			for _, p := range paramFlags {
				k, v, ok := strings.Cut(p, "=")
				if !ok {
					return fmt.Errorf("--param expects key=value, got %q", p)
				}
				params[k] = v
			}
			body := map[string]any{
				"id":         args[0],
				"scenarioId": scenario,
				"cron":       cron,
			}
			if target != "" {
				body["targetId"] = target
			}
			if timezone != "" {
				body["timezone"] = timezone
			}
			if len(params) > 0 {
				body["parameters"] = params
			}
			raw, _, err := newClient().do("POST", "/api/schedules", body, nil)
			if err != nil {
				return err
			}
			var pretty interface{}
			_ = json.Unmarshal(raw, &pretty)
			out, _ := json.MarshalIndent(pretty, "", "  ")
			fmt.Println(string(out))
			return nil
		},
	}
	c.Flags().StringVar(&scenario, "scenario", "", "Scenario WorkflowTemplate name (required)")
	c.Flags().StringVar(&target, "target", "", "Optional remote target ID")
	c.Flags().StringVar(&cron, "cron", "", "5-field cron expression (required)")
	c.Flags().StringVar(&timezone, "timezone", "", "IANA tz name (default UTC)")
	c.Flags().StringArrayVarP(&paramFlags, "param", "p", nil, "Parameter override key=value (repeatable)")
	return c
}

func scheduleLsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List schedules",
		RunE: func(_ *cobra.Command, _ []string) error {
			raw, _, err := newClient().do("GET", "/api/schedules", nil, nil)
			if err != nil {
				return err
			}
			var resp struct {
				Items []map[string]any `json:"items"`
			}
			if err := json.Unmarshal(raw, &resp); err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tSCENARIO\tTARGET\tCRON\tSUSPENDED\tLAST FIRED\tACTIVE")
			for _, r := range resp.Items {
				lastFired := "—"
				if t, ok := r["lastScheduledAt"].(string); ok && t != "" {
					lastFired = t
				}
				suspended := "false"
				if v, ok := r["suspended"].(bool); ok && v {
					suspended = "true"
				}
				active := "0"
				if v, ok := r["activeCount"].(float64); ok {
					active = fmt.Sprintf("%v", int(v))
				}
				target := "—"
				if v, ok := r["target"].(string); ok && v != "" {
					target = v
				}
				fmt.Fprintf(tw, "%v\t%v\t%v\t%v\t%v\t%v\t%v\n",
					r["id"], r["scenario"], target, r["cron"], suspended, lastFired, active)
			}
			return tw.Flush()
		},
	}
}

func scheduleShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show schedule detail",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			raw, _, err := newClient().do("GET", "/api/schedules/"+url.PathEscape(args[0]), nil, nil)
			if err != nil {
				return err
			}
			var pretty interface{}
			_ = json.Unmarshal(raw, &pretty)
			out, _ := json.MarshalIndent(pretty, "", "  ")
			fmt.Println(string(out))
			return nil
		},
	}
}

// Stubs — implemented in Task 9.
func schedulePauseCmd() *cobra.Command  { return &cobra.Command{Use: "pause", Hidden: true} }
func scheduleResumeCmd() *cobra.Command { return &cobra.Command{Use: "resume", Hidden: true} }
func scheduleDeleteCmd() *cobra.Command { return &cobra.Command{Use: "delete", Hidden: true} }
```

- [ ] **Step 2: Register `scheduleCmd()` in `cmd/dlh/root.go`.**

Find:
```go
root.AddCommand(runCmd(), runsCmd(), loginCmd())
```

Replace with:
```go
root.AddCommand(runCmd(), runsCmd(), loginCmd(), scheduleCmd())
```

- [ ] **Step 3: Build + smoke.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan19/controlplane
make cli
./bin/dlh schedule --help
./bin/dlh schedule create --help
./bin/dlh schedule ls --help
./bin/dlh schedule show --help
```

Expected: parent + 3 subcommands documented in help output.

- [ ] **Step 4: Commit.**

```bash
git add controlplane/cmd/dlh/schedule.go controlplane/cmd/dlh/root.go
git commit -m "feat(controlplane/cli): dlh schedule create/ls/show

POSTs to /api/schedules with id + scenario + cron + optional target /
timezone / params. ls renders a tabwriter with last-fired + active.
show pretty-prints the JSON detail."
```

---

## Task 9: schedule pause / resume / delete

**Files:**
- Modify: `controlplane/cmd/dlh/schedule.go`

- [ ] **Step 1: Replace the 3 stubs at the bottom of schedule.go with real implementations:**

```go
func schedulePauseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pause <id>",
		Short: "Pause a schedule (sets spec.suspend=true)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			_, _, err := newClient().do("POST", "/api/schedules/"+url.PathEscape(args[0])+"/pause", nil, nil)
			if err != nil {
				return err
			}
			fmt.Println("paused")
			return nil
		},
	}
}

func scheduleResumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resume <id>",
		Short: "Resume a paused schedule (sets spec.suspend=false)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			_, _, err := newClient().do("POST", "/api/schedules/"+url.PathEscape(args[0])+"/resume", nil, nil)
			if err != nil {
				return err
			}
			fmt.Println("resumed")
			return nil
		},
	}
}

func scheduleDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a schedule",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			_, _, err := newClient().do("DELETE", "/api/schedules/"+url.PathEscape(args[0]), nil, nil)
			if err != nil {
				return err
			}
			fmt.Println("deleted")
			return nil
		},
	}
}
```

(Use Edit to replace the three stub functions.)

- [ ] **Step 2: Build + smoke.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan19/controlplane
make cli
./bin/dlh schedule --help
./bin/dlh schedule pause --help
./bin/dlh schedule resume --help
./bin/dlh schedule delete --help
```

Expected: all 6 subcommands visible + help screens.

- [ ] **Step 3: Commit.**

```bash
git add controlplane/cmd/dlh/schedule.go
git commit -m "feat(controlplane/cli): dlh schedule pause / resume / delete"
```

---

## Task 10: TestSchedule_RunFromCronWorkflow_TargetLabel — confirms scheduled runs carry dlh.target

**Files:**
- Modify: `controlplane/internal/schedules/manager_test.go`

The existing Task 2's `TestCreate_WithTarget` confirms the CronWorkflow's `workflowMetadata.labels` carry `dlh.target`. This task adds a stronger assertion: simulating a Workflow stamped by the Argo CronController inherits both labels, so the Plan 17 Syncer's `dlh.target` propagation continues to work for scheduled runs.

- [ ] **Step 1: Append test.**

```go
func TestCreate_WorkflowMetadataPropagatesToChildWorkflows(t *testing.T) {
	// We can't easily simulate the Argo CronController here. Instead,
	// verify that the CronWorkflow's spec.workflowMetadata.labels
	// includes everything the Syncer needs — dlh.scenario AND dlh.target.
	tmpl := &wfv1.WorkflowTemplate{ObjectMeta: metav1.ObjectMeta{Name: "mysql-pod-delete", Namespace: "dlh-test-fw"}}
	argo := wfake.NewSimpleClientset(tmpl)
	m := &Manager{Argo: argo, Namespace: "dlh-test-fw"}
	got, err := m.Create(context.Background(), CreateRequest{
		Name: "nightly-staging", ScenarioID: "mysql-pod-delete",
		TargetID: "staging-mysql", Cron: "0 2 * * *",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.Spec.WorkflowMetadata == nil {
		t.Fatal("workflowMetadata not set")
	}
	wantLabels := map[string]string{
		"dlh.scenario": "mysql-pod-delete",
		"dlh.target":   "staging-mysql",
	}
	for k, v := range wantLabels {
		if got.Spec.WorkflowMetadata.Labels[k] != v {
			t.Errorf("workflowMetadata.label[%s]: got %q, want %q",
				k, got.Spec.WorkflowMetadata.Labels[k], v)
		}
	}
}
```

- [ ] **Step 2: Build + test:**

```bash
cd /Users/allen/repo/dlh-test-fw-plan19/controlplane
go test ./internal/schedules/... -v
```

Expected: 12 tests pass.

- [ ] **Step 3: Commit.**

```bash
git add controlplane/internal/schedules/manager_test.go
git commit -m "test(controlplane/schedules): CronWorkflow workflowMetadata propagates Syncer labels

Locks the contract: a Schedule's child Workflow inherits both dlh.scenario
and dlh.target via spec.workflowMetadata.labels. Plan 17 Syncer reads
these — without them, scheduled runs would lose the target chain."
```

---

## Task 11: CLI smoke

**Files:** None. Verification only.

- [ ] **Step 1: Rebuild CLI + smoke help.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan19/controlplane
make cli
./bin/dlh --help | head -15
./bin/dlh schedule --help
```

Expected: `schedule` parent in the main `--help`; 6 subcommands listed under `schedule --help` (create / ls / show / pause / resume / delete).

No commit. Gate before Section C.

**Section B complete.** CLI is feature-complete for schedules.

---

# Section C — UI Schedules page + Run detail linkage (Tasks 12-14)

## Task 12: Schedules page (read + pause/resume + delete + inline create)

**Files:**
- Create: `controlplane/web/src/pages/SchedulesPage.tsx`
- Modify: `controlplane/web/src/App.tsx`

- [ ] **Step 1: Write `controlplane/web/src/pages/SchedulesPage.tsx`:**

```tsx
import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/gen";

type Schedule = components["schemas"]["Schedule"];

export function SchedulesPage() {
  const [items, setItems] = useState<Schedule[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);

  // Inline-create form state.
  const [createOpen, setCreateOpen] = useState(false);
  const [newId, setNewId] = useState("");
  const [newScenario, setNewScenario] = useState("");
  const [newTarget, setNewTarget] = useState("");
  const [newCron, setNewCron] = useState("");
  const [newTimezone, setNewTimezone] = useState("");

  const reload = () =>
    api.GET("/api/schedules", {}).then(({ data, error }) => {
      if (error) setError(JSON.stringify(error));
      else setItems(data?.items ?? []);
    });

  useEffect(() => {
    reload();
  }, []);

  const doPause = async (id: string) => {
    setBusy(id);
    try {
      await api.POST("/api/schedules/{id}/pause", { params: { path: { id } } });
      await reload();
    } finally {
      setBusy(null);
    }
  };
  const doResume = async (id: string) => {
    setBusy(id);
    try {
      await api.POST("/api/schedules/{id}/resume", { params: { path: { id } } });
      await reload();
    } finally {
      setBusy(null);
    }
  };
  const doDelete = async (id: string) => {
    if (!confirm(`Delete schedule "${id}"?`)) return;
    setBusy(id);
    try {
      await api.DELETE("/api/schedules/{id}", { params: { path: { id } } });
      await reload();
    } finally {
      setBusy(null);
    }
  };

  const doCreate = async () => {
    if (!newId || !newScenario || !newCron) {
      alert("id, scenario, cron required");
      return;
    }
    setBusy("__create__");
    try {
      const body: any = { id: newId, scenarioId: newScenario, cron: newCron };
      if (newTarget) body.targetId = newTarget;
      if (newTimezone) body.timezone = newTimezone;
      const { error } = await api.POST("/api/schedules", { body });
      if (error) {
        alert("Failed: " + JSON.stringify(error));
        return;
      }
      setNewId("");
      setNewScenario("");
      setNewTarget("");
      setNewCron("");
      setNewTimezone("");
      setCreateOpen(false);
      await reload();
    } finally {
      setBusy(null);
    }
  };

  if (error) return <p className="text-rose-700">Error: {error}</p>;
  if (!items) return <p>Loading…</p>;
  return (
    <section className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold">Schedules</h1>
        <button
          onClick={() => setCreateOpen(!createOpen)}
          className="rounded bg-emerald-600 px-3 py-1 text-xs font-medium text-white hover:bg-emerald-700"
        >
          {createOpen ? "Cancel" : "+ New schedule"}
        </button>
      </div>

      {createOpen && (
        <div className="rounded border border-slate-200 bg-slate-50 p-3 text-sm">
          <div className="grid grid-cols-2 gap-2 md:grid-cols-3">
            <input
              placeholder="id (e.g. nightly-mysql)"
              value={newId}
              onChange={(e) => setNewId(e.target.value)}
              className="rounded border border-slate-300 bg-white px-2 py-1"
            />
            <input
              placeholder="scenario (e.g. mysql-pod-delete)"
              value={newScenario}
              onChange={(e) => setNewScenario(e.target.value)}
              className="rounded border border-slate-300 bg-white px-2 py-1"
            />
            <input
              placeholder="target (optional)"
              value={newTarget}
              onChange={(e) => setNewTarget(e.target.value)}
              className="rounded border border-slate-300 bg-white px-2 py-1"
            />
            <input
              placeholder="cron (e.g. 0 2 * * *)"
              value={newCron}
              onChange={(e) => setNewCron(e.target.value)}
              className="rounded border border-slate-300 bg-white px-2 py-1"
            />
            <input
              placeholder="timezone (e.g. Asia/Tokyo)"
              value={newTimezone}
              onChange={(e) => setNewTimezone(e.target.value)}
              className="rounded border border-slate-300 bg-white px-2 py-1"
            />
            <button
              onClick={doCreate}
              disabled={busy === "__create__"}
              className="rounded bg-emerald-600 px-3 py-1 text-xs font-medium text-white hover:bg-emerald-700"
            >
              {busy === "__create__" ? "creating…" : "Create"}
            </button>
          </div>
        </div>
      )}

      {items.length === 0 ? (
        <p className="text-slate-600">No schedules yet. Click "+ New schedule".</p>
      ) : (
        <table className="w-full border-collapse text-sm">
          <thead>
            <tr className="border-b border-slate-200 text-left text-slate-600">
              <th className="py-2">ID</th>
              <th>Scenario</th>
              <th>Target</th>
              <th>Cron</th>
              <th>Last Fired</th>
              <th>Active</th>
              <th>Status</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {items.map((s) => (
              <tr key={s.id} className="border-b border-slate-100">
                <td className="py-2 font-mono text-xs">{s.id}</td>
                <td>{s.scenario}</td>
                <td>{s.target ?? "local"}</td>
                <td className="font-mono text-xs">{s.cron}</td>
                <td className="text-xs text-slate-600">
                  {s.lastScheduledAt ? new Date(s.lastScheduledAt).toLocaleString() : "—"}
                </td>
                <td>{s.activeCount ?? 0}</td>
                <td>
                  {s.suspended ? (
                    <span className="text-amber-700">paused</span>
                  ) : (
                    <span className="text-emerald-700">active</span>
                  )}
                </td>
                <td className="space-x-1">
                  {s.suspended ? (
                    <button
                      onClick={() => doResume(s.id)}
                      disabled={busy === s.id}
                      className="rounded border border-slate-300 px-2 py-0.5 text-xs hover:bg-slate-100"
                    >
                      resume
                    </button>
                  ) : (
                    <button
                      onClick={() => doPause(s.id)}
                      disabled={busy === s.id}
                      className="rounded border border-slate-300 px-2 py-0.5 text-xs hover:bg-slate-100"
                    >
                      pause
                    </button>
                  )}
                  <button
                    onClick={() => doDelete(s.id)}
                    disabled={busy === s.id}
                    className="rounded border border-rose-300 px-2 py-0.5 text-xs text-rose-700 hover:bg-rose-50"
                  >
                    delete
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}
```

- [ ] **Step 2: Wire `/schedules` route + nav link in `App.tsx`.**

Read current `App.tsx`:

```bash
cat controlplane/web/src/App.tsx
```

Add import:
```tsx
import { SchedulesPage } from "./pages/SchedulesPage";
```

Add a nav link next to existing entries: `<Link to="/schedules">Schedules</Link>`.

Add a route: `<Route path="/schedules" element={<SchedulesPage />} />`.

Use Edit on unique anchor lines (e.g., the existing Targets nav link, or the existing Targets route).

- [ ] **Step 3: Regenerate TS types + build UI.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan19/controlplane/web
pnpm openapi-typescript ../api/openapi.yaml -o src/api/gen.ts
pnpm build 2>&1 | tail -5
```

Expected: clean build.

- [ ] **Step 4: Commit.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan19
git add controlplane/web/src/pages/SchedulesPage.tsx controlplane/web/src/App.tsx
git commit -m "feat(controlplane/web): Schedules page

Read-only list with last-fired + active counts; inline pause/resume +
delete buttons; collapsible '+ New schedule' form."
```

---

## Task 13: Run detail surfaces the parent CronWorkflow when present

**Files:**
- Modify: `controlplane/web/src/pages/RunDetailPage.tsx`

The existing Workflow informer already returns Workflows with `ownerReferences` populated. The current Run detail doesn't surface them. Phase F adds: if the run's parent is a CronWorkflow, show a "Triggered by schedule: <name>" link to `/schedules`.

The API doesn't currently expose `ownerReferences` on the Run / RunDetail DTO. Adding a `triggeredBy` field is the cleanest path.

- [ ] **Step 1: Extend the `Run` schema in openapi.yaml** with an optional `triggeredBy` field.

This needs an OpenAPI change. Find the existing `Run` schema:

```bash
cd /Users/allen/repo/dlh-test-fw-plan19/controlplane
grep -A 12 "    Run:" api/openapi.yaml | head -15
```

Use Edit to add a property to `Run` (the additional field flows automatically into `RunDetail` via allOf, or whatever inheritance pattern codegen uses — for this codebase it's flat, so add to both).

Add to Run's properties (find a unique anchor — probably the `workflowName` line — and append):

```yaml
        triggeredBy:
          type: object
          description: "Set when the run was fired by a Schedule (CronWorkflow)."
          properties:
            kind: { type: string, example: "Schedule" }
            id:   { type: string, description: "Schedule id (CronWorkflow name)" }
```

Repeat the same field addition in `RunDetail`'s properties block.

- [ ] **Step 2: Regenerate.**

```bash
go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -config api/oapi-codegen-server.yaml api/openapi.yaml
go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -config api/oapi-codegen-types.yaml api/openapi.yaml
cd web && pnpm openapi-typescript ../api/openapi.yaml -o src/api/gen.ts && cd ..
```

- [ ] **Step 3: Populate `triggeredBy` in the model converter** at `internal/model/types.go`.

Read the existing model:

```bash
grep -A 30 "func RunFromWorkflow" internal/model/types.go
```

Add a block that inspects `wf.OwnerReferences`:

```go
for _, owner := range wf.OwnerReferences {
	if owner.Kind == "CronWorkflow" {
		kind := "Schedule"
		idVal := owner.Name
		r.TriggeredBy = &struct {
			Id   *string `json:"id,omitempty"`
			Kind *string `json:"kind,omitempty"`
		}{Id: &idVal, Kind: &kind}
		break
	}
}
```

(The exact struct shape comes from codegen — adjust based on whatever `gen.Run.TriggeredBy` was emitted as. Use Edit to insert after the existing target-population block.)

Do the same in `RunDetailFromWorkflow` (or wherever RunDetail is built).

- [ ] **Step 4: Surface in `RunDetailPage.tsx`.**

Read current file:

```bash
cat controlplane/web/src/pages/RunDetailPage.tsx
```

Find the header section (near the run id + status badge). Add:

```tsx
{run.triggeredBy?.id && (
  <a
    href={`/schedules`}
    className="text-xs text-blue-700 hover:underline"
  >
    Triggered by schedule: {run.triggeredBy.id}
  </a>
)}
```

(Adjust per the existing JSX shape.)

- [ ] **Step 5: Build + smoke.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan19/controlplane
go build ./...
go test ./...
cd web && pnpm build 2>&1 | tail -5
```

Expected: clean Go + TS build.

- [ ] **Step 6: Commit.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan19
git add controlplane/api/openapi.yaml controlplane/internal/api/gen/ \
        controlplane/internal/model/types.go controlplane/web/src/api/gen.ts \
        controlplane/web/src/pages/RunDetailPage.tsx
git commit -m "feat(controlplane): Run.triggeredBy surfaces parent CronWorkflow

When a Workflow has ownerReferences pointing at a CronWorkflow, the
Run DTO carries triggeredBy.{kind, id}. UI Run-detail shows a link
back to /schedules so users can navigate from a firing to its schedule."
```

---

## Task 14: TS gen + UI smoke

**Files:** None. Verification only.

- [ ] **Step 1: Rebuild UI + spot-check.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan19/controlplane/web
pnpm build 2>&1 | tail -5
```

Expected: clean build; reasonable bundle size (under 200KB after gzip).

- [ ] **Step 2: Verify the chart deploy path is healthy:**

```bash
cd /Users/allen/repo/dlh-test-fw-plan19
helm template dlh helm/dlh-test-fw > /tmp/rendered-plan19.yaml
grep -c "argoproj.io" controlplane/deploy/role.yaml
```

Expected: at least 3 (workflows + workflowtemplates + cronworkflows).

No commit. Section C complete.

---

# Section D — Docs + smoke + merge (Tasks 15-18)

## Task 15: FINDINGS + CLAUDE + README

**Files:**
- Modify: `docs/FINDINGS.md`
- Modify: `CLAUDE.md`
- Modify: `README.md`

- [ ] **Step 1: Append Plan 19 to docs/FINDINGS.md.** Find the last line + append after it:

```markdown

---

## Plan 19 — controlplane Phase F (Schedules) (2026-05-24)

### What landed

- `controlplane/internal/schedules/` — `Manager` wraps Argo's `CronWorkflow`: Create / List / Get / Delete / Pause / Resume.
- `POST /api/schedules`, `GET /api/schedules`, `GET /api/schedules/{id}`, `DELETE /api/schedules/{id}`, `POST /api/schedules/{id}/pause`, `POST /api/schedules/{id}/resume`.
- `dlh schedule create / ls / show / pause / resume / delete`.
- UI Schedules page with inline create form + pause/resume/delete actions.
- Run detail surfaces `triggeredBy.{kind, id}` when the Run has a CronWorkflow owner reference; UI shows a "Triggered by schedule" link.
- Role extended: cronworkflows alongside workflows verbs.

### Operational pitfalls discovered

1. **`workflowMetadata.labels` is the only way to propagate labels to fired Workflows.** `CronWorkflow.metadata.labels` apply only to the CronWorkflow itself, not the Workflows it spawns. The Manager sets `dlh.scenario` + `dlh.target` in BOTH places — the cron-level labels make the Schedule queryable; the workflow-level labels keep Plan 17's Syncer working for scheduled runs.

2. **CronWorkflow pause is `spec.suspend`, not a status field.** Argo's argocli walks the same merge-patch pattern; the controlplane mirrors it. Idempotent — patching `suspend=true` on an already-paused schedule is a no-op.

3. **Argo CronWorkflow does not enforce single-cron-vs-schedules mutual exclusion.** If both `spec.schedule` (singular) AND `spec.schedules` (plural) are set, Argo prefers `schedules`. The Manager only writes `spec.schedule`; the older field is universally supported and v1 only needs single-cron.

4. **`OwnerReferences` on Workflows fired by CronWorkflows includes a single entry with `Kind: CronWorkflow`.** The model converter walks the list and stops at the first match — there's no nested-owner case in practice for Argo's cron-fired Workflows.

5. **OpenAPI `Run.triggeredBy` is a nested anonymous object** in our codegen output. If we wanted a named type (`TriggeredBy`), we'd need a top-level schema entry. v1 keeps it inline; future plans can promote it.

6. **Schedules + Argo TTL.** CronWorkflows fire and Workflows go into the Argo TTL pool (existing behavior). Plan 15's Phase B GetRun manifest fallback already handles TTL'd workflows for scheduled runs — no new work required.

### Carry-forward for future plans

- Schedule edit endpoint (PATCH /api/schedules/{id}) — v1 ships create-only; users must delete-then-create to change a schedule.
- Multi-cron (`spec.schedules: []string`) support for "every weekday at 2am AND noon" patterns.
- Schedule run history view in the UI — for now, navigate to `/runs?scenario=<scenario>` and filter visually.
- Stop strategy (`spec.stopStrategy`) — Argo 3.6's "stop scheduling after N successes" pattern is unused but supported by the underlying CRD.
```

- [ ] **Step 2: Add Phase F subsection to CLAUDE.md.**

Find the existing `## dlh-controlplane (Phase B onwards)` section + its `### Phase E additions (Plan 18)` subsection. Append after that subsection (before the next `## ` heading):

```markdown

### Phase F additions (Plan 19)

- Schedules (Argo `CronWorkflow`) are first-class resources via the
  controlplane:
  - `POST /api/schedules` + `GET /api/schedules{,/<id>}` + `DELETE` +
    `POST /api/schedules/<id>/{pause,resume}`.
  - `dlh schedule create / ls / show / pause / resume / delete`.
  - UI Schedules page with inline create form.
- A scheduled run carries `dlh.scenario` + `dlh.target` labels via
  `spec.workflowMetadata.labels` on the CronWorkflow — Plan 17 Syncer
  picks it up automatically; no submitter changes needed.
- Run detail surfaces `triggeredBy.{kind, id}` when the firing
  Workflow has a CronWorkflow owner reference; UI links to /schedules.
- Role extended to grant cronworkflows alongside workflows verbs.
```

- [ ] **Step 3: Add Plan 19 row to README.md.**

Find the Plan 18 row + insert after:

```markdown
| Plan 19 | `XXXXXXX` | dlh-controlplane Phase F (Schedules) — POST/GET/DELETE /api/schedules + pause/resume; dlh schedule CLI; UI Schedules page; Run.triggeredBy surfaces parent CronWorkflow |
```

- [ ] **Step 4: Commit.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan19
git add docs/FINDINGS.md CLAUDE.md README.md
git commit -m "docs: Plan 19 — Phase F FINDINGS + CLAUDE.md + README"
```

---

## Task 16: Final verification gate

**Files:** None. Verification only.

- [ ] **Step 1: Go vet + go test.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan19/controlplane
go vet ./...
go test ./...
```

Expected: clean; all tests pass (Phase E total + schedules tests + new api tests).

- [ ] **Step 2: UI build.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan19/controlplane/web
pnpm openapi-typescript ../api/openapi.yaml -o src/api/gen.ts
pnpm build 2>&1 | tail -5
```

Expected: clean.

- [ ] **Step 3: CLI build.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan19/controlplane
make cli
./bin/dlh --help | head -15
./bin/dlh schedule --help
```

Expected: all subcommands visible.

- [ ] **Step 4: Chart + kubeconform.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan19
helm dependency update helm/dlh-test-fw 2>&1 | tail -3
helm lint helm/dlh-test-fw 2>&1 | tail -3
helm template dlh helm/dlh-test-fw > /tmp/rendered-plan19-final.yaml
grep -c "cronworkflows" controlplane/deploy/role.yaml
kubeconform -skip CustomResourceDefinition -strict -summary \
  -schema-location default \
  -schema-location 'https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json' \
  /tmp/rendered-plan19-final.yaml controlplane/deploy/role.yaml controlplane/deploy/service.yaml controlplane/deploy/serviceaccount.yaml controlplane/deploy/deployment.yaml controlplane/deploy/rolebinding.yaml controlplane/deploy/ingress.yaml controlplane/deploy/roles-configmap.yaml 2>&1 | tail -3
```

Expected: lint passes; cronworkflows in role.yaml; kubeconform Invalid=0.

No commit. Gate before push.

---

## Task 17: Smoke against minikube

The smoke verifies a real CronWorkflow gets created + fires + the Syncer captures the firing as a Run.

**Files:** None modified normally. Fix commits land if issues surface.

- [ ] **Step 1: Confirm cluster + chart state.**

```bash
minikube status
kubectl -n dlh-test-fw get pods | head -10
```

Expected: cluster Ready; argo + chaos-mesh + vm + grafana + minio pods Running.

- [ ] **Step 2: helm upgrade so the Role gains cronworkflows + chart includes the new WTs (if any chart changes).**

```bash
cd /Users/allen/repo/dlh-test-fw-plan19
helm upgrade --install dlh helm/dlh-test-fw \
  -f helm/dlh-test-fw/values.yaml \
  -f helm/dlh-test-fw/values-minikube.yaml \
  --namespace dlh-test-fw \
  --wait --timeout 5m 2>&1 | tail -10
```

- [ ] **Step 3: Build + reload controlplane image.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan19/controlplane
make reload-minikube 2>&1 | tail -5
make cli 2>&1 | tail -3
```

- [ ] **Step 4: Apply the controlplane manifests with local-smoke patch.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan19
kubectl --context minikube -n dlh-test-fw apply -f controlplane/deploy/role.yaml
kubectl --context minikube -n dlh-test-fw apply -f controlplane/deploy/deployment.yaml
kubectl --context minikube -n dlh-test-fw apply -f controlplane/deploy/service.yaml || true
kubectl --context minikube -n dlh-test-fw patch deployment dlh-controlplane --type=json -p='[
  {"op": "add", "path": "/spec/template/spec/containers/0/env/-", "value": {"name": "DLH_AUTH_DISABLED", "value": "true"}}
]' 2>&1 | tail -3
kubectl --context minikube -n dlh-test-fw rollout status deployment/dlh-controlplane --timeout=120s
```

- [ ] **Step 5: Port-forward + CLI smoke.**

```bash
kubectl --context minikube -n dlh-test-fw port-forward svc/dlh-controlplane 18080:80 >/dev/null 2>&1 &
PF=$!
trap "kill $PF 2>/dev/null || true" EXIT
sleep 2
export DLH_ENDPOINT=http://localhost:18080
export DLH_TOKEN='fake:tester:tester@example.com:dlh-admins'

# Create a schedule that fires every minute (so we can observe one firing in <2 min).
./controlplane/bin/dlh schedule create smoke-every-minute \
  --scenario mysql-pod-delete --cron '* * * * *' --param vus=2 --param load_duration=20s --param chaos_duration=10s
./controlplane/bin/dlh schedule ls
```

Expected: schedule appears in the list with cron `* * * * *`.

- [ ] **Step 6: Wait ~70s + observe a firing Workflow.**

```bash
sleep 75
kubectl --context minikube -n dlh-test-fw get cronworkflow smoke-every-minute -o jsonpath='{.status.lastScheduledTime}'
echo
./controlplane/bin/dlh runs ls --scenario mysql-pod-delete --limit 5
kubectl --context minikube -n dlh-test-fw get workflow -l dlh.scenario=mysql-pod-delete | tail -5
```

Expected: status.lastScheduledTime populated; runs ls shows a recent run; kubectl confirms a Workflow with `dlh.scenario=mysql-pod-delete` label.

- [ ] **Step 7: Pause + verify no further firings.**

```bash
./controlplane/bin/dlh schedule pause smoke-every-minute
./controlplane/bin/dlh schedule show smoke-every-minute
# Wait 70s — should NOT see a new firing.
sleep 75
LATEST_AFTER_PAUSE=$(kubectl --context minikube -n dlh-test-fw get cronworkflow smoke-every-minute -o jsonpath='{.status.lastScheduledTime}')
echo "lastScheduledTime: $LATEST_AFTER_PAUSE"
# Compare against the firing count or timestamp before pause.
```

Expected: lastScheduledTime did NOT advance after the pause.

- [ ] **Step 8: Resume + cleanup.**

```bash
./controlplane/bin/dlh schedule resume smoke-every-minute
sleep 70
./controlplane/bin/dlh schedule delete smoke-every-minute
./controlplane/bin/dlh schedule ls
```

Expected: schedule removed; subsequent ls is empty (or shows other schedules).

- [ ] **Step 9: Fix any bugs as individual commits** (`fix(controlplane/schedules): <one-line>`).

- [ ] **Step 10: Teardown patches.**

```bash
kill $PF 2>/dev/null || true
kubectl --context minikube -n dlh-test-fw delete deployment dlh-controlplane 2>&1 | tail -1
```

(Chart-managed resources stay; only the local-smoke deployment is torn down.)

---

## Task 18: Push, watch CI, merge

**Files:** Backfill README hash only.

- [ ] **Step 1: Push feature branch.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan19
git push -u origin feat/plan19-controlplane-schedules 2>&1 | tail -5
```

Open a draft PR to trigger CI (the workflow only fires on PR + push-to-main):

```bash
gh pr create --draft \
  --title "Plan 19: controlplane Phase F (Schedules)" \
  --body "Subagent-driven implementation. Merged via local --no-ff once CI is green." \
  --base main \
  --head feat/plan19-controlplane-schedules 2>&1 | tail -3

sleep 10
RUN_ID=$(gh run list --branch feat/plan19-controlplane-schedules --limit 1 --json databaseId -q '.[0].databaseId')
echo "Watching CI run $RUN_ID"
gh run watch "$RUN_ID" --interval 30 || true
gh run view "$RUN_ID" --json conclusion -q .conclusion
```

Expected: `success`. If CI fails, fix on the feature branch + commit + re-watch.

Close the draft PR (we merge locally):

```bash
gh pr close --comment "Merging via local --no-ff to preserve atomic per-task history" \
  $(gh pr list --head feat/plan19-controlplane-schedules --json number -q '.[0].number') 2>&1 | tail -2
```

- [ ] **Step 2: Merge to main with --no-ff.**

```bash
cd /Users/allen/repo/dlh-test-fw
git checkout main
git pull --ff-only origin main 2>&1 | tail -3
git merge --no-ff feat/plan19-controlplane-schedules -m "Merge feat/plan19-controlplane-schedules: Phase F (Schedules)

Wraps Argo CronWorkflow as a first-class Schedule resource. New
endpoints: POST/GET/DELETE /api/schedules + pause/resume. dlh CLI
gets a 'schedule' parent with 6 subcommands. UI Schedules page with
inline create + pause/resume + delete. Run.triggeredBy surfaces the
parent CronWorkflow on the Run detail page.

Plan 19 of dlh-test-fw. See:
- docs/superpowers/specs/2026-05-21-dlh-controlplane-design.md
- docs/superpowers/plans/2026-05-24-01-controlplane-schedules.md"
```

- [ ] **Step 3: Backfill README hash.**

```bash
cd /Users/allen/repo/dlh-test-fw
MERGE_HASH=$(git log --first-parent --format=%h -1)
sed -i "" "s|| Plan 19 | \`XXXXXXX\`|| Plan 19 | \`$MERGE_HASH\`|" README.md
grep "^| Plan 19 " README.md
git add README.md
git commit -m "docs(readme): backfill Plan 19 merge hash"
```

- [ ] **Step 4: Push main + verify.**

```bash
git push origin main 2>&1 | tail -5
git status
git rev-list --count origin/main..main
sleep 10
RUN_MAIN=$(gh run list --branch main --limit 1 --json databaseId -q '.[0].databaseId')
gh run watch "$RUN_MAIN" --interval 30 || true
gh run view "$RUN_MAIN" --json conclusion -q .conclusion
```

Expected: rev-list=0; CI success.

- [ ] **Step 5: Cleanup.**

```bash
cd /Users/allen/repo/dlh-test-fw
git worktree remove /Users/allen/repo/dlh-test-fw-plan19 2>&1 | tail -3
git branch -d feat/plan19-controlplane-schedules 2>&1 | tail -3
git push origin --delete feat/plan19-controlplane-schedules 2>&1 | tail -3
git worktree list
```

- [ ] **Step 6: Final state.**

```bash
git log --first-parent --oneline -5
ls controlplane/internal/schedules/
ls controlplane/cmd/dlh/
grep "^| Plan 19 " README.md
```

Expected: merge + backfill at the top; schedules/ package present; cmd/dlh/ includes schedule.go.

---

## Done

Plan 19 closes Phase F. The controlplane API surface is now complete per the spec — Scenarios, Runs, Targets, and Schedules all expose CRUD verbs through OpenAPI + CLI + UI. The framework is fully self-sufficient: no shell scripts, no kubectl, no argo CLI required for any operator workflow.
