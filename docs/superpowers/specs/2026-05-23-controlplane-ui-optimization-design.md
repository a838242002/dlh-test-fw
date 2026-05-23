# Controlplane UI Optimization — Design

**Date:** 2026-05-23
**Status:** Approved (brainstorm)
**Scope:** `controlplane/web` (all pages) + a full-stack Run-detail deep-linking feature (`api/openapi.yaml`, `controlplane/internal/...`, `controlplane/deploy/`). Builds on the shadcn refresh from `2026-05-23-controlplane-ui-refresh-design.md`.

---

## Goal

A second, deeper optimization pass over the controlplane UI — visual polish, information density, in-page layout restructure, and interaction UX — across **Runs, Scenarios, Targets, Schedules, and Run detail**. Plus a new capability the first pass didn't cover: **Run detail deep-links out to the Argo Workflows UI and Grafana**. Design fidelity and interactions are checked with **Playwright (dev-time, via MCP)** — no test files or dependencies are added to the repo.

The first refresh made the UI consistent (shadcn primitives, dark+indigo theme). This pass makes it *good*: tighter, denser, more navigable, and connected to the tools operators actually drill into.

---

## Decisions (locked during brainstorm)

| Decision | Choice |
|----------|--------|
| App shell | **Refined top nav** (keep horizontal nav; add icons, tighten density, widen container). No sidebar. |
| Scenarios layout | **Grouped by category** with section headers + counts, richer cards, search across all. |
| Runs table | Add a filter bar (**search + status + category + time-range + "failed only" toggle**) + sort; replace always-empty **Score** with **Duration** + **Verdict** columns. |
| Run detail | **Full redesign** (not just polish): summary header, inline meta strip, verdict-first, cleaned step list. |
| Deep-linking | **Backend-assembled** Argo + Grafana links, driven by configurable base URLs; UI hides buttons when unset. Grafana = **run dashboard + per-target-type dashboard** (mysql/kafka/doris). |
| Playwright | **Dev-time verification only** (MCP). No committed E2E suite, no new deps. |
| Timestamps | **Relative** (`2h ago`) with absolute on hover, everywhere. |

---

## Workstream 1 — Web UI refresh (`controlplane/web` only)

No backend/API changes. Gated by `pnpm build` + `pnpm test` (for new pure logic) + Playwright visual check.

### 1.1 Cross-cutting
- **Top nav:** a lucide icon beside each label (Runs/Scenarios/Targets/Schedules), tighter vertical padding, keep the dark+indigo identity, theme toggle, and identity. Active state uses the indigo soft-bg pill + keeps the existing underline behaviour or replaces it — implementer's choice as long as the active route is unambiguous.
- **Container width:** widen from `max-w-6xl` to `max-w-7xl` so content stops floating in a narrow column.
- **Density:** reduce oversized paddings on stat cards and panels; let primary tables/grids fill the viewport instead of leaving the lower 60% empty.
- **Relative time helper:** a small pure function `relativeTime(iso)` → `"2h ago"` etc., with the absolute timestamp rendered as the element's `title`. Unit-tested with Vitest. Used by Runs, Schedules, Run detail.
- **Category system:** a pure `deriveCategory(id)` helper (see §1.3) → `{ key, label, icon, colorVar }`, reused by Scenarios and (for the header icon) Run detail. Unit-tested.

### 1.2 Runs page
- **Compact stat cards:** smaller, each with a small leading icon; same four metrics (pass rate 7d, runs today, running now, active schedules).
- **Filter bar:** a search input (matches scenario substring) + three `Select`s — **status**, **category** (chaos/fixture/load/verdict/util, via `deriveCategory`), **time range** (all / last 24h / last 7d) — plus a **"Failed only"** quick toggle (highlighted in the failed color when on). Filters compose (AND). Sortable columns (at least Started + Duration). All filter + sort logic extracted to a pure, Vitest-tested module (`filterRuns(runs, criteria)` / `sortRuns`).
- **Columns:** Scenario · Target · Status · Started (relative) · **Duration** (`finishedAt − startedAt`, `—` if not finished) · **Verdict** (✓ pass / ✗ fail chip, shown only when a verdict exists; `—` otherwise). The old always-`—` Score column is removed.
- Keep 5s polling + "live · updated Ns ago".

### 1.3 Scenarios page (grouped)
- **Sections by category**, ordered **chaos → fixture → load → verdict → util → other**, each with a count badge. Empty categories (after search) are hidden.
- **Richer cards:** colored category icon, scenario id, a derived **target-type label** (`deriveTargetType(id)` → `mysql`/`kafka`/`doris`/`generic`; the API exposes no `targetType` field, so it's derived — a TS helper mirroring the Go one in §2.3, Vitest-tested), TargetPicker + Run button (with a play icon). Descriptions are omitted (the API returns none today).
- **Search** filters across all scenarios; sections with no matches disappear.
- **Category derivation ruleset** (`deriveCategory`, pure + tested) — prefixes are unreliable, so:
  1. Prefix match: `fixture-`→fixture, `util-`→util, `load-`→load, `verdict-`→verdict, `chaos-`→chaos.
  2. Keyword fallback for unprefixed chaos scenarios — id contains `pod-delete`, `network-loss`, or `broker-partition` → chaos (covers `mysql-pod-delete`, `kafka-broker-partition`, `doris-be-network-loss`).
  3. Fallback → `other` (rendered last). 
- Category metadata (label, icon, color token) lives in one table reused by the page.

### 1.4 Run detail page (full redesign)
- **Summary header:** back-link to `/runs`, category icon, run id (monospace), live `StatusBadge` (existing SSE retained).
- **Inline meta strip** (one bordered row, not a wasteful card): Scenario · Target · Started (relative, hover absolute) · Duration · Triggered by (`manual` or schedule link via existing `triggeredBy`).
- **Verdict moved up** (it's the outcome), using the existing `VerdictView`, with **numeric value formatting** — large/small floats rendered readably (e.g. `3.50e-6`, `0.2956`) instead of `0.0000034999847412105`. Threshold table + collapsible raw JSON retained.
- **Steps cleaned up:** hide Argo DAG/step-group placeholder nodes whose names are bracketed indices (`[0]`, `[1]`, …); render only the real named steps, each with a status icon + phase (+ per-step duration when available). The panel header notes hidden group nodes. The "real named step" filter is a small pure, tested predicate.
- **Deep-link buttons** in the header — see Workstream 2 (rendered only when the backend supplies the URLs).

### 1.5 Targets page
- Refreshed table styling, icons, tighter spacing; the already-good empty state stays.
- **Configured state** as a colored badge (`✓ configured` / `✗ missing`).
- **Test-connection UX:** a per-row **Test** button → disabled "Testing…" state → result shown **inline** in the row as a badge (`OK · 42 ms` / `unreachable`) **and** as a toast. The inline badge persists after the toast dismisses so the last result stays visible.

### 1.6 Schedules page
- Refreshed table styling, relative `Last Fired`, active/paused as status pills, per-row pause↔resume toggle + delete.
- **Create** via a `Dialog` (modal) with fields id / scenario / target (optional) / cron / timezone (optional) and Cancel + Create actions; closes and refreshes on success, toast on success/error.
- **Delete** via an `AlertDialog` confirm ("Delete schedule "{id}"? … In-flight runs are not affected.") with a destructive Delete action; toast on completion. Pause/resume toast as well.

---

## Workstream 2 — Run detail deep-linking (full-stack)

A coherent, separable feature: Run detail links out to the Argo Workflows UI and Grafana. Backend assembles the URLs (it owns the namespace, workflow name, the `dlh_scenario` label convention, and dashboard UIDs — the frontend must not hardcode these). Gated by `go test ./...` + `make build` + `pnpm build` + Playwright.

### 2.1 Config (Go)
- Add two optional env knobs to `controlplane/internal/config`:
  - `DLH_ARGO_BASE_URL` (e.g. `https://argo.example.com`)
  - `DLH_GRAFANA_BASE_URL` (e.g. `https://grafana.example.com`)
- Both default empty. Empty = that link is omitted (feature degrades gracefully; local minikube with no stable host simply shows no buttons unless set to a port-forward URL).

### 2.2 API contract (`api/openapi.yaml` — regenerated, not hand-edited)
- `RunDetail` gains `argoUrl?: string`. `grafanaUrls?: [{ label, url }]` already exists.
- Regenerate Go types (oapi-codegen) **and** TS types (openapi-typescript) via `make codegen` / `make ui-build` — per repo convention, handlers' request/response types are never hand-edited.

### 2.3 URL assembly (Go, in the RunDetail builder — `internal/model/types.go` + `internal/api/handlers.go`)
- **Argo** (when `DLH_ARGO_BASE_URL` set): `argoUrl = {ArgoBaseURL}/workflows/{namespace}/{workflowName}`.
- **Grafana** (when `DLH_GRAFANA_BASE_URL` set): append one or two entries to `grafanaUrls`, each `{ label, url }`:
  1. **Run dashboard** (always): `{GrafanaBaseURL}/d/dlh-run/dlh-run?var-dlh_scenario={scenario}&from={startMs}&to={endMs}`, label `"Run dashboard"`.
  2. **Per-target-type dashboard** (only when the type is recognized): `{GrafanaBaseURL}/d/{uid}/{uid}?var-dlh_scenario={scenario}&from={startMs}&to={endMs}`, label e.g. `"MySQL dashboard"`. UID map: `mysql→dlh-mysql`, `kafka→dlh-kafka`, `doris→dlh-doris`.
  - `startMs` = `startedAt` epoch ms; `endMs` = `finishedAt` epoch ms, or `now` if the run hasn't finished. (Grafana `from`/`to` accept epoch ms.)
- **Target-type derivation** (`deriveTargetType`, pure + Go-tested): from the scenario id — contains `mysql`→mysql, `kafka`→kafka, `doris`→doris, else none (no per-type dashboard). Covers `mysql-pod-delete`, `kafka-broker-partition`, `doris-be-network-loss`, `fixture-minio-load-mysql`, etc.
- URL construction (Grafana query) and `deriveTargetType` are small and pure → unit-tested in Go. The dashboard UIDs + the `dlh_scenario` var name live in one constant block (they couple to `dashboards/grafana/` — FINDINGS #1, #8).

### 2.4 Deploy (`controlplane/deploy/deployment.yaml`)
- Add the two env vars to the controlplane Deployment, sourced so they can be set per-environment (prod ingress hosts; local left empty or pointed at a port-forward). Document the knobs in the relevant runbook/`CLAUDE.md`.

### 2.5 Frontend
- Run detail header (right side) renders an **"Argo"** button (when `argoUrl` present) and one ghost button per `grafanaUrls` entry using its `label` (e.g. **"Run dashboard"**, **"MySQL dashboard"**), each with an external-link icon, opening in a new tab. When the backend supplies neither field, no buttons render — no empty affordance.

---

## Verification (Playwright MCP, dev-time)

After each page migration:
- Port-forward minikube (`kubectl -n dlh-test-fw port-forward svc/dlh-controlplane 18080:80`), navigate, screenshot at **1440px** and one **narrow** width (responsive sanity).
- Exercise interactions: nav active state, theme toggle persistence across reload, Scenarios search/group filtering, Runs filter bar (search + status + category + time range + failed-only) + sort, Run detail steps (group nodes hidden) + formatted verdict, Targets test-connection (inline result badge), Schedules create-`Dialog` + delete-`AlertDialog`.
- **Populated states:** create a throwaway schedule via the UI to verify the populated Schedules table. For deep-link buttons, set `DLH_ARGO_BASE_URL`/`DLH_GRAFANA_BASE_URL` to dummy values in the minikube Deployment to verify the buttons render and point at the right URLs, then verify the unset path hides them.
- **Limitations:** the live cluster has no registered **Targets**, so the populated Targets table is verified by build + data shape, not end-to-end. Argo/Grafana destinations themselves aren't reachable in local minikube — verification confirms the *links are correct and rendered*, not that the destination loads.

---

## Testing strategy

- **Vitest** (new pure logic, web): `relativeTime`, `deriveCategory`, `deriveTargetType`, runs filter (status/category/time-range/failed-only compose) + sort, the named-step predicate, verdict value formatting.
- **Go tests** (Workstream 2): config parsing of the two URLs; `deriveTargetType`; and the URL-assembly functions (Argo path + Grafana run/per-type queries with epoch-ms window + `dlh_scenario` var + correct dashboard UIDs).
- **Build gates:** `pnpm build` (web), `make ui-build && make build` (embed), `go test ./...`.
- **Playwright:** dev-time visual + interaction check per the section above. Not committed.

---

## Implementation decomposition

This spec produces **two plans** (repo convention: one plan per executable unit, multiple plans per spec is fine):
1. **Web UI refresh** — Workstream 1 only; `web/` changes; gated by `pnpm build` + `pnpm test` + Playwright.
2. **Run detail deep-linking** — Workstream 2; `api/openapi.yaml` + Go + `controlplane/deploy/` + the frontend buttons; gated by `go test` + `make build` + Playwright. Depends on Plan 1's Run-detail redesign existing (the header action area).

---

## Non-goals (YAGNI)

- No sidebar/shell rewrite (top nav stays).
- No committed Playwright suite or new test dependencies.
- No new backend **endpoints** (deep-linking enriches the existing `RunDetail`; it does not add routes).
- Grafana links cover the **run dashboard + per-target-type dashboard** only; no history/overview dashboards or arbitrary panel deep-links.
- No charts/graphs embedded in the UI, no mobile-first layout, no i18n.

---

## Risks & considerations

- **Category and target-type derivation** are both heuristic (scenario names, not typed fields). Each ruleset must be unit-tested with the real scenario ids — `deriveCategory` falls back to `other`, `deriveTargetType` falls back to `generic`/"no per-type dashboard" — rather than mis-bucket. `deriveTargetType` is implemented **twice** (TS for the Scenarios card label, Go for the Grafana dashboard mapping) because the stack is split; keep the two rule lists identical and tested. If the API later exposes a real category/`targetType`, prefer it over the heuristics and collapse the duplication.
- **Deep-link base URLs differ per environment.** Local minikube has no stable Argo/Grafana host; the feature must degrade to "no buttons" cleanly when unset. Document the env knobs so prod/GitOps sets them.
- **Grafana dashboard contract:** the link assumes the `dlh-run` dashboard UID and the `dlh_scenario` template variable (FINDINGS #1). If the dashboard UID/var changes, the link breaks silently — keep the UID/var in one constant and note the coupling.
- **Step filtering** must not hide a genuinely-named step that happens to look bracketed; the predicate targets only pure `[<int>]` names. Verify against a real multi-step run (e.g. `mysql-pod-delete`).
