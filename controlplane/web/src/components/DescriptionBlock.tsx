import { type ReactNode } from "react";
import { cn } from "@/lib/utils";

export function DescriptionBlock({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <p className={cn("max-w-2xl border-l-2 border-primary/40 pl-3 text-sm leading-relaxed text-muted-foreground", className)}>
      {children}
    </p>
  );
}
