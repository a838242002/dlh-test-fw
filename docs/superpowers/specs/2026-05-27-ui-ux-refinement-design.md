# Controlplane UI/UX Refinement — Design Spec

*Design doc — 2026-05-27. Output of `superpowers:brainstorming`, refined against an interactive HTML prototype ("Direction A") reviewed page-by-page in Playwright.*

> **Milestone context:** A presentation-layer refresh of the existing embedded React UI
> (`controlplane/web`). No backend/API changes. Follows the priority feature
> (Plan 2026-05-26) which added the Queue + Default-priorities pages.

## Problem

The controlplane UI is functional but its visual craft is uneven:

- The base is a slightly **blue-tinted navy** (`#0b1020`) — everything reads faintly purple.
- **Boxy, equal stat cards** with bare numbers and no secondary context.
- **Bare-text statuses/verdicts** in places; inconsistent chip treatment.
- **Description blocks** (page subtitles, scenario card text, run-detail description, the Queue rules strip) are inconsistent and under-styled.
- Some pages under-inform: **Targets** is a bare read-only list; **Scenarios** cards lack description/last-run context; **Queue** lanes don't show slot usage.

## Goal

A comprehensive refinement across **all 7 pages** — a refreshed visual identity
("Direction A") applied **consistently**, plus per-page information-architecture /
density improvements — **without changing functionality**. All four drivers are in
scope (confirmed in brainstorming): visual consistency, per-page weak spots,
IA/density, and visual identity.

## Scope

- **Pages:** Runs (dashboard), Run detail, Scenarios, Queue, Targets, Schedules, Default priorities.
- **Foundation first:** establish the design language (tokens + shared components),
  then apply per-page.
- **Presentation-layer only.** No REST/OpenAPI changes, no submitter/handler changes.
  Existing Vitest-tested pure logic (`web/src/lib/{time,category,run,runsFilter,steps,format,stats,verdict,tier}.ts`)
  is unchanged in behavior.

**Prototype:** the direction was agreed against a throwaway HTML prototype
(Direction A, mock data, reviewed in Playwright). That prototype is **ephemeral**
— this spec (the token tables + per-page sections below) is the **durable visual
contract**. The real implementation renders the same patterns from the live API /
generated client via the vendored shadcn primitives.

---

## Design language — "Direction A"

### Surfaces (replace the tinted navy with a truer dark slate)

| Token | Value | Use |
|---|---|---|
| `ink-950` | `#0a0c12` | app background |
| `ink-900` | `#0e1118` | card / panel |
| `ink-850` | `#141822` | input / inset |
| `ink-800` | `#1a1f2b` | hover / chip |
| `ink-700` | `#252b3a` | strong hover / scrollbar |
| `line` | `rgba(255,255,255,0.08)` | hairline borders / rings |

### Accent + semantics (one confident accent; semantic state colors everywhere)

| Token | Value | Soft bg | Use |
|---|---|---|---|
| `accent` | `#7c8cff` (text `#aab4ff`) | `rgba(124,140,255,0.14)` | primary actions, active nav, NEXT/tier badges |
| `ok` | `#34d399` | `rgba(52,211,153,0.13)` | pass / succeeded |
| `warn` | `#fbbf24` | `rgba(251,191,36,0.13)` | pending |
| `bad` | `#fb7185` | `rgba(251,113,133,0.13)` | fail / error |
| `run` | `#60a5fa` | `rgba(96,165,250,0.13)` | running |

### Typography

- **Page title:** `text-xl font-semibold tracking-tight`.
- **Card / section title:** `text-base font-medium`.
- **Body:** `text-sm`.
- **Micro-label:** `text-xs font-medium uppercase tracking-wide text-slate-500`.
- **Numerics (IDs, durations, priorities, metric values):** monospace + `tabular-nums`.

### Components (vendored shadcn primitives, restyled — not replaced)

- **Status:** dot + pill, `bg-<sem>-soft text-<sem>`, `animate-pulse` dot when running. (Evolves `StatusBadge`.)
- **Verdict:** pass/fail chip with `✓ / ✗` icon (`VerdictView` / list cell).
- **Card:** `rounded-xl bg-ink-900 ring-1 ring-line`.
- **Stat row:** a single hairline-divided panel (`divide-x divide-line`), not 4 separate cards; primary metric (pass-rate) gains a **sparkline + trend**; every stat gets a **secondary context line**.
- **Buttons:** primary (`bg-accent text-ink-950`), secondary (`bg-ink-800 ring-1 ring-line`), ghost (`hover:bg-ink-800`).
- **Inputs:** `bg-ink-850 ring-1 ring-line focus:ring-accent`.
- **Tier chips:** a **segmented control** (joined buttons), active = `bg-accent text-ink-950`.
- **Nav:** pill nav with a logo mark; active item = `bg-accent-soft`; theme-toggle chip. `max-w-7xl` content width.
- **Scenario / target icon chip:** target-type initials (`deriveTargetType`) in an `ink-800` rounded square.

### Description blocks (consistent treatment — explicit user request)

Four distinct "description" contexts, each with a defined treatment:

1. **Page subtitle** — `text-sm text-slate-500` under the page title; on **list pages** append a live count: Scenarios "· N available", Targets "· N registered", Schedules "· N active".
2. **Card description** (Scenarios) — `text-sm leading-relaxed text-slate-400`, 2-line clamp with a `min-h` so run-controls align across a row; target-type rendered as an uppercase chip separate from the description.
3. **Detail description** (Run detail) — a quiet left-accent block: `max-w-2xl border-l-2 border-accent/40 pl-3 text-sm leading-relaxed text-slate-400`.
4. **Help / rules band** (Queue) — leading `i` badge (`bg-accent-soft`) + the rule with **key terms emphasized** (`font-medium text-slate-200`: "1 slot", "priority", "in parallel").

### Light theme

The app already ships a light toggle (`src/lib/theme.tsx`, persisted under
`localStorage["dlh-theme"]`). Direction A **must define light-mode equivalents**
for every token role above (surfaces, accent, semantics) so the toggle stays
first-class. Dark remains the default.

---

## Per-page refinements

**Runs (dashboard).** Hairline stat row (Pass rate · 7d with sparkline + trend;
Runs today "3 chaos · 9 load"; Running now with live dot + elapsed; Active
schedules "next in 41m"). Table: scenario icon chip, status dot-pill, verdict
chip, right-aligned mono Priority/Duration, row hover. Filter toolbar restyled.
Structure (stats, columns, filters) unchanged.

**Scenarios.** Cards grouped by derived category with a count badge. Each card:
icon chip, name, **target-type chip + default tier**, **2-line description**
(aligned heights), **last-run + verdict** line, then the inline run control
(priority input + target picker + Run). Search in the header.

**Queue.** One lane per target type. Lane header shows a **slot indicator
(`n/1 slot`)**. Running section (live dot + `p<priority> · <elapsed>`), Queued ·
release order (rank, NEXT badge, `p<priority>`, to-front ↑ + cancel ✕). Idle
lanes render a dashed empty state. Refined rules band (see description blocks).

**Targets.** Cards per registered target (incl. `local`): name + mono id,
**reachable/unreachable status pill**, and a stat row (**Type / Latency / Runs**),
plus a Test-connection action. Replaces the current bare read-only list. Empty
state when none registered.

**Schedules.** Table: active/paused dot + name, scenario chip, **mono cron**,
target, priority (mono right), next, last verdict chip, and pause/resume +
overflow actions. Create moves to a **button → modal/drawer form** (see Behavior
decisions).

**Default priorities.** Table: scenario (icon chip), **tier segmented control**
(Low/Normal/High/Urgent = 10/100/200/500), effective value input, and override
status ("overridden · baked N" / "= baked default (N)"). **Auto-save** (see
Behavior decisions).

**Run detail.** Title row (icon chip + name + status pill + deep-link buttons:
Argo / Run dashboard / per-target dashboard). Mono run-id. Left-accent
description block. Hairline 6-cell meta strip (Target / Chaos·SLO / Priority /
Started / Duration / Triggered by). **Verdict-first** card (PASS/FAIL banner +
threshold table with value/bound/**window chips**/result). Steps card with the
**color legend** + timeline bars (chaos=warn, load=run, verdict=accent,
prep/util=slate).

---

## Behavior decisions (settled in brainstorming; recommended choices)

| Decision | Choice | Rationale |
|---|---|---|
| **Schedules create** | Button → **modal/drawer form** | Keeps the page a clean list; the form appears on demand instead of an always-open inline panel. |
| **Default priorities save** | **Auto-save** on tier-click and on input commit (blur/Enter), with a toast; no per-row Save button | Fewer clicks; the override status line gives immediate confirmation. PUT is idempotent and admin-gated. |

---

## Out of scope

- New features, pages, or routes; any backend/API/OpenAPI change.
- Changes to priority/queue/schedule **logic** (just-merged feature) — only its presentation.
- Data visualization beyond the pass-rate sparkline.
- The local-dev `dlh-roles` seeding follow-up (tracked separately).

---

## Implementation approach (existing codebase)

1. **Foundation:** update theme tokens — Tailwind config + the CSS variables in
   `web/src/index.css` / `web/src/lib/theme.tsx` (dark + light) — and restyle the
   vendored shadcn primitives in `web/src/components/ui/` (button, badge, card,
   input, select, table) to the Direction A patterns. Evolve `StatusBadge` and
   `VerdictView`; add small shared pieces (stat-row panel, icon chip,
   description-block wrappers, segmented tier control).
2. **Reference page:** apply the language to **Runs** first as the canonical
   example, verify, then roll out.
3. **Per-page application:** `web/src/pages/*` — apply layout/IA changes page by
   page, reusing `deriveTargetType`/`deriveCategory` for icon chips and grouping.
4. **Schedules create modal + Default-priorities auto-save** behavior changes.
5. Keep the generated API client and all data wiring as-is; this is styling +
   markup + small interaction changes only.

**Suggested plan decomposition** (for `writing-plans`): Plan 1 = design-system
foundation (tokens + light/dark + shared components + nav + Runs reference). Plans
2…N = per-page application (one plan per page or small batches). Each plan is
independently shippable and visually verifiable.

---

## Testing / verification

- **Pure logic:** existing Vitest suites unchanged; add tests only if new pure
  helpers are introduced (e.g. a count/label formatter).
- **Build gate:** `pnpm build` (tsc + vite) clean.
- **Visual:** Playwright walkthrough of each page against the prototype reference;
  assert 0 console errors; confirm light/dark toggle on every page.
- **No regressions:** the priority/queue/schedule flows verified in the prior
  milestone must still pass (re-walk the key interactions).
