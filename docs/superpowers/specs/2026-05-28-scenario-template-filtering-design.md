# Scenario Template Filtering — Design

**Date:** 2026-05-28
**Status:** Approved (brainstorm → ready for plan)
**Component:** `controlplane/internal/api` (handlers) + `controlplane/internal/runs` (submitter)

---

## Problem

The Argo namespace holds many `WorkflowTemplate`s. Only three are real, runnable
scenarios — `mysql-pod-delete`, `kafka-broker-partition`, `doris-be-network-loss`
— each declaring its per-target-type semaphore (`spec.synchronization.semaphores`
→ `dlh-scenario-locks/{mysql,kafka,doris}`). The rest are **building blocks**
invoked *by* those scenarios: `chaos-*` (chaos injection steps), `fixture-*`,
`util-*`, `verdict-*`, `load-*`. Building blocks deliberately carry no scenario
semaphore (the parent scenario holds the lock).

`ListScenarios` (`GET /api/scenarios`) and `GetScenarioPriorities`
(`GET /api/scenario-priorities`) list **every** template with no filter
(`internal/k8s/templates.go:ListTemplates` does an unfiltered List). So building
blocks appear as runnable scenarios in the UI and can be submitted directly via
`POST /api/runs` / `dlh run`.

Observed symptom: running `chaos-kafka-broker-partition` directly creates a
kafka-related workflow that holds no semaphore, so it never joins the `kafka`
queue lane — making it look like "kafka scenarios don't share one queue." (The
queue lane logic itself is correct — see
`2026-05-28-queue-semaphore-status-design.md`. This is an upstream "what counts
as a scenario" problem.)

Each template is already categorized in the chart with a `dlh.category` label:
`scenario` (×3), `chaos`, `fixture`, `util`, `verdict`, `load`. The fix uses
that existing label.

---

## Approach

### Change A — filter scenario lists by label

Add a helper in the api package:

```go
func isScenarioTemplate(t *wfv1.WorkflowTemplate) bool {
	return t.Labels["dlh.category"] == "scenario"
}
```

- `ListScenarios`: include only templates where `isScenarioTemplate(t)`.
- `GetScenarioPriorities`: same filter (a building block has no tunable scenario
  priority, so it should not appear on the Default-priorities admin page either —
  kept consistent with the scenario list).

Filtering happens in the **handler** (not the k8s lister) so it is unit-testable
with the existing fake `TemplateLister`. `ListTemplates`/`GetTemplate` stay
generic.

### Change B — reject non-scenario submissions

In `internal/runs/submit.go`, `Submit` already fetches the template
(`submit.go:49`). Immediately after a successful Get, guard:

```go
if tmpl.Labels["dlh.category"] != "scenario" {
	return nil, ErrNotScenario
}
```

Add the sentinel to the `runs` package:

```go
// ErrNotScenario is returned when a submit targets a WorkflowTemplate that is
// not a runnable scenario (dlh.category != "scenario") — e.g. a chaos/fixture
// building block.
var ErrNotScenario = errors.New("template is not a runnable scenario")
```

In `CreateRun` (`handlers.go`), map it to 400 (checked BEFORE the existing
`"not found"` substring check):

```go
if errors.Is(err, runs.ErrNotScenario) {
	return gen.CreateRun400Response{}, nil
}
if strings.Contains(err.Error(), "not found") {
	return gen.CreateRun404Response{}, nil
}
```

The CLI `dlh run` inherits this (a 400 surfaces as a submit error).

### Change C — ensure live templates carry the label

The chart source already labels every template, but the deployed copies may
predate the labels. Since the filter keys on `dlh.category=scenario`, **if the
live templates lack the label the Scenarios page would go empty.** Verification
must confirm the live templates carry the label and, if missing, run
`helm upgrade` (chart already contains the labels; safe in local-dev). No chart
file edits are needed.

---

## Testing

Handler unit tests (`internal/api`, fake `TemplateLister` returning a mix of
`dlh.category=scenario` and `dlh.category=chaos` templates):

1. `ListScenarios` returns only the scenario-labeled templates; building blocks
   excluded.
2. `GetScenarioPriorities` returns only scenario-labeled templates.
3. `CreateRun` for a non-scenario template → 400.
4. `CreateRun` for a missing template → still 404 (regression).
5. `CreateRun` for a real scenario → still 202 (regression).

Submitter unit test (`internal/runs`, fake Argo clientset):

6. `Submit` against a `dlh.category=chaos` template returns `ErrNotScenario`.
7. `Submit` against a `dlh.category=scenario` template creates the Workflow.

Live re-verification on minikube: confirm the Scenarios page shows exactly the 3
real scenarios (no `chaos-*`/`fixture-*`/`util-*`), and that submitting a
building block via CLI returns a 400. 0 console errors.

---

## Out of scope

- The queue lane logic (`internal/queue`) — already correct.
- Chart template contents — the `dlh.category` labels already exist; we only
  `helm upgrade` if the live cluster is missing them.
- Adding semaphores to building blocks — they correctly have none (the parent
  scenario holds the lock); they simply should not be standalone scenarios.
- Reworking `deriveCategory` heuristics in the web app (Runs/Scenarios grouping
  is unaffected).
