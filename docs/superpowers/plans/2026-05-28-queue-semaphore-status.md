# Queue Lane Semaphore-Status Fix — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix `BuildLanes` so a lane's Running/Queued split comes from Argo's `status.synchronization.semaphore.holding/.waiting`, not the workflow phase — eliminating the impossible `2/1 slot` count.

**Architecture:** Pure rewrite of the bucketing loop in `internal/queue/queue.go`. A workflow is a Running holder of a lane iff its own status lists that lane's lock in `.Holding`, and a Queued waiter iff in `.Waiting`. Lane key = last path segment of the semaphore name, filtered to the known ConfigMap keys `BuildLanes` already receives. No handler, API, or UI change.

**Tech Stack:** Go, Argo Workflows `v1alpha1` types (v3.6.5), `go test`. Commands from `controlplane/`.

**Conventions:** NEVER `git add -A`/globs. Commit trailer `Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>`. Spec: `docs/superpowers/specs/2026-05-28-queue-semaphore-status-design.md`.

---

## File Structure

- **Modify:** `internal/queue/queue.go` — rewrite `BuildLanes` bucketing loop; add `lockKey` + `addEntry` helpers; add `strings` import. `entryOf`, `prioVal`, `isTerminal`, and the sort logic are unchanged.
- **Modify:** `internal/queue/queue_test.go` — replace the `wf(...)` helper and the two existing tests with synchronization-driven fixtures + the 9 spec cases.

No other files. The handler (`internal/api/handlers.go:GetQueue`) already passes full workflow objects (which carry `.Status.Synchronization`) and the `keys []LockKey` set; its code does not change.

---

### Task 1: Rewrite `BuildLanes` to classify by semaphore status (TDD)

**Files:**
- Modify: `internal/queue/queue.go`
- Test: `internal/queue/queue_test.go`

- [ ] **Step 1: Replace the test file with synchronization-driven fixtures**

Replace the entire contents of `internal/queue/queue_test.go` with:

```go
package queue

import (
	"testing"
	"time"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// semName builds Argo's fully-qualified semaphore name for a bare lock key,
// matching the live format "dlh-test-fw/ConfigMap/dlh-scenario-locks/<key>".
func semName(key string) string {
	return "dlh-test-fw/ConfigMap/dlh-scenario-locks/" + key
}

// wf builds a workflow fixture. holding/waiting are bare lock keys (e.g.
// "mysql"); each is expanded to a fully-qualified semaphore name. phase sets
// the overall workflow phase — deliberately decoupled from lock ownership so
// tests can prove classification ignores phase.
func wf(name, scenario, phase string, prio int32, created time.Time, holding, waiting []string) *wfv1.Workflow {
	w := &wfv1.Workflow{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			CreationTimestamp: metav1.NewTime(created),
			Labels:            map[string]string{"dlh.scenario": scenario},
		},
		Spec:   wfv1.WorkflowSpec{Priority: &prio},
		Status: wfv1.WorkflowStatus{Phase: wfv1.WorkflowPhase(phase)},
	}
	if len(holding) > 0 || len(waiting) > 0 {
		sem := &wfv1.SemaphoreStatus{}
		for _, k := range holding {
			sem.Holding = append(sem.Holding, wfv1.SemaphoreHolding{Semaphore: semName(k), Holders: []string{name}})
		}
		for _, k := range waiting {
			// Argo records the *blocker* in waiting[].holders, not self — the
			// implementation must not rely on holders contents.
			sem.Waiting = append(sem.Waiting, wfv1.SemaphoreHolding{Semaphore: semName(k), Holders: []string{"some-other-holder"}})
		}
		w.Status.Synchronization = &wfv1.SynchronizationStatus{Semaphore: sem}
	}
	return w
}

func TestBuildLanes_HolderIsRunning(t *testing.T) {
	t0 := time.Date(2026, 5, 26, 0, 0, 0, 0, time.UTC)
	wfs := []*wfv1.Workflow{
		wf("m-run", "mysql-pod-delete", "Running", 100, t0, []string{"mysql"}, nil),
	}
	lanes := BuildLanes(wfs, []LockKey{{Key: "mysql", Slots: 1}})
	if len(lanes[0].Running) != 1 || lanes[0].Running[0].ID != "m-run" {
		t.Errorf("holder should be Running: %+v", lanes[0])
	}
	if len(lanes[0].Pending) != 0 {
		t.Errorf("no pending expected: %+v", lanes[0].Pending)
	}
}

func TestBuildLanes_WaiterIsQueued(t *testing.T) {
	t0 := time.Date(2026, 5, 26, 0, 0, 0, 0, time.UTC)
	wfs := []*wfv1.Workflow{
		wf("m-run", "mysql-pod-delete", "Running", 100, t0, []string{"mysql"}, nil),
		wf("m-wait", "mysql-pod-delete", "Pending", 100, t0.Add(time.Minute), nil, []string{"mysql"}),
	}
	lanes := BuildLanes(wfs, []LockKey{{Key: "mysql", Slots: 1}})
	if len(lanes[0].Running) != 1 || lanes[0].Running[0].ID != "m-run" {
		t.Errorf("running: %+v", lanes[0].Running)
	}
	if len(lanes[0].Pending) != 1 || lanes[0].Pending[0].ID != "m-wait" {
		t.Errorf("pending: %+v", lanes[0].Pending)
	}
}

// Regression for the 2/1 bug: two phase=Running workflows, but only one holds
// the lock. The other (here phase=Running too, but in .waiting) must be Queued.
func TestBuildLanes_OverSubscriptionGuard(t *testing.T) {
	t0 := time.Date(2026, 5, 26, 0, 0, 0, 0, time.UTC)
	wfs := []*wfv1.Workflow{
		wf("k-hold", "kafka-broker-partition", "Running", 100, t0, []string{"kafka"}, nil),
		wf("k-wait", "chaos-kafka-broker-partition", "Running", 100, t0.Add(time.Minute), nil, []string{"kafka"}),
	}
	lanes := BuildLanes(wfs, []LockKey{{Key: "kafka", Slots: 1}})
	if len(lanes[0].Running) != 1 {
		t.Fatalf("exactly one holder expected, got %d: %+v", len(lanes[0].Running), lanes[0].Running)
	}
	if lanes[0].Running[0].ID != "k-hold" {
		t.Errorf("wrong holder: %+v", lanes[0].Running)
	}
	if len(lanes[0].Pending) != 1 || lanes[0].Pending[0].ID != "k-wait" {
		t.Errorf("waiter should be Queued: %+v", lanes[0].Pending)
	}
}

// A phase=Running workflow with no synchronization (pre-gate or post-release)
// contends for nothing and must appear in no lane.
func TestBuildLanes_NoSyncIsAbsent(t *testing.T) {
	t0 := time.Date(2026, 5, 26, 0, 0, 0, 0, time.UTC)
	wfs := []*wfv1.Workflow{
		wf("m-prep", "mysql-pod-delete", "Running", 100, t0, nil, nil),
	}
	lanes := BuildLanes(wfs, []LockKey{{Key: "mysql", Slots: 1}})
	if len(lanes[0].Running) != 0 || len(lanes[0].Pending) != 0 {
		t.Errorf("non-contending workflow must be absent: %+v", lanes[0])
	}
}

func TestBuildLanes_MultiLaneIsolation(t *testing.T) {
	t0 := time.Date(2026, 5, 26, 0, 0, 0, 0, time.UTC)
	wfs := []*wfv1.Workflow{
		wf("m-run", "mysql-pod-delete", "Running", 100, t0, []string{"mysql"}, nil),
		wf("k-run", "kafka-broker-partition", "Running", 100, t0, []string{"kafka"}, nil),
	}
	keys := []LockKey{{Key: "mysql", Slots: 1}, {Key: "kafka", Slots: 1}, {Key: "doris", Slots: 1}}
	lanes := BuildLanes(wfs, keys)
	if len(lanes) != 3 {
		t.Fatalf("expected 3 lanes, got %d", len(lanes))
	}
	if lanes[0].Key != "mysql" || len(lanes[0].Running) != 1 || lanes[0].Running[0].ID != "m-run" {
		t.Errorf("mysql lane: %+v", lanes[0])
	}
	if lanes[1].Key != "kafka" || len(lanes[1].Running) != 1 || lanes[1].Running[0].ID != "k-run" {
		t.Errorf("kafka lane: %+v", lanes[1])
	}
}

func TestBuildLanes_IdleLane(t *testing.T) {
	wfs := []*wfv1.Workflow{}
	lanes := BuildLanes(wfs, []LockKey{{Key: "doris", Slots: 1}})
	if len(lanes) != 1 || lanes[0].Key != "doris" {
		t.Fatalf("expected doris lane, got %+v", lanes)
	}
	if len(lanes[0].Running) != 0 || len(lanes[0].Pending) != 0 {
		t.Errorf("idle lane should be empty: %+v", lanes[0])
	}
}

func TestBuildLanes_PendingOrder(t *testing.T) {
	t0 := time.Date(2026, 5, 26, 0, 0, 0, 0, time.UTC)
	wfs := []*wfv1.Workflow{
		wf("m-run", "mysql-pod-delete", "Running", 100, t0, []string{"mysql"}, nil),
		wf("m-lowprio-old", "mysql-pod-delete", "Pending", 100, t0.Add(1*time.Minute), nil, []string{"mysql"}),
		wf("m-highprio-new", "mysql-pod-delete", "Pending", 500, t0.Add(2*time.Minute), nil, []string{"mysql"}),
	}
	lanes := BuildLanes(wfs, []LockKey{{Key: "mysql", Slots: 1}})
	p := lanes[0].Pending
	// higher priority first even though submitted later; oldest breaks ties
	if len(p) != 2 || p[0].ID != "m-highprio-new" || p[1].ID != "m-lowprio-old" {
		t.Errorf("pending order: %+v", p)
	}
}

func TestBuildLanes_UnknownKeyIgnored(t *testing.T) {
	t0 := time.Date(2026, 5, 26, 0, 0, 0, 0, time.UTC)
	wfs := []*wfv1.Workflow{
		wf("x-run", "some-scenario", "Running", 100, t0, []string{"redis"}, nil),
	}
	lanes := BuildLanes(wfs, []LockKey{{Key: "mysql", Slots: 1}})
	if len(lanes[0].Running) != 0 || len(lanes[0].Pending) != 0 {
		t.Errorf("unknown-key holder must be ignored: %+v", lanes[0])
	}
}

func TestBuildLanes_TerminalIgnored(t *testing.T) {
	t0 := time.Date(2026, 5, 26, 0, 0, 0, 0, time.UTC)
	wfs := []*wfv1.Workflow{
		// stale synchronization on a finished workflow must not leak in
		wf("m-done", "mysql-pod-delete", "Succeeded", 100, t0, []string{"mysql"}, nil),
		wf("m-failed", "mysql-pod-delete", "Failed", 100, t0, []string{"mysql"}, nil),
	}
	lanes := BuildLanes(wfs, []LockKey{{Key: "mysql", Slots: 1}})
	if len(lanes[0].Running) != 0 || len(lanes[0].Pending) != 0 {
		t.Errorf("terminal workflows must not appear: %+v", lanes[0])
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd controlplane && go test ./internal/queue/...`
Expected: FAIL — the current `BuildLanes` classifies by phase, so `TestBuildLanes_HolderIsRunning` / `WaiterIsQueued` / `NoSyncIsAbsent` / `OverSubscriptionGuard` / `UnknownKeyIgnored` fail (e.g. `m-run` not found in Running because old code reads phase; `m-prep` wrongly appears). Compilation succeeds (helper signature changed but all call sites updated in this file).

- [ ] **Step 3: Rewrite `BuildLanes` and add helpers in `queue.go`**

In `internal/queue/queue.go`, add `"strings"` to the import block:

```go
import (
	"sort"
	"strings"
	"time"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"

	"github.com/dlh/dlh-test-fw/controlplane/internal/links"
)
```

(The `links` import is now unused by this file — leave it ONLY if another symbol in the file references it; otherwise remove it to satisfy the compiler. After this rewrite `DeriveTargetType` is no longer called here, so **remove the `links` import line**.)

Replace the entire `BuildLanes` function (the doc comment through its closing brace) with:

```go
// lockKey extracts the bare semaphore key (last path segment) from Argo's
// fully-qualified semaphore name, e.g.
// "dlh-test-fw/ConfigMap/dlh-scenario-locks/mysql" -> "mysql".
func lockKey(semaphore string) string {
	if i := strings.LastIndex(semaphore, "/"); i >= 0 {
		return semaphore[i+1:]
	}
	return semaphore
}

// addEntry appends e under key, deduping by ID (a workflow lists a given lock
// at most once, but guard against duplicate holding/waiting entries).
func addEntry(m map[string][]Entry, key string, e Entry) {
	for _, x := range m[key] {
		if x.ID == e.ID {
			return
		}
	}
	m[key] = append(m[key], e)
}

// BuildLanes groups non-terminal workflows into one lane per lock key,
// preserving the key order given. Classification comes from Argo's own
// synchronization record, NOT the workflow phase: a workflow is a Running
// holder of a lane iff its status lists that lane's lock in .Holding, and a
// Queued waiter iff in .Waiting. Workflows contending for nothing (pre-gate or
// post-release) appear in no lane. Pending entries are ordered priority-desc
// then oldest-first (Argo's release order).
func BuildLanes(wfs []*wfv1.Workflow, keys []LockKey) []Lane {
	known := make(map[string]bool, len(keys))
	for _, k := range keys {
		known[k.Key] = true
	}

	running := map[string][]Entry{}
	pending := map[string][]Entry{}
	for _, w := range wfs {
		if w == nil || isTerminal(w.Status.Phase) {
			continue
		}
		sync := w.Status.Synchronization
		if sync == nil || sync.Semaphore == nil {
			continue
		}
		e := entryOf(w)
		for _, h := range sync.Semaphore.Holding {
			if key := lockKey(h.Semaphore); known[key] {
				addEntry(running, key, e)
			}
		}
		for _, h := range sync.Semaphore.Waiting {
			if key := lockKey(h.Semaphore); known[key] {
				addEntry(pending, key, e)
			}
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

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd controlplane && go test ./internal/queue/...`
Expected: PASS (all 9 tests, `ok github.com/dlh/dlh-test-fw/controlplane/internal/queue`).

- [ ] **Step 5: Vet + full backend build**

Run: `cd controlplane && go vet ./internal/queue/... && go build ./...`
Expected: exit 0, no output. (Confirms the `links` import removal and `strings` addition compile cleanly across the module.)

- [ ] **Step 6: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/internal/queue/queue.go controlplane/internal/queue/queue_test.go
git commit -m "fix(queue): classify lanes by Argo semaphore status not workflow phase

BuildLanes counted any phase=Running workflow as a slot holder, so a
saturated lane could show an impossible 2/1. Classify Running vs Queued
from status.synchronization.semaphore.holding/.waiting instead, keyed by
the semaphore name. running count can no longer exceed slots.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 2: Live re-verification on minikube

**Files:** none.

- [ ] **Step 1: Build, reload, restart**

Run:
```bash
cd /Users/allen/repo/dlh-test-fw/controlplane && make build && make reload-minikube
kubectl -n dlh-test-fw rollout restart deployment/dlh-controlplane
kubectl -n dlh-test-fw rollout status deployment/dlh-controlplane --timeout=120s
```
(`make build` rebuilds the Go binary with the embedded UI already present from the prior page work; no `ui-build` needed since no UI changed. If `make reload-minikube` reports the UI dist missing, run `make ui-build` first.)

- [ ] **Step 2: Ensure port-forward**

Run:
```bash
pgrep -f "port-forward.*dlh-controlplane" >/dev/null || \
  (kubectl -n dlh-test-fw port-forward deployment/dlh-controlplane 8080:8080 >/tmp/dlh-pf.log 2>&1 &)
for i in $(seq 1 12); do curl -sf -o /dev/null http://localhost:8080/ 2>/dev/null && break; sleep 1; done
```

- [ ] **Step 3: Seed a holder + a waiter on the mysql lane**

Run:
```bash
cd /Users/allen/repo/dlh-test-fw/controlplane
TOK="fake:runner:runner@local:dlh-runner"; EP=http://localhost:8080
kubectl -n dlh-test-fw delete wf -l dlh.scenario=mysql-pod-delete >/dev/null 2>&1; sleep 2
DLH_TOKEN=$TOK go run ./cmd/dlh run mysql-pod-delete --endpoint $EP
sleep 10
DLH_TOKEN=$TOK go run ./cmd/dlh run mysql-pod-delete --priority 300 --endpoint $EP
sleep 20
curl -sf "$EP/api/queue" -H "Authorization: Bearer fake:viewer:v@local:dlh-viewer" | \
  python3 -c "import sys,json;d=json.load(sys.stdin);[print(l['key'],'running:',[r['id'] for r in l['running']],'pending:',[p['id'] for p in l['pending']]) for l in d['lanes']]"
```
Expected: the `mysql` line shows exactly **1** id under `running:` and **1** under `pending:` — never 2 running.

- [ ] **Step 4: Playwright visual check**

Navigate to `http://localhost:8080/queue`. Confirm: mysql lane header `1/1 slot` (accented, not `2/1`); one RUNNING entry; one QUEUED `#1 NEXT` entry; doris/kafka `0/1 slot` + dashed Idle. **0 console errors.**

- [ ] **Step 5: Clean up**

Run: `kubectl -n dlh-test-fw delete wf -l dlh.scenario=mysql-pod-delete` and remove any `*.png` / `.playwright-mcp` artifacts from `docs/superpowers/specs/`.

---

## Self-Review

**Spec coverage:**
- Classify by `status.synchronization` not phase → Task 1 Step 3 `BuildLanes`. ✓
- Lane key = last path segment, filtered to known keys → `lockKey` + `known` map. ✓
- `holders` contents ignored → fixture sets a bogus holder in `.waiting`; impl never reads `.Holders`. ✓ (`TestBuildLanes_WaiterIsQueued`, `OverSubscriptionGuard`)
- entryOf / pending ordering / running ordering unchanged → reused verbatim. ✓ (`TestBuildLanes_PendingOrder`)
- Handler / API / UI unchanged → no tasks touch them (stated in File Structure). ✓
- `n/slots` can never exceed slots → `OverSubscriptionGuard` regression test. ✓
- 9 spec test cases → 9 `TestBuildLanes_*` functions. ✓
- Live re-verification → Task 2. ✓

**Placeholder scan:** none. The `links`-import note is a conditional compile instruction with the explicit resolution ("remove the import line"), not a placeholder.

**Type consistency:** `lockKey(string) string`, `addEntry(map[string][]Entry, string, Entry)`, `Entry`, `Lane`, `LockKey`, `prioVal`, `isTerminal`, `entryOf` all match `queue.go`. Argo paths verified: `WorkflowStatus.Synchronization *SynchronizationStatus` → `.Semaphore *SemaphoreStatus` → `.Holding/.Waiting []SemaphoreHolding` → `.Semaphore string` / `.Holders []string`. Test helper `wf(...)` signature matches every call site in the rewritten test file.
