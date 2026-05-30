# Scenario Template Filtering — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Only `dlh.category=scenario` WorkflowTemplates are listed as scenarios and runnable; building blocks (`chaos-*`/`fixture-*`/`util-*`/…) are filtered out and rejected on submit (400).

**Architecture:** A `model.IsScenarioTemplate` label helper gates `ListScenarios` and `GetScenarioPriorities`; the submitter rejects non-scenario templates with a new `runs.ErrNotScenario` sentinel that `CreateRun` maps to 400. No chart edits (the `dlh.category` labels already exist). Spec: `docs/superpowers/specs/2026-05-28-scenario-template-filtering-design.md`.

**Tech Stack:** Go, Argo `v1alpha1` types, `go test`. Commands from `controlplane/`.

**Conventions:** NEVER `git add -A`/globs. Commit trailer `Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>`. Tasks 1→2 are sequential (Task 2 references `ErrNotScenario` from Task 1).

---

## File Structure

- **Modify:** `internal/runs/submit.go` — add `ErrNotScenario` sentinel + category guard after the template Get; add `"errors"` import.
- **Modify:** `internal/runs/submit_test.go` — label existing scenario fixtures; add reject test; add `"errors"` import.
- **Modify:** `internal/model/types.go` — add `IsScenarioTemplate` helper.
- **Modify:** `internal/api/handlers.go` — filter `ListScenarios` + `GetScenarioPriorities`; map `ErrNotScenario`→400 in `CreateRun`.
- **Modify:** `internal/api/handlers_test.go` — rewrite `TestListScenarios` to assert filtering; label existing fixtures; add `TestGetScenarioPriorities_FiltersBuildingBlocks` + `TestCreateRun_400OnNonScenario`.

Unaffected (verified): schedule creation (`internal/schedules/manager.go`) only checks template existence, not category — out of scope; `schedules_test.go` needs no change. The `internal/queue` package is unrelated.

---

### Task 1: Submitter rejects non-scenario templates (TDD)

**Files:**
- Modify: `internal/runs/submit.go`
- Test: `internal/runs/submit_test.go`

- [ ] **Step 1: Add the failing reject test**

In `internal/runs/submit_test.go`, add `"errors"` to the import block (between `"context"` and `"strings"`):

```go
import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	wfake "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)
```

Append this test at the end of the file:

```go
func TestSubmit_RejectsNonScenario(t *testing.T) {
	ns := "dlh-test-fw"
	tmpl := &wfv1.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "chaos-kafka-broker-partition", Namespace: ns,
			Labels: map[string]string{"dlh.category": "chaos"}},
	}
	argo := wfake.NewSimpleClientset(tmpl)
	s := &Submitter{Argo: argo, Namespace: ns}
	_, err := s.Submit(context.Background(), SubmitRequest{ScenarioID: "chaos-kafka-broker-partition"})
	if !errors.Is(err, ErrNotScenario) {
		t.Fatalf("expected ErrNotScenario, got %v", err)
	}
}
```

- [ ] **Step 2: Run it — expect a compile failure**

Run: `cd controlplane && go test ./internal/runs/...`
Expected: FAIL to compile — `undefined: ErrNotScenario`.

- [ ] **Step 3: Add the sentinel + guard in `submit.go`**

Add `"errors"` to the import block (before `"fmt"`):

```go
import (
	"context"
	"errors"
	"fmt"
	"time"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	wfclient "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)
```

Add the sentinel just above `// ScenarioDefaults looks up...`:

```go
// ErrNotScenario is returned when a submit targets a WorkflowTemplate that is
// not a runnable scenario (dlh.category != "scenario") — e.g. a chaos or
// fixture building block.
var ErrNotScenario = errors.New("template is not a runnable scenario")
```

In `Submit`, immediately AFTER the template-Get error block (after the closing `}` of `if err != nil { ... }` that ends with `return nil, fmt.Errorf("get workflowtemplate %q: %w", ...)`) and BEFORE `now := time.Now().UTC()`, insert:

```go
	// Building blocks (chaos/fixture/util/…) are not runnable scenarios.
	if tmpl.Labels["dlh.category"] != "scenario" {
		return nil, ErrNotScenario
	}

```

- [ ] **Step 4: Run the reject test — expect PASS, existing tests now FAIL**

Run: `cd controlplane && go test ./internal/runs/...`
Expected: `TestSubmit_RejectsNonScenario` PASSES, but the existing scenario tests (`TestSubmit_CreatesWorkflowWithTemplateRef`, `TestSubmit_PriorityOverrideStampsWorkflow`, `TestSubmit_PriorityFallsBackToBaked`, `TestSubmit_PriorityUsesScenarioDefault`, `TestSubmit_WithTargetID`) now FAIL with `ErrNotScenario` — their `mysql-pod-delete` fixtures lack the label. This is the expected ripple.

- [ ] **Step 5: Label the existing scenario fixtures**

In `internal/runs/submit_test.go`, replace ALL occurrences of:

```go
metav1.ObjectMeta{Name: "mysql-pod-delete", Namespace: ns}
```

with:

```go
metav1.ObjectMeta{Name: "mysql-pod-delete", Namespace: ns, Labels: map[string]string{"dlh.category": "scenario"}}
```

(Use a replace-all; there are 5 identical occurrences. The `TestSubmit_404ForUnknownScenario` and `TestSubmit_EmptyScenarioRejected` tests have no template and are unaffected.)

- [ ] **Step 6: Run tests + vet — expect all PASS**

Run: `cd controlplane && go test ./internal/runs/... && go vet ./internal/runs/...`
Expected: PASS, vet clean.

- [ ] **Step 7: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/internal/runs/submit.go controlplane/internal/runs/submit_test.go
git commit -m "fix(runs): reject submit of non-scenario templates

Building-block templates (chaos/fixture/util) are invoked by scenarios,
not run directly. Submit now returns ErrNotScenario when the target
template's dlh.category != scenario.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 2: Filter scenario lists + map ErrNotScenario to 400 (TDD)

**Files:**
- Modify: `internal/model/types.go`
- Modify: `internal/api/handlers.go`
- Test: `internal/api/handlers_test.go`

- [ ] **Step 1: Add failing/ripple tests in `handlers_test.go`**

Replace the ENTIRE existing `TestListScenarios` function with (adds a building block + asserts it's filtered out):

```go
func TestListScenarios(t *testing.T) {
	deps := &Deps{
		Templates: &fakeTemplates{items: []wfv1.WorkflowTemplate{
			{ObjectMeta: metav1.ObjectMeta{Name: "mysql-pod-delete",
				Labels: map[string]string{"dlh.category": "scenario"}}},
			{ObjectMeta: metav1.ObjectMeta{Name: "chaos-kafka-broker-partition",
				Labels: map[string]string{"dlh.category": "chaos"}}},
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
		t.Errorf("only scenario-labeled templates expected, got %+v", out.Items)
	}
}
```

Append two new tests at the end of the file:

```go
func TestGetScenarioPriorities_FiltersBuildingBlocks(t *testing.T) {
	deps := &Deps{
		Templates: &fakeTemplates{items: []wfv1.WorkflowTemplate{
			{ObjectMeta: metav1.ObjectMeta{Name: "mysql-pod-delete",
				Labels: map[string]string{"dlh.category": "scenario"}}},
			{ObjectMeta: metav1.ObjectMeta{Name: "util-write-slo",
				Labels: map[string]string{"dlh.category": "util"}}},
		}},
		Priorities: &fakePriorities{m: map[string]int{}},
	}
	h := &Handlers{deps: deps}
	resp, _ := h.GetScenarioPriorities(context.Background(), gen.GetScenarioPrioritiesRequestObject{})
	out := resp.(gen.GetScenarioPriorities200JSONResponse)
	if len(out.Items) != 1 || out.Items[0].Scenario != "mysql-pod-delete" {
		t.Errorf("only scenario-labeled templates expected: %+v", out.Items)
	}
}

func TestCreateRun_400OnNonScenario(t *testing.T) {
	ns := "dlh-test-fw"
	tmpl := &wfv1.WorkflowTemplate{ObjectMeta: metav1.ObjectMeta{
		Name: "chaos-kafka-broker-partition", Namespace: ns,
		Labels: map[string]string{"dlh.category": "chaos"}}}
	argo := wfake.NewSimpleClientset(tmpl)
	deps := &Deps{
		Templates: &fakeTemplates{items: []wfv1.WorkflowTemplate{*tmpl}},
		Submitter: &runs.Submitter{Argo: argo, Namespace: ns},
		Manifests: &runs.ManifestWriter{Bucket: "artifacts"},
	}
	h := &Handlers{deps: deps}
	resp, err := h.CreateRun(context.Background(), gen.CreateRunRequestObject{
		Body: &gen.CreateRunRequest{ScenarioId: "chaos-kafka-broker-partition"}})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if _, ok := resp.(gen.CreateRun400Response); !ok {
		t.Fatalf("expected 400, got %T", resp)
	}
}
```

- [ ] **Step 2: Run — expect failures**

Run: `cd controlplane && go test ./internal/api/...`
Expected: FAIL. `TestListScenarios` fails (no filter yet → 2 items). `TestGetScenarioPriorities_FiltersBuildingBlocks` fails (2 items). `TestCreateRun_400OnNonScenario` fails (Submit returns `ErrNotScenario`, currently mapped to a 500 / non-nil error, not 400). Also `TestCreateRun_Submits`, `TestCreateRun_ForwardsPriority`, `TestPutAndGetScenarioPriorities` will fail once the filter/guard land — handled in Step 5.

- [ ] **Step 3: Add the `IsScenarioTemplate` helper to `model`**

In `internal/model/types.go`, add (the package already imports `wfv1`):

```go
// IsScenarioTemplate reports whether a WorkflowTemplate is a runnable scenario
// (vs a chaos/fixture/util building block), per its dlh.category label.
func IsScenarioTemplate(t *wfv1.WorkflowTemplate) bool {
	return t.Labels["dlh.category"] == "scenario"
}
```

- [ ] **Step 4: Filter the handlers + map the 400**

In `internal/api/handlers.go`:

(a) `ListScenarios` — replace its loop body:

```go
	for i := range tmpls {
		out = append(out, model.ScenarioFromTemplate(&tmpls[i]))
	}
```

with:

```go
	for i := range tmpls {
		if !model.IsScenarioTemplate(&tmpls[i]) {
			continue
		}
		out = append(out, model.ScenarioFromTemplate(&tmpls[i]))
	}
```

(b) `GetScenarioPriorities` — add the guard as the first line inside its `for _, t := range tmpls {` loop:

```go
	for _, t := range tmpls {
		if !model.IsScenarioTemplate(&t) {
			continue
		}
		baked := 0
```

(c) `CreateRun` — replace the error block:

```go
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return gen.CreateRun404Response{}, nil
		}
		return nil, err
	}
```

with (ErrNotScenario checked first):

```go
	if err != nil {
		if errors.Is(err, runs.ErrNotScenario) {
			return gen.CreateRun400Response{}, nil
		}
		if strings.Contains(err.Error(), "not found") {
			return gen.CreateRun404Response{}, nil
		}
		return nil, err
	}
```

- [ ] **Step 5: Label the existing handler fixtures**

In `internal/api/handlers_test.go`, replace ALL occurrences of:

```go
metav1.ObjectMeta{Name: "mysql-pod-delete", Namespace: ns}
```

with:

```go
metav1.ObjectMeta{Name: "mysql-pod-delete", Namespace: ns, Labels: map[string]string{"dlh.category": "scenario"}}
```

(3 occurrences: `TestCreateRun_Submits`, `TestCreateRun_ForwardsPriority`, `TestPutAndGetScenarioPriorities`. The `TestGetQueue_GroupsAndOrders` helper uses a different literal and is NOT matched. `TestCreateRun_404OnUnknownScenario` has no template and is unaffected.)

- [ ] **Step 6: Run the full backend suite + vet + build**

Run: `cd controlplane && go test ./... && go vet ./... && go build ./...`
Expected: all PASS, vet/build clean. (Confirms no other package regressed, including `internal/schedules`.)

- [ ] **Step 7: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/internal/model/types.go controlplane/internal/api/handlers.go controlplane/internal/api/handlers_test.go
git commit -m "feat(api): list only dlh.category=scenario templates; 400 on non-scenario submit

ListScenarios and GetScenarioPriorities now filter to scenario-labeled
templates via model.IsScenarioTemplate; CreateRun maps runs.ErrNotScenario
to 400. Building blocks no longer appear as runnable scenarios.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 3: Live re-verification on minikube

**Files:** none.

- [ ] **Step 1: Ensure live templates carry the `dlh.category` label**

Run (retry-tolerant; the cluster API has been intermittently slow this session):
```bash
cd /Users/allen/repo/dlh-test-fw
kubectl -n dlh-test-fw get workflowtemplates -l dlh.category=scenario -o name
```
Expected: the 3 scenario templates listed (`mysql-pod-delete`, `kafka-broker-partition`, `doris-be-network-loss`).

IF the list is EMPTY (deployed templates predate the labels), apply the chart so the labels land (local-dev; chart already contains them):
```bash
helm upgrade --install dlh helm/dlh-test-fw -f helm/dlh-test-fw/values.yaml -f helm/dlh-test-fw/values-minikube.yaml -n dlh-test-fw --create-namespace
```
Then re-run the `-l dlh.category=scenario` check and confirm the 3 appear.

- [ ] **Step 2: Build, reload, restart**

```bash
cd /Users/allen/repo/dlh-test-fw/controlplane && make build && make reload-minikube
kubectl -n dlh-test-fw rollout restart deployment/dlh-controlplane
kubectl -n dlh-test-fw rollout status deployment/dlh-controlplane --timeout=120s
```

- [ ] **Step 3: Ensure port-forward**

```bash
pgrep -f "port-forward.*dlh-controlplane" >/dev/null && curl -sf -o /dev/null http://localhost:8080/ || \
  { pkill -f "port-forward.*dlh-controlplane"; sleep 2; (kubectl -n dlh-test-fw port-forward deployment/dlh-controlplane 8080:8080 >/tmp/dlh-pf.log 2>&1 &); }
for i in $(seq 1 12); do curl -sf -o /dev/null http://localhost:8080/ 2>/dev/null && break; sleep 1; done
```

- [ ] **Step 4: API checks**

```bash
TOK="fake:runner:runner@local:dlh-runner"; EP=http://localhost:8080
# scenarios list = exactly the 3 real scenarios
curl -sf "$EP/api/scenarios" -H "Authorization: Bearer $TOK" | python3 -c "import sys,json;d=json.load(sys.stdin);print(sorted(s['id'] for s in d['items']))"
# submitting a building block → 400
curl -s -o /dev/null -w "submit chaos building block: HTTP %{http_code}\n" -X POST "$EP/api/runs" \
  -H "Authorization: Bearer $TOK" -H "Content-Type: application/json" \
  -d '{"scenarioId":"chaos-kafka-broker-partition"}'
```
Expected: scenarios list = `['doris-be-network-loss', 'kafka-broker-partition', 'mysql-pod-delete']`; submit → `HTTP 400`.

- [ ] **Step 5: Playwright check**

Navigate to `http://localhost:8080/scenarios`. Confirm only the 3 real scenarios appear (no `chaos-*`/`fixture-*`/`util-*`/`verdict-*`/`load-*`). **0 console errors.** Remove any screenshot/`.playwright-mcp` artifacts from `docs/superpowers/specs/` afterward.

---

## Self-Review

**Spec coverage:**
- Filter `ListScenarios` → Task 2 Step 4(a). ✓
- Filter `GetScenarioPriorities` → Task 2 Step 4(b). ✓
- `isScenarioTemplate` helper → `model.IsScenarioTemplate` (Task 2 Step 3). ✓
- Block submission with `ErrNotScenario` → Task 1 Step 3. ✓
- Map to 400 in `CreateRun` (before not-found) → Task 2 Step 4(c). ✓
- CLI inherits → no code; 400 surfaces as submit error (verified Task 3 Step 4). ✓
- Ensure live labels / helm upgrade → Task 3 Step 1. ✓
- Tests (7 spec cases) → Task 1 (reject) + Task 2 (filter ×2, 400, regressions kept green via fixture labels). ✓

**Placeholder scan:** none. The "IF empty" branch in Task 3 Step 1 is a conditional verification instruction with the exact command, not a placeholder.

**Type consistency:** `model.IsScenarioTemplate(*wfv1.WorkflowTemplate) bool` used consistently; `runs.ErrNotScenario` defined in Task 1, referenced in Task 2; `gen.CreateRun400Response` confirmed to exist (handlers.go already returns it for empty body). `errors` import present in handlers.go; added to submit.go + submit_test.go. The 3 shared `metav1.ObjectMeta{Name: "mysql-pod-delete", Namespace: ns}` literals in handlers_test.go and 5 in submit_test.go are exact replace-all targets; `TestGetQueue` uses a different literal and is not matched.
