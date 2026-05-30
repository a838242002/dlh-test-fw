# Queue + Priority UI Refinement — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Unify priority representation across the controlplane UI as a colored tier chip carrying tier name + raw int (`[Normal·100]`), adopt it consistently on Queue/Scenarios/Runs/Run-detail, rework Default-priorities with the existing `SegmentedTiers` + `PageHeader`, and make the Queue chip the reprioritize control (replacing the standalone to-front button).

**Architecture:** Frontend-only — no API, no backend, no Go code change. Two new components (`PriorityChip` pure display, `PriorityChipMenu` clickable wrapper using shadcn DropdownMenu) consume two new helpers in `lib/tier.ts` and five new CSS tier color tokens. Each page swaps its raw-int rendering for the chip; the Queue page drops the standalone "to-front" `ArrowUpToLine` button (its menu's Urgent item replaces it).

**Tech Stack:** React, TypeScript, Tailwind (with `<alpha-value>` HSL tokens), shadcn/ui primitives, Radix Primitives, lucide-react, Vitest (pure-logic only). Commands from `controlplane/web` unless noted.

**Conventions:** NEVER `git add -A`/globs; never commit `dist/`. Build gate `pnpm build`. Pure logic in `src/lib/` has Vitest tests (`pnpm test`); components are gated by `pnpm build` + the live verification task (the project intentionally does not vendor RTL/jsdom). Commit trailer `Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>`. Spec: `docs/superpowers/specs/2026-05-29-queue-priority-refinement-design.md`.

**Task dependency order:** Task 1 (tier.ts) and Task 2 (CSS tokens) are independent foundations. Task 3 (dropdown-menu) is independent. Task 4 (PriorityChip) needs 1+2. Task 5 (PriorityChipMenu) needs 3+4. Tasks 6–9 (page adoptions) all need 4+5. Task 10 (live verify) needs 6–9.

---

## File Structure

- **Modify:** `controlplane/web/src/lib/tier.ts` — add `TierKey`, `tierKeyForPriority`, `tierLabelForPriority`.
- **Modify:** `controlplane/web/src/lib/tier.test.ts` — extend with cases for the new helpers.
- **Modify:** `controlplane/web/src/index.css` — add 5 tier color token pairs under both `:root` (light) and `.dark`.
- **Modify:** `controlplane/web/tailwind.config.js` — expose `tier-{key}-{bg,fg}` colors so components stay in classes.
- **Modify:** `controlplane/web/package.json` + `controlplane/web/pnpm-lock.yaml` — add `@radix-ui/react-dropdown-menu`.
- **Create:** `controlplane/web/src/components/ui/dropdown-menu.tsx` — standard shadcn vendor wrapper.
- **Create:** `controlplane/web/src/components/PriorityChip.tsx` — pure display chip.
- **Create:** `controlplane/web/src/components/PriorityChipMenu.tsx` — clickable chip + dropdown.
- **Modify:** `controlplane/web/src/pages/RunsPage.tsx` — Priority column cell uses `PriorityChip`.
- **Modify:** `controlplane/web/src/pages/RunDetailPage.tsx` — Priority meta uses `PriorityChip` (via existing `children` slot — no `Meta` signature change).
- **Modify:** `controlplane/web/src/pages/QueuePage.tsx` — RUNNING uses `PriorityChip`, QUEUED uses `PriorityChipMenu`; drop the to-front button and the now-unused `ArrowUpToLine` import.
- **Modify:** `controlplane/web/src/pages/ScenariosPage.tsx` — replace cramped `prio` Input with `PriorityChipMenu`; migrate `submitPriority` from `Record<string, string>` to `Record<string, number>`; card-header `default` text uses `PriorityChip`.
- **Modify:** `controlplane/web/src/pages/DefaultPrioritiesPage.tsx` — adopt `PageHeader`, three-column layout, reuse `SegmentedTiers`, add `Custom…` inline editor, status cell uses `PriorityChip`.

The throwaway prototype at `docs/superpowers/specs/proto-queue-priority.html` is NOT modified or deleted — it's the spec's visual reference.

---

### Task 1: Tier-key helpers (TDD)

**Files:**
- Modify: `controlplane/web/src/lib/tier.ts`
- Test: `controlplane/web/src/lib/tier.test.ts`

- [ ] **Step 1: Write the failing tests**

Append to `/Users/allen/repo/dlh-test-fw/controlplane/web/src/lib/tier.test.ts` (after the existing tests):

```ts
import { tierKeyForPriority, tierLabelForPriority } from "./tier";

describe("tierKeyForPriority", () => {
  it("maps exact tier values to their key", () => {
    expect(tierKeyForPriority(10)).toBe("low");
    expect(tierKeyForPriority(100)).toBe("normal");
    expect(tierKeyForPriority(200)).toBe("high");
    expect(tierKeyForPriority(500)).toBe("urgent");
  });
  it("falls back to 'custom' for any other value", () => {
    expect(tierKeyForPriority(0)).toBe("custom");
    expect(tierKeyForPriority(137)).toBe("custom");
    expect(tierKeyForPriority(99)).toBe("custom");
    expect(tierKeyForPriority(600)).toBe("custom");
  });
});

describe("tierLabelForPriority", () => {
  it("returns the tier label for exact tier values", () => {
    expect(tierLabelForPriority(10)).toBe("Low");
    expect(tierLabelForPriority(100)).toBe("Normal");
    expect(tierLabelForPriority(200)).toBe("High");
    expect(tierLabelForPriority(500)).toBe("Urgent");
  });
  it("returns 'Custom' for any other value", () => {
    expect(tierLabelForPriority(137)).toBe("Custom");
    expect(tierLabelForPriority(0)).toBe("Custom");
  });
});
```

If `describe` / `it` / `expect` aren't already imported at the top of `tier.test.ts`, leave the existing import alone — Vitest's globals are enabled via the project's `vitest.config.*` (the existing tests use the same globals).

- [ ] **Step 2: Run tests, expect compile failure**

Run: `cd controlplane/web && pnpm test --run`
Expected: FAIL with `Cannot find name 'tierKeyForPriority'` / `tierLabelForPriority`.

- [ ] **Step 3: Add the helpers in `tier.ts`**

Append to `/Users/allen/repo/dlh-test-fw/controlplane/web/src/lib/tier.ts` (do NOT change existing exports):

```ts
export type TierKey = "low" | "normal" | "high" | "urgent" | "custom";

/** Returns the bare tier key for an exact-match priority value, else "custom". */
export function tierKeyForPriority(priority: number): TierKey {
  const t = TIERS.find((t) => t.value === priority);
  return t ? (t.label.toLowerCase() as TierKey) : "custom";
}

/** Returns the human label ("Low" | "Normal" | "High" | "Urgent" | "Custom"). */
export function tierLabelForPriority(priority: number): string {
  return tierForPriority(priority) ?? "Custom";
}
```

- [ ] **Step 4: Run tests, expect PASS**

Run: `cd controlplane/web && pnpm test --run`
Expected: PASS (existing tests + the new ones).

- [ ] **Step 5: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/web/src/lib/tier.ts controlplane/web/src/lib/tier.test.ts
git commit -m "feat(web): tier-key helpers (tierKeyForPriority, tierLabelForPriority)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 2: CSS tier tokens + Tailwind exposure

**Files:**
- Modify: `controlplane/web/src/index.css`
- Modify: `controlplane/web/tailwind.config.js`

- [ ] **Step 1: Add the 5 tier token pairs to `index.css`**

In `/Users/allen/repo/dlh-test-fw/controlplane/web/src/index.css`, find the `:root { ... }` block (the light theme; the existing `--status-pending` is the last status token there). Add **before** the closing `}` of `:root`:

```css
    /* tier palette — light */
    --tier-low-bg: 220 14% 92%;
    --tier-low-fg: 220 14% 35%;
    --tier-normal-bg: 213 94% 92%;
    --tier-normal-fg: 217 90% 35%;
    --tier-high-bg: 43 96% 90%;
    --tier-high-fg: 35 85% 35%;
    --tier-urgent-bg: 351 94% 92%;
    --tier-urgent-fg: 351 75% 38%;
    --tier-custom-fg: 215 18% 40%;
```

Find the `.dark { ... }` block (the dark theme). Add **before** the closing `}` of `.dark`:

```css
    /* tier palette — dark */
    --tier-low-bg: 220 14% 22%;
    --tier-low-fg: 220 14% 75%;
    --tier-normal-bg: 217 90% 22%;
    --tier-normal-fg: 213 94% 80%;
    --tier-high-bg: 35 85% 22%;
    --tier-high-fg: 43 96% 78%;
    --tier-urgent-bg: 351 75% 22%;
    --tier-urgent-fg: 351 94% 80%;
    --tier-custom-fg: 215 18% 70%;
```

(Note: `custom` has no `bg` token — it uses `transparent` with a border, so only `fg` is themed.)

- [ ] **Step 2: Expose the tokens in `tailwind.config.js`**

Open `/Users/allen/repo/dlh-test-fw/controlplane/web/tailwind.config.js` and find `theme.extend.colors`. Inside the `colors:` object, add this nested `tier:` block alongside the existing `status:`/etc.:

```js
        tier: {
          "low-bg":     "hsl(var(--tier-low-bg) / <alpha-value>)",
          "low-fg":     "hsl(var(--tier-low-fg) / <alpha-value>)",
          "normal-bg":  "hsl(var(--tier-normal-bg) / <alpha-value>)",
          "normal-fg":  "hsl(var(--tier-normal-fg) / <alpha-value>)",
          "high-bg":    "hsl(var(--tier-high-bg) / <alpha-value>)",
          "high-fg":    "hsl(var(--tier-high-fg) / <alpha-value>)",
          "urgent-bg":  "hsl(var(--tier-urgent-bg) / <alpha-value>)",
          "urgent-fg":  "hsl(var(--tier-urgent-fg) / <alpha-value>)",
          "custom-fg":  "hsl(var(--tier-custom-fg) / <alpha-value>)",
        },
```

This makes class names like `bg-tier-normal-bg` and `text-tier-normal-fg` valid.

- [ ] **Step 3: Build to verify no regression**

Run: `cd controlplane/web && pnpm build`
Expected: exit 0 (no class is yet used, but Tailwind compiles the additions).

- [ ] **Step 4: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/web/src/index.css controlplane/web/tailwind.config.js
git commit -m "feat(web): tier color tokens (light + dark) exposed via Tailwind

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 3: Add Radix dropdown-menu + vendor `dropdown-menu.tsx`

**Files:**
- Modify: `controlplane/web/package.json` + `controlplane/web/pnpm-lock.yaml`
- Create: `controlplane/web/src/components/ui/dropdown-menu.tsx`

- [ ] **Step 1: Install the Radix dropdown-menu primitive**

Run: `cd controlplane/web && pnpm add @radix-ui/react-dropdown-menu`
Expected: dependency added to `package.json` and `pnpm-lock.yaml` updated.

- [ ] **Step 2: Vendor the shadcn `DropdownMenu` wrapper**

Create `/Users/allen/repo/dlh-test-fw/controlplane/web/src/components/ui/dropdown-menu.tsx` with the standard shadcn implementation (matching the pattern of the existing `alert-dialog.tsx`/`dialog.tsx` vendors in the same directory):

```tsx
import * as React from "react";
import * as DropdownMenuPrimitive from "@radix-ui/react-dropdown-menu";
import { Check, ChevronRight, Circle } from "lucide-react";

import { cn } from "@/lib/utils";

const DropdownMenu = DropdownMenuPrimitive.Root;
const DropdownMenuTrigger = DropdownMenuPrimitive.Trigger;
const DropdownMenuGroup = DropdownMenuPrimitive.Group;
const DropdownMenuPortal = DropdownMenuPrimitive.Portal;
const DropdownMenuSub = DropdownMenuPrimitive.Sub;
const DropdownMenuRadioGroup = DropdownMenuPrimitive.RadioGroup;

const DropdownMenuSubTrigger = React.forwardRef<
  React.ElementRef<typeof DropdownMenuPrimitive.SubTrigger>,
  React.ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.SubTrigger> & { inset?: boolean }
>(({ className, inset, children, ...props }, ref) => (
  <DropdownMenuPrimitive.SubTrigger
    ref={ref}
    className={cn(
      "flex cursor-default select-none items-center rounded-sm px-2 py-1.5 text-sm outline-none focus:bg-accent data-[state=open]:bg-accent",
      inset && "pl-8",
      className
    )}
    {...props}
  >
    {children}
    <ChevronRight className="ml-auto h-4 w-4" />
  </DropdownMenuPrimitive.SubTrigger>
));
DropdownMenuSubTrigger.displayName = DropdownMenuPrimitive.SubTrigger.displayName;

const DropdownMenuSubContent = React.forwardRef<
  React.ElementRef<typeof DropdownMenuPrimitive.SubContent>,
  React.ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.SubContent>
>(({ className, ...props }, ref) => (
  <DropdownMenuPrimitive.SubContent
    ref={ref}
    className={cn(
      "z-50 min-w-[8rem] overflow-hidden rounded-md border bg-popover p-1 text-popover-foreground shadow-lg data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95",
      className
    )}
    {...props}
  />
));
DropdownMenuSubContent.displayName = DropdownMenuPrimitive.SubContent.displayName;

const DropdownMenuContent = React.forwardRef<
  React.ElementRef<typeof DropdownMenuPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.Content>
>(({ className, sideOffset = 4, ...props }, ref) => (
  <DropdownMenuPrimitive.Portal>
    <DropdownMenuPrimitive.Content
      ref={ref}
      sideOffset={sideOffset}
      className={cn(
        "z-50 min-w-[8rem] overflow-hidden rounded-md border bg-popover p-1 text-popover-foreground shadow-md data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95",
        className
      )}
      {...props}
    />
  </DropdownMenuPrimitive.Portal>
));
DropdownMenuContent.displayName = DropdownMenuPrimitive.Content.displayName;

const DropdownMenuItem = React.forwardRef<
  React.ElementRef<typeof DropdownMenuPrimitive.Item>,
  React.ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.Item> & { inset?: boolean }
>(({ className, inset, ...props }, ref) => (
  <DropdownMenuPrimitive.Item
    ref={ref}
    className={cn(
      "relative flex cursor-default select-none items-center rounded-sm px-2 py-1.5 text-sm outline-none transition-colors focus:bg-accent focus:text-accent-foreground data-[disabled]:pointer-events-none data-[disabled]:opacity-50",
      inset && "pl-8",
      className
    )}
    {...props}
  />
));
DropdownMenuItem.displayName = DropdownMenuPrimitive.Item.displayName;

const DropdownMenuCheckboxItem = React.forwardRef<
  React.ElementRef<typeof DropdownMenuPrimitive.CheckboxItem>,
  React.ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.CheckboxItem>
>(({ className, children, checked, ...props }, ref) => (
  <DropdownMenuPrimitive.CheckboxItem
    ref={ref}
    className={cn(
      "relative flex cursor-default select-none items-center rounded-sm py-1.5 pl-8 pr-2 text-sm outline-none transition-colors focus:bg-accent focus:text-accent-foreground data-[disabled]:pointer-events-none data-[disabled]:opacity-50",
      className
    )}
    checked={checked}
    {...props}
  >
    <span className="absolute left-2 flex h-3.5 w-3.5 items-center justify-center">
      <DropdownMenuPrimitive.ItemIndicator>
        <Check className="h-4 w-4" />
      </DropdownMenuPrimitive.ItemIndicator>
    </span>
    {children}
  </DropdownMenuPrimitive.CheckboxItem>
));
DropdownMenuCheckboxItem.displayName = DropdownMenuPrimitive.CheckboxItem.displayName;

const DropdownMenuRadioItem = React.forwardRef<
  React.ElementRef<typeof DropdownMenuPrimitive.RadioItem>,
  React.ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.RadioItem>
>(({ className, children, ...props }, ref) => (
  <DropdownMenuPrimitive.RadioItem
    ref={ref}
    className={cn(
      "relative flex cursor-default select-none items-center rounded-sm py-1.5 pl-8 pr-2 text-sm outline-none transition-colors focus:bg-accent focus:text-accent-foreground data-[disabled]:pointer-events-none data-[disabled]:opacity-50",
      className
    )}
    {...props}
  >
    <span className="absolute left-2 flex h-3.5 w-3.5 items-center justify-center">
      <DropdownMenuPrimitive.ItemIndicator>
        <Circle className="h-2 w-2 fill-current" />
      </DropdownMenuPrimitive.ItemIndicator>
    </span>
    {children}
  </DropdownMenuPrimitive.RadioItem>
));
DropdownMenuRadioItem.displayName = DropdownMenuPrimitive.RadioItem.displayName;

const DropdownMenuLabel = React.forwardRef<
  React.ElementRef<typeof DropdownMenuPrimitive.Label>,
  React.ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.Label> & { inset?: boolean }
>(({ className, inset, ...props }, ref) => (
  <DropdownMenuPrimitive.Label
    ref={ref}
    className={cn("px-2 py-1.5 text-sm font-semibold", inset && "pl-8", className)}
    {...props}
  />
));
DropdownMenuLabel.displayName = DropdownMenuPrimitive.Label.displayName;

const DropdownMenuSeparator = React.forwardRef<
  React.ElementRef<typeof DropdownMenuPrimitive.Separator>,
  React.ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.Separator>
>(({ className, ...props }, ref) => (
  <DropdownMenuPrimitive.Separator
    ref={ref}
    className={cn("-mx-1 my-1 h-px bg-muted", className)}
    {...props}
  />
));
DropdownMenuSeparator.displayName = DropdownMenuPrimitive.Separator.displayName;

const DropdownMenuShortcut = ({ className, ...props }: React.HTMLAttributes<HTMLSpanElement>) => (
  <span className={cn("ml-auto text-xs tracking-widest opacity-60", className)} {...props} />
);
DropdownMenuShortcut.displayName = "DropdownMenuShortcut";

export {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuCheckboxItem,
  DropdownMenuRadioItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
  DropdownMenuGroup,
  DropdownMenuPortal,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuRadioGroup,
};
```

- [ ] **Step 3: Build to verify the vendor compiles**

Run: `cd controlplane/web && pnpm build`
Expected: exit 0.

- [ ] **Step 4: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/web/package.json controlplane/web/pnpm-lock.yaml controlplane/web/src/components/ui/dropdown-menu.tsx
git commit -m "feat(web): vendor shadcn DropdownMenu (Radix primitive)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 4: `PriorityChip` (pure display)

**Files:**
- Create: `controlplane/web/src/components/PriorityChip.tsx`

- [ ] **Step 1: Create the component**

Create `/Users/allen/repo/dlh-test-fw/controlplane/web/src/components/PriorityChip.tsx`:

```tsx
import { tierKeyForPriority, tierLabelForPriority, type TierKey } from "@/lib/tier";
import { cn } from "@/lib/utils";

// Color classes per tier — sourced from the CSS tokens in index.css and
// exposed via tailwind.config.js (theme.extend.colors.tier).
const TIER_CLASSES: Record<TierKey, string> = {
  low:    "bg-tier-low-bg text-tier-low-fg",
  normal: "bg-tier-normal-bg text-tier-normal-fg",
  high:   "bg-tier-high-bg text-tier-high-fg",
  urgent: "bg-tier-urgent-bg text-tier-urgent-fg",
  custom: "bg-transparent text-tier-custom-fg border border-border",
};

/**
 * Colored tier pill carrying the tier label and the raw integer priority.
 * Pure display — no interaction. Renders an em-dash placeholder when priority
 * is null (e.g. an unsubmitted run, or a missing field). Callers handle null
 * by passing it through; the helpers in `lib/tier.ts` operate on real numbers
 * only.
 */
export function PriorityChip({ priority }: { priority: number | null }) {
  if (priority == null) {
    return <span className="text-xs text-muted-foreground">—</span>;
  }
  const key = tierKeyForPriority(priority);
  const label = tierLabelForPriority(priority);
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[11px] font-medium leading-[18px] whitespace-nowrap",
        TIER_CLASSES[key],
      )}
    >
      {label}
      <span className="font-normal opacity-85 tabular-nums">·{priority}</span>
    </span>
  );
}
```

- [ ] **Step 2: Build to verify it compiles**

Run: `cd controlplane/web && pnpm build`
Expected: exit 0. (No page imports it yet; this verifies syntax + class generation only.)

- [ ] **Step 3: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/web/src/components/PriorityChip.tsx
git commit -m "feat(web): PriorityChip — colored tier+int pill (display only)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 5: `PriorityChipMenu` (chip + dropdown)

**Files:**
- Create: `controlplane/web/src/components/PriorityChipMenu.tsx`

- [ ] **Step 1: Create the component**

Create `/Users/allen/repo/dlh-test-fw/controlplane/web/src/components/PriorityChipMenu.tsx`:

```tsx
import { useState } from "react";
import { ChevronDown } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { PriorityChip } from "./PriorityChip";
import { TIERS } from "@/lib/tier";

/**
 * Clickable priority chip with a tier menu. Picking a tier fires onChange with
 * the tier's integer value. The "Custom…" item swaps the menu body to an
 * inline number input (Enter commits, Escape cancels) so off-tier values stay
 * possible without a separate modal.
 *
 * When `disabled`, renders a read-only PriorityChip (no caret, no handler).
 * Used on the Queue page's RUNNING row, where the backend rejects reprioritize
 * on non-pending workflows.
 */
export function PriorityChipMenu({
  value,
  onChange,
  align = "start",
  disabled = false,
}: {
  value: number | null;
  onChange: (priority: number) => void;
  align?: "start" | "end";
  disabled?: boolean;
}) {
  const [customOpen, setCustomOpen] = useState(false);
  const [customDraft, setCustomDraft] = useState("");

  if (disabled) {
    return <PriorityChip priority={value} />;
  }

  const commitCustom = () => {
    const n = Number(customDraft);
    if (Number.isFinite(n) && customDraft.trim() !== "") {
      onChange(n);
    }
    setCustomOpen(false);
    setCustomDraft("");
  };

  return (
    <DropdownMenu onOpenChange={(open) => { if (!open) { setCustomOpen(false); setCustomDraft(""); } }}>
      <DropdownMenuTrigger className="inline-flex items-center gap-0.5 outline-none focus-visible:ring-2 focus-visible:ring-ring rounded-full">
        <PriorityChip priority={value} />
        <ChevronDown className="h-3 w-3 opacity-60" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align={align} className="min-w-[176px]">
        {TIERS.map((t) => (
          <DropdownMenuItem
            key={t.label}
            onSelect={(e) => { e.preventDefault(); onChange(t.value); }}
            className="flex items-center justify-between gap-3"
          >
            <span>{t.label}</span>
            <PriorityChip priority={t.value} />
          </DropdownMenuItem>
        ))}
        <DropdownMenuSeparator />
        {!customOpen ? (
          <DropdownMenuItem
            onSelect={(e) => { e.preventDefault(); setCustomOpen(true); }}
            className="text-muted-foreground"
          >
            Custom…
          </DropdownMenuItem>
        ) : (
          <div className="flex items-center gap-1 p-1">
            <input
              autoFocus
              type="number"
              inputMode="numeric"
              value={customDraft}
              onChange={(e) => setCustomDraft(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") { e.preventDefault(); commitCustom(); }
                if (e.key === "Escape") { e.preventDefault(); setCustomOpen(false); setCustomDraft(""); }
              }}
              placeholder="int"
              className="h-7 w-24 rounded border border-border bg-background px-2 text-xs tabular-nums outline-none focus:ring-1 focus:ring-ring"
            />
          </div>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
```

- [ ] **Step 2: Build to verify it compiles**

Run: `cd controlplane/web && pnpm build`
Expected: exit 0.

- [ ] **Step 3: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/web/src/components/PriorityChipMenu.tsx
git commit -m "feat(web): PriorityChipMenu — chip + tier dropdown + custom inline editor

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 6: Adopt the chip on Runs + Run-detail

**Files:**
- Modify: `controlplane/web/src/pages/RunsPage.tsx`
- Modify: `controlplane/web/src/pages/RunDetailPage.tsx`

- [ ] **Step 1: Update `RunsPage.tsx`**

Add the import near the top (with the other component imports — group with `@/components/...`):

```tsx
import { PriorityChip } from "@/components/PriorityChip";
```

Find the `Priority` cell line (currently `<TableCell className="text-right font-mono tabular-nums text-muted-foreground">{r.priority ?? "—"}</TableCell>`) and replace with:

```tsx
                  <TableCell className="text-right"><PriorityChip priority={r.priority ?? null} /></TableCell>
```

- [ ] **Step 2: Update `RunDetailPage.tsx`**

Add the import:

```tsx
import { PriorityChip } from "@/components/PriorityChip";
```

Find the line `<Meta label="Priority" value={run.priority != null ? String(run.priority) : "—"} />` and replace with:

```tsx
        <Meta label="Priority"><PriorityChip priority={run.priority ?? null} /></Meta>
```

(The existing `Meta` already accepts `children: ReactNode` and renders it in place of `value` when provided — no `Meta` signature change is needed.)

- [ ] **Step 3: Build + tests**

Run: `cd controlplane/web && pnpm build && pnpm test --run`
Expected: build exit 0, all tests pass.

- [ ] **Step 4: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/web/src/pages/RunsPage.tsx controlplane/web/src/pages/RunDetailPage.tsx
git commit -m "feat(web): adopt PriorityChip on Runs list + Run-detail meta

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 7: Adopt on Queue (RUNNING read-only, QUEUED clickable, drop to-front)

**Files:**
- Modify: `controlplane/web/src/pages/QueuePage.tsx`

- [ ] **Step 1: Update imports**

In `/Users/allen/repo/dlh-test-fw/controlplane/web/src/pages/QueuePage.tsx`, the current lucide import is `import { ArrowUpToLine, Settings, X } from "lucide-react";`. **Remove `ArrowUpToLine`** so it becomes:

```tsx
import { Settings, X } from "lucide-react";
```

Add the chip imports (with the other `@/components/...` imports):

```tsx
import { PriorityChip } from "@/components/PriorityChip";
import { PriorityChipMenu } from "@/components/PriorityChipMenu";
```

- [ ] **Step 2: Add a per-id reprioritize helper**

The page already has `toFront` (which calls `POST /api/runs/{id}/priority` with `max+100`). Add a sibling helper that targets a specific priority. Just after the existing `toFront = async (lane: Lane, id: string) => { ... }`, add:

```tsx
  const reprioritize = async (id: string, priority: number) => {
    const { error: e } = await api.POST("/api/runs/{id}/priority", {
      params: { path: { id } }, body: { priority },
    });
    if (e) toast.error("Reprioritize failed", { description: JSON.stringify(e) });
    else { toast.success(`Priority set to ${priority}`); reload(); }
  };
```

Then **delete the entire `toFront` declaration** — its only caller (the to-front button) is being removed.

- [ ] **Step 3: Replace the RUNNING priority span**

In the `LaneCard`'s `lane.running.map(...)`, the row currently ends with:

```tsx
                <span className="font-mono text-xs text-muted-foreground">p{e.priority ?? "—"} · {relativeTime(e.submittedAt)}</span>
```

Replace it with:

```tsx
                <span className="flex items-center gap-2 text-xs text-muted-foreground">
                  <PriorityChip priority={e.priority ?? null} />
                  <span>· {relativeTime(e.submittedAt)}</span>
                </span>
```

- [ ] **Step 4: Replace the QUEUED priority + drop the to-front button**

In `lane.pending.map((e, i) => ...)`, the inner right span currently looks like:

```tsx
                  <span className="flex items-center gap-2 text-xs text-muted-foreground">
                    <span title={new Date(e.submittedAt).toLocaleString()}>{relativeTime(e.submittedAt)}</span>
                    <span className="font-mono">p{e.priority ?? "—"}</span>
                    {i > 0 && (
                      <Button size="sm" variant="ghost" title="Move to front" onClick={() => onToFront(e.id)}>
                        <ArrowUpToLine className="h-3.5 w-3.5" />
                      </Button>
                    )}
                    <Button size="sm" variant="ghost" title="Cancel" onClick={() => onCancel(e.id)}>
                      <X className="h-3.5 w-3.5" />
                    </Button>
                  </span>
```

Replace it with (note the `PriorityChipMenu`, no to-front Button, no `i > 0` branch):

```tsx
                  <span className="flex items-center gap-2 text-xs text-muted-foreground">
                    <span title={new Date(e.submittedAt).toLocaleString()}>{relativeTime(e.submittedAt)}</span>
                    <PriorityChipMenu value={e.priority ?? null} onChange={(p) => onReprioritize(e.id, p)} align="end" />
                    <Button size="sm" variant="ghost" title="Cancel" onClick={() => onCancel(e.id)}>
                      <X className="h-3.5 w-3.5" />
                    </Button>
                  </span>
```

- [ ] **Step 5: Update `LaneCard` props + the call site**

`LaneCard` currently has props `{ lane, onToFront, onCancel }`. Change them to `{ lane, onReprioritize, onCancel }`:

```tsx
function LaneCard({ lane, onReprioritize, onCancel }: { lane: Lane; onReprioritize: (id: string, priority: number) => void; onCancel: (id: string) => void }) {
```

In `QueuePage`, the call site is currently:

```tsx
          <LaneCard key={lane.key} lane={lane} onToFront={(id) => toFront(lane, id)} onCancel={cancel} />
```

Change to:

```tsx
          <LaneCard key={lane.key} lane={lane} onReprioritize={reprioritize} onCancel={cancel} />
```

- [ ] **Step 6: Build + tests**

Run: `cd controlplane/web && pnpm build && pnpm test --run`
Expected: build exit 0; all tests pass. (TypeScript will catch any missed import or unused identifier — fix and re-run.)

- [ ] **Step 7: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/web/src/pages/QueuePage.tsx
git commit -m "feat(web): Queue chip-as-control — RUNNING readonly, QUEUED reprioritize via PriorityChipMenu

Drops the standalone to-front button: picking Urgent in the chip menu is
the natural way to move a queued run to front. RUNNING rows show a
read-only PriorityChip — backend rejects reprioritize on non-pending runs.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 8: Adopt on Scenarios (replace `prio` input, migrate state)

**Files:**
- Modify: `controlplane/web/src/pages/ScenariosPage.tsx`

- [ ] **Step 1: Update imports**

Remove the now-unused `tierForPriority` import if it's only used for the inline tier text in the card header, AND remove the `Input` import IF nothing else on the page uses it (search the file — the page-header search input also uses `Input`, so KEEP `Input` if so). Add:

```tsx
import { PriorityChip } from "@/components/PriorityChip";
import { PriorityChipMenu } from "@/components/PriorityChipMenu";
```

- [ ] **Step 2: Migrate `submitPriority` state to numeric model**

Find the line:

```tsx
  const [submitPriority, setSubmitPriority] = useState<Record<string, string>>({});
```

Replace with:

```tsx
  const [submitPriority, setSubmitPriority] = useState<Record<string, number>>({});
```

In `handleRun`, the existing block is:

```tsx
      const raw = submitPriority[s.id];
      const priority = raw && raw.trim() !== "" ? Number(raw) : undefined;
```

Replace with:

```tsx
      const priority = submitPriority[s.id];
```

- [ ] **Step 3: Compute the scenario's effective default for chip display**

Just inside the component body, after `const defaults = ...` (the existing state holding scenario defaults), the page already has a `defaults` map. The submitter resolution order is request-override → per-scenario default → baked template `spec.priority`. For the chip, we want the effective default to fall back to baked when no override is set.

Find where the card header renders the default text. It is currently:

```tsx
                                  <span className="text-muted-foreground">default {tierForPriority(defaults[s.id]) ?? defaults[s.id]}</span>
```

Replace with:

```tsx
                                  <span className="flex items-center gap-1.5 text-muted-foreground">default <PriorityChip priority={defaults[s.id] ?? s.bakedPriority ?? null} /></span>
```

If `Scenario` (from `gen.ts`) does NOT have a `bakedPriority` field (it isn't always populated by the backend list endpoint), simplify the fallback to just the per-scenario default:

```tsx
                                  <span className="flex items-center gap-1.5 text-muted-foreground">default <PriorityChip priority={defaults[s.id] ?? null} /></span>
```

(Use the second form — it matches the data the page already has reliably; the old code only used `defaults[s.id]` too. Don't introduce new API dependencies.)

- [ ] **Step 4: Replace the run-control `Input` with `PriorityChipMenu`**

The existing run-control Input block is roughly:

```tsx
                            <Input
                              type="number"
                              value={submitPriority[s.id] ?? ""}
                              onChange={(e) => setSubmitPriority((r) => ({ ...r, [s.id]: e.target.value }))}
                              placeholder="prio"
                              title="Priority override (blank = scenario default)"
                              className="h-8 w-[72px] tabular-nums"
                            />
```

Replace with:

```tsx
                            <PriorityChipMenu
                              value={submitPriority[s.id] ?? defaults[s.id] ?? null}
                              onChange={(p) => setSubmitPriority((r) => ({ ...r, [s.id]: p }))}
                              align="start"
                            />
```

- [ ] **Step 5: Build + tests**

Run: `cd controlplane/web && pnpm build && pnpm test --run`
Expected: build exit 0; all tests pass.

- [ ] **Step 6: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/web/src/pages/ScenariosPage.tsx
git commit -m "feat(web): Scenarios run-control uses PriorityChipMenu (no more cramped prio Input)

Card header default shown as a PriorityChip; submitPriority state migrated
from Record<string,string> to Record<string,number>.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 9: Rewrite `DefaultPrioritiesPage` (PageHeader + SegmentedTiers + chip status)

**Files:**
- Modify: `controlplane/web/src/pages/DefaultPrioritiesPage.tsx`

- [ ] **Step 1: Replace the entire file contents**

Replace the contents of `/Users/allen/repo/dlh-test-fw/controlplane/web/src/pages/DefaultPrioritiesPage.tsx` with:

```tsx
import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { ArrowLeft } from "lucide-react";
import { toast } from "sonner";
import { api } from "../api/client";
import type { components } from "../api/gen";
import { ErrorState } from "@/components/ErrorState";
import { PageHeader } from "@/components/PageHeader";
import { PriorityChip } from "@/components/PriorityChip";
import { SegmentedTiers } from "@/components/SegmentedTiers";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

type SP = components["schemas"]["ScenarioPriority"];

export function DefaultPrioritiesPage() {
  const [items, setItems] = useState<SP[] | null>(null);
  const [error, setError] = useState<unknown>(null);

  const reload = useCallback(() => {
    api.GET("/api/scenario-priorities", {}).then(({ data, error: e }) => {
      if (e) setError(e);
      else { setItems((data?.items ?? []) as SP[]); setError(null); }
    });
  }, []);

  useEffect(() => { reload(); }, [reload]);

  const save = async (scenario: string, priority: number) => {
    const { error: e } = await api.PUT("/api/scenario-priorities/{id}", {
      params: { path: { id: scenario } },
      body: { priority },
    });
    if (e) toast.error("Save failed", { description: JSON.stringify(e) });
    else { toast.success(`${scenario} → priority ${priority}`); reload(); }
  };

  if (error) return <ErrorState message="Failed to load priorities" details={error} />;
  if (!items) return <div className="space-y-4"><Skeleton className="h-8 w-64" /><Skeleton className="h-40 w-full" /></div>;

  return (
    <section className="space-y-5">
      <PageHeader
        title="Default priorities"
        subtitle="Set the baked default each scenario uses when no priority is given on submit."
        action={
          <Link to="/queue" className="inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground">
            <ArrowLeft className="h-4 w-4" /> Queue
          </Link>
        }
      />
      <Card>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Scenario</TableHead>
                <TableHead>Priority</TableHead>
                <TableHead>Status</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((sp) => (
                <PriorityRow key={sp.scenario} sp={sp} onSave={save} />
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </section>
  );
}

function PriorityRow({ sp, onSave }: { sp: SP; onSave: (s: string, p: number) => void }) {
  const [customOpen, setCustomOpen] = useState(false);
  const [customDraft, setCustomDraft] = useState(String(sp.effective));
  const overridden = sp.override != null;

  const commitCustom = () => {
    const n = Number(customDraft);
    if (Number.isFinite(n) && customDraft.trim() !== "") {
      onSave(sp.scenario, n);
    }
    setCustomOpen(false);
  };

  return (
    <TableRow>
      <TableCell className="font-medium">{sp.scenario}</TableCell>
      <TableCell>
        <div className="flex items-center gap-3">
          <SegmentedTiers value={sp.effective} onPick={(p) => onSave(sp.scenario, p)} />
          {!customOpen ? (
            <button
              type="button"
              onClick={() => { setCustomDraft(String(sp.effective)); setCustomOpen(true); }}
              className="text-[11px] text-muted-foreground hover:text-foreground hover:underline"
            >
              Custom…
            </button>
          ) : (
            <input
              autoFocus
              type="number"
              inputMode="numeric"
              value={customDraft}
              onChange={(e) => setCustomDraft(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") { e.preventDefault(); commitCustom(); }
                if (e.key === "Escape") { e.preventDefault(); setCustomOpen(false); }
              }}
              onBlur={() => setCustomOpen(false)}
              placeholder="int"
              className="h-7 w-24 rounded border border-border bg-background px-2 text-xs tabular-nums outline-none focus:ring-1 focus:ring-ring"
            />
          )}
        </div>
      </TableCell>
      <TableCell>
        <div className="flex items-center gap-2">
          <PriorityChip priority={sp.effective} />
          <span className="text-xs text-muted-foreground">
            {overridden ? `overridden · baked ${sp.baked}` : "= baked default"}
          </span>
        </div>
      </TableCell>
    </TableRow>
  );
}
```

- [ ] **Step 2: Build + tests**

Run: `cd controlplane/web && pnpm build && pnpm test --run`
Expected: build exit 0; all tests pass.

- [ ] **Step 3: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add controlplane/web/src/pages/DefaultPrioritiesPage.tsx
git commit -m "feat(web): Default-priorities — PageHeader + SegmentedTiers + chip status

Three columns, single source of truth per row. Custom… opens an inline
int input (Enter commits, Escape cancels). No redundant Save button —
clicking a tier persists immediately, matching previous behaviour.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 10: Live re-verification on minikube

**Files:** none.

- [ ] **Step 1: Build, reload, restart**

```bash
cd /Users/allen/repo/dlh-test-fw/controlplane && make ui-build && make build && make reload-minikube
kubectl -n dlh-test-fw rollout restart deployment/dlh-controlplane
kubectl -n dlh-test-fw rollout status deployment/dlh-controlplane --timeout=120s
```

- [ ] **Step 2: Ensure port-forward**

```bash
pgrep -f "port-forward.*dlh-controlplane" >/dev/null || \
  (kubectl -n dlh-test-fw port-forward svc/dlh-controlplane 8080:80 >/tmp/dlh-pf.log 2>&1 &)
for i in $(seq 1 12); do curl -sf -o /dev/null http://localhost:8080/ 2>/dev/null && break; sleep 1; done
```

- [ ] **Step 3: Seed a holder + waiter on mysql so QUEUED state is exercised**

```bash
cd /Users/allen/repo/dlh-test-fw/controlplane
TOK="fake:dev:dev@example.com:dlh-admins"; EP=http://localhost:8080
kubectl -n dlh-test-fw delete wf -l dlh.scenario=mysql-pod-delete >/dev/null 2>&1; sleep 2
DLH_TOKEN=$TOK go run ./cmd/dlh run mysql-pod-delete --endpoint $EP
sleep 10
DLH_TOKEN=$TOK go run ./cmd/dlh run mysql-pod-delete --priority 500 --endpoint $EP
sleep 22
```

- [ ] **Step 4: Playwright walkthrough — confirm each surface**

Navigate to each page and confirm visually + 0 console errors:

1. `/queue` — mysql lane shows `1/1 slot`; RUNNING row has a read-only `[Normal·100]` chip (no caret) + elapsed; QUEUED `#1 NEXT` row has a clickable `[Urgent·500 ▾]` chip + `✕` cancel (no to-front button). Click the QUEUED chip → menu opens with Low/Normal/High/Urgent + `Custom…`; pick `High` → toast `Priority set to 200`, chip becomes `[High·200]`, the row reorders if applicable.
2. `/scenarios` — each card's run control shows the default chip with `▾`; no `pric` Input anywhere. Click the chip on a card → menu with tiers; pick `Urgent` → chip becomes `[Urgent·500]`. Click `Custom…` → inline int input; type `137`, Enter → chip becomes `[Custom·137]`. Click `Run`.
3. `/admin/priorities` — `PageHeader` "Default priorities" with subtitle + `← Queue`. Each row: scenario name | `SegmentedTiers` + `Custom…` link | `[Normal·100]` chip + status text. Click `High` on a row → toast `→ priority 200`, Status updates to `[High·200] overridden · baked 100`.
4. `/runs` — Priority column shows chips (no raw `100` text).
5. `/runs/<id>` — Priority meta shows a chip.
6. Toggle theme (top-right) — chips remain legible in both light and dark.

- [ ] **Step 5: Clean up**

```bash
kubectl -n dlh-test-fw delete wf -l dlh.scenario=mysql-pod-delete
rm -f docs/superpowers/specs/proto-*.png docs/superpowers/specs/disc-*.png
rm -rf docs/superpowers/specs/.playwright-mcp
pkill -f "python3 -m http.server 5501" 2>/dev/null || true
```

(The prototype HTML at `docs/superpowers/specs/proto-queue-priority.html` is the spec's visual reference — do NOT delete.)

---

## Self-Review

**Spec coverage:**
- Decision 1 (chip vocabulary): `PriorityChip` (Task 4), CSS tokens (Task 2), tier helpers (Task 1). Adopted on Queue (Task 7), Scenarios (Task 8), Default-priorities (Task 9), Runs (Task 6), Run-detail (Task 6). ✓
- Decision 2 (Scenarios run control): `PriorityChipMenu` (Task 5) adopted on `/scenarios` (Task 8); state migrated from string to number. ✓
- Decision 3 (Default-priorities rework): PageHeader + SegmentedTiers + Custom… + chip status (Task 9). ✓
- Decision 4 (Queue chip-as-control): QUEUED uses `PriorityChipMenu`, RUNNING uses read-only `PriorityChip` via the menu's `disabled` short-circuit path AND directly on the RUNNING row (Task 7); to-front button + `ArrowUpToLine` import removed. ✓
- Backend untouched: no Go file touched in any task. ✓
- New CSS tokens for both light and dark: Task 2 adds both blocks. ✓
- Component tests: tier helpers (Task 1, Vitest); components gated by `pnpm build` + live verification (Task 10) — matches the project's stated test convention. ✓

**Placeholder scan:** none. The Task 8 `bakedPriority` conditional includes the explicit fallback to use ("Use the second form") so the implementer doesn't have to decide.

**Type consistency:**
- `TierKey` defined in Task 1, used in Task 4's `TIER_CLASSES` mapping. ✓
- `PriorityChip` signature `{ priority: number | null }` consistent across Tasks 4, 6, 7, 8, 9. ✓
- `PriorityChipMenu` signature `{ value, onChange, align?, disabled? }` consistent across Tasks 5, 7, 8. ✓
- `submitPriority` type migration (Record<string,string> → Record<string,number>) handled in a single task (Task 8). ✓
- `Meta` component unchanged — Task 6 uses its existing `children: ReactNode` slot. ✓
- `LaneCard` props renamed atomically in Task 7 (declaration + call site). ✓
