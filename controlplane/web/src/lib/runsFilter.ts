import type { components } from "@/api/gen";
import { deriveCategory } from "@/lib/category";

type Run = components["schemas"]["Run"];

export interface RunFilter {
  search: string;
  status: string; // "" = any
  category: string; // "" = any
  timeRange: "" | "24h" | "7d";
  failedOnly: boolean;
}

export const EMPTY_FILTER: RunFilter = {
  search: "",
  status: "",
  category: "",
  timeRange: "",
  failedOnly: false,
};

export function filterRuns(runs: Run[], f: RunFilter, now: number = Date.now()): Run[] {
  const q = f.search.trim().toLowerCase();
  const maxMs = f.timeRange === "24h" ? 24 * 3_600_000 : f.timeRange === "7d" ? 7 * 86_400_000 : Infinity;
  return runs.filter((r) => {
    if (q && !r.scenario.toLowerCase().includes(q)) return false;
    if (f.status && r.status !== f.status) return false;
    if (f.failedOnly && r.status !== "Failed" && r.status !== "Error") return false;
    if (f.category && deriveCategory(r.scenario) !== f.category) return false;
    if (maxMs !== Infinity && now - new Date(r.startedAt).getTime() > maxMs) return false;
    return true;
  });
}

export type SortKey = "started" | "duration";
export type SortDir = "asc" | "desc";
export interface RunSort {
  key: SortKey;
  dir: SortDir;
}

function durationMs(r: Run): number {
  if (!r.finishedAt) return -1;
  return new Date(r.finishedAt).getTime() - new Date(r.startedAt).getTime();
}

export function sortRuns(runs: Run[], s: RunSort): Run[] {
  const sign = s.dir === "desc" ? -1 : 1;
  return [...runs].sort((a, b) => {
    const av = s.key === "started" ? new Date(a.startedAt).getTime() : durationMs(a);
    const bv = s.key === "started" ? new Date(b.startedAt).getTime() : durationMs(b);
    return sign * (av - bv);
  });
}
