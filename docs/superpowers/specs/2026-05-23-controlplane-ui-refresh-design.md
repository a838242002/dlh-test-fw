# Controlplane UI Refresh — Design

**Date:** 2026-05-23
**Status:** Approved (brainstorm)
**Scope:** `controlplane/web` (embedded React SPA). No backend API changes.

---

## Goal

Refresh the dlh-controlplane web UI with both **visual polish** and **UX
improvements** across all five pages. The current UI is functional but plain
(vanilla Tailwind, no design system, no icons), and has rough interaction
edges (`alert()`/`confirm()` dialogs, raw `JSON.stringify` error and verdict
display, static lists with no auto-refresh, full-page-reload navigation).

The audience is a **broader internal team** — engineers plus occasional
PMs/managers checking results — so the UI should be approachable and
presentable, desktop-first but tidy.

---

## Design decisions (locked during brainstorm)

| Decision | Choice |
|----------|--------|
| Layout | **Dashboard-forward**: summary stat cards on top, then lists. Top navigation. |
| Theme | **Dark by default**, with a **light toggle** (persisted per browser). |
| Accent | **Indigo** primary. Status colors stay semantic (green/blue/red/amber). |
| Tooling | **shadcn/ui** (Radix primitives copied into the repo) + **lucide-react** icons. |
| Stats source | **Computed client-side** from existing `/api/runs` + `/api/schedules`. No new endpoint. |
| Rollout | **Foundation-first**: design system + shell + primitives, then migrate pages one at a time. |

---

## 1. Tooling & theme system

Add to `controlplane/web`:

- **shadcn/ui** dependencies: Radix primitives (per component), `class-variance-authority`, `clsx`, `tailwind-merge`, `tailwindcss-animate`.
- **lucide-react** for icons.
- **Vitest** + React Testing Library (dev) for unit-testing pure logic.

Wiring:

- `@/*` path alias in `vite.config.ts` and `tsconfig.json` (`paths`), plus `components.json` for the shadcn CLI.
- `tailwind.config.ts` extended with shadcn's color tokens mapped to CSS variables; add the `tailwindcss-animate` plugin.
- `src/index.css` defines CSS variables for both themes under `:root` (light) and `.dark` (dark). Tokens: `--background`, `--foreground`, `--card`, `--border`, `--primary` (indigo), `--muted`, `--accent`, `--destructive`, plus **custom semantic status tokens** (`--status-success`, `--status-running`, `--status-failed`, `--status-pending`) tuned for legible contrast in **both** themes.

Theme behavior:

- A `ThemeProvider` reads `localStorage` (key e.g. `dlh-theme`), **defaults to dark**, and toggles the `.dark` class on `<html>`.
- A theme toggle control (sun/moon icon) lives in the top nav.

Build pipeline is unchanged: `make ui-build` runs `tsc -b && vite build` and
copies `web/dist` → `internal/api/dist` (consumed by `go:embed`). The bundle
grows (Radix); acceptable for an internal tool. **CI invariant:** `tsc -b &&
vite build` must stay green, and `go build` (which embeds `dist`) must succeed.

## 2. App shell & shared primitives

**Shell** (`App.tsx`), preserving the existing `/api/auth/info` auth bootstrap
(fake token when `authDisabled` is true, otherwise a session token from
`localStorage`):

- Top nav with **active-link highlighting** via react-router `NavLink`.
- Theme toggle + identity display (email from the token/`/api/auth/info`).
- A mounted **Toaster** (shadcn Sonner) for app-wide notifications.

**shadcn primitives** generated into `src/components/ui/`: Button, Card,
Table, Badge, Dialog, AlertDialog, Select, Input, Skeleton, Tooltip, Sonner
(toasts), DropdownMenu.

**Custom components** in `src/components/`:

- `StatusBadge` — maps a workflow phase to a status-token variant (replaces the existing color-map component).
- `StatCard` — a dashboard summary tile (label + big value + accent).
- `PageHeader` — title + optional action slot.
- `EmptyState` — icon + message + optional hint (used by every list).
- `ErrorState` — friendly message with an expandable details section (replaces raw `JSON.stringify`).
- `VerdictView` — readable verdict rendering (see Run detail).

## 3. Per-page changes

All five pages keep their current routes and data sources; only presentation
and interaction change.

### Runs — landing (`/` and `/runs`)
- Four `StatCard`s computed client-side: **pass rate (7d)**, **runs today**, **running now**, **active schedules** (the last from `/api/schedules`).
- Runs in a shadcn `Table` with `StatusBadge` and formatted score.
- **Polling auto-refresh** (~5s) with a "live · refreshed Ns ago" indicator.
- Skeleton rows while loading; `EmptyState` when no runs; `ErrorState` on failure.
- Row → run detail via client-side navigation (`useNavigate`), not full reload.

### Scenarios (`/scenarios`)
- shadcn `Card` grid.
- TargetPicker reimplemented on shadcn `Select`.
- Run button with loading state; on submit, **toast** success/error and **client-side navigate** to the new run (replaces `window.location.href`).

### Targets (`/targets`)
- shadcn `Table`; `Badge` for configured ✓/✗.
- Test-connection result shown inline **and** as a toast.
- `EmptyState` referencing `docs/operations/register-target.md`.

### Schedules (`/schedules`)
- shadcn `Table`; active/paused as badges.
- Create form moves into a **Dialog** (replaces the inline toggle block).
- Pause/resume/delete use an **AlertDialog** confirmation (replaces `confirm()`) plus toasts.

### Run detail (`/runs/:id`)
- Header: live `StatusBadge` (existing SSE retained), target, and a `triggeredBy` link via react-router `Link`.
- Steps rendered as a clean table.
- **`VerdictView`**: a pass/fail banner driven by the verdict's `overall` field, a thresholds table (metric / value / bound / result), and the raw JSON tucked behind a "view raw" collapsible (replaces the raw `<pre>` dump).

## 4. Cross-cutting UX rules

Applied during each page migration:

- Every `alert()` → toast.
- Every `confirm()` → `AlertDialog`.
- Raw `JSON.stringify(error)` → `ErrorState`.
- `window.location.href` navigation → `useNavigate` / `Link`.
- `"Loading…"` text → `Skeleton` components.
- Lists poll for freshness; **SSE remains only on Run detail**.
- Desktop-first but responsive: stat cards stack and tables scroll horizontally on narrow widths.

## 5. Implementation phasing (foundation-first)

1. shadcn + theme tokens + path-alias/build wiring (build stays green).
2. App shell (nav, active links, theme toggle, Toaster) + shared primitives.
3. Migrate **Runs** + stat cards.
4. Migrate **Scenarios**.
5. Migrate **Targets**.
6. Migrate **Schedules**.
7. Migrate **Run detail** + `VerdictView`.
8. Extract pure logic — `computeStats(runs, schedules)` and `parseVerdict(report)` — into standalone modules with **Vitest** unit tests.

Each phase is a self-contained, reviewable commit on a stable base, matching
the repo's atomic-commit conventions.

## 6. Testing strategy

- **Build gate:** `tsc -b && vite build` must pass; `go build` must embed the new `dist` successfully.
- **Unit tests (Vitest):** cover the pure data logic that has real behavior — `computeStats` (pass-rate window, "today" boundary, running count) and `parseVerdict` (overall pass/fail, threshold extraction, malformed input). Presentational shadcn components are not unit-tested.
- Existing Go test suites remain untouched and green.

## 7. Non-goals (YAGNI)

- No new backend endpoints (stats are client-side).
- No real OIDC login UI flow — the existing auth bootstrap is unchanged.
- No mobile-first layout or PWA.
- No time-series charts/graphs — summary stats are numbers only.
- No internationalization.

## 8. Risks & considerations

- **shadcn + Tailwind v3 integration** with the existing Vite config and the `go:embed` pipeline must be verified early (phase 1) — path aliases and the CSS-variable theme must not break `make ui-build`.
- **Dark-mode contrast** for status colors must remain legible; tune the status tokens explicitly rather than reusing light-theme values.
- **Bundle size** increases with Radix; accepted for an internal tool, but keep imports per-component (no barrel imports) to avoid bloat.
