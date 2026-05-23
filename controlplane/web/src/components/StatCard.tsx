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
