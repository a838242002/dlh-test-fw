# UI Refinement — Queue Page Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Apply the remaining Direction A refinements to the Queue page — a slot indicator (`n/1`, accented when running), the `InfoBand` rules band with emphasized key terms, running-entry elapsed time, and a consistent `PageHeader`.

**Architecture:** The Queue page already implements lanes + running/queued sections + to-front/cancel + idle state (built in the priority milestone) and already inherits the Direction A tokens from Plan 01. This is a small markup-only refinement — no new logic, no API changes. Branch `feat/ui-refinement-foundation` (continuing the series).

**Tech Stack:** React, Tailwind, shadcn/ui, lucide-react. Commands from `controlplane/web`.

**Conventions:** NEVER `git add -A`/globs; never commit `dist/`. Build gate `pnpm build`. Commit trailer `Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>`.

---

## File Structure

- **Modify:** `web/src/pages/QueuePage.tsx` — `PageHeader` (was inline `<h1>`), `InfoBand`+`Term` (was a plain `<p>`), slot indicator `n/1` accented, running elapsed.

Reused from foundation: `PageHeader` (`@/components/PageHeader`), `InfoBand`+`Term` (`@/components/InfoBand`), `cn` (`@/lib/utils`), `relativeTime` (`@/lib/time`). No new components.

---

### Task 1: Refine the Queue page

**Files:**
- Modify: `web/src/pages/QueuePage.tsx`

- [ ] **Step 1: Update imports**

In `web/src/pages/QueuePage.tsx`, the current lucide import is `import { ArrowUpToLine, Settings, X } from "lucide-react";`. Add the new component imports (keep existing ones):

```tsx
import { PageHeader } from "@/components/PageHeader";
import { InfoBand, Term } from "@/components/InfoBand";
import { cn } from "@/lib/utils";
```

- [ ] **Step 2: Replace the header + rules paragraph**

Replace the page's header block — the `<div className="flex items-center justify-between">…</div>` containing `<h1>Queue</h1>` and the Default-priorities `<Link>`, AND the `<p className="rounded-md border …">…</p>` rules line — with a `PageHeader` (carrying the link as its `action`) followed by an `InfoBand`:

```tsx
      <PageHeader
        title="Queue"
        action={
          <Link to="/admin/priorities" className="inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground">
            <Settings className="h-4 w-4" /> Default priorities
          </Link>
        }
      />
      <InfoBand>
        <Term>1 slot</Term> per target type · releases by <Term>priority</Term> (high→low, then oldest) · types run <Term>in parallel</Term>
      </InfoBand>
```

(The outer `<section className="space-y-5">` stays; `PageHeader` has its own `mb-6` but the `space-y-5` still spaces the InfoBand from the grid — acceptable. If the doubled gap looks off, change the section to `<section>` and add `className="mb-5"` to a wrapping `<div>` around `InfoBand`. Verify visually in Task 2.)

- [ ] **Step 3: Slot indicator `n/1`, accented when running**

In `LaneCard`, replace the `CardHeader` slot span:

```tsx
        <span className="text-xs text-muted-foreground">{lane.slots} slot{lane.slots === 1 ? "" : "s"}</span>
```

with:

```tsx
        <span className={cn(
          "rounded-full px-2 py-0.5 text-xs tabular-nums",
          lane.running.length > 0 ? "bg-status-running/15 text-status-running" : "bg-muted text-muted-foreground"
        )}>{lane.running.length}/{lane.slots} slot</span>
```

- [ ] **Step 4: Running-entry elapsed**

In `LaneCard`'s running `.map`, replace the priority span:

```tsx
                <span className="font-mono text-xs text-muted-foreground">p{e.priority ?? "—"}</span>
```

with (adds elapsed since start):

```tsx
                <span className="font-mono text-xs text-muted-foreground">p{e.priority ?? "—"} · {relativeTime(e.submittedAt)}</span>
```

- [ ] **Step 5: Verify build + tests**

Run: `cd controlplane/web && pnpm build && pnpm test`
Expected: build exit 0; all tests pass (no logic change).

- [ ] **Step 6: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/web/src/pages/QueuePage.tsx
git commit -m "feat(web): apply Direction A to Queue page (slot indicator, InfoBand)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 2: Visual verification

**Files:** none.

- [ ] **Step 1: Build + deploy**

Run:
```bash
cd /Users/allen/repo/dlh-test-fw/controlplane && make ui-build && make reload-minikube
kubectl -n dlh-test-fw rollout restart deployment/dlh-controlplane
kubectl -n dlh-test-fw rollout status deployment/dlh-controlplane --timeout=120s
```

- [ ] **Step 2: Seed a saturated lane** (so a lane shows running + queued)

Run (port-forward if needed):
```bash
cd /Users/allen/repo/dlh-test-fw/controlplane
TOK="fake:runner:runner@local:dlh-runner"; EP=http://localhost:8080
DLH_TOKEN=$TOK go run ./cmd/dlh run mysql-pod-delete --endpoint $EP
sleep 8; DLH_TOKEN=$TOK go run ./cmd/dlh run mysql-pod-delete --priority 500 --endpoint $EP
```

- [ ] **Step 3: Playwright check**

Navigate to `http://localhost:8080/queue`. Confirm: `PageHeader` "Queue" + Default-priorities link; `InfoBand` with the "i" badge and emphasized "1 slot" / "priority" / "in parallel"; mysql lane header shows `1/1 slot` accented blue; running entry shows `p<n> · <elapsed>`; queued entry shows NEXT + to-front/cancel; other lanes show `0/1 slot` + dashed Idle. Toggle theme — clean in both. **0 console errors.** Clean up the seeded runs afterward (`kubectl -n dlh-test-fw delete wf -l dlh.scenario=mysql-pod-delete`).

---

## Self-Review

**Spec coverage (Queue):** slot indicator `n/1` ✓ (accented when running), running elapsed ✓ (`relativeTime(submittedAt)`), queued to-front/cancel ✓ (already present, unchanged), idle dashed state ✓ (already present), `InfoBand` rules band with `Term` ✓, consistent `PageHeader` ✓.

**Placeholder scan:** none. The Step 2 note about the possible doubled gap is a verify-and-adjust instruction, not a placeholder.

**Type consistency:** `PageHeader`, `InfoBand`, `Term`, `cn`, `relativeTime` reused with confirmed signatures. `lane.running`/`lane.pending`/`lane.slots`/`e.priority`/`e.submittedAt` are existing `QueueLane`/`QueueEntry` fields already used on the page. No new types.

**Presentation-only check:** no API/logic changes; the to-front/cancel handlers and the 5s poll are untouched.
