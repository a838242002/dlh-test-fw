import { describe, it, expect } from "vitest";
import { lastRunByScenario } from "@/lib/scenarioRuns";
import type { components } from "@/api/gen";

type Run = components["schemas"]["Run"];
const run = (scenario: string, startedAt: string, score: number | null = null): Run =>
  ({ id: `${scenario}-${startedAt}`, scenario, status: "Succeeded", startedAt, score }) as Run;

describe("lastRunByScenario", () => {
  it("keeps the latest run per scenario", () => {
    const m = lastRunByScenario([
      run("mysql-pod-delete", "2026-05-26T10:00:00Z", 1),
      run("mysql-pod-delete", "2026-05-26T12:00:00Z", 0),
      run("kafka-broker-partition", "2026-05-26T11:00:00Z", 1),
    ]);
    expect(m["mysql-pod-delete"]).toEqual({ startedAt: "2026-05-26T12:00:00Z", score: 0 });
    expect(m["kafka-broker-partition"]).toEqual({ startedAt: "2026-05-26T11:00:00Z", score: 1 });
  });
  it("returns an empty map for no runs", () => {
    expect(lastRunByScenario([])).toEqual({});
  });
});
