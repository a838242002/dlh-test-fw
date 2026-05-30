import { describe, it, expect } from "vitest";
import { sparklinePoints } from "@/lib/sparkline";

describe("sparklinePoints", () => {
  it("maps values to a polyline points string within the box", () => {
    const pts = sparklinePoints([0, 5, 10], 100, 20);
    // 3 points, evenly spaced on x (0, 50, 100); y inverted (0→bottom, 10→top)
    expect(pts).toBe("0,20 50,10 100,0");
  });
  it("handles a flat series (all equal) by drawing a mid line", () => {
    expect(sparklinePoints([4, 4, 4], 100, 20)).toBe("0,10 50,10 100,10");
  });
  it("returns empty string for <2 points", () => {
    expect(sparklinePoints([1], 100, 20)).toBe("");
    expect(sparklinePoints([], 100, 20)).toBe("");
  });
});
