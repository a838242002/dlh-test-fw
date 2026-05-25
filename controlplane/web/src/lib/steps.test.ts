import { describe, it, expect } from "vitest";
import { isGroupNode, namedSteps, timelineLayout } from "@/lib/steps";

describe("isGroupNode", () => {
  it("matches only bracketed integer names", () => {
    expect(isGroupNode("[0]")).toBe(true);
    expect(isGroupNode("[12]")).toBe(true);
    expect(isGroupNode(" [3] ")).toBe(true);
    expect(isGroupNode("chaos")).toBe(false);
    expect(isGroupNode("step[0]")).toBe(false);
    expect(isGroupNode("[a]")).toBe(false);
  });
});

describe("namedSteps", () => {
  const steps = [
    { name: "[0]", phase: "Succeeded" },
    { name: "chaos", phase: "Succeeded" },
    { name: "wf-root", phase: "Succeeded" },
    { name: "[1]", phase: "Succeeded" },
    { name: "verdict", phase: "Succeeded" },
  ];
  it("drops group nodes", () => {
    expect(namedSteps(steps).map((s) => s.name)).toEqual(["chaos", "wf-root", "verdict"]);
  });
  it("also drops the root node when its name is given", () => {
    expect(namedSteps(steps, "wf-root").map((s) => s.name)).toEqual(["chaos", "verdict"]);
  });
});

const tlSteps = [
  { name: "prep", startedAt: "2026-01-01T00:00:00Z", finishedAt: "2026-01-01T00:00:30Z" },
  { name: "load", startedAt: "2026-01-01T00:00:30Z", finishedAt: "2026-01-01T00:04:16Z" },
];

describe("timelineLayout", () => {
  it("maps offset/width as % of the run window", () => {
    const lay = timelineLayout(tlSteps, undefined);
    expect(lay.windowMs).toBe(256000);
    expect(lay.bars[0].offsetPct).toBeCloseTo(0, 3);
    expect(lay.bars[1].offsetPct).toBeCloseTo((30 / 256) * 100, 1);
    expect(lay.bars[1].widthPct).toBeCloseTo((226 / 256) * 100, 1);
  });
  it("applies a minimum visible width", () => {
    const lay = timelineLayout([{ name: "x", startedAt: "2026-01-01T00:00:00Z", finishedAt: "2026-01-01T00:00:00Z" }], undefined);
    expect(lay.bars[0].widthPct).toBeGreaterThanOrEqual(0.7);
  });
});
