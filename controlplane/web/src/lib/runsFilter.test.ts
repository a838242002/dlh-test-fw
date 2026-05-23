import { describe, it, expect } from "vitest";
import { verdictFromScore } from "@/lib/run";
import { filterRuns, sortRuns, EMPTY_FILTER } from "@/lib/runsFilter";
import type { components } from "@/api/gen";

type Run = components["schemas"]["Run"];
const NOW = new Date("2026-05-23T12:00:00Z").getTime();
const hAgo = (h: number) => new Date(NOW - h * 3_600_000).toISOString();
function run(p: Partial<Run>): Run {
  return { id: "x", scenario: "mysql-pod-delete", status: "Succeeded", startedAt: hAgo(1), ...p } as Run;
}

describe("verdictFromScore", () => {
  it("maps 1/0/null", () => {
    expect(verdictFromScore(1)).toBe("pass");
    expect(verdictFromScore(0)).toBe("fail");
    expect(verdictFromScore(null)).toBeNull();
    expect(verdictFromScore(undefined)).toBeNull();
  });
});

describe("filterRuns", () => {
  const runs = [
    run({ scenario: "mysql-pod-delete", status: "Succeeded", startedAt: hAgo(1) }),
    run({ scenario: "fixture-minio-load-mysql", status: "Failed", startedAt: hAgo(2) }),
    run({ scenario: "load-k6-run", status: "Succeeded", startedAt: hAgo(40) }),
  ];
  it("returns all with empty filter", () => {
    expect(filterRuns(runs, EMPTY_FILTER, NOW)).toHaveLength(3);
  });
  it("search matches scenario substring", () => {
    expect(filterRuns(runs, { ...EMPTY_FILTER, search: "minio" }, NOW)).toHaveLength(1);
  });
  it("status + failedOnly + category + timeRange compose", () => {
    expect(filterRuns(runs, { ...EMPTY_FILTER, status: "Failed" }, NOW)).toHaveLength(1);
    expect(filterRuns(runs, { ...EMPTY_FILTER, failedOnly: true }, NOW)).toHaveLength(1);
    expect(filterRuns(runs, { ...EMPTY_FILTER, category: "load" }, NOW)).toHaveLength(1);
    expect(filterRuns(runs, { ...EMPTY_FILTER, timeRange: "24h" }, NOW)).toHaveLength(2);
  });
});

describe("sortRuns", () => {
  const a = run({ id: "a", startedAt: hAgo(1), finishedAt: new Date(NOW - 1 * 3_600_000 + 60_000).toISOString() });
  const b = run({ id: "b", startedAt: hAgo(3), finishedAt: new Date(NOW - 3 * 3_600_000 + 600_000).toISOString() });
  it("sorts by started desc/asc", () => {
    expect(sortRuns([b, a], { key: "started", dir: "desc" })[0].id).toBe("a");
    expect(sortRuns([b, a], { key: "started", dir: "asc" })[0].id).toBe("b");
  });
  it("sorts by duration desc", () => {
    expect(sortRuns([a, b], { key: "duration", dir: "desc" })[0].id).toBe("b");
  });
});
