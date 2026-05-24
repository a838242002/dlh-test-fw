# Run-detail UI Optimization — Design Spec

*Design doc — 2026-05-25. Output of `superpowers:brainstorming`, refined against an interactive HTML prototype reviewed in-browser.*

> **Milestone context:** This is **Spec 2 of 2** (UI-first). The companion
> **priority spec** (`2026-05-25-controlplane-priority-design.md`) is sequenced
> after this one. Priority *control* is out of scope here; this spec only
> **displays** a run's priority in the meta strip (read-only) and reserves the
> cell — the priority spec wires the data + controls.

## Problem

A live UI walkthrough (Playwright against the running controlplane) surfaced
real defects on the run-detail surface:

1. **Runs-list VERDICT column is dead** — every run shows "—" even when it
   passed. `internal/model/types.go` builds each list `Run` but **never sets
   `Score`**, so it is always `null`; the frontend column derives pass/fail
   from `score`. The field was designed to carry the verdict, never populated.
2. **Run-detail step order is non-deterministic** — the step list reordered on
   *every* page load (observed live). `types.go` ranges `wf.Status.Nodes`, a Go
   map → randomized order.
3. **Live updates broken** — `GET /api/runs/{id}/events` (SSE) returns **401**
   (`EventSource` can't send an `Authorization` header and the route ignores
   the auth-disabled bypass).
4. **No sense of *what a scenario is*** — the UI shows only the id
   (`mysql-pod-delete`); nothing says what it does. Scenarios carry no
   machine-readable description (only source comments + `slo_name` params).
5. **Verdict values are raw** — `3.00e-6`, `0.2961` with no units.

(A suspected theme bug turned out to be a Playwright `fullPage` screenshot
artifact, not user-facing — downgraded to optional hardening.)

## Goal

Make run-detail (and the coupled Runs-list verdict column) read accurately and
clearly — a populated verdict column, deterministic steps shown as a
window-aware timeline, working live updates, human-readable verdict values, and
a plain-language sense of what each scenario does — **without new backend
dependencies**.

## Decisions (settled in brainstorming + prototype review)

| Topic | Decision |
|---|---|
| Runs-list verdict source | MinIO `report.json` `overall`, **cached per workflow** (immutable once finished). NOT VictoriaMetrics (no VM client today; gauge staleness caveat). |
| Steps treatment | **Timeline bars** + a dedicated **Verdict-windows lane** (chaos/recovery) + axis/gridlines. |
| SSE auth | Honor `DLH_AUTH_DISABLED`; accept token via `?access_token=` query param. |
| Scenario description | **Curated annotation per scenario + auto-derived fallback.** |
| Theme | Minor optional hardening (root `html/body` bg). Not a confirmed bug. |
| Priority cell | Display-only here; data + control land in the priority spec. |

## Work items

### 1. Verdict rendering

**1a. Runs-list column (backend).** In the list path, set each item's `score`
(`1.0`/`0.0`/`nil`) from the MinIO report's `overall`, reusing the existing
`internal/minio` `ReportReader`, **cached by workflow name** (`map`+`RWMutex`
or small LRU). Running/pending → `nil`, uncached (resolves once finished).
MinIO errors → `nil`+log, never fail the list. Frontend: none — the existing
score-based column lights up (verify `web/src/lib/run.ts` mapping).

**1b. Run-detail value formatting (frontend).** Pure formatter in
`web/src/lib/verdict.ts`, unit inferred from metric name: `*latency*`/
`*duration*` → time (`3.00e-6 s` → `3 µs`; bound `< 1` → `< 1 s`),
`*rate*`/`*error*` → percent (`0.2961` → `29.6 %`; `< 0.5` → `< 50 %`),
fallback → `toPrecision(3)`. Vitest-tested with the real metric names.

**1c. Verdict card layout (frontend).** Add a **Window** column to the
threshold table (`chaos` / `recovery` pills, from the report's `window`
field). Add a banner summary ("2 / 2 thresholds met"). Keep "View raw JSON".

### 2. Steps timeline

**2a. Deterministic order (backend).** In `types.go`, `sort.Slice` steps by
`StartedAt` asc (nil last), tiebreak `Name`. Group `[n]` nodes stay filtered.
Go-tested for stable order.

**2b. Timeline + windows lane (frontend).** Replace the plain table
(`RunDetailPage.tsx`) with a timeline; layout math in `web/src/lib/steps.ts`
(Vitest):
- run window = `[min(startedAt), max(finishedAt|now)]`; per step
  `offsetPct`/`widthPct` (min-visible floor).
- bar **colored by kind** (prep/util grey, load blue, **chaos amber — not red**,
  verdict indigo); phase via row icon.
- a **Verdict-windows lane** above the step rows: `chaos` (amber) and
  `recovery` (blue) as labeled segments on the same axis, sourced from the
  report's `chaos_window_*` / recovery window; **dashed boundary guides** drop
  through the step rows. This visually ties the timeline to the verdict's
  Window column. (No full-height tint — bars stay un-muddied.)
- **gridlines + a minute axis** (`0 → run duration`).

### 3. SSE live updates (backend)

`internal/api/sse.go` + `server.go`: the events route honors
`DLH_AUTH_DISABLED`; for authenticated deployments accept the token via
`?access_token=<jwt>` (validated like a header token). Frontend `EventSource`
URL appends `?access_token=` when auth is enabled. Go-tested (disabled bypass,
query-param accept/reject). Running runs then live-update.

### 4. Scenario description (“what is this scenario?”)

- **Source:** a curated annotation on each scenario WorkflowTemplate
  (`dlh.scenario/description`), exposed by the scenarios API and on run detail.
  **Auto-derived fallback** when absent: a generated summary from chaos type +
  target type + SLO (e.g. *"pod-delete chaos on a MySQL target, evaluated
  against the pod-delete SLO."*) so it is never blank.
- **Display:** one clean muted sentence under the run title (no chip, no
  technical tail — the chaos/SLO/target facts live in the meta strip).
- Surfaced on **run detail** now; the **Scenarios** cards can reuse the same
  field (follow-up, not required here).

### 5. Header + meta strip (frontend)

- **Title leads with the scenario name** (`mysql-pod-delete`); the full
  timestamped run id becomes a mono/copyable subtitle.
- **Meta strip de-duped + grouped:** drop the redundant Scenario cell (it's the
  title); merge **Chaos · SLO** into one cell; group scenario-definition facts
  (Target, Chaos·SLO) from run-instance facts (Started, Duration, Triggered
  by). Add a **Priority** cell (display-only; populated by the priority spec —
  renders "—"/hidden until then).

### 6. States (frontend)

- **Failed run:** FAIL verdict banner; the **failing threshold highlighted**;
  failed steps show their `message` (the model already carries it) inline.
- **Chaos-only run (no verdict):** friendly "No verdict — chaos-only run"
  instead of an empty card.
- **Running run:** Running badge + live indicator; in-progress steps render an
  open-ended/animated bar; window end = now.

### 7. Theme hardening (minor, optional)

Dark default background on root `html`/`body` rather than only a
viewport-height wrapper. ~1 line. Not a confirmed bug.

## Files touched

**Backend:** `internal/model/types.go` (step sort), `internal/api/handlers.go`
(list verdict enrichment + cache), `internal/minio/reports.go` (reuse; optional
cache), `internal/api/sse.go` + `server.go` (events auth), scenarios handler +
WorkflowTemplate annotations (description), `api/openapi.yaml` (populate
`score`; document `?access_token=`; add scenario `description`).

**Frontend:** `web/src/lib/verdict.ts` (formatting), `web/src/lib/steps.ts`
(timeline + windows math), `web/src/components/VerdictView.tsx` (window column +
summary), `web/src/pages/RunDetailPage.tsx` (title, description, meta strip,
timeline, states), `web/src/lib/run.ts` / `RunsPage.tsx` (verify mapping),
`web/src/index.css` (root bg).

## Testing

- **Vitest:** `lib/verdict.ts` (unit inference/precision), `lib/steps.ts`
  (offset/width, window segments, overlap, running).
- **Go:** step-sort determinism; list verdict enrichment + cache; SSE auth;
  scenario description (curated + derived fallback).
- **Live (Playwright):** verdict column populated; run-detail steps ordered with
  window-lane + readable values; a running run live-updates (no 401); a failed
  run shows the failing threshold + step message.

## Out of scope

- **Priority control** (companion spec) — only the read-only meta cell here.
- Scenarios catalog "building-block vs runnable" trap.
- VictoriaMetrics client / verdict-source redesign.
