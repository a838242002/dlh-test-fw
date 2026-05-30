import { deriveTargetType } from "@/lib/category";
import { cn } from "@/lib/utils";

// Two-letter glyph derived from the scenario's target type (mysql→MY, etc.).
export function TargetGlyph({ scenario, className }: { scenario: string; className?: string }) {
  const initials = deriveTargetType(scenario).slice(0, 2).toUpperCase();
  return (
    <span className={cn("grid h-7 w-7 shrink-0 place-items-center rounded-lg bg-accent text-[11px] font-semibold uppercase text-muted-foreground", className)}>
      {initials}
    </span>
  );
}
