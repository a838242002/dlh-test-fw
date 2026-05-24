# Controlplane Priority — Visibility & Control — Design Spec

*Design doc — 2026-05-25. Output of `superpowers:brainstorming`, refined against an interactive HTML prototype reviewed in-browser.*

> **Milestone context:** Companion to `2026-05-25-run-detail-ui-optimization-design.md`.
> Built **after** the run-detail UI spec (which reserves the read-only Priority
> cell this spec populates).

## Problem

The priority mechanism exists in the cluster but the controlplane is **blind to
it**:

- Each scenario WorkflowTemplate bakes `spec.priority: 100`; the per-target-type
  semaphore (`dlh-scenario-locks`: `mysql`/`kafka`/`doris` = **1 slot each**)
  serializes same-type runs; Argo releases blocked workflows in
  **(priority desc, creationTimestamp asc)** order. Different types run in
  parallel.
- But: **no `priority` on the API**, the submitter never sets it, there is **no
  `dlh run --priority`**, and **nothing in the UI** shows priority or the queue.
  So priority can't be seen or controlled — only the baked default ever applies.

## Goal

Make priority **visible and controllable** in the controlplane across three
layers (all three confirmed in brainstorming):

1. **Submit-time override + display** — set a run's priority when launching it;
   show it everywhere.
2. **Live re-prioritize pending runs** — reorder a queued (not-yet-running) run.
3. **Editable per-scenario defaults** — admin baseline priority per scenario.

…surfaced through a new **Queue** view that makes the per-target-type semaphore
mechanism legible.

## Decisions (settled in brainstorming + prototype review)

| Topic | Decision |
|---|---|
| Queue placement | A **dedicated `Queue` nav page** (per-target-type lanes), not folded into Runs. |
| Reorder control | Per-queued-run **move up/down + "to front" + cancel** (drag-and-drop optional later). |
| Quiet states | Lanes show **Running / Queued / Idle** explicitly (contention is rare — design the calm case). |
| Default priorities | Dedicated **admin page** behind a link from Queue; **named tiers** (Low/Normal/High/Urgent = 10/100/200/500) **plus** a raw numeric stepper; per-row **override/reset** vs baked. |
| Default storage | A `dlh-scenario-priorities` ConfigMap the controlplane reads at submit; overrides baked `spec.priority`; **never** affects already-queued runs. |
| Live re-prioritize | **Needs a feasibility spike first** (see Risks) — Argo fixes priority at admission; verify a pending workflow's `spec.priority` patch actually re-orders the semaphore queue, else fall back to cancel + resubmit. |

## Layer 1 — Submit-time override + display

- **API:** add `priority` (int, optional) to `CreateRunRequest`; add `priority`
  to `Run` (list) and `RunDetail`.
- **Submitter** (`internal/runs`): resolve priority = request value → else
  scenario default (Layer 3 ConfigMap) → else baked `spec.priority`; set
  `wf.spec.priority`.
- **Model:** read `wf.spec.priority` back into `Run`/`RunDetail`.
- **CLI:** `dlh run <scenario> --priority N`.
- **UI:** a priority field on the Scenarios "Run" control (defaults to the
  scenario default); priority shown in the Runs list and the run-detail meta
  strip (the cell reserved by the UI spec).

## Layer 2 — Live re-prioritize pending runs

- **API:** `POST /api/runs/{id}/priority` (body `{priority}`) — **valid only
  while the run is Pending** (queued, not holding the slot); 409 otherwise.
- **Mechanism (pending spike):** patch the pending workflow's `spec.priority`;
  Argo re-evaluates semaphore release order. If the spike shows Argo ignores a
  post-admission priority change, fall back to **cancel + resubmit** with the
  new priority (same id semantics where possible).
- **CLI:** `dlh runs reprioritize <id> <priority>` (or `--to-front`).
- **UI (Queue page):** move up/down, "to front", and **cancel** on each queued
  run; running runs are not reorderable.

## Layer 3 — Editable per-scenario defaults

- **Storage:** `dlh-scenario-priorities` ConfigMap (`<scenario>: <priority>`),
  read by the submitter when a run doesn't specify a priority; overrides baked
  `spec.priority`.
- **API:** `GET /api/scenario-priorities`, `PUT /api/scenario-priorities/{id}`
  (admin role). Response includes baked value + current override for
  "overridden / reset" display.
- **UI:** dedicated **Default priorities** admin page — per scenario: tier chips
  (Low/Normal/High/Urgent → 10/100/200/500) + numeric stepper, with
  `= baked default` / `overridden · reset` status. Auto-save with toast.
- Tiers are pure UI sugar over the underlying int; raw value always available.

## The Queue view (the mechanism, made visible)

- **API:** `GET /api/queue` → per semaphore key (`mysql`/`kafka`/`doris`):
  the **running holder** (id, scenario, priority, started) + the ordered
  **pending** runs (id, scenario, priority, submittedAt) + slot count.
- **UI page (`Queue`, new nav item):**
  - A scannable **rules strip**: *1 slot per target type · releases by priority
    (high→low, then oldest) · types run in parallel.*
  - One **lane per target type**: a **Running** section (live dot + optional
    progress) and a **Queued · release order** section (rank `#1…`, NEXT badge,
    wait context, priority, reorder/cancel controls). **Idle** lanes render a
    calm empty state.
  - Polls live (5s), consistent with the Runs page.

## Roles / RBAC

- Submit with priority + reprioritize: **runner**.
- Edit default priorities: **admin**.
- View Queue: **viewer**.

## Risks / feasibility spike (do first)

**Live re-prioritize (Layer 2) is the one unknown.** Argo assigns semaphore
order using workflow priority; whether patching `spec.priority` on an
already-admitted *pending* workflow causes re-ordering at the next slot release
must be verified against the pinned Argo version before building the UI. If it
doesn't hold, Layer 2 uses cancel + resubmit. **Spike before committing the
reprioritize endpoint + UI.**

## Files touched (anticipated)

**Backend:** `api/openapi.yaml` (priority on CreateRunRequest/Run/RunDetail; new
`/api/queue`, `/api/runs/{id}/priority`, `/api/scenario-priorities`),
`internal/runs` (submit priority resolution), `internal/api/handlers.go` +
new queue/priority handlers, `internal/model/types.go` (priority + pending),
`internal/config` (scenario-priorities CM name), `cmd/dlh` (`--priority`,
`queue`, `reprioritize`), chart (`dlh-scenario-priorities` ConfigMap + RBAC for
cronworkflows/workflows patch).

**Frontend:** new `web/src/pages/QueuePage.tsx` + `DefaultPrioritiesPage.tsx`,
priority field on `ScenariosPage` Run control, priority display on
`RunsPage`/`RunDetailPage`, `web/src/lib/` helpers (queue ordering, tier
mapping) — Vitest-tested.

## Testing

- **Vitest:** tier↔number mapping; queue release-order sort (priority desc,
  oldest first); reorder math.
- **Go:** submit priority resolution (request > default > baked); `/api/queue`
  grouping + order; reprioritize Pending-only guard; scenario-priorities CRUD +
  RBAC.
- **Spike test:** prove Argo re-orders a pending workflow after a priority
  patch (or document the cancel+resubmit fallback).
- **Live (Playwright):** submit two same-type runs at different priorities →
  Queue shows correct order; reprioritize/to-front reorders; cancel removes;
  different types run in parallel; default-priority edit affects the next run.

## Out of scope

- Cross-target global priority / preemption of a *running* run (semaphore is
  per-type, 1 slot; we never preempt a holder).
- Queue-position estimates / ETAs beyond "behind #N".
- The run-detail UI optimization (companion spec).

## Suggested implementation phasing

1. **Layer 1** (submit + display) — foundational; everything else needs the API
   to know priority.
2. **Queue view** (read-only `/api/queue` + page).
3. **Layer 3** (default priorities) — independent admin surface.
4. **Layer 2** (live re-prioritize) — **after the spike**; highest risk.
