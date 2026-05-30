import { type ReactNode } from "react";

export function InfoBand({ children }: { children: ReactNode }) {
  return (
    <div className="flex items-center gap-3 rounded-lg border bg-card px-4 py-3 text-xs">
      <span className="grid h-5 w-5 shrink-0 place-items-center rounded-full bg-primary/15 text-[11px] font-semibold text-primary">i</span>
      <span className="text-muted-foreground">{children}</span>
    </div>
  );
}

// Helper to emphasize a key term inside an InfoBand body.
export function Term({ children }: { children: ReactNode }) {
  return <span className="font-medium text-foreground">{children}</span>;
}
