# UI Refinement — Design-System Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land the "Direction A" design language as a reusable foundation — re-valued theme tokens (dark + light), restyled shared components, a few new shared primitives (stat panel, sparkline, description blocks, target glyph, segmented control), a refined nav — and apply it to the **Runs** page as the canonical reference.

**Architecture:** The UI is a Vite + React + Tailwind + shadcn SPA whose theme is driven by HSL CSS variables in `web/src/index.css` (`--background`, `--card`, `--primary`, `--status-*`, …) consumed via Tailwind tokens (`bg-background`, `text-primary`, `bg-status-success/15`, …). Direction A is implemented primarily by **re-valuing those CSS variables** (so the whole app inherits the new surfaces/accent/semantics), then restyling/adding a small set of shared components. Per-page application (Scenarios, Queue, Targets, Schedules, Run detail, Default priorities) follows in subsequent plans (see "Plan series" at the end). Spec: `docs/superpowers/specs/2026-05-27-ui-ux-refinement-design.md`.

**Tech Stack:** React 18, Vite 5, Tailwind 3 (`tailwindcss-animate`), shadcn/ui (vendored in `web/src/components/ui/`), lucide-react, Vitest, react-router-dom.

**Conventions (from CLAUDE.md / prior plans):**
- **NEVER `git add -A` / globs.** Stage only the files each task names. `controlplane/internal/api/dist/` and `controlplane/web/dist/` must NEVER be committed.
- All commands run from `controlplane/web` unless stated. Build gate: `pnpm build` (`tsc -b && vite build`). Test: `pnpm test` (Vitest). Pure logic only is unit-tested; styling/markup is gated by `pnpm build` + a Playwright visual check.
- **Local verification uses the Vite dev server** (`pnpm dev`, default `:5173`) which proxies `/api` + `/healthz` to the controlplane on `:8080` — fast, no Go rebuild. (Only `make ui-build` + image reload is needed to bake the UI into the Go binary; not required for these tasks.)
- Commit messages end with: `Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>`.
- Dark is the default theme; the light toggle (`web/src/lib/theme.tsx`, `localStorage["dlh-theme"]`) must stay first-class.

---

## File Structure

**Modify:**
- `web/src/index.css` — re-value all theme CSS variables (`:root` light + `.dark`) to Direction A; add `--accent-strong` for the indigo action color (shadcn's `--accent` is the hover surface, so the action indigo lives in `--primary`; no new var needed there). Add `--line` is NOT needed — we keep `border-border`.
- `web/src/App.tsx` — refined pill nav (logo mark, active = `bg-primary/15`, `max-w-7xl`).
- `web/src/components/PageHeader.tsx` — add optional `subtitle`.
- `web/src/components/StatusBadge.tsx` — pulse the dot when running.
- `web/src/components/ui/button.tsx` — primary uses `text-primary-foreground` (already); add a subtle ring on secondary/outline to match Direction A (small tweak).

**Create:**
- `web/src/lib/sparkline.ts` + `web/src/lib/sparkline.test.ts` — pure SVG polyline-points math.
- `web/src/components/Sparkline.tsx` — renders the polyline.
- `web/src/components/StatPanel.tsx` — the hairline-divided stat row (replaces 4 separate `StatCard`s on Runs).
- `web/src/components/InfoBand.tsx` — the "i" + emphasized-terms help band (Queue rules; reusable).
- `web/src/components/DescriptionBlock.tsx` — the left-accent quiet description (Run detail; reusable).
- `web/src/components/TargetGlyph.tsx` — target-type initials in a rounded square (table/card icon chip).
- `web/src/components/SegmentedTiers.tsx` — segmented tier control (Default priorities; built here so the token/contract is fixed).

**Unchanged behavior:** all `web/src/lib/*` logic except the new `sparkline.ts`. `StatCard.tsx` stays (other pages may still use it until their plans land) but Runs switches to `StatPanel`.

---

### Task 1: Re-value theme tokens (Direction A, dark + light)

**Files:**
- Modify: `web/src/index.css` (the `:root` and `.dark` blocks)

**Context:** shadcn vars are space-separated HSL **without** the `hsl()` wrapper. Direction A hex → HSL (computed): bg `#0a0c12`→`225 29% 6%`, card `#0e1118`→`221 26% 8%`, inset `#141822`→`222 26% 11%`, hover `#1a1f2b`→`222 25% 14%`, accent-indigo `#7c8cff`→`233 100% 74%`, accent-text `#aab4ff`→`233 100% 83%`, emerald `#34d399`→`158 64% 52%`, blue `#60a5fa`→`213 94% 68%`, rose `#fb7185`→`351 94% 71%`, amber `#fbbf24`→`43 96% 56%`. The key semantic change: **pending becomes amber** (was slate).

- [ ] **Step 1: Replace the `.dark` block**

In `web/src/index.css`, replace the entire `.dark { … }` block with:

```css
  .dark {
    --background: 225 29% 6%;
    --foreground: 210 40% 96%;
    --card: 221 26% 8%;
    --card-foreground: 210 40% 96%;
    --popover: 221 26% 8%;
    --popover-foreground: 210 40% 96%;
    --primary: 233 100% 74%;
    --primary-foreground: 225 29% 6%;
    --secondary: 222 26% 11%;
    --secondary-foreground: 210 40% 96%;
    --muted: 222 26% 11%;
    --muted-foreground: 215 18% 60%;
    --accent: 222 25% 14%;
    --accent-foreground: 210 40% 96%;
    --destructive: 351 94% 71%;
    --destructive-foreground: 225 29% 6%;
    --border: 222 22% 16%;
    --input: 222 24% 13%;
    --ring: 233 100% 74%;

    --status-success: 158 64% 52%;
    --status-running: 213 94% 68%;
    --status-failed: 351 94% 71%;
    --status-pending: 43 96% 56%;
  }
```

- [ ] **Step 2: Replace the `:root` (light) block**

Replace the entire `:root { … }` block with Direction A's light equivalents (near-white surfaces, same indigo accent slightly deepened for contrast, darker semantics):

```css
  :root {
    --background: 210 40% 99%;
    --foreground: 222 47% 11%;
    --card: 0 0% 100%;
    --card-foreground: 222 47% 11%;
    --popover: 0 0% 100%;
    --popover-foreground: 222 47% 11%;
    --primary: 233 80% 63%;
    --primary-foreground: 0 0% 100%;
    --secondary: 214 32% 95%;
    --secondary-foreground: 222 47% 11%;
    --muted: 214 32% 95%;
    --muted-foreground: 215 16% 42%;
    --accent: 214 32% 93%;
    --accent-foreground: 222 47% 11%;
    --destructive: 351 74% 52%;
    --destructive-foreground: 0 0% 100%;
    --border: 214 32% 89%;
    --input: 214 32% 89%;
    --ring: 233 80% 63%;
    --radius: 0.625rem;

    --status-success: 158 64% 36%;
    --status-running: 213 80% 50%;
    --status-failed: 351 74% 52%;
    --status-pending: 38 92% 44%;
  }
```

(Note `--radius` moved to `0.625rem` for the slightly rounder Direction A feel; it only exists in `:root` and is inherited by `.dark`.)

- [ ] **Step 3: Verify the build + eyeball the shift**

Run: `cd controlplane/web && pnpm build`
Expected: exit 0.

Run: `cd controlplane/web && pnpm dev` (leave running) and open `http://localhost:5173/runs`.
Expected: the whole app shifts to the truer dark-slate base with the indigo accent; **no layout change** (this task only re-values colors). Toggle the theme — light mode renders cleanly.

- [ ] **Step 4: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/web/src/index.css
git commit -m "feat(web): re-value theme tokens to Direction A (dark + light)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 2: Refined nav (logo mark, pill, max-w-7xl)

**Files:**
- Modify: `web/src/App.tsx` (the `<header>`/`<nav>` block, ~lines 79-106)

- [ ] **Step 1: Replace the header/nav markup**

In `web/src/App.tsx`, replace the `<header>…</header>` block with:

```tsx
      <header className="border-b">
        <nav className="mx-auto flex max-w-7xl items-center gap-1 px-6 py-3 text-sm">
          <span className="mr-4 flex items-center gap-2 font-semibold tracking-tight">
            <span className="grid h-6 w-6 place-items-center rounded-md bg-primary text-[13px] font-bold text-primary-foreground">d</span>
            dlh
          </span>
          {NAV.map((n) => (
            <NavLink
              key={n.to}
              to={n.to}
              className={({ isActive }) =>
                cn(
                  "flex items-center gap-1.5 rounded-lg px-3 py-1.5 transition-colors",
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
          <div className="ml-auto flex items-center gap-3">
            {identity && <span className="text-xs text-muted-foreground">{identity}</span>}
            <ThemeToggle />
          </div>
        </nav>
      </header>
```

- [ ] **Step 2: Verify**

Run: `cd controlplane/web && pnpm build`
Expected: exit 0. On `:5173`, the nav shows the `d` logo mark + pill nav items; the active item has the indigo-soft pill.

- [ ] **Step 3: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/web/src/App.tsx
git commit -m "feat(web): refined pill nav with logo mark

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 3: Sparkline math (pure) + component

**Files:**
- Create: `web/src/lib/sparkline.ts`
- Create: `web/src/lib/sparkline.test.ts`
- Create: `web/src/components/Sparkline.tsx`

- [ ] **Step 1: Write the failing test**

Create `web/src/lib/sparkline.test.ts`:

```ts
import { describe, it, expect } from "vitest";
import { sparklinePoints } from "@/lib/sparkline";

describe("sparklinePoints", () => {
  it("maps values to a polyline points string within the box", () => {
    const pts = sparklinePoints([0, 5, 10], 100, 20);
    // 3 points, evenly spaced on x (0, 50, 100); y inverted (0→bottom, 10→top)
    expect(pts).toBe("0,20 50,10 100,0");
  });
  it("handles a flat series (all equal) by drawing a mid line", () => {
    expect(sparklinePoints([4, 4, 4], 100, 20)).toBe("0,10 50,10 100,10");
  });
  it("returns empty string for <2 points", () => {
    expect(sparklinePoints([1], 100, 20)).toBe("");
    expect(sparklinePoints([], 100, 20)).toBe("");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd controlplane/web && pnpm test -- sparkline`
Expected: FAIL — `@/lib/sparkline` does not exist.

- [ ] **Step 3: Implement**

Create `web/src/lib/sparkline.ts`:

```ts
// Pure SVG-polyline math for a tiny trend sparkline. Returns a `points`
// string ("x,y x,y …") mapping values across [0,w] x [0,h], y-inverted
// (higher value = higher on screen). Flat series draw a mid line.
export function sparklinePoints(values: number[], w: number, h: number): string {
  if (values.length < 2) return "";
  const min = Math.min(...values);
  const max = Math.max(...values);
  const span = max - min;
  const stepX = w / (values.length - 1);
  return values
    .map((v, i) => {
      const x = Math.round(i * stepX);
      const y = span === 0 ? h / 2 : h - ((v - min) / span) * h;
      return `${x},${Math.round(y)}`;
    })
    .join(" ");
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd controlplane/web && pnpm test -- sparkline`
Expected: PASS (3 tests).

- [ ] **Step 5: Create the component**

Create `web/src/components/Sparkline.tsx`:

```tsx
import { sparklinePoints } from "@/lib/sparkline";

export function Sparkline({ values, className }: { values: number[]; className?: string }) {
  const pts = sparklinePoints(values, 120, 24);
  if (!pts) return null;
  return (
    <svg viewBox="0 0 120 24" className={className} preserveAspectRatio="none" aria-hidden>
      <polyline
        points={pts}
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}
```

- [ ] **Step 6: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/web/src/lib/sparkline.ts controlplane/web/src/lib/sparkline.test.ts controlplane/web/src/components/Sparkline.tsx
git commit -m "feat(web): sparkline points helper + component

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 4: StatPanel (hairline-divided stat row)

**Files:**
- Create: `web/src/components/StatPanel.tsx`

**Context:** Replaces the 4 separate `StatCard`s with one divided panel. Each stat is `{label, value, secondary?, accent?, trend?}`. `trend` (optional `number[]`) renders the Sparkline in the accent color. `accent` keys map to existing tokens.

- [ ] **Step 1: Create the component**

Create `web/src/components/StatPanel.tsx`:

```tsx
import { type ReactNode } from "react";
import { Sparkline } from "@/components/Sparkline";
import { cn } from "@/lib/utils";

export type Stat = {
  label: string;
  value: ReactNode;
  secondary?: ReactNode;
  accent?: "success" | "running" | "failed" | "default";
  trend?: number[];
};

const ACCENT: Record<NonNullable<Stat["accent"]>, string> = {
  success: "text-status-success",
  running: "text-status-running",
  failed: "text-status-failed",
  default: "text-foreground",
};

export function StatPanel({ stats }: { stats: Stat[] }) {
  return (
    <div
      className="grid divide-x divide-border overflow-hidden rounded-xl border bg-card"
      style={{ gridTemplateColumns: `repeat(${stats.length}, minmax(0, 1fr))` }}
    >
      {stats.map((s, i) => {
        const accent = ACCENT[s.accent ?? "default"];
        return (
          <div key={i} className="p-5">
            <div className="flex items-center justify-between">
              <span className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{s.label}</span>
            </div>
            <div className={cn("mt-2 text-3xl font-semibold tracking-tight tabular-nums", accent)}>{s.value}</div>
            {s.trend && <Sparkline values={s.trend} className={cn("mt-2 h-6 w-full", accent)} />}
            {s.secondary && <div className="mt-1 text-xs text-muted-foreground">{s.secondary}</div>}
          </div>
        );
      })}
    </div>
  );
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd controlplane/web && pnpm build`
Expected: exit 0 (component is unused until Task 7 — that's fine; tsc allows unused exports).

- [ ] **Step 3: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/web/src/components/StatPanel.tsx
git commit -m "feat(web): StatPanel hairline-divided stat row

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 5: Shared description-block + glyph + segmented primitives

**Files:**
- Modify: `web/src/components/PageHeader.tsx`
- Create: `web/src/components/InfoBand.tsx`
- Create: `web/src/components/DescriptionBlock.tsx`
- Create: `web/src/components/TargetGlyph.tsx`
- Create: `web/src/components/SegmentedTiers.tsx`

- [ ] **Step 1: Add `subtitle` to PageHeader**

Replace `web/src/components/PageHeader.tsx` with:

```tsx
import { type ReactNode } from "react";

export function PageHeader({
  title,
  subtitle,
  action,
}: {
  title: string;
  subtitle?: ReactNode;
  action?: ReactNode;
}) {
  return (
    <div className="mb-6 flex items-end justify-between">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">{title}</h1>
        {subtitle && <p className="mt-0.5 text-sm text-muted-foreground">{subtitle}</p>}
      </div>
      {action}
    </div>
  );
}
```

- [ ] **Step 2: Create InfoBand** (the Queue "i + emphasized terms" help band)

Create `web/src/components/InfoBand.tsx`:

```tsx
import { type ReactNode } from "react";

export function InfoBand({ children }: { children: ReactNode }) {
  return (
    <div className="flex items-center gap-3 rounded-lg border bg-card px-4 py-3 text-xs">
      <span className="grid h-5 w-5 shrink-0 place-items-center rounded-full bg-primary/15 text-[11px] font-semibold text-primary">i</span>
      <span className="text-muted-foreground">{children}</span>
    </div>
  );
}

// Helper to emphasize a key term inside an InfoBand body.
export function Term({ children }: { children: ReactNode }) {
  return <span className="font-medium text-foreground">{children}</span>;
}
```

- [ ] **Step 3: Create DescriptionBlock** (run-detail left-accent quiet block)

Create `web/src/components/DescriptionBlock.tsx`:

```tsx
import { type ReactNode } from "react";
import { cn } from "@/lib/utils";

export function DescriptionBlock({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <p className={cn("max-w-2xl border-l-2 border-primary/40 pl-3 text-sm leading-relaxed text-muted-foreground", className)}>
      {children}
    </p>
  );
}
```

- [ ] **Step 4: Create TargetGlyph** (target-type initials chip)

Create `web/src/components/TargetGlyph.tsx`:

```tsx
import { deriveTargetType } from "@/lib/category";
import { cn } from "@/lib/utils";

// Two-letter glyph derived from the scenario's target type (mysql→MY, etc.).
export function TargetGlyph({ scenario, className }: { scenario: string; className?: string }) {
  const initials = deriveTargetType(scenario).slice(0, 2).toUpperCase();
  return (
    <span className={cn("grid h-7 w-7 shrink-0 place-items-center rounded-lg bg-accent text-[11px] font-semibold uppercase text-muted-foreground", className)}>
      {initials}
    </span>
  );
}
```

> Note: confirm `deriveTargetType` is exported from `web/src/lib/category.ts` (it is — used by RunDetailPage/links). If the export name differs, match it.

- [ ] **Step 5: Create SegmentedTiers** (segmented tier control)

Create `web/src/components/SegmentedTiers.tsx`:

```tsx
import { TIERS } from "@/lib/tier";
import { cn } from "@/lib/utils";

// Segmented control over the named priority tiers. `value` is the current
// numeric priority; the matching tier (if any) is highlighted. onPick fires
// the tier's numeric value.
export function SegmentedTiers({ value, onPick }: { value: number; onPick: (priority: number) => void }) {
  return (
    <div className="inline-flex overflow-hidden rounded-lg border">
      {TIERS.map((t, i) => (
        <button
          key={t.label}
          onClick={() => onPick(t.value)}
          className={cn(
            "px-2.5 py-1 text-xs transition-colors",
            i > 0 && "border-l",
            value === t.value
              ? "bg-primary font-medium text-primary-foreground"
              : "text-muted-foreground hover:bg-accent"
          )}
        >
          {t.label}
        </button>
      ))}
    </div>
  );
}
```

> `TIERS` is `[{label,value}]` from `web/src/lib/tier.ts` (created in the priority milestone).

- [ ] **Step 6: Verify build**

Run: `cd controlplane/web && pnpm build && pnpm test`
Expected: build exit 0; all existing tests pass (no behavior change).

- [ ] **Step 7: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/web/src/components/PageHeader.tsx controlplane/web/src/components/InfoBand.tsx controlplane/web/src/components/DescriptionBlock.tsx controlplane/web/src/components/TargetGlyph.tsx controlplane/web/src/components/SegmentedTiers.tsx
git commit -m "feat(web): shared description-block, glyph, segmented-tier primitives

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 6: StatusBadge pulse + VerdictPill cell helper

**Files:**
- Modify: `web/src/components/StatusBadge.tsx`
- Create: `web/src/components/VerdictPill.tsx`

- [ ] **Step 1: Pulse the running dot**

Replace `web/src/components/StatusBadge.tsx` with:

```tsx
import { cn } from "@/lib/utils";

const STATUS_STYLES: Record<string, string> = {
  Succeeded: "bg-status-success/15 text-status-success",
  Running: "bg-status-running/15 text-status-running",
  Failed: "bg-status-failed/15 text-status-failed",
  Error: "bg-status-failed/15 text-status-failed",
  Pending: "bg-status-pending/15 text-status-pending",
  Unknown: "bg-status-pending/15 text-status-pending",
};

export function StatusBadge({ status }: { status: string }) {
  const style = STATUS_STYLES[status] ?? STATUS_STYLES.Unknown;
  return (
    <span className={cn("inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium", style)}>
      <span className={cn("h-1.5 w-1.5 rounded-full bg-current", status === "Running" && "animate-pulse")} />
      {status}
    </span>
  );
}
```

- [ ] **Step 2: Create VerdictPill** (the pass/fail chip used in list/table cells)

Create `web/src/components/VerdictPill.tsx`:

```tsx
import { verdictFromScore } from "@/lib/run";
import { cn } from "@/lib/utils";

// Compact pass/fail chip from a run score (1 = pass, 0 = fail, null = none).
export function VerdictPill({ score }: { score: number | null | undefined }) {
  const v = verdictFromScore(score);
  if (v == null) return <span className="text-muted-foreground">—</span>;
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 rounded-md px-2 py-0.5 text-xs font-semibold",
        v === "pass" ? "bg-status-success/15 text-status-success" : "bg-status-failed/15 text-status-failed"
      )}
    >
      {v === "pass" ? "✓ pass" : "✗ fail"}
    </span>
  );
}
```

> `verdictFromScore` is exported from `web/src/lib/run.ts` (returns `"pass" | "fail" | null`). Confirm the signature; if it takes a different arg shape, match it.

- [ ] **Step 3: Verify**

Run: `cd controlplane/web && pnpm build`
Expected: exit 0.

- [ ] **Step 4: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/web/src/components/StatusBadge.tsx controlplane/web/src/components/VerdictPill.tsx
git commit -m "feat(web): StatusBadge running pulse + VerdictPill cell chip

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 7: Apply Direction A to the Runs page (reference page)

**Files:**
- Modify: `web/src/pages/RunsPage.tsx`

**Context:** Read the current `RunsPage.tsx` first. It computes stats (via `web/src/lib/stats.ts`) and renders 4 `StatCard`s + a filter toolbar + a table. This task: (a) swap the 4 cards for one `StatPanel`, (b) add the `subtitle` via `PageHeader` (or its inline header), (c) restyle table rows to use `TargetGlyph`, `StatusBadge` (now pulsing), and `VerdictPill`, with right-aligned mono Priority/Duration. Keep all data wiring, filters, sorting, and the 5s poll exactly as-is.

- [ ] **Step 1: Read the current page**

Run: `sed -n '1,220p' controlplane/web/src/pages/RunsPage.tsx`
Note the exact names: the stats object/shape from `computeStats`, the `runs` array type, the filter state, and the existing table header/body markup (the priority milestone added the Priority column + verdict cell).

- [ ] **Step 2: Replace the stat cards with StatPanel**

Add imports at the top of `RunsPage.tsx`:

```tsx
import { StatPanel, type Stat } from "@/components/StatPanel";
import { TargetGlyph } from "@/components/TargetGlyph";
import { VerdictPill } from "@/components/VerdictPill";
```

Replace the 4-`StatCard` grid block with a `StatPanel`. **Important:** `computeStats` (see `web/src/lib/stats.ts`) returns `passRate7d` as a **0–1 ratio or `null`** (NOT a percent), plus `runsToday`, `runningNow`, `activeSchedules`. It does **not** provide trend or secondary-context data, so those StatPanel fields are left undefined here (adding a 7-day pass-rate series / secondary lines is real derivation logic, deferred to the Runs enhancement plan — do NOT invent it here). Format pass-rate exactly as the current page does (ratio → rounded percent, `—` when null):

```tsx
const statItems: Stat[] = [
  { label: "Pass rate · 7d", value: stats.passRate7d == null ? "—" : `${Math.round(stats.passRate7d * 100)}%`, accent: "success" },
  { label: "Runs today", value: stats.runsToday },
  { label: "Running now", value: stats.runningNow, accent: "running" },
  { label: "Active schedules", value: stats.activeSchedules },
];
```

```tsx
<StatPanel stats={statItems} />
```

> Cross-check Step 1: if the current page formats `passRate7d` via a shared helper, reuse that exact formatting instead of inlining `Math.round`. The StatPanel sparkline (`trend`) stays unused until a pass-rate-history series exists — that's the deferred Runs enhancement, not this task.

- [ ] **Step 3: Restyle the table rows**

In the table body row, replace the scenario cell, status cell, priority cell, duration cell, and verdict cell to:

```tsx
<TableCell>
  <div className="flex items-center gap-2.5">
    <TargetGlyph scenario={r.scenario} />
    <span className="font-medium">{r.scenario}</span>
  </div>
</TableCell>
<TableCell className="text-muted-foreground">{r.target || "local"}</TableCell>
<TableCell><StatusBadge status={String(r.status)} /></TableCell>
<TableCell className="text-right font-mono tabular-nums text-muted-foreground">{r.priority ?? "—"}</TableCell>
<TableCell className="text-muted-foreground tabular-nums" title={new Date(r.startedAt).toLocaleString()}>{relativeTime(r.startedAt)}</TableCell>
<TableCell className="text-right font-mono tabular-nums text-muted-foreground">{formatDuration(r.startedAt, r.finishedAt)}</TableCell>
<TableCell><VerdictPill score={r.score} /></TableCell>
```

And make the matching `<TableHead>` cells for Priority and Duration right-aligned: add `className="text-right"` to those two headers.

(Keep the row `onClick`/navigation, `key`, and any existing classes like `cursor-pointer`.)

- [ ] **Step 4: Verify build + visual**

Run: `cd controlplane/web && pnpm build && pnpm test`
Expected: build exit 0; tests pass.

With `pnpm dev` running, open `http://localhost:5173/runs`. Expected: single hairline stat panel (pass-rate accent green, running accent blue), table rows with target glyphs, pulsing running dot, right-aligned mono Priority/Duration, pass/fail verdict chips. Toggle light theme — still clean.

- [ ] **Step 5: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/web/src/pages/RunsPage.tsx
git commit -m "feat(web): apply Direction A to Runs page (reference)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 8: Foundation visual verification (Playwright, against live API)

**Files:** none (verification only)

- [ ] **Step 1: Bake the UI + deploy (so Playwright hits the real app)**

Run:
```bash
cd /Users/allen/repo/dlh-test-fw/controlplane && make ui-build && make reload-minikube
kubectl -n dlh-test-fw rollout restart deployment/dlh-controlplane
kubectl -n dlh-test-fw rollout status deployment/dlh-controlplane --timeout=120s
```
(Or verify purely on the `:5173` dev server if the cluster isn't running — the dev server proxies to `:8080`.)

- [ ] **Step 2: Seed a little data** (so Runs isn't empty)

Run (port-forward if needed):
```bash
cd /Users/allen/repo/dlh-test-fw/controlplane
DLH_TOKEN="fake:runner:runner@local:dlh-runner" go run ./cmd/dlh run mysql-pod-delete --priority 200 --endpoint http://localhost:8080
```

- [ ] **Step 3: Playwright check**

Navigate to `http://localhost:8080/runs` (or `:5173`), screenshot. Confirm: refined nav + stat panel + table chips render; **0 console errors**; theme toggle works. This is the foundation acceptance gate.

- [ ] **Step 4: No commit** (verification task). Record any issues as follow-ups.

---

## Plan series (subsequent per-page plans — write after this lands)

Each consumes the foundation primitives from this plan and is independently shippable + visually verifiable. Write each in full (`writing-plans`) once the foundation has merged and the shared-component APIs are final:

- **`2026-05-27-02-ui-refinement-scenarios.md`** — Scenarios cards: `TargetGlyph`, target-type chip + default tier, `text-sm leading-relaxed` description with `min-h` row alignment, last-run + `VerdictPill`, tightened inline run control; `PageHeader` subtitle "· N available".
- **`2026-05-27-03-ui-refinement-queue.md`** — Queue lanes: slot indicator (`n/1`), running elapsed, queued to-front/cancel, idle dashed state, `InfoBand` rules band (with `Term`).
- **`2026-05-27-04-ui-refinement-targets.md`** — Targets cards: reachable/unreachable status pill, Type/Latency/Runs stat row, Test-connection; empty state; `PageHeader` subtitle "· N registered".
- **`2026-05-27-05-ui-refinement-schedules.md`** — Schedules table + active/paused dot + `VerdictPill`; **create → modal/drawer form** (behavior decision); `PageHeader` subtitle "· N active".
- **`2026-05-27-06-ui-refinement-default-priorities.md`** — `SegmentedTiers` control, effective input, override status; **auto-save** on tier-pick/input-commit with toast (behavior decision).
- **`2026-05-27-07-ui-refinement-run-detail.md`** — Run detail: title row + deep-links, `DescriptionBlock`, hairline meta strip, verdict-first `VerdictView` (already close — align window-chip colors to tokens), steps timeline + legend.

---

## Self-Review

**Spec coverage (foundation portion):**
- Surfaces/accent/semantics tokens (dark+light) → Task 1. ✓ (pending→amber included.)
- Type scale + mono numerics → applied in StatPanel/table (Task 4, 7); the scale is utility-class convention, documented in the spec, used here. ✓
- Status dot+pill / verdict chip → Task 6 (StatusBadge pulse, VerdictPill). ✓
- Stat row (hairline panel + sparkline + secondary) → Tasks 3, 4, 7. ✓
- Nav refinement → Task 2. ✓
- Description-block treatments (page subtitle / card / detail / rules band) → Task 5 (`PageHeader.subtitle`, `DescriptionBlock`, `InfoBand`) + applied per-page in the series. Scenario card-description treatment is page-local (Scenarios plan) but uses the documented utility pattern. ✓
- Segmented tier control → Task 5 (`SegmentedTiers`), applied in the Default-priorities plan. ✓
- Per-page application → deferred to the Plan series (explicitly listed). ✓
- Light theme parity → Task 1 `:root`. ✓
- Behavior decisions (modal, auto-save) → in the respective per-page plans. ✓

**Placeholder scan:** No TBD/TODO. The one conditional ("if `computeStats` doesn't expose X, omit") is a deliberate guard, not a placeholder — it instructs reading the real shape in Step 1 and passing only existing fields; StatPanel renders correctly with optional fields absent.

**Type consistency:** `Stat` type defined in Task 4 and imported in Task 7. `TargetGlyph`/`VerdictPill`/`StatPanel`/`SegmentedTiers`/`InfoBand`/`DescriptionBlock` names consistent between definition (Tasks 4-6) and consumption (Task 7 + series). `verdictFromScore` (lib/run), `TIERS` (lib/tier), `deriveTargetType` (lib/category) are reused, not redefined — Step notes flag verifying their exact exports against the codebase before relying on them.
