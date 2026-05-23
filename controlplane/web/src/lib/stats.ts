import type { components } from "@/api/gen";

type Run = components["schemas"]["Run"];
type Schedule = components["schemas"]["Schedule"];

export interface Stats {
  passRate7d: number | null;
  runsToday: number;
  runningNow: number;
  activeSchedules: number;
}

const SEVEN_DAYS_MS = 7 * 86_400_000;

export function computeStats(runs: Run[], schedules: Schedule[]): Stats {
  const now = Date.now();
  const midnight = new Date();
  midnight.setHours(0, 0, 0, 0);

  let succeeded = 0;
  let terminal = 0;
  let runsToday = 0;
  let runningNow = 0;

  for (const r of runs) {
    const started = new Date(r.startedAt).getTime();
    if (r.status === "Running") runningNow++;
    if (started >= midnight.getTime()) runsToday++;
    if (now - started <= SEVEN_DAYS_MS) {
      if (r.status === "Succeeded") {
        succeeded++;
        terminal++;
      } else if (r.status === "Failed" || r.status === "Error") {
        terminal++;
      }
    }
  }

  const activeSchedules = schedules.filter((s) => !s.suspended).length;

  return {
    passRate7d: terminal === 0 ? null : succeeded / terminal,
    runsToday,
    runningNow,
    activeSchedules,
  };
}
