export type CategoryKey = "chaos" | "fixture" | "load" | "verdict" | "util" | "other";
export type TargetType = "mysql" | "kafka" | "doris" | "generic";

export interface CategoryMeta {
  key: CategoryKey;
  label: string;
  /** Tailwind text-color class for the category accent. */
  accent: string;
}

// Order = the render order of Scenarios sections.
export const CATEGORIES: CategoryMeta[] = [
  { key: "chaos", label: "Chaos", accent: "text-red-400" },
  { key: "fixture", label: "Fixture", accent: "text-amber-400" },
  { key: "load", label: "Load", accent: "text-blue-400" },
  { key: "verdict", label: "Verdict", accent: "text-violet-400" },
  { key: "util", label: "Util", accent: "text-emerald-400" },
  { key: "other", label: "Other", accent: "text-slate-400" },
];

export function deriveCategory(id: string): CategoryKey {
  if (id.startsWith("fixture-")) return "fixture";
  if (id.startsWith("util-")) return "util";
  if (id.startsWith("load-")) return "load";
  if (id.startsWith("verdict-")) return "verdict";
  if (id.startsWith("chaos-")) return "chaos";
  if (id.includes("pod-delete") || id.includes("network-loss") || id.includes("broker-partition")) return "chaos";
  return "other";
}

export function deriveTargetType(id: string): TargetType {
  if (id.includes("mysql")) return "mysql";
  if (id.includes("kafka")) return "kafka";
  if (id.includes("doris")) return "doris";
  return "generic";
}
