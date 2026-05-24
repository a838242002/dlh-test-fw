# Run-detail UI Optimization — Design Spec

*Design doc — 2026-05-25. Output of `superpowers:brainstorming`.*

> **Milestone context:** This is **Spec 2 of 2** in a controlplane enhancement
> pair. The companion **priority spec** (priority visibility & control —
> submit-time override, live re-prioritize, per-scenario defaults) is
> sequenced *after* this one. We do UI-first; priority *display* lands with
> that later spec. This spec only **reserves a layout slot** for it.

## Problem

A live UI walkthrough (Playwright against the running controlplane) surfaced
three real defects and one non-defect on the run-detail surface:

1. **Runs-list VERDICT column is dead** — every run shows "—" even when it
   passed. Root cause: `internal/model/types.go` builds each list `Run` but
   **never sets `Score`**, so it is always `null`; the frontend column derives
   pass/fail from `score` (1/0/null). The field was designed to carry the
   verdict — it was simply never populated.
2. **Run-detail step order is non-deterministic** — the step list reordered on
   *every* page load (observed live). Root cause: `types.go` ranges
   `wf.Status.Nodes`, a Go map, so iteration order is randomized.
3. **Live updates are broken** — `GET /api/runs/{id}/events` (SSE) returns
   **401**. `EventSource` cannot send an `Authorization` header and that route
   does not honor the auth-disabled bypass / has no header-less auth path.
4. **(Non-defect) theme** — a full-page screenshot rendered the page light, but
   direct load *and* click-through both render correctly dark. This was a
   Playwright `fullPage` capture artifact, **not** a user-facing bug.

Additionally, run-detail verdict threshold values render raw (`3.00e-6`,
`0.2961`) with no units — hard to read.

## Goal

Make the run-detail (and the closely-coupled Runs-list verdict column) read
accurately and clearly: a populated verdict column, deterministically-ordered
steps shown as a scannable timeline, working live updates, and human-readable
verdict values — without introducing new backend dependencies.

## Decisions (from brainstorming)

| Question | Decision |
|---|---|
| Scope | Verdict rendering (list + detail), steps timeline, SSE live updates, optional theme hardening. Priority display is **out** (later spec); reserve a meta-strip slot. |
| Runs-list verdict source | **MinIO `report.json` `overall`, cached per workflow.** NOT VictoriaMetrics — the controlplane has no VM client today, and VM gauges carry a 5-min staleness caveat. MinIO reports are immutable + authoritative + cacheable. |
| Steps rework ambition | **Timeline bars** — chronological + a duration bar per step scaled to the run window (load‖chaos overlap visible), phase via icon/color. |
| SSE auth | Honor `DLH_AUTH_DISABLED` on the events route (local-dev), and accept the token via **query param** (`?access_token=`) for authenticated deployments. |
| Theme | Downgrade to a **minor defensive hardening** (root `html/body` background) — not a confirmed bug. |

## Work item 1 — Verdict rendering

### 1a. Runs-list column (backend + frontend)

**Backend** (`internal/api/handlers.go`, `internal/model/types.go`,
`internal/minio/reports.go`):
- In the list path, resolve each run's verdict and set `score` to `1.0` /
  `0.0` / `nil` from the MinIO report's `overall`, reusing the existing
  `ReportReader` (already used by the detail handler).
- **Cache** keyed by workflow name (`map[string]float64` + `sync.RWMutex`, or a
  small bounded LRU). A finished run's report is immutable → cache permanently.
  Running/pending runs have no report → return `nil` and **do not cache**, so
  the verdict resolves once the run finishes.
- Errors (MinIO unreachable, no object) → `nil` + log; **never fail the list**.

**Data flow:** `GET /api/runs` → list Workflows → per run: `cache.Get(wf)`; on
miss *and* terminal phase → `ReportReader.Read(wf)` → cache `overall` → set
`score`.

**Cost:** Runs page polls ~5s. With the cache, each finished run is read once
then O(1); steady state is cheap.

**Frontend:** none required — the existing Runs column already derives from
`score`; populating it lights the column up. Verify the
`score → pass/fail/—` mapping in `web/src/lib/run.ts`.

### 1b. Run-detail value formatting (frontend)

A pure formatter in `web/src/lib/verdict.ts`, unit inferred from metric name:
- `*latency*` / `*duration*` → time (`3.00e-6 s` → `3µs`; bound `< 1` → `< 1s`).
- `*rate*` / `*error*` → percent (`0.2961` → `29.6%`; `< 0.5` → `< 50%`).
- fallback → `toPrecision(3)` (no scientific notation in normal ranges).

Vitest-tested against the real metric names (`p95-latency-chaos`,
`error-rate-recovery`). `RunDetailPage.tsx`'s threshold table calls it for
`value` and `bound`.

## Work item 2 — Steps timeline

### 2a. Deterministic order (backend)
In `internal/model/types.go`, after collecting steps from `wf.Status.Nodes`,
`sort.Slice` by `StartedAt` ascending (nil-safe: nil sorts last), tiebreak by
`Name`. Group `[n]` nodes remain filtered (frontend `isGroupNode`, unchanged).
Go-tested: a fixed set of nodes always yields the same order.

### 2b. Timeline rendering (frontend)
Replace the plain table in `RunDetailPage.tsx` with a timeline. Pure layout
math in `web/src/lib/steps.ts` (Vitest):
- run window = `[min(startedAt), max(finishedAt|now)]`.
- per step: `offsetPct = (start − windowStart)/windowSpan`,
  `widthPct = max(duration/windowSpan, minVisible)`.
- row: phase icon/color + name + start-offset label + duration + the bar.
Overlapping steps (load‖chaos) visibly overlap. Running steps render an
open-ended/animated bar.

## Work item 3 — SSE live updates (backend)

`internal/api/sse.go` + auth wiring (`internal/api/server.go`):
- The `/api/runs/{id}/events` route must honor `DLH_AUTH_DISABLED` (immediate
  local-dev fix).
- For authenticated deployments, accept the bearer token via query param
  (`?access_token=<jwt>`) since `EventSource` cannot set headers; validate it
  the same way header tokens are validated.
- Frontend: the run-detail `EventSource` URL appends `?access_token=` (the
  token the SPA already holds) when auth is enabled.
- Go-tested: events route returns 200 under `DLH_AUTH_DISABLED=true`, and with
  a valid `?access_token=` when auth is enabled; 401 only when neither.

## Work item 4 — Theme hardening (minor, optional)

Apply the dark default background to root `html`/`body` (e.g. in `index.css`)
rather than only a viewport-height wrapper, so long pages and screenshot
tooling never expose a white gap. ~1 line. Not a confirmed user bug.

## Run-detail layout: reserve priority slot

The meta strip (Scenario / Target / Started / Duration / Triggered by) gets a
**Priority** cell designed in now but populated by the later priority spec
(until then it renders "—" or is hidden when absent). No backend priority work
in this spec.

## Files touched

**Backend:** `internal/model/types.go` (step sort), `internal/api/handlers.go`
(+ list verdict enrichment + cache), `internal/minio/reports.go` (reuse;
optional cache layer), `internal/api/sse.go` + `internal/api/server.go` (events
auth), `internal/config/config.go` (only if a config knob is needed).
`api/openapi.yaml`: `score` already exists on the list item — just populated;
document the `?access_token=` query param on the events operation.

**Frontend:** `web/src/lib/verdict.ts` (value formatting), `web/src/lib/steps.ts`
(timeline math), `web/src/pages/RunDetailPage.tsx` (timeline + formatting +
priority slot), `web/src/lib/run.ts` / `RunsPage.tsx` (verify score→verdict
mapping), `web/src/index.css` (root bg).

## Testing

- **Vitest:** `lib/verdict.ts` (unit inference + precision), `lib/steps.ts`
  (offset/width math, overlap, running step).
- **Go:** step-sort determinism; list verdict enrichment + cache (hit/miss,
  terminal-vs-running, MinIO error → nil); SSE auth (disabled bypass,
  `?access_token=` accept/reject).
- **Live (Playwright) re-verification:** Runs column populated (pass/fail/—),
  run-detail steps ordered with scaled bars + readable verdict values, and a
  *running* run live-updates without a 401.

## Out of scope

- **Priority display & control** — the companion priority spec.
- The Scenarios catalog "building-block vs runnable" trap.
- Any VictoriaMetrics client / broader verdict-source redesign.
- Backend changes beyond what these four items require.
