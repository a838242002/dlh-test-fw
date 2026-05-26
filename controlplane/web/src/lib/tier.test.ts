import { describe, it, expect } from "vitest";
import { TIERS, tierForPriority, priorityForTier } from "@/lib/tier";

describe("tier mapping", () => {
  it("exposes the four named tiers", () => {
    expect(TIERS.map((t) => t.label)).toEqual(["Low", "Normal", "High", "Urgent"]);
    expect(TIERS.map((t) => t.value)).toEqual([10, 100, 200, 500]);
  });
  it("maps a priority to its exact tier label, else null", () => {
    expect(tierForPriority(100)).toBe("Normal");
    expect(tierForPriority(500)).toBe("Urgent");
    expect(tierForPriority(150)).toBeNull(); // custom value, no exact tier
  });
  it("maps a tier label to its priority value", () => {
    expect(priorityForTier("High")).toBe(200);
    expect(priorityForTier("nope")).toBeNull();
  });
});
