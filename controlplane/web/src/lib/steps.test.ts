import { describe, it, expect } from "vitest";
import { isGroupNode, namedSteps } from "@/lib/steps";

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
