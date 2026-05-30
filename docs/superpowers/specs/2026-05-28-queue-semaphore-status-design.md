# Queue Lane Classification — Semaphore-Status Fix (Design)

**Date:** 2026-05-28
**Status:** Approved (brainstorm → ready for plan)
**Component:** `controlplane/internal/queue` (`BuildLanes`) + `controlplane/internal/queue/queue_test.go`

---

## Problem

The Queue page renders a per-target-type lane with a `n/slots` slot indicator,
a Running section, and a Queued section. On a saturated lane it displayed
**`2/1 slot`** — two workflows shown as Running holders in a lane whose
semaphore allows only one concurrent holder. That count is logically
impossible under the "1 concurrent run per target type" semaphore.

### Root cause

`BuildLanes` (`internal/queue/queue.go`) classifies a workflow as a Running
holder purely by its **overall workflow phase**:

```go
case wfv1.WorkflowRunning:
    running[key] = append(running[key], e)
```

In Argo a workflow's phase is `Running` for its entire active lifetime — from
the first step to the last — which is unrelated to whether it currently holds
the semaphore. The semaphore (`synchronization`) is acquired by a specific
step/node (the chaos step), not by the workflow as a whole. So two failure
modes produce an inflated count:

1. **Two concurrent runs, one waiting.** Both workflows are phase `Running`,
   but only one holds the lock; the other is blocked at the chaos gate.
   `BuildLanes` counts both as holders → `2/1`.
2. **Post-release run still active.** A run that has finished its chaos step
   has *released* the slot but is still phase `Running` (doing
   `run-testrun` / `cleanup`). The next waiter has already acquired the slot.
   `BuildLanes` counts the releaser plus the new holder → `2/1`.

The slot indicator faithfully rendered a count the backend computed wrong; the
UI refinement merely made the pre-existing backend bug visible.

### Live evidence (Argo 3.6.5, minikube)

Two contending `mysql-pod-delete` runs:

- **Holder** (`...172238`): phase `Running`,
  `status.synchronization.semaphore.holding = [{semaphore: ".../dlh-scenario-locks/mysql", holders: [".../172238"]}]`
- **Waiter** (`...172313`): phase `Pending`,
  `status.synchronization.semaphore.waiting = [{semaphore: ".../dlh-scenario-locks/mysql", holders: [".../172238"]}]`

Note the waiter's phase was `Pending` here, while an earlier kafka waiter was
observed as `Running` — confirming phase is not a reliable signal. Note also
that the waiter's `waiting[].holders` lists the **blocker** (`172238`), not the
waiter itself: a workflow's role is determined by which array is populated on
its **own** status, and the `holders` string contents are informational only.

---

## Approach (chosen)

Classify Running vs Queued from Argo's own record, `status.synchronization`,
instead of the workflow phase. This is the authoritative source: Argo writes
exactly who holds each lock and who waits on it.

### Argo types (`v1alpha1`, v3.6.5)

```go
type SynchronizationStatus struct {
    Semaphore *SemaphoreStatus
    Mutex     *MutexStatus
}
type SemaphoreStatus struct {
    Holding []SemaphoreHolding
    Waiting []SemaphoreHolding
}
type SemaphoreHolding struct {
    Semaphore string   // e.g. "dlh-test-fw/ConfigMap/dlh-scenario-locks/mysql"
    Holders   []string // node/workflow names — informational, not used for role
}
```

### Classification rule

For each non-terminal workflow `W`, read `W.Status.Synchronization.Semaphore`:

- For each entry in `.Holding` whose lock key is known → `W` is a **Running
  holder** in that lane.
- For each entry in `.Waiting` whose lock key is known → `W` is a **Queued
  waiter** in that lane.
- If neither is populated for any known key → `W` is not contending
  (pre-gate, or already released post-chaos) → not shown in any lane.

The **lock key** is the last `/`-delimited segment of the `Semaphore` string
(`dlh-test-fw/ConfigMap/dlh-scenario-locks/mysql` → `mysql`). Only keys present
in the `keys []LockKey` set that `BuildLanes` already receives are bucketed;
unknown keys are ignored (defensive — filters out any unrelated semaphore). A
workflow is added at most once per lane per role.

### What stays the same

- **`entryOf`** (scenario / priority / submittedAt extraction) — unchanged.
- **Pending ordering** — priority desc, then oldest-first (Argo release order).
- **Running ordering** — oldest-first.
- **Handler `GetQueue`** — unchanged; still `Locks.Keys()` + `Workflows.List()`
  + `BuildLanes`. Full workflow objects already carry `.Status.Synchronization`.
- **API response shape** (`Queue` / `QueueLane` / `QueueEntry`) — unchanged.
- **The entire Queue UI** — unchanged.

This is a pure rewrite of the bucketing loop inside `BuildLanes`. No API, no
handler, no UI, no plumbing changes.

### Role of the lock-key list (ConfigMap)

`keys []LockKey` (from the `dlh-scenario-locks` ConfigMap) still defines:
- which lanes exist and their display order,
- each lane's slot count (`Slots`),
- idle lanes (a key with zero holders/waiters renders as Idle).

`links.DeriveTargetType` is no longer used by the queue package (it remains in
`internal/links` for its other consumers; not removed).

---

## Consequences

- `len(lane.Running) ≤ lane.Slots` always holds → the slot indicator can never
  show `2/1`.
- A run that has released its slot drops out of the lane immediately; the next
  waiter appears as Running once it acquires.
- **Accepted trade-off:** a just-submitted run does not appear in the lane until
  it reaches the chaos gate (a few seconds of prep steps). No third "starting"
  state is added (YAGNI). During that brief window the run is genuinely not
  contending for the lock.

---

## Testing

`internal/queue/queue_test.go` is pure-unit (no cluster). It is rewritten to
build `wfv1.Workflow` fixtures with `Status.Synchronization.Semaphore.Holding`
/ `.Waiting` rather than `Status.Phase`. Cases:

1. **Holder → Running.** One workflow holding `mysql` → mysql lane has it in
   Running, slots respected.
2. **Waiter → Queued.** One holder + one waiter on `mysql` → 1 Running, 1
   Pending.
3. **Over-subscription guard.** Two workflows both phase `Running`, but only one
   in `.Holding` (other in `.Waiting`) → 1 Running, 1 Queued. (Regression test
   for the `2/1` bug.)
4. **Neither populated → absent.** A phase-`Running` workflow with empty
   synchronization (pre-gate / post-release) → appears in no lane.
5. **Multi-lane isolation.** Holders on `mysql` and `kafka` land in their own
   lanes; no cross-contamination.
6. **Idle lane.** A key in `keys` with no holders/waiters renders Running and
   Pending empty (UI shows Idle).
7. **Pending order.** Multiple waiters → ordered priority desc, then
   oldest-first.
8. **Unknown key ignored.** A holding entry for a semaphore not in `keys` is
   skipped.
9. **Terminal workflows skipped.** Succeeded/Failed/Error workflows are
   excluded regardless of stale synchronization fields.

Gate: `go test ./internal/queue/...` plus the package's existing `go vet` /
build. Live re-verification on minikube with two contending runs (one holder,
one waiter) confirming the lane shows `1/1 slot` + one Queued entry, 0 console
errors.

---

## Out of scope

- No change to how scenarios declare their semaphore (chart WorkflowTemplates).
- No new API endpoint or response field.
- No UI change (the page already renders `n/slots` and the Queued section
  correctly given correct data).
- `links.DeriveTargetType` is not removed.
