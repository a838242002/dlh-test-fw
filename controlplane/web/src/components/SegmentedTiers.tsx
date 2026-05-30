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
