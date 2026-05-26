import type { components } from "@/api/gen";

type Run = components["schemas"]["Run"];
export type LastRun = { startedAt: string; score: number | null | undefined };

// Reduce a run list to the most-recent run per scenario id (by startedAt).
export function lastRunByScenario(runs: Run[]): Record<string, LastRun> {
  const out: Record<string, LastRun> = {};
  for (const r of runs) {
    const cur = out[r.scenario];
    if (!cur || new Date(r.startedAt).getTime() > new Date(cur.startedAt).getTime()) {
      out[r.scenario] = { startedAt: r.startedAt, score: r.score };
    }
  }
  return out;
}
