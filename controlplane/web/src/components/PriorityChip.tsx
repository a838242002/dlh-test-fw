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
      <span className="font-normal opacity-80 tabular-nums">·{priority}</span>
    </span>
  );
}
