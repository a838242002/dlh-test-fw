import { Card, CardContent } from "@/components/ui/card";
import { cn } from "@/lib/utils";

export function StatCard({
  label,
  value,
  accent,
}: {
  label: string;
  value: string;
  accent?: "primary" | "success" | "running" | "failed";
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
      <CardContent className="p-4">
        <div className="text-sm text-muted-foreground">{label}</div>
        <div className={cn("mt-1 text-2xl font-bold", accentClass)}>{value}</div>
      </CardContent>
    </Card>
  );
}
