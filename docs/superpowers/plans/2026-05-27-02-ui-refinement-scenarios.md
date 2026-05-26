# UI Refinement — Scenarios Page Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Apply Direction A to the Scenarios page — refined cards (target glyph, target-type chip + default tier, readable aligned descriptions, last-run + verdict), a `PageHeader` subtitle with a live count, and the tightened inline run control.

**Architecture:** Builds on the Plan-01 foundation (theme tokens + `TargetGlyph`, `VerdictPill`, `PageHeader.subtitle`). Presentation-only: no API changes. Two facts the `Scenario` object lacks — the **default priority** and the **last run/verdict** — are sourced from *existing* endpoints consumed client-side: default tier from `GET /api/scenario-priorities` (viewer), last-run from `GET /api/runs` reduced per scenario via a new pure helper. Runs on branch `feat/ui-refinement-foundation` (the foundation is not yet merged; this continues on top of it).

**Tech Stack:** React 18, Vite, Tailwind, shadcn/ui, lucide-react, Vitest. Commands from `controlplane/web`.

**Conventions:** NEVER `git add -A`/globs — stage only named files; never commit `dist/`. Build gate `pnpm build`; test `pnpm test`. Commit trailer `Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>`.

---

## File Structure

- **Create:** `web/src/lib/scenarioRuns.ts` + `web/src/lib/scenarioRuns.test.ts` — pure "latest run per scenario" reducer.
- **Modify:** `web/src/pages/ScenariosPage.tsx` — fetch scenario-priorities + runs; compute default-tier + last-run maps; refine card markup + subtitle.

Existing reused exports (verified): `TargetGlyph` (`@/components/TargetGlyph`), `VerdictPill` (`@/components/VerdictPill`), `PageHeader` (now takes `subtitle`), `tierForPriority(priority)→TierLabel|null` (`@/lib/tier`), `relativeTime(iso)` (`@/lib/time`), `deriveCategory`/`deriveTargetType`/`CATEGORIES` (`@/lib/category`). `Scenario` has `id, displayName, description?, targetType?`. `ScenarioPriority` has `scenario, baked, override?, effective`.

---

### Task 1: `lastRunByScenario` pure reducer

**Files:**
- Create: `web/src/lib/scenarioRuns.ts`
- Create: `web/src/lib/scenarioRuns.test.ts`

- [ ] **Step 1: Write the failing test**

Create `web/src/lib/scenarioRuns.test.ts`:

```ts
import { describe, it, expect } from "vitest";
import { lastRunByScenario } from "@/lib/scenarioRuns";
import type { components } from "@/api/gen";

type Run = components["schemas"]["Run"];
const run = (scenario: string, startedAt: string, score: number | null = null): Run =>
  ({ id: `${scenario}-${startedAt}`, scenario, status: "Succeeded", startedAt, score }) as Run;

describe("lastRunByScenario", () => {
  it("keeps the latest run per scenario", () => {
    const m = lastRunByScenario([
      run("mysql-pod-delete", "2026-05-26T10:00:00Z", 1),
      run("mysql-pod-delete", "2026-05-26T12:00:00Z", 0),
      run("kafka-broker-partition", "2026-05-26T11:00:00Z", 1),
    ]);
    expect(m["mysql-pod-delete"]).toEqual({ startedAt: "2026-05-26T12:00:00Z", score: 0 });
    expect(m["kafka-broker-partition"]).toEqual({ startedAt: "2026-05-26T11:00:00Z", score: 1 });
  });
  it("returns an empty map for no runs", () => {
    expect(lastRunByScenario([])).toEqual({});
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd controlplane/web && pnpm test -- scenarioRuns`
Expected: FAIL — module missing.

- [ ] **Step 3: Implement**

Create `web/src/lib/scenarioRuns.ts`:

```ts
import type { components } from "@/api/gen";

type Run = components["schemas"]["Run"];
export type LastRun = { startedAt: string; score: number | null | undefined };

// Reduce a run list to the most-recent run per scenario id (by startedAt).
export function lastRunByScenario(runs: Run[]): Record<string, LastRun> {
  const out: Record<string, LastRun> = {};
  for (const r of runs) {
    const cur = out[r.scenario];
    if (!cur || new Date(r.startedAt).getTime() > new Date(cur.startedAt).getTime()) {
      out[r.scenario] = { startedAt: r.startedAt, score: r.score };
    }
  }
  return out;
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd controlplane/web && pnpm test -- scenarioRuns`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/web/src/lib/scenarioRuns.ts controlplane/web/src/lib/scenarioRuns.test.ts
git commit -m "feat(web): lastRunByScenario reducer

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 2: Refine the Scenarios page

**Files:**
- Modify: `web/src/pages/ScenariosPage.tsx`

**Context:** The page currently fetches `/api/scenarios` only and renders category-grouped cards with a `CategoryIcon` tile + `displayName` + target-type + the inline run control. This task adds two best-effort fetches (`/api/scenario-priorities`, `/api/runs`) to drive **default tier** and **last-run/verdict**, then refines the card. Keep the category grouping, search, and submit logic intact.

- [ ] **Step 1: Add imports + the two data fetches**

In `web/src/pages/ScenariosPage.tsx`, add imports:

```tsx
import { TargetGlyph } from "@/components/TargetGlyph";
import { VerdictPill } from "@/components/VerdictPill";
import { tierForPriority } from "@/lib/tier";
import { relativeTime } from "@/lib/time";
import { lastRunByScenario, type LastRun } from "@/lib/scenarioRuns";
```

Add state alongside the existing `items`/`error` state:

```tsx
  const [defaults, setDefaults] = useState<Record<string, number>>({});
  const [lastRuns, setLastRuns] = useState<Record<string, LastRun>>({});
```

Extend the existing `useEffect` (the one that GETs `/api/scenarios`) to also fetch priorities + runs (best-effort — failures just leave the maps empty; they're decorative):

```tsx
  useEffect(() => {
    api.GET("/api/scenarios", {}).then(({ data, error }) => {
      if (error) setError(error);
      else setItems(data?.items ?? []);
    });
    api.GET("/api/scenario-priorities", {}).then(({ data }) => {
      const m: Record<string, number> = {};
      for (const sp of data?.items ?? []) m[sp.scenario] = sp.effective;
      setDefaults(m);
    });
    api.GET("/api/runs", {}).then(({ data }) => {
      setLastRuns(lastRunByScenario(data?.items ?? []));
    });
  }, []);
```

- [ ] **Step 2: Add the subtitle**

Change the `<PageHeader title="Scenarios" …>` to include the count subtitle (keep the existing search `action`):

```tsx
      <PageHeader
        title="Scenarios"
        subtitle={items ? <>Pick a scenario and launch a run · <span className="tabular-nums text-foreground/70">{items.length}</span> available</> : "Pick a scenario and launch a run"}
        action={
          <div className="relative">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <Input value={search} onChange={(e) => setSearch(e.target.value)} placeholder="Search scenarios…" className="h-8 w-[220px] pl-8" />
          </div>
        }
      />
```

- [ ] **Step 3: Refine the card markup**

Replace the `<Card className="h-full">…</Card>` block (inside the `scns.map`) with:

```tsx
                      <Card className="h-full transition hover:ring-1 hover:ring-primary/40">
                        <CardContent className="flex h-full flex-col p-4">
                          <div className="flex items-start gap-3">
                            <TargetGlyph scenario={s.id} />
                            <div className="min-w-0">
                              <div className="truncate font-medium">{s.displayName}</div>
                              <div className="mt-0.5 flex items-center gap-1.5 text-xs">
                                <span className="rounded bg-accent px-1.5 py-0.5 font-medium uppercase tracking-wide text-muted-foreground">{tt}</span>
                                {defaults[s.id] != null && (
                                  <span className="text-muted-foreground">default {tierForPriority(defaults[s.id]) ?? defaults[s.id]}</span>
                                )}
                              </div>
                            </div>
                          </div>

                          <p className="mt-2.5 line-clamp-2 min-h-[2.5rem] text-sm leading-relaxed text-muted-foreground">
                            {s.description ?? "—"}
                          </p>

                          <div className="mt-2 flex items-center gap-1.5 text-xs text-muted-foreground">
                            {lastRuns[s.id] ? (
                              <>last run {relativeTime(lastRuns[s.id].startedAt)} <VerdictPill score={lastRuns[s.id].score} /></>
                            ) : (
                              <span>no runs yet</span>
                            )}
                          </div>

                          <div className="mt-3 flex items-center gap-2 border-t pt-3">
                            <Input
                              type="number"
                              value={submitPriority[s.id] ?? ""}
                              onChange={(e) => setSubmitPriority((r) => ({ ...r, [s.id]: e.target.value }))}
                              placeholder="prio"
                              title="Priority override (blank = scenario default)"
                              className="h-8 w-[64px]"
                            />
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
```

(The `CategoryIcon` import may become unused on the card tile but is still used in the category section header — keep the import. `tt` is the existing `const tt = s.targetType ?? deriveTargetType(s.id);`.)

- [ ] **Step 4: Verify build + tests**

Run: `cd controlplane/web && pnpm build && pnpm test`
Expected: build exit 0; all tests pass (incl. the new `scenarioRuns` tests).

- [ ] **Step 5: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/web/src/pages/ScenariosPage.tsx
git commit -m "feat(web): apply Direction A to Scenarios page

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 3: Visual verification

**Files:** none.

- [ ] **Step 1: Build + deploy**

Run:
```bash
cd /Users/allen/repo/dlh-test-fw/controlplane && make ui-build && make reload-minikube
kubectl -n dlh-test-fw rollout restart deployment/dlh-controlplane
kubectl -n dlh-test-fw rollout status deployment/dlh-controlplane --timeout=120s
```
(Or verify on the `:5173` vite dev server.)

- [ ] **Step 2: Playwright check**

Navigate to `http://localhost:8080/scenarios`. Confirm: subtitle "· N available"; cards show target glyph, displayName, target-type chip + default tier (where set), 2-line aligned descriptions, last-run + verdict pill (where a run exists), tightened run control. Toggle theme — clean in both. **0 console errors.**

---

## Self-Review

**Spec coverage (Scenarios):** target glyph ✓ (TargetGlyph), target-type chip + default tier ✓ (chip + `tierForPriority(defaults[id])`), 2-line aligned description ✓ (`line-clamp-2 min-h-[2.5rem] text-sm leading-relaxed`), last-run + verdict ✓ (lastRunByScenario + VerdictPill), tightened run control ✓, subtitle "· N available" ✓.

**Placeholder scan:** none. The two extra fetches are best-effort/decorative (maps default empty); the page still works if they fail.

**Type consistency:** `LastRun` defined in Task 1, imported in Task 2. `lastRunByScenario`, `tierForPriority`, `relativeTime`, `TargetGlyph`, `VerdictPill` all reused with confirmed signatures. `sp.effective` is a plain `number` (ScenarioPriority.effective is required). `defaults[s.id]` guarded with `!= null`.

**Presentation-only check:** no OpenAPI/handler changes; only consumes existing GET endpoints (`/api/scenarios`, `/api/scenario-priorities`, `/api/runs`).
