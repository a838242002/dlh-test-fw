import { describe, it, expect } from "vitest";
import { computeStats } from "@/lib/stats";
import type { components } from "@/api/gen";

type Run = components["schemas"]["Run"];
type Schedule = components["schemas"]["Schedule"];

const iso = (d: Date) => d.toISOString();
const daysAgo = (n: number) => new Date(Date.now() - n * 86_400_000);
const hoursAgo = (n: number) => new Date(Date.now() - n * 3_600_000);

function run(partial: Partial<Run>): Run {
  return { id: "x", scenario: "s", status: "Succeeded", startedAt: iso(new Date()), ...partial } as Run;
}

describe("computeStats", () => {
  it("pass rate counts Succeeded over terminal runs in last 7d", () => {
    const runs = [
      run({ status: "Succeeded", startedAt: iso(daysAgo(1)) }),
      run({ status: "Succeeded", startedAt: iso(daysAgo(2)) }),
      run({ status: "Failed", startedAt: iso(daysAgo(3)) }),
      run({ status: "Error", startedAt: iso(daysAgo(4)) }),
      run({ status: "Running", startedAt: iso(daysAgo(1)) }), // ignored (not terminal)
      run({ status: "Succeeded", startedAt: iso(daysAgo(9)) }), // ignored (>7d)
    ];
    const s = computeStats(runs, []);
    expect(s.passRate7d).toBeCloseTo(0.5); // 2 succeeded / 4 terminal
  });

  it("passRate7d is null when there are no terminal runs in window", () => {
    const s = computeStats([run({ status: "Running", startedAt: iso(hoursAgo(1)) })], []);
    expect(s.passRate7d).toBeNull();
  });

  it("runsToday counts runs started since local midnight", () => {
    const midnight = new Date();
    midnight.setHours(0, 0, 0, 0);
    const runs = [
      run({ startedAt: iso(new Date(midnight.getTime() + 3_600_000)) }), // today
      run({ startedAt: iso(new Date(midnight.getTime() - 3_600_000)) }), // yesterday
    ];
    expect(computeStats(runs, []).runsToday).toBe(1);
  });

  it("runningNow counts Running status", () => {
    const runs = [run({ status: "Running" }), run({ status: "Running" }), run({ status: "Succeeded" })];
    expect(computeStats(runs, []).runningNow).toBe(2);
  });

  it("activeSchedules counts non-suspended schedules", () => {
    const schedules = [
      { id: "a", scenario: "s", cron: "* * * * *", suspended: false } as Schedule,
      { id: "b", scenario: "s", cron: "* * * * *", suspended: true } as Schedule,
      { id: "c", scenario: "s", cron: "* * * * *" } as Schedule, // undefined => active
    ];
    expect(computeStats([], schedules).activeSchedules).toBe(2);
  });
});
