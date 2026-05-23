# Controlplane UI Optimization — Plan 1 (Web UI refresh) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver Workstream 1 of `docs/superpowers/specs/2026-05-23-controlplane-ui-optimization-design.md` — a web-only refresh of all controlplane pages (refined nav, denser Runs with a filter bar + Duration/Verdict columns, grouped Scenarios, redesigned Run detail, Targets inline test-result, Schedules polish).

**Architecture:** Pure-logic-first. Build small tested helper modules in `src/lib/` (time, category, runs filter/sort, step filtering, metric formatting), then migrate each page onto them. No backend/API changes — Plan 2 covers the Argo/Grafana deep-linking. Each pure module is TDD with Vitest; each page is gated by `pnpm build` and a final Playwright pass.

**Tech Stack:** React 18 + react-router 6 + Vite 5 + Tailwind 3.4 + shadcn/ui (vendored) + lucide-react + openapi-fetch, Vitest (node env), pnpm.

---

## Conventions for this plan

- All commands run from `controlplane/web` unless stated. Repo root: `/Users/allen/repo/dlh-test-fw`.
- Package manager is **pnpm**. No new dependencies are added in this plan (everything needed — `lucide-react`, shadcn primitives, Vitest — already exists).
- **Per-task gate:** `pnpm build` (= `tsc -b && vite build`) must pass; `pnpm test` must pass for tasks that add/modify `src/**/*.test.ts`.
- `pnpm test` runs `vitest run`; it already has passing files (`stats.test.ts`, `verdict.test.ts`), so it will not error on "no files".
- **Worktree:** multi-commit chart-adjacent work. Create a feature branch + worktree at execution start (via `superpowers:using-git-worktrees` if available, else `git worktree add ../dlh-test-fw-ui-opt -b feat/controlplane-ui-optimization main`). Merge to `main` with `--no-ff` at the end (Task 14).
- **Theme tokens already exist** (`--status-success/running/failed/pending`, `--primary`, etc.). Category accent colors use Tailwind's built-in palette classes (`text-red-400`, `text-amber-400`, `text-blue-400`, `text-violet-400`, `text-emerald-400`, `text-slate-400`) — no new theme tokens.

### API types (from `src/api/gen.ts` — import, do not redefine)

```ts
type Run = components["schemas"]["Run"];
// { id; scenario; status:"Pending"|"Running"|"Succeeded"|"Failed"|"Error"|"Unknown";
//   startedAt:string; finishedAt?:string; score?:number|null; workflowName?:string;
//   target?:string; triggeredBy?:{kind?:string;id?:string} }
type RunDetail = components["schemas"]["RunDetail"]; // Run & { parameters?; steps?:{name;phase;startedAt?;finishedAt?;message?}[]; verdict?:Record<string,unknown>|null; grafanaUrls?:{label;url}[] }
type Scenario = components["schemas"]["Scenario"];   // { id; displayName; description?; targetType?; parameters? }
type Target   = components["schemas"]["Target"];     // { id; displayName?; namespace?; allowedTargetTypes?; configured? }
type Schedule = components["schemas"]["Schedule"];   // { id; scenario; target?; cron; timezone?; suspended?; lastScheduledAt?; activeCount? }
// POST /api/targets/{id}/test → ProbeResult { ok:boolean; latencyNanos?:number; error?:string }
```

**Domain fact (verified):** `Run.score` is set by the syncer to **1.0 (overall PASS)**, **0.0 (overall FAIL)**, or **null** (no verdict report). The Verdict column derives directly from this — no threshold guessing.

---

## Task 1: `lib/time.ts` — relative time + duration (TDD)

**Files:**
- Create: `controlplane/web/src/lib/time.ts`
- Test: `controlplane/web/src/lib/time.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
import { describe, it, expect } from "vitest";
import { relativeTime, formatDuration } from "@/lib/time";

const NOW = new Date("2026-05-23T12:00:00Z").getTime();
const ago = (ms: number) => new Date(NOW - ms).toISOString();

describe("relativeTime", () => {
  it("formats seconds/minutes/hours/days", () => {
    expect(relativeTime(ago(5_000), NOW)).toBe("5s ago");
    expect(relativeTime(ago(90_000), NOW)).toBe("1m ago");
    expect(relativeTime(ago(2 * 3_600_000), NOW)).toBe("2h ago");
    expect(relativeTime(ago(3 * 86_400_000), NOW)).toBe("3d ago");
  });
  it("clamps future to 0s and handles invalid input", () => {
    expect(relativeTime(new Date(NOW + 10_000).toISOString(), NOW)).toBe("0s ago");
    expect(relativeTime("not-a-date", NOW)).toBe("—");
  });
});

describe("formatDuration", () => {
  it("returns — when either endpoint is missing/invalid", () => {
    expect(formatDuration("2026-05-23T12:00:00Z", undefined)).toBe("—");
    expect(formatDuration(undefined, "2026-05-23T12:00:00Z")).toBe("—");
    expect(formatDuration("bad", "2026-05-23T12:00:00Z")).toBe("—");
  });
  it("formats s / m s / h m", () => {
    const s = "2026-05-23T12:00:00Z";
    expect(formatDuration(s, "2026-05-23T12:00:45Z")).toBe("45s");
    expect(formatDuration(s, "2026-05-23T12:04:16Z")).toBe("4m 16s");
    expect(formatDuration(s, "2026-05-23T13:05:00Z")).toBe("1h 5m");
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `pnpm test`
Expected: FAIL — `@/lib/time` cannot be imported.

- [ ] **Step 3: Implement `src/lib/time.ts`**

```ts
export function relativeTime(iso: string, now: number = Date.now()): string {
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) return "—";
  const sec = Math.max(0, Math.floor((now - t) / 1000));
  if (sec < 60) return `${sec}s ago`;
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min}m ago`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr}h ago`;
  return `${Math.floor(hr / 24)}d ago`;
}

export function formatDuration(startIso?: string, endIso?: string): string {
  if (!startIso || !endIso) return "—";
  const start = new Date(startIso).getTime();
  const end = new Date(endIso).getTime();
  if (Number.isNaN(start) || Number.isNaN(end) || end < start) return "—";
  let s = Math.floor((end - start) / 1000);
  if (s < 60) return `${s}s`;
  let m = Math.floor(s / 60);
  s = s % 60;
  if (m < 60) return `${m}m ${s}s`;
  const h = Math.floor(m / 60);
  m = m % 60;
  return `${h}h ${m}m`;
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `pnpm test`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add controlplane/web/src/lib/time.ts controlplane/web/src/lib/time.test.ts
git commit -m "feat(web): relativeTime + formatDuration helpers"
```

---

## Task 2: `lib/category.ts` — category + target-type derivation (TDD)

**Files:**
- Create: `controlplane/web/src/lib/category.ts`
- Test: `controlplane/web/src/lib/category.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
import { describe, it, expect } from "vitest";
import { deriveCategory, deriveTargetType, CATEGORIES } from "@/lib/category";

describe("deriveCategory", () => {
  it("matches explicit prefixes", () => {
    expect(deriveCategory("fixture-kafka-topic-seed")).toBe("fixture");
    expect(deriveCategory("util-write-slo")).toBe("util");
    expect(deriveCategory("load-k6-run")).toBe("load");
    expect(deriveCategory("verdict-slo-eval")).toBe("verdict");
    expect(deriveCategory("chaos-network-loss")).toBe("chaos");
  });
  it("falls back to chaos for unprefixed chaos scenarios", () => {
    expect(deriveCategory("mysql-pod-delete")).toBe("chaos");
    expect(deriveCategory("kafka-broker-partition")).toBe("chaos");
    expect(deriveCategory("doris-be-network-loss")).toBe("chaos");
  });
  it("falls back to other when nothing matches", () => {
    expect(deriveCategory("something-weird")).toBe("other");
  });
});

describe("deriveTargetType", () => {
  it("detects engine from id, else generic", () => {
    expect(deriveTargetType("mysql-pod-delete")).toBe("mysql");
    expect(deriveTargetType("fixture-kafka-topic-seed")).toBe("kafka");
    expect(deriveTargetType("doris-be-network-loss")).toBe("doris");
    expect(deriveTargetType("load-k6-run")).toBe("generic");
  });
});

describe("CATEGORIES", () => {
  it("is ordered chaos→fixture→load→verdict→util→other", () => {
    expect(CATEGORIES.map((c) => c.key)).toEqual(["chaos", "fixture", "load", "verdict", "util", "other"]);
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `pnpm test`
Expected: FAIL — `@/lib/category` cannot be imported.

- [ ] **Step 3: Implement `src/lib/category.ts`**

```ts
export type CategoryKey = "chaos" | "fixture" | "load" | "verdict" | "util" | "other";
export type TargetType = "mysql" | "kafka" | "doris" | "generic";

export interface CategoryMeta {
  key: CategoryKey;
  label: string;
  /** Tailwind text-color class for the category accent. */
  accent: string;
}

// Order = the render order of Scenarios sections.
export const CATEGORIES: CategoryMeta[] = [
  { key: "chaos", label: "Chaos", accent: "text-red-400" },
  { key: "fixture", label: "Fixture", accent: "text-amber-400" },
  { key: "load", label: "Load", accent: "text-blue-400" },
  { key: "verdict", label: "Verdict", accent: "text-violet-400" },
  { key: "util", label: "Util", accent: "text-emerald-400" },
  { key: "other", label: "Other", accent: "text-slate-400" },
];

export function deriveCategory(id: string): CategoryKey {
  if (id.startsWith("fixture-")) return "fixture";
  if (id.startsWith("util-")) return "util";
  if (id.startsWith("load-")) return "load";
  if (id.startsWith("verdict-")) return "verdict";
  if (id.startsWith("chaos-")) return "chaos";
  if (id.includes("pod-delete") || id.includes("network-loss") || id.includes("broker-partition")) return "chaos";
  return "other";
}

export function deriveTargetType(id: string): TargetType {
  if (id.includes("mysql")) return "mysql";
  if (id.includes("kafka")) return "kafka";
  if (id.includes("doris")) return "doris";
  return "generic";
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `pnpm test`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add controlplane/web/src/lib/category.ts controlplane/web/src/lib/category.test.ts
git commit -m "feat(web): deriveCategory + deriveTargetType + CATEGORIES table"
```

---

## Task 3: `lib/run.ts` + `lib/runsFilter.ts` — verdict-from-score, filter, sort (TDD)

**Files:**
- Create: `controlplane/web/src/lib/run.ts`
- Create: `controlplane/web/src/lib/runsFilter.ts`
- Test: `controlplane/web/src/lib/runsFilter.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
import { describe, it, expect } from "vitest";
import { verdictFromScore } from "@/lib/run";
import { filterRuns, sortRuns, EMPTY_FILTER } from "@/lib/runsFilter";
import type { components } from "@/api/gen";

type Run = components["schemas"]["Run"];
const NOW = new Date("2026-05-23T12:00:00Z").getTime();
const hAgo = (h: number) => new Date(NOW - h * 3_600_000).toISOString();
function run(p: Partial<Run>): Run {
  return { id: "x", scenario: "mysql-pod-delete", status: "Succeeded", startedAt: hAgo(1), ...p } as Run;
}

describe("verdictFromScore", () => {
  it("maps 1/0/null", () => {
    expect(verdictFromScore(1)).toBe("pass");
    expect(verdictFromScore(0)).toBe("fail");
    expect(verdictFromScore(null)).toBeNull();
    expect(verdictFromScore(undefined)).toBeNull();
  });
});

describe("filterRuns", () => {
  const runs = [
    run({ scenario: "mysql-pod-delete", status: "Succeeded", startedAt: hAgo(1) }),
    run({ scenario: "fixture-minio-load-mysql", status: "Failed", startedAt: hAgo(2) }),
    run({ scenario: "load-k6-run", status: "Succeeded", startedAt: hAgo(40) }),
  ];
  it("returns all with empty filter", () => {
    expect(filterRuns(runs, EMPTY_FILTER, NOW)).toHaveLength(3);
  });
  it("search matches scenario substring", () => {
    expect(filterRuns(runs, { ...EMPTY_FILTER, search: "minio" }, NOW)).toHaveLength(1);
  });
  it("status + failedOnly + category + timeRange compose", () => {
    expect(filterRuns(runs, { ...EMPTY_FILTER, status: "Failed" }, NOW)).toHaveLength(1);
    expect(filterRuns(runs, { ...EMPTY_FILTER, failedOnly: true }, NOW)).toHaveLength(1);
    expect(filterRuns(runs, { ...EMPTY_FILTER, category: "load" }, NOW)).toHaveLength(1);
    expect(filterRuns(runs, { ...EMPTY_FILTER, timeRange: "24h" }, NOW)).toHaveLength(2);
  });
});

describe("sortRuns", () => {
  const a = run({ id: "a", startedAt: hAgo(1), finishedAt: new Date(NOW - 1 * 3_600_000 + 60_000).toISOString() });
  const b = run({ id: "b", startedAt: hAgo(3), finishedAt: new Date(NOW - 3 * 3_600_000 + 600_000).toISOString() });
  it("sorts by started desc/asc", () => {
    expect(sortRuns([b, a], { key: "started", dir: "desc" })[0].id).toBe("a");
    expect(sortRuns([b, a], { key: "started", dir: "asc" })[0].id).toBe("b");
  });
  it("sorts by duration desc", () => {
    expect(sortRuns([a, b], { key: "duration", dir: "desc" })[0].id).toBe("b");
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `pnpm test`
Expected: FAIL — `@/lib/run` and `@/lib/runsFilter` cannot be imported.

- [ ] **Step 3: Implement `src/lib/run.ts`**

```ts
export type Verdict = "pass" | "fail" | null;

/** Run.score is 1.0 (PASS), 0.0 (FAIL), or null (no verdict report). */
export function verdictFromScore(score: number | null | undefined): Verdict {
  if (score == null) return null;
  return score >= 1 ? "pass" : "fail";
}
```

- [ ] **Step 4: Implement `src/lib/runsFilter.ts`**

```ts
import type { components } from "@/api/gen";
import { deriveCategory } from "@/lib/category";

type Run = components["schemas"]["Run"];

export interface RunFilter {
  search: string;
  status: string; // "" = any
  category: string; // "" = any
  timeRange: "" | "24h" | "7d";
  failedOnly: boolean;
}

export const EMPTY_FILTER: RunFilter = {
  search: "",
  status: "",
  category: "",
  timeRange: "",
  failedOnly: false,
};

export function filterRuns(runs: Run[], f: RunFilter, now: number = Date.now()): Run[] {
  const q = f.search.trim().toLowerCase();
  const maxMs = f.timeRange === "24h" ? 24 * 3_600_000 : f.timeRange === "7d" ? 7 * 86_400_000 : Infinity;
  return runs.filter((r) => {
    if (q && !r.scenario.toLowerCase().includes(q)) return false;
    if (f.status && r.status !== f.status) return false;
    if (f.failedOnly && r.status !== "Failed" && r.status !== "Error") return false;
    if (f.category && deriveCategory(r.scenario) !== f.category) return false;
    if (maxMs !== Infinity && now - new Date(r.startedAt).getTime() > maxMs) return false;
    return true;
  });
}

export type SortKey = "started" | "duration";
export type SortDir = "asc" | "desc";
export interface RunSort {
  key: SortKey;
  dir: SortDir;
}

function durationMs(r: Run): number {
  if (!r.finishedAt) return -1;
  return new Date(r.finishedAt).getTime() - new Date(r.startedAt).getTime();
}

export function sortRuns(runs: Run[], s: RunSort): Run[] {
  const sign = s.dir === "desc" ? -1 : 1;
  return [...runs].sort((a, b) => {
    const av = s.key === "started" ? new Date(a.startedAt).getTime() : durationMs(a);
    const bv = s.key === "started" ? new Date(b.startedAt).getTime() : durationMs(b);
    return sign * (av - bv);
  });
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `pnpm test`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add controlplane/web/src/lib/run.ts controlplane/web/src/lib/runsFilter.ts controlplane/web/src/lib/runsFilter.test.ts
git commit -m "feat(web): verdictFromScore + runs filter/sort logic"
```

---

## Task 4: `lib/steps.ts` — hide Argo group nodes (TDD)

**Files:**
- Create: `controlplane/web/src/lib/steps.ts`
- Test: `controlplane/web/src/lib/steps.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
import { describe, it, expect } from "vitest";
import { isGroupNode, namedSteps } from "@/lib/steps";

describe("isGroupNode", () => {
  it("matches only bracketed integer names", () => {
    expect(isGroupNode("[0]")).toBe(true);
    expect(isGroupNode("[12]")).toBe(true);
    expect(isGroupNode(" [3] ")).toBe(true);
    expect(isGroupNode("chaos")).toBe(false);
    expect(isGroupNode("step[0]")).toBe(false);
    expect(isGroupNode("[a]")).toBe(false);
  });
});

describe("namedSteps", () => {
  const steps = [
    { name: "[0]", phase: "Succeeded" },
    { name: "chaos", phase: "Succeeded" },
    { name: "wf-root", phase: "Succeeded" },
    { name: "[1]", phase: "Succeeded" },
    { name: "verdict", phase: "Succeeded" },
  ];
  it("drops group nodes", () => {
    expect(namedSteps(steps).map((s) => s.name)).toEqual(["chaos", "wf-root", "verdict"]);
  });
  it("also drops the root node when its name is given", () => {
    expect(namedSteps(steps, "wf-root").map((s) => s.name)).toEqual(["chaos", "verdict"]);
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `pnpm test`
Expected: FAIL — `@/lib/steps` cannot be imported.

- [ ] **Step 3: Implement `src/lib/steps.ts`**

```ts
/** True for Argo DAG/step-group placeholder nodes named like "[0]", "[12]". */
export function isGroupNode(name: string): boolean {
  return /^\[\d+\]$/.test(name.trim());
}

/** Keep only real named steps: drop bracketed group nodes and (optionally) the workflow root node. */
export function namedSteps<T extends { name: string }>(steps: T[], rootName?: string): T[] {
  return steps.filter((s) => !isGroupNode(s.name) && s.name !== rootName);
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `pnpm test`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add controlplane/web/src/lib/steps.ts controlplane/web/src/lib/steps.test.ts
git commit -m "feat(web): step filtering (hide Argo group nodes)"
```

---

## Task 5: `lib/format.ts` — readable metric values (TDD)

**Files:**
- Create: `controlplane/web/src/lib/format.ts`
- Test: `controlplane/web/src/lib/format.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
import { describe, it, expect } from "vitest";
import { formatMetricValue } from "@/lib/format";

describe("formatMetricValue", () => {
  it("uses scientific notation for very small/large magnitudes", () => {
    expect(formatMetricValue(0.0000034999847412105)).toBe("3.50e-6");
    expect(formatMetricValue(2_500_000)).toBe("2.50e+6");
  });
  it("rounds mid-range to <=4 significant figures and trims zeros", () => {
    expect(formatMetricValue(0.295585588666926)).toBe("0.2956");
    expect(formatMetricValue(2.5)).toBe("2.5");
    expect(formatMetricValue(1)).toBe("1");
  });
  it("handles 0 and non-finite", () => {
    expect(formatMetricValue(0)).toBe("0");
    expect(formatMetricValue(NaN)).toBe("NaN");
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `pnpm test`
Expected: FAIL — `@/lib/format` cannot be imported.

- [ ] **Step 3: Implement `src/lib/format.ts`**

```ts
/** Render a metric/threshold number readably: sci-notation at the extremes, trimmed 4 sig-figs otherwise. */
export function formatMetricValue(v: number): string {
  if (!Number.isFinite(v)) return String(v);
  if (v === 0) return "0";
  const abs = Math.abs(v);
  if (abs < 1e-3 || abs >= 1e6) return v.toExponential(2);
  return parseFloat(v.toPrecision(4)).toString();
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `pnpm test`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add controlplane/web/src/lib/format.ts controlplane/web/src/lib/format.test.ts
git commit -m "feat(web): formatMetricValue for readable verdict numbers"
```

---

## Task 6: Presentational primitives — compact `StatCard` + `CategoryIcon`

**Files:**
- Modify: `controlplane/web/src/components/StatCard.tsx`
- Create: `controlplane/web/src/components/CategoryIcon.tsx`

- [ ] **Step 1: Replace `src/components/StatCard.tsx` (add optional icon, denser layout)**

```tsx
import { type ReactNode } from "react";
import { Card, CardContent } from "@/components/ui/card";
import { cn } from "@/lib/utils";

export function StatCard({
  label,
  value,
  accent,
  icon,
}: {
  label: string;
  value: string;
  accent?: "primary" | "success" | "running" | "failed";
  icon?: ReactNode;
}) {
  const accentClass =
    accent === "success"
      ? "text-status-success"
      : accent === "running"
      ? "text-status-running"
      : accent === "failed"
      ? "text-status-failed"
      : accent === "primary"
      ? "text-primary"
      : "text-foreground";
  return (
    <Card>
      <CardContent className="flex items-center gap-3 p-4">
        {icon && (
          <div className={cn("flex h-9 w-9 items-center justify-center rounded-lg bg-muted", accentClass)}>
            {icon}
          </div>
        )}
        <div>
          <div className={cn("text-xl font-bold leading-none", accentClass)}>{value}</div>
          <div className="mt-1 text-xs text-muted-foreground">{label}</div>
        </div>
      </CardContent>
    </Card>
  );
}
```

- [ ] **Step 2: Create `src/components/CategoryIcon.tsx`**

```tsx
import { Zap, Wrench, Activity, CheckCircle2, Hammer, Box, type LucideIcon } from "lucide-react";
import { cn } from "@/lib/utils";
import { CATEGORIES, type CategoryKey } from "@/lib/category";

const ICONS: Record<CategoryKey, LucideIcon> = {
  chaos: Zap,
  fixture: Wrench,
  load: Activity,
  verdict: CheckCircle2,
  util: Hammer,
  other: Box,
};

export function CategoryIcon({ category, className }: { category: CategoryKey; className?: string }) {
  const Icon = ICONS[category];
  const accent = CATEGORIES.find((c) => c.key === category)?.accent ?? "text-slate-400";
  return <Icon className={cn("h-4 w-4", accent, className)} />;
}
```

- [ ] **Step 3: Verify build**

Run: `pnpm build`
Expected: PASS. (StatCard's old call sites still compile — `icon` is optional; RunsPage is updated in Task 8.)

- [ ] **Step 4: Commit**

```bash
git add controlplane/web/src/components/StatCard.tsx controlplane/web/src/components/CategoryIcon.tsx
git commit -m "feat(web): compact StatCard with icon slot + CategoryIcon"
```

---

## Task 7: App shell — nav icons + wider container

**Files:**
- Modify: `controlplane/web/src/App.tsx`

- [ ] **Step 1: Replace the `NAV` constant and nav rendering in `src/App.tsx`**

Replace the existing `NAV` constant (the `const NAV = [ ... ];` block) with:

```tsx
import { Activity, LayoutGrid, Crosshair, Clock } from "lucide-react";

const NAV = [
  { to: "/runs", label: "Runs", Icon: Activity },
  { to: "/scenarios", label: "Scenarios", Icon: LayoutGrid },
  { to: "/targets", label: "Targets", Icon: Crosshair },
  { to: "/schedules", label: "Schedules", Icon: Clock },
];
```

Keep the existing `import { Moon, Sun } from "lucide-react";` line — or merge all lucide imports into one line. Then replace the `NAV.map(...)` block inside `<nav>` with:

```tsx
          {NAV.map((n) => (
            <NavLink
              key={n.to}
              to={n.to}
              className={({ isActive }) =>
                cn(
                  "flex items-center gap-1.5 rounded-md px-2.5 py-1 transition-colors",
                  isActive
                    ? "bg-primary/15 font-medium text-foreground"
                    : "text-muted-foreground hover:text-foreground"
                )
              }
            >
              <n.Icon className="h-4 w-4" />
              {n.label}
            </NavLink>
          ))}
```

- [ ] **Step 2: Widen both containers to `max-w-7xl`**

In `src/App.tsx`, change the nav container `className="mx-auto flex max-w-6xl items-center gap-4 px-6 py-3 text-sm"` → `max-w-7xl`, and the `<main className="mx-auto max-w-6xl px-6 py-8">` → `max-w-7xl`.

- [ ] **Step 3: Verify build**

Run: `pnpm build`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add controlplane/web/src/App.tsx
git commit -m "feat(web): nav icons + active pill + wider max-w-7xl shell"
```

---

## Task 8: Runs page — filter bar, Duration + Verdict columns, relative time, sort

**Files:**
- Modify: `controlplane/web/src/pages/RunsPage.tsx` (full replace)

- [ ] **Step 1: Replace `src/pages/RunsPage.tsx`**

```tsx
import { useCallback, useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Activity, AlertTriangle, ArrowUpDown, CalendarClock, CalendarDays, CheckCircle2, Search } from "lucide-react";
import { api } from "../api/client";
import type { components } from "../api/gen";
import { StatusBadge } from "@/components/StatusBadge";
import { StatCard } from "@/components/StatCard";
import { PageHeader } from "@/components/PageHeader";
import { EmptyState } from "@/components/EmptyState";
import { ErrorState } from "@/components/ErrorState";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { computeStats } from "@/lib/stats";
import { relativeTime, formatDuration } from "@/lib/time";
import { verdictFromScore } from "@/lib/run";
import { filterRuns, sortRuns, type RunFilter, type RunSort } from "@/lib/runsFilter";

type Run = components["schemas"]["Run"];
type Schedule = components["schemas"]["Schedule"];

const POLL_MS = 5000;
const ANY = "__any__";

export function RunsPage() {
  const navigate = useNavigate();
  const [runs, setRuns] = useState<Run[] | null>(null);
  const [schedules, setSchedules] = useState<Schedule[]>([]);
  const [error, setError] = useState<unknown>(null);
  const [secondsAgo, setSecondsAgo] = useState(0);

  const [search, setSearch] = useState("");
  const [status, setStatus] = useState("");
  const [category, setCategory] = useState("");
  const [timeRange, setTimeRange] = useState<RunFilter["timeRange"]>("");
  const [failedOnly, setFailedOnly] = useState(false);
  const [sort, setSort] = useState<RunSort>({ key: "started", dir: "desc" });

  const reload = useCallback(() => {
    api.GET("/api/runs", {}).then(({ data, error: e }) => {
      if (e) {
        setError(e);
        return;
      }
      setRuns(data?.items ?? []);
      setSecondsAgo(0);
      setError(null);
    });
    api.GET("/api/schedules", {}).then(({ data }) => setSchedules(data?.items ?? []));
  }, []);

  useEffect(() => {
    reload();
    const poll = setInterval(reload, POLL_MS);
    const tick = setInterval(() => setSecondsAgo((n) => n + 1), 1000);
    return () => {
      clearInterval(poll);
      clearInterval(tick);
    };
  }, [reload]);

  const stats = runs ? computeStats(runs, schedules) : null;

  const visible = useMemo(() => {
    if (!runs) return [];
    return sortRuns(filterRuns(runs, { search, status, category, timeRange, failedOnly }), sort);
  }, [runs, search, status, category, timeRange, failedOnly, sort]);

  if (error) return <ErrorState message="Failed to load runs" details={error} />;

  const toggleSort = (key: RunSort["key"]) =>
    setSort((s) => (s.key === key ? { key, dir: s.dir === "desc" ? "asc" : "desc" } : { key, dir: "desc" }));

  return (
    <section>
      <PageHeader title="Runs" />

      <div className="mb-5 grid grid-cols-2 gap-3 md:grid-cols-4">
        <StatCard
          label="Pass rate (7d)"
          accent="success"
          icon={<CheckCircle2 className="h-4 w-4" />}
          value={stats == null ? "—" : stats.passRate7d == null ? "—" : `${Math.round(stats.passRate7d * 100)}%`}
        />
        <StatCard label="Runs today" icon={<CalendarDays className="h-4 w-4" />} value={stats == null ? "—" : String(stats.runsToday)} />
        <StatCard label="Running now" accent="running" icon={<Activity className="h-4 w-4" />} value={stats == null ? "—" : String(stats.runningNow)} />
        <StatCard label="Active schedules" icon={<CalendarClock className="h-4 w-4" />} value={stats == null ? "—" : String(stats.activeSchedules)} />
      </div>

      <Card>
        <div className="flex flex-wrap items-center gap-2 border-b px-4 py-3">
          <div className="relative">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <Input value={search} onChange={(e) => setSearch(e.target.value)} placeholder="Search scenario…" className="h-8 w-[200px] pl-8" />
          </div>
          <Select value={status === "" ? ANY : status} onValueChange={(v) => setStatus(v === ANY ? "" : v)}>
            <SelectTrigger className="h-8 w-[140px]"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value={ANY}>Any status</SelectItem>
              <SelectItem value="Succeeded">Succeeded</SelectItem>
              <SelectItem value="Failed">Failed</SelectItem>
              <SelectItem value="Running">Running</SelectItem>
            </SelectContent>
          </Select>
          <Select value={category === "" ? ANY : category} onValueChange={(v) => setCategory(v === ANY ? "" : v)}>
            <SelectTrigger className="h-8 w-[150px]"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value={ANY}>Any category</SelectItem>
              <SelectItem value="chaos">chaos</SelectItem>
              <SelectItem value="fixture">fixture</SelectItem>
              <SelectItem value="load">load</SelectItem>
              <SelectItem value="verdict">verdict</SelectItem>
              <SelectItem value="util">util</SelectItem>
            </SelectContent>
          </Select>
          <Select value={timeRange === "" ? ANY : timeRange} onValueChange={(v) => setTimeRange((v === ANY ? "" : v) as RunFilter["timeRange"])}>
            <SelectTrigger className="h-8 w-[130px]"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value={ANY}>All time</SelectItem>
              <SelectItem value="24h">Last 24h</SelectItem>
              <SelectItem value="7d">Last 7d</SelectItem>
            </SelectContent>
          </Select>
          <Button
            type="button"
            variant="outline"
            size="sm"
            className={failedOnly ? "border-status-failed/40 bg-status-failed/10 text-status-failed" : ""}
            onClick={() => setFailedOnly((f) => !f)}
          >
            <AlertTriangle className="h-3.5 w-3.5" /> Failed only
          </Button>
          {runs && <span className="ml-auto text-xs text-muted-foreground">● live · updated {secondsAgo}s ago</span>}
        </div>

        {!runs ? (
          <div className="space-y-2 p-4">
            {Array.from({ length: 5 }).map((_, i) => (
              <Skeleton key={i} className="h-8 w-full" />
            ))}
          </div>
        ) : visible.length === 0 ? (
          <div className="p-4">
            <EmptyState
              message={runs.length === 0 ? "No runs yet" : "No matching runs"}
              hint={runs.length === 0 ? "Submit a scenario from the Scenarios page." : "Adjust the filters above."}
            />
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Scenario</TableHead>
                <TableHead>Target</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>
                  <button className="inline-flex items-center gap-1 uppercase" onClick={() => toggleSort("started")}>
                    Started <ArrowUpDown className="h-3 w-3" />
                  </button>
                </TableHead>
                <TableHead>
                  <button className="inline-flex items-center gap-1 uppercase" onClick={() => toggleSort("duration")}>
                    Duration <ArrowUpDown className="h-3 w-3" />
                  </button>
                </TableHead>
                <TableHead>Verdict</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {visible.map((r) => {
                const v = verdictFromScore(r.score);
                return (
                  <TableRow key={r.id} className="cursor-pointer" onClick={() => navigate(`/runs/${r.id}`)}>
                    <TableCell className="font-medium">{r.scenario}</TableCell>
                    <TableCell className="text-muted-foreground">{r.target || "local"}</TableCell>
                    <TableCell><StatusBadge status={String(r.status)} /></TableCell>
                    <TableCell className="text-muted-foreground" title={new Date(r.startedAt).toLocaleString()}>
                      {relativeTime(r.startedAt)}
                    </TableCell>
                    <TableCell className="text-muted-foreground">{formatDuration(r.startedAt, r.finishedAt)}</TableCell>
                    <TableCell>
                      {v == null ? (
                        <span className="text-muted-foreground">—</span>
                      ) : v === "pass" ? (
                        <span className="text-xs font-semibold text-status-success">✓ pass</span>
                      ) : (
                        <span className="text-xs font-semibold text-status-failed">✗ fail</span>
                      )}
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        )}
      </Card>
    </section>
  );
}
```

- [ ] **Step 2: Verify build**

Run: `pnpm build`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add controlplane/web/src/pages/RunsPage.tsx
git commit -m "feat(web): Runs filter bar + Duration/Verdict columns + relative time + sort"
```

---

## Task 9: Scenarios page — grouped by category, search, richer cards

**Files:**
- Modify: `controlplane/web/src/pages/ScenariosPage.tsx` (full replace)

- [ ] **Step 1: Replace `src/pages/ScenariosPage.tsx`**

```tsx
import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Play, Search } from "lucide-react";
import { toast } from "sonner";
import { api } from "../api/client";
import type { components } from "../api/gen";
import { TargetPicker } from "@/components/TargetPicker";
import { CategoryIcon } from "@/components/CategoryIcon";
import { PageHeader } from "@/components/PageHeader";
import { EmptyState } from "@/components/EmptyState";
import { ErrorState } from "@/components/ErrorState";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { CATEGORIES, deriveCategory, deriveTargetType, type CategoryKey } from "@/lib/category";

type Scenario = components["schemas"]["Scenario"];

export function ScenariosPage() {
  const navigate = useNavigate();
  const [items, setItems] = useState<Scenario[] | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [submitTarget, setSubmitTarget] = useState<Record<string, string>>({});
  const [submitting, setSubmitting] = useState<string | null>(null);
  const [search, setSearch] = useState("");

  useEffect(() => {
    api.GET("/api/scenarios", {}).then(({ data, error }) => {
      if (error) setError(error);
      else setItems(data?.items ?? []);
    });
  }, []);

  const handleRun = async (s: Scenario) => {
    setSubmitting(s.id);
    try {
      const targetId = submitTarget[s.id] || undefined;
      const { data, error } = await api.POST("/api/runs", { body: { scenarioId: s.id, targetId } });
      if (error) toast.error("Submit failed", { description: JSON.stringify(error) });
      else if (data?.id) {
        toast.success(`Run ${data.id} submitted`);
        navigate(`/runs/${data.id}`);
      }
    } finally {
      setSubmitting(null);
    }
  };

  const { grouped, total } = useMemo(() => {
    const q = search.trim().toLowerCase();
    const map = new Map<CategoryKey, Scenario[]>();
    let count = 0;
    for (const s of items ?? []) {
      if (q && !s.id.toLowerCase().includes(q)) continue;
      const key = deriveCategory(s.id);
      const arr = map.get(key) ?? [];
      arr.push(s);
      map.set(key, arr);
      count++;
    }
    return { grouped: map, total: count };
  }, [items, search]);

  if (error) return <ErrorState message="Failed to load scenarios" details={error} />;

  return (
    <section>
      <PageHeader
        title="Scenarios"
        action={
          <div className="relative">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <Input value={search} onChange={(e) => setSearch(e.target.value)} placeholder="Search scenarios…" className="h-8 w-[220px] pl-8" />
          </div>
        }
      />

      {!items ? (
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2 lg:grid-cols-3">
          {Array.from({ length: 6 }).map((_, i) => (
            <Skeleton key={i} className="h-28 w-full" />
          ))}
        </div>
      ) : items.length === 0 ? (
        <EmptyState message="No scenarios available" />
      ) : total === 0 ? (
        <EmptyState message="No matching scenarios" hint="Try a different search." />
      ) : (
        CATEGORIES.map((cat) => {
          const scns = grouped.get(cat.key);
          if (!scns || scns.length === 0) return null;
          return (
            <div key={cat.key} className="mb-6">
              <div className={cn("mb-2 flex items-center gap-2 font-semibold", cat.accent)}>
                <CategoryIcon category={cat.key} />
                {cat.label.toUpperCase()}
                <span className="rounded-full border bg-card px-2 text-xs font-medium text-muted-foreground">{scns.length}</span>
              </div>
              <ul className="grid grid-cols-1 gap-3 md:grid-cols-2 lg:grid-cols-3">
                {scns.map((s) => {
                  const tt = s.targetType ?? deriveTargetType(s.id);
                  return (
                    <li key={s.id}>
                      <Card className="h-full">
                        <CardContent className="flex h-full flex-col gap-3 p-4">
                          <div className="flex items-start gap-3">
                            <div className="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-lg bg-muted">
                              <CategoryIcon category={cat.key} />
                            </div>
                            <div className="min-w-0">
                              <div className="truncate font-semibold">{s.displayName}</div>
                              <div className="text-xs text-muted-foreground">{tt}</div>
                            </div>
                          </div>
                          <div className="mt-auto flex items-center gap-2">
                            <TargetPicker
                              value={submitTarget[s.id] ?? ""}
                              onChange={(v) => setSubmitTarget((r) => ({ ...r, [s.id]: v }))}
                              filterType={s.targetType ?? undefined}
                            />
                            <Button size="sm" disabled={submitting === s.id} onClick={() => handleRun(s)}>
                              <Play className="h-3.5 w-3.5" />
                              {submitting === s.id ? "Submitting…" : "Run"}
                            </Button>
                          </div>
                        </CardContent>
                      </Card>
                    </li>
                  );
                })}
              </ul>
            </div>
          );
        })
      )}
    </section>
  );
}
```

- [ ] **Step 2: Verify build**

Run: `pnpm build`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add controlplane/web/src/pages/ScenariosPage.tsx
git commit -m "feat(web): grouped Scenarios page with search + richer cards"
```

---

## Task 10: Run detail — header, meta strip, verdict-first, cleaned steps + VerdictView formatting

**Files:**
- Modify: `controlplane/web/src/components/VerdictView.tsx`
- Modify: `controlplane/web/src/pages/RunDetailPage.tsx` (full replace)

- [ ] **Step 1: Format metric values in `src/components/VerdictView.tsx`**

Add the import at the top:

```tsx
import { formatMetricValue } from "@/lib/format";
```

Change the threshold value cell from `<TableCell className="font-mono text-xs">{t.value}</TableCell>` to:

```tsx
                <TableCell className="font-mono text-xs">{formatMetricValue(t.value)}</TableCell>
```

And change the raw-PromQL value rendering — replace `{parsed.rawPromQL.query}</code>{" "}` line's following value usage so the value is formatted. Concretely, the `rawPromQL` block becomes:

```tsx
      {parsed.rawPromQL && (
        <div className="text-sm">
          <span className="text-muted-foreground">Raw PromQL: </span>
          <code className="font-mono text-xs">{parsed.rawPromQL.query}</code>{" "}
          <span className="font-mono text-xs text-muted-foreground">= {formatMetricValue(parsed.rawPromQL.value)}</span>{" "}
          <span className={parsed.rawPromQL.passed ? "text-status-success" : "text-status-failed"}>
            ({parsed.rawPromQL.passed ? "pass" : "fail"})
          </span>
        </div>
      )}
```

- [ ] **Step 2: Replace `src/pages/RunDetailPage.tsx`**

```tsx
import { useEffect, useState, type ReactNode } from "react";
import { Link, useParams } from "react-router-dom";
import { ArrowLeft, CheckCircle2, Circle, Loader2, XCircle } from "lucide-react";
import { api } from "../api/client";
import type { components } from "../api/gen";
import { StatusBadge } from "@/components/StatusBadge";
import { CategoryIcon } from "@/components/CategoryIcon";
import { ErrorState } from "@/components/ErrorState";
import { VerdictView } from "@/components/VerdictView";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { relativeTime, formatDuration } from "@/lib/time";
import { deriveCategory } from "@/lib/category";
import { namedSteps } from "@/lib/steps";

type RunDetail = components["schemas"]["RunDetail"];

function StepIcon({ phase }: { phase: string }) {
  if (phase === "Succeeded") return <CheckCircle2 className="h-4 w-4 text-status-success" />;
  if (phase === "Failed" || phase === "Error") return <XCircle className="h-4 w-4 text-status-failed" />;
  if (phase === "Running") return <Loader2 className="h-4 w-4 text-status-running" />;
  return <Circle className="h-4 w-4 text-status-pending" />;
}

function Meta({ label, value, title, children }: { label: string; value?: string; title?: string; children?: ReactNode }) {
  return (
    <div>
      <div className="text-xs uppercase tracking-wide text-muted-foreground">{label}</div>
      <div className="font-medium" title={title}>{children ?? value}</div>
    </div>
  );
}

export function RunDetailPage() {
  const { id } = useParams<{ id: string }>();
  const [run, setRun] = useState<RunDetail | null>(null);
  const [liveStatus, setLiveStatus] = useState<string | null>(null);
  const [error, setError] = useState<unknown>(null);

  useEffect(() => {
    if (!id) return;
    api.GET("/api/runs/{id}", { params: { path: { id } } }).then(({ data, error }) => {
      if (error) setError(error);
      else setRun(data as RunDetail);
    });

    const es = new EventSource(`/api/runs/${id}/events`);
    const onEvent = (e: MessageEvent) => {
      try {
        const d = JSON.parse(e.data);
        if (d.phase) setLiveStatus(d.phase);
      } catch {
        /* ignore */
      }
    };
    es.addEventListener("snapshot", onEvent);
    es.addEventListener("MODIFIED", onEvent);
    es.addEventListener("ADDED", onEvent);
    es.addEventListener("DELETED", onEvent);
    return () => es.close();
  }, [id]);

  if (error) return <ErrorState message="Failed to load run" details={error} />;
  if (!run) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-40 w-full" />
      </div>
    );
  }

  const status = liveStatus ?? String(run.status ?? "Unknown");
  const allSteps = run.steps ?? [];
  const visibleSteps = namedSteps(allSteps, run.id);
  const hidden = allSteps.length - visibleSteps.length;

  return (
    <section className="space-y-5">
      <Link to="/runs" className="inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground">
        <ArrowLeft className="h-4 w-4" /> Runs
      </Link>

      <div className="flex flex-wrap items-center gap-3">
        <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-muted">
          <CategoryIcon category={deriveCategory(run.scenario)} />
        </div>
        <h1 className="font-mono text-lg font-semibold">{run.id}</h1>
        <StatusBadge status={status} />
      </div>

      <div className="flex flex-wrap gap-x-10 gap-y-3 rounded-lg border bg-card px-5 py-4">
        <Meta label="Scenario" value={run.scenario} />
        <Meta label="Target" value={run.target || "local"} />
        <Meta label="Started" value={relativeTime(run.startedAt)} title={new Date(run.startedAt).toLocaleString()} />
        <Meta label="Duration" value={formatDuration(run.startedAt, run.finishedAt)} />
        <Meta label="Triggered by">
          {run.triggeredBy?.id ? (
            <Link to="/schedules" className="text-primary hover:underline">{run.triggeredBy.id}</Link>
          ) : (
            <span className="text-muted-foreground">manual</span>
          )}
        </Meta>
      </div>

      <Card>
        <CardHeader><CardTitle className="text-base">Verdict</CardTitle></CardHeader>
        <CardContent><VerdictView verdict={run.verdict} /></CardContent>
      </Card>

      {visibleSteps.length > 0 && (
        <Card>
          <CardHeader className="flex flex-row items-center justify-between">
            <CardTitle className="text-base">Steps</CardTitle>
            <span className="text-xs text-muted-foreground">
              {visibleSteps.length} steps{hidden > 0 ? " · group nodes hidden" : ""}
            </span>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Step</TableHead>
                  <TableHead>Phase</TableHead>
                  <TableHead>Duration</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {visibleSteps.map((s, i) => (
                  <TableRow key={i}>
                    <TableCell className="flex items-center gap-2 font-medium">
                      <StepIcon phase={s.phase} />
                      {s.name}
                    </TableCell>
                    <TableCell className="text-muted-foreground">{s.phase}</TableCell>
                    <TableCell className="text-muted-foreground">{formatDuration(s.startedAt, s.finishedAt)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}
    </section>
  );
}
```

- [ ] **Step 3: Verify build**

Run: `pnpm build`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add controlplane/web/src/components/VerdictView.tsx controlplane/web/src/pages/RunDetailPage.tsx
git commit -m "feat(web): Run detail redesign (header/meta/verdict-first/clean steps) + formatted verdict values"
```

---

## Task 11: Targets page — persistent inline test-connection result

**Files:**
- Modify: `controlplane/web/src/pages/TargetsPage.tsx` (full replace)

- [ ] **Step 1: Replace `src/pages/TargetsPage.tsx`**

```tsx
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { api } from "../api/client";
import type { components } from "../api/gen";
import { PageHeader } from "@/components/PageHeader";
import { EmptyState } from "@/components/EmptyState";
import { ErrorState } from "@/components/ErrorState";
import { Card } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

type Target = components["schemas"]["Target"];
type TestResult = { ok: boolean; ms?: number; error?: string };

export function TargetsPage() {
  const [items, setItems] = useState<Target[] | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [testing, setTesting] = useState<string | null>(null);
  const [results, setResults] = useState<Record<string, TestResult>>({});

  useEffect(() => {
    api.GET("/api/targets", {}).then(({ data, error }) => {
      if (error) setError(error);
      else setItems(data?.items ?? []);
    });
  }, []);

  const testConn = async (id: string) => {
    setTesting(id);
    try {
      const { data, error } = await api.POST("/api/targets/{id}/test", { params: { path: { id } } });
      if (error) {
        setResults((r) => ({ ...r, [id]: { ok: false, error: "request failed" } }));
        toast.error(`Test failed: ${id}`, { description: JSON.stringify(error) });
      } else if (data?.ok) {
        const ms = Math.round((data.latencyNanos ?? 0) / 1_000_000);
        setResults((r) => ({ ...r, [id]: { ok: true, ms } }));
        toast.success(`${id} OK (${ms} ms)`);
      } else {
        setResults((r) => ({ ...r, [id]: { ok: false, error: data?.error ?? "unknown" } }));
        toast.error(`${id} unreachable`, { description: data?.error ?? "unknown" });
      }
    } finally {
      setTesting(null);
    }
  };

  if (error) return <ErrorState message="Failed to load targets" details={error} />;

  return (
    <section>
      <PageHeader title="Targets" />
      {!items ? (
        <p className="text-muted-foreground">Loading…</p>
      ) : items.length === 0 ? (
        <EmptyState
          message="No targets registered"
          hint={<>Targets are added by PR — see <code>docs/operations/register-target.md</code>.</>}
        />
      ) : (
        <Card>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>ID</TableHead>
                <TableHead>Display Name</TableHead>
                <TableHead>Namespace</TableHead>
                <TableHead>Allowed Types</TableHead>
                <TableHead>Configured</TableHead>
                <TableHead></TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((t) => {
                const res = results[t.id];
                return (
                  <TableRow key={t.id}>
                    <TableCell className="font-medium">{t.id}</TableCell>
                    <TableCell>{t.displayName ?? t.id}</TableCell>
                    <TableCell className="text-muted-foreground">{t.namespace ?? "—"}</TableCell>
                    <TableCell>{(t.allowedTargetTypes ?? []).join(", ") || "—"}</TableCell>
                    <TableCell>
                      {t.configured ? (
                        <Badge variant="outline" className="bg-status-success/15 text-status-success">configured</Badge>
                      ) : (
                        <Badge variant="outline" className="bg-status-failed/15 text-status-failed">missing</Badge>
                      )}
                    </TableCell>
                    <TableCell className="whitespace-nowrap text-right">
                      {res && (
                        <span
                          className={
                            "mr-2 rounded-md px-2 py-0.5 text-xs font-medium " +
                            (res.ok ? "bg-status-success/15 text-status-success" : "bg-status-failed/15 text-status-failed")
                          }
                        >
                          {res.ok ? `OK · ${res.ms} ms` : "unreachable"}
                        </span>
                      )}
                      <Button variant="outline" size="sm" disabled={testing === t.id} onClick={() => testConn(t.id)}>
                        {testing === t.id ? "Testing…" : "Test"}
                      </Button>
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </Card>
      )}
    </section>
  );
}
```

- [ ] **Step 2: Verify build**

Run: `pnpm build`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add controlplane/web/src/pages/TargetsPage.tsx
git commit -m "feat(web): Targets persistent inline test-connection result badge"
```

---

## Task 12: Schedules page — relative time + density polish

**Files:**
- Modify: `controlplane/web/src/pages/SchedulesPage.tsx`

The page already uses `Dialog` (create) + `AlertDialog` (delete) + toasts (built in the prior refresh). Only two changes: relative `Last Fired`, and a `Loading…` → skeleton swap is out of scope; keep minimal.

- [ ] **Step 1: Import the relative-time helper**

Add to the imports in `src/pages/SchedulesPage.tsx`:

```tsx
import { relativeTime } from "@/lib/time";
```

- [ ] **Step 2: Replace the "Last Fired" cell**

Change:

```tsx
                  <TableCell className="text-xs text-muted-foreground">
                    {s.lastScheduledAt ? new Date(s.lastScheduledAt).toLocaleString() : "—"}
                  </TableCell>
```

to:

```tsx
                  <TableCell className="text-xs text-muted-foreground" title={s.lastScheduledAt ? new Date(s.lastScheduledAt).toLocaleString() : undefined}>
                    {s.lastScheduledAt ? relativeTime(s.lastScheduledAt) : "—"}
                  </TableCell>
```

- [ ] **Step 3: Verify build**

Run: `pnpm build`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add controlplane/web/src/pages/SchedulesPage.tsx
git commit -m "feat(web): Schedules relative Last Fired time"
```

---

## Task 13: Full verification — build, tests, deploy, Playwright

**Files:** none (verification + deploy only)

- [ ] **Step 1: Full web build + unit tests**

```bash
cd controlplane/web
pnpm build
pnpm test
```
Expected: build PASS; all Vitest files pass (`stats`, `verdict`, `time`, `category`, `runsFilter`, `steps`, `format`).

- [ ] **Step 2: Embed build + Go sanity**

```bash
cd controlplane
make ui-build
make build
go test ./...
```
Expected: `make ui-build` copies `web/dist` → `internal/api/dist`; `make build` compiles the binary; `go test ./...` stays green (no Go changed).

- [ ] **Step 3: Reload into minikube**

```bash
cd controlplane && make reload-minikube
kubectl -n dlh-test-fw rollout restart deployment/dlh-controlplane
kubectl -n dlh-test-fw rollout status deployment/dlh-controlplane --timeout=60s
```

Note (known local-dev gotcha): the image build pulls `gcr.io/distroless/static-debian12:nonroot`, which can be unreachable on this machine (DNS). If `make image`/`reload-minikube` fails on that pull, apply the documented local workaround — temporarily set the runtime stage in `controlplane/Dockerfile` to `FROM alpine:3.19` + `RUN adduser -D -u 65532 nonroot`, build/reload, then revert the Dockerfile. Do NOT commit the Dockerfile change.

- [ ] **Step 4: Playwright dev-time verification (via MCP)**

Port-forward and drive the browser:
```bash
kubectl -n dlh-test-fw port-forward svc/dlh-controlplane 18080:80
```
Then, using the Playwright MCP tools at 1440×900 (and once at ~700px wide for responsive sanity), confirm:
- **Runs:** compact stat cards with icons; type "mysql" in search → table filters; status/category/time selects + "Failed only" toggle filter; click "Started"/"Duration" headers → sort flips; rows show relative time (hover = absolute), Duration, and Verdict ✓/✗/—; clicking a row navigates to Run detail.
- **Scenarios:** sections CHAOS/FIXTURE/LOAD/VERDICT/UTIL with counts + colored icons; search filters across groups; cards show derived target-type + Run button.
- **Run detail:** back-link, category icon + run id + status; meta strip; Verdict above Steps with formatted values (e.g. `3.50e-6`); Steps list shows only named steps (no `[0]`/`[1]`), header notes "group nodes hidden".
- **Targets:** empty-state renders (live cluster has none).
- **Schedules:** click "+ New schedule" → dialog; create a throwaway schedule → appears in table with relative Last Fired; "delete" → AlertDialog confirm → row removed. (Verifies populated table + dialogs.)
- **Theme toggle** flips dark/light and persists across reload; nav active pill tracks the route.

Capture a screenshot of each page; if anything is visually off, fix in the relevant page file and re-run `pnpm build` before continuing.

- [ ] **Step 5: Update `CLAUDE.md` (controlplane UI refresh subsection)**

In `/Users/allen/repo/dlh-test-fw/CLAUDE.md`, under the existing `### controlplane UI refresh` subsection, append:

```markdown
- UI optimization pass (Plan `2026-05-23-01`): refined top-nav (icons + active
  pill, `max-w-7xl`), Runs filter bar (search/status/category/time/failed-only)
  + Duration/Verdict columns (Verdict derived from `Run.score` 1/0/null), grouped
  Scenarios by derived category, redesigned Run detail (meta strip, verdict-first,
  Argo group-node steps hidden). Pure logic in `src/lib/{time,category,run,runsFilter,steps,format}.ts`
  is Vitest-tested. `deriveCategory`/`deriveTargetType` are heuristic on scenario id.
```

- [ ] **Step 6: Commit docs**

```bash
cd /Users/allen/repo/dlh-test-fw
git add CLAUDE.md
git commit -m "docs: note UI optimization (Plan 01) conventions in CLAUDE.md"
```

---

## Task 14: Merge to main

**Files:** none

- [ ] **Step 1: Merge `--no-ff` per repo convention**

```bash
cd /Users/allen/repo/dlh-test-fw
git checkout main
git merge --no-ff feat/controlplane-ui-optimization -m "Merge feat/controlplane-ui-optimization: web UI optimization (Plan 01)

Refined nav, Runs filter bar + Duration/Verdict columns, grouped Scenarios,
redesigned Run detail (verdict-first, clean steps), Targets inline test
result, Schedules relative time. Pure logic (time/category/run/runsFilter/
steps/format) covered by Vitest. No backend changes — deep-linking is Plan 02."
```

- [ ] **Step 2: Remove the worktree (if one was used)**

```bash
git worktree remove ../dlh-test-fw-ui-opt
```
(Add `--force` if a build artifact lingers.)

---

## Notes & deviations from the spec

- **Verdict column data source:** the Runs *list* endpoint exposes `score` (1.0/0.0/null), not a verdict object, so the Verdict chip is derived via `verdictFromScore`. This is exact, not heuristic (the syncer maps `report.json` `overall` → 1.0/0.0). When a run has no verdict report, `score` is null → `—`.
- **`deriveTargetType` duplication:** this plan adds the TS copy (Scenarios card label). The Go copy (Grafana dashboard mapping) is Plan 02. The two rule lists must stay identical — see the spec risk note.
- **Schedules already had dialogs:** the prior refresh shipped the create-`Dialog` + delete-`AlertDialog` + toasts, so Task 12 is just the relative-time polish, not a rebuild.
- **VerdictView formatting** is folded into Task 10 (it's the consumer of `formatMetricValue` and lives beside the Run detail work).
- **Filter selects use a `__any__` sentinel** mapped to `""` because Radix `Select` forbids empty-string item values (same pattern as the existing `TargetPicker` `__local__`).
