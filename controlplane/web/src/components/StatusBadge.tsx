import { cn } from "@/lib/utils";

const STATUS_STYLES: Record<string, string> = {
  Succeeded: "bg-status-success/15 text-status-success",
  Running: "bg-status-running/15 text-status-running",
  Failed: "bg-status-failed/15 text-status-failed",
  Error: "bg-status-failed/15 text-status-failed",
  Pending: "bg-status-pending/15 text-status-pending",
  Unknown: "bg-status-pending/15 text-status-pending",
};

export function StatusBadge({ status }: { status: string }) {
  const style = STATUS_STYLES[status] ?? STATUS_STYLES.Unknown;
  return (
    <span className={cn("inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium", style)}>
      <span className={cn("h-1.5 w-1.5 rounded-full bg-current", status === "Running" && "animate-pulse")} />
      {status}
    </span>
  );
}
