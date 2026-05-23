import { Zap, Wrench, Activity, CheckCircle2, Hammer, Box, type LucideIcon } from "lucide-react";
import { cn } from "@/lib/utils";
import { CATEGORIES, type CategoryKey } from "@/lib/category";

const ICONS: Record<CategoryKey, LucideIcon> = {
  chaos: Zap,
  fixture: Wrench,
  load: Activity,
  verdict: CheckCircle2,
  util: Hammer,
  other: Box,
};

export function CategoryIcon({ category, className }: { category: CategoryKey; className?: string }) {
  const Icon = ICONS[category];
  const accent = CATEGORIES.find((c) => c.key === category)?.accent ?? "text-slate-400";
  return <Icon className={cn("h-4 w-4", accent, className)} />;
}
