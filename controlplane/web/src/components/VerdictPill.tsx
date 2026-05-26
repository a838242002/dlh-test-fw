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
