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
