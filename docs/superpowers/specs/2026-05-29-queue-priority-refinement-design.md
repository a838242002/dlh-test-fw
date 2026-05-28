# Queue + Priority Refinement — Design

**Date:** 2026-05-29
**Status:** Approved (brainstorm → ready for plan)
**Component:** `controlplane/web` (frontend only — no API, no backend changes)
**Prototype:** `docs/superpowers/specs/proto-queue-priority.html`

---

## Problem

Priority is represented **three different ways** across the controlplane UI today:

| Surface | Current |
|---|---|
| Queue | raw `p100` / `p500` (font-mono) |
| Scenarios card (header) | tier label `default Normal` |
| Scenarios card (run control) | cramped `<Input>` with placeholder visually truncated to `pric` |
| Default-priorities | redundant: 4 inline tier `<Button>`s **plus** a separate numeric `<Input>` + Save |
| Runs list | raw `100` (right-aligned, font-mono) |
| Run-detail meta | raw `String(priority)` |

A polished `SegmentedTiers` component (`components/SegmentedTiers.tsx`) exists but is unused. `lib/tier.ts` already maps integers ↔ tier labels (Low=10 / Normal=100 / High=200 / Urgent=500). The Default-priorities page also bypasses the refined `PageHeader` pattern, using a plain `← Queue` link + bare `<h1>`.

Additionally, the Queue page exposes a standalone "move to front" icon button that calls reprioritize-to-`max+100`. Combined with the to-front+cancel icons, the per-row controls are dense and don't expose any tier-aware reprioritize.

## Goal

One priority vocabulary everywhere — a **color-coded tier chip carrying tier name + raw int**, with a graceful fallback for off-tier values — and the right interaction per surface (compact chip-menu where space is tight, segmented control on the admin page where it's about setting values). No backend or API change.

---

## Decisions (approved)

1. **Priority chip vocabulary** (the keystone). A colored pill: `[Normal·100]`. Colors encode urgency: Low=slate, Normal=blue, High=amber, Urgent=red. Off-tier values render as a neutral outline `[Custom·137]`. Editable variant carries a `▾` caret; read-only chips don't. Used on every priority surface.
2. **Scenarios run control** — replace the cramped `pric` `<Input>` with a clickable chip that opens a small menu (Low / Normal / High / Urgent / Custom…). Default = scenario's effective default priority.
3. **Default-priorities page** — adopt `PageHeader`; collapse the redundant Tiers/Effective/Save columns into one `Priority` column using the existing `SegmentedTiers` component, with a `Custom…` link for off-tier values; the `Status` column shows the effective value as a chip plus a one-line note (`baked default` / `overridden · baked N`). Clicking a tier saves immediately (no separate Save button).
4. **Queue** — on QUEUED rows the chip is the reprioritize control (chip → menu, replacing the standalone "to-front" button; selecting **Urgent** is the natural way to move to front). On RUNNING rows the chip is **read-only** (no caret) — the backend rejects reprioritize on non-pending runs (`ErrNotPending`/409). Cancel (`✕`) stays as a separate icon.

---

## Components

### `PriorityChip` (new)

```ts
type PriorityChipProps = {
  priority: number | null; // null → em-dash placeholder, neutral
};
```

Renders the colored tier pill `[<Tier>·<priority>]`. If `priority` matches a `TIERS` value, uses that tier's color class; otherwise `Custom` (neutral outline). `null` renders an em-dash placeholder (no tier color). Pure display — no interaction.

### `PriorityChipMenu` (new)

```ts
type PriorityChipMenuProps = {
  value: number | null;
  onChange: (priority: number) => void;
  align?: "start" | "end";
  disabled?: boolean;
};
```

Wraps a clickable `PriorityChip` (with `▾` caret) and a `DropdownMenu` (shadcn) containing one menu item per tier and a `Custom…` item. Picking a tier fires `onChange(tierValue)`. The `Custom…` item swaps the menu body to a small inline number input + Apply (Enter submits) — staying inside the menu rather than a separate modal. `disabled` renders as a read-only chip (no caret, no click handler) — used when a row is non-pending.

### `lib/tier.ts` — additions

Keep `TIERS`, `tierForPriority`, `priorityForTier`. Add:

```ts
export type TierKey = "low" | "normal" | "high" | "urgent" | "custom";
export function tierKeyForPriority(p: number): TierKey;     // exact-match tier, else "custom"
export function tierLabelForPriority(p: number): string;    // "Low" | "Normal" | "High" | "Urgent" | "Custom"
```

Callers handle `null` themselves (rendering the em-dash placeholder); the helpers operate on real numbers only.

### CSS tokens (extend `web/src/index.css`)

Add five tier color pairs (`--tier-low-bg/-fg`, `--tier-normal-*`, `--tier-high-*`, `--tier-urgent-*`, `--tier-custom-*`) under both `:root` (light) and `.dark` blocks. Values are the prototype's palette tuned per theme. Tailwind config exposes them as `tier-{key}-{bg,fg}` colors so component code stays in classes.

### `SegmentedTiers` — adopted, not changed

Used as-is on Default-priorities. (Currently it lives in `components/` but no page imports it — Default-priorities re-implements the same buttons inline.)

---

## Per-surface changes

### `pages/QueuePage.tsx`
- RUNNING row: replace `<span>p{e.priority} · {relativeTime(...)}</span>` with `<PriorityChip priority={e.priority} />` + `· {relativeTime(...)}`.
- QUEUED row: replace `<span>p{e.priority}</span>` with `<PriorityChipMenu value={e.priority} onChange={(p) => reprioritize(e.id, p)} disabled={false} align="end" />`.
- Delete the standalone `Move to front` icon `Button` (the `ArrowUpToLine` import becomes unused — remove it). Cancel (`X`) stays.
- `reprioritize(id, p)` calls the existing `POST /api/runs/{id}/priority` (same endpoint the to-front button used).

### `pages/ScenariosPage.tsx`
- Remove the `prio` `<Input>` and the local `submitPriority` `Record<string, string>` state in favor of `Record<string, number | undefined>`. (Numeric model, not string-edit-buffer.)
- Render `<PriorityChipMenu value={submitPriority[s.id] ?? effectiveDefault(s)} onChange={(p) => setSubmitPriority((r) => ({ ...r, [s.id]: p }))} />`, where `effectiveDefault(s) = defaults[s.id] ?? bakedFromTemplate(s)` — mirroring the submitter's resolution order (request override → per-scenario default → baked spec.priority).
- Card header keeps `default <PriorityChip priority={effectiveDefault(s)} />` (replacing the existing `tierForPriority(...)` text branch).

### `pages/DefaultPrioritiesPage.tsx`
- Adopt `PageHeader` (title `Default priorities`, subtitle one line, `← Queue` link as `action`).
- Three columns: `Scenario` | `Priority` | `Status`.
- `Priority`: `<SegmentedTiers value={effective} onPick={(p) => save(scenario, p)} />` followed by a small `Custom…` link that toggles an inline number input (Enter applies, calls `save`).
- `Status`: `<PriorityChip priority={effective} />` followed by `= baked default` or `overridden · baked {baked}`.
- Drop the standalone numeric `<Input>` column and `Save` button — clicking a tier saves immediately (mirrors today's button behaviour, but without the redundant second affordance).

### `pages/RunsPage.tsx`
- Replace the `Priority` column cell `{r.priority ?? "—"}` with `<PriorityChip priority={r.priority ?? null} />`. Keep the column right-aligned; remove `font-mono tabular-nums` (chip carries its own style).

### `pages/RunDetailPage.tsx`
- Replace the meta value `String(run.priority)` with `<PriorityChip priority={run.priority ?? null} />` rendered inside the existing `Meta` value slot (slight `Meta` adjustment to accept a `ReactNode` value rather than only `string`).

---

## Out of scope

- No backend / API / OpenAPI change. The reprioritize, scenario-priorities, and create-run endpoints already support every behaviour the new UI needs.
- No change to the `dlh` CLI.
- No new tier (still Low/Normal/High/Urgent + Custom).
- No change to the `BuildLanes` queue logic (the recent semaphore-status fix stands).
- No restructuring of `QueuePage` lane card layout beyond the per-row chip swap and to-front-button removal.
- Light theme uses the same tier hues (re-tuned saturations) — no separate visual rethink.

---

## Testing

Pure `lib/tier.ts` additions are Vitest-unit-tested:
- `tierKeyForPriority(10) === "low"`, `(100) === "normal"`, `(200) === "high"`, `(500) === "urgent"`.
- `tierKeyForPriority(137) === "custom"`; `(null) === "custom"` (caller renders `"—"` for null).
- `tierLabelForPriority(...)` matching the tier or `"Custom"` or `"—"`.

Component sanity (Vitest + jsdom or React Testing Library if present; otherwise a focused snapshot/render test):
- `<PriorityChip priority={100}>` renders text containing `Normal` and `100` and carries a `tier-normal` color class.
- `<PriorityChip priority={137}>` renders `Custom` + `137`, carries the `tier-custom` class.
- `<PriorityChip priority={null}>` renders an em-dash, no tier color.
- `<PriorityChipMenu value={100} onChange={cb}>` fires `cb(500)` when the Urgent menu item is clicked; the `Custom…` path fires `cb(<typedInt>)` after submitting the inline input.
- `disabled` variant: no caret rendered; `onClick` no-op.

Build gate: `pnpm build` (tsc + vite build). Existing tests in `lib/` stay green.

Live re-verification on minikube once the new pieces land: walk through Queue (chip menu reprioritizes a pending run; `BuildLanes` correctly reorders), Scenarios (chip menu changes the submitted priority), Default-priorities (clicking a tier persists; refresh shows it), Runs/Run-detail (chip rendered). 0 console errors.

---

## Risks & open questions

- **Color contrast in light theme** — the chip palette needs separate light-theme values; the prototype is dark-only. Light variants get a quick pass with the same hue keys but tuned saturations / fg colors. Verified via the existing theme toggle.
- **Custom… UX inside the menu** — keeping the editor inline (no modal) is simpler but constrains the input to small widths; acceptable for integer entry.
- **`Meta` component signature change** — Run-detail's `<Meta value=... />` needs to accept `ReactNode` to host the chip. Minor; the only other callers in `RunDetailPage` already pass strings and will keep working.

---

## References

- Prototype: `docs/superpowers/specs/proto-queue-priority.html` (open via `python3 -m http.server 5501` in the spec dir, then `http://127.0.0.1:5501/proto-queue-priority.html`).
- Existing tier helpers: `controlplane/web/src/lib/tier.ts`.
- Existing segmented control: `controlplane/web/src/components/SegmentedTiers.tsx`.
- Theme tokens: `controlplane/web/src/index.css` (`:root` light + `.dark` dark).
- Backend reprioritize: `POST /api/runs/{id}/priority` (returns 409 if non-pending — `runs.ErrNotPending`).
