import { describe, it, expect } from "vitest";
import { TIERS, tierForPriority, priorityForTier, tierKeyForPriority, tierLabelForPriority } from "@/lib/tier";

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

describe("tierKeyForPriority", () => {
  it("maps exact tier values to their key", () => {
    expect(tierKeyForPriority(10)).toBe("low");
    expect(tierKeyForPriority(100)).toBe("normal");
    expect(tierKeyForPriority(200)).toBe("high");
    expect(tierKeyForPriority(500)).toBe("urgent");
  });
  it("falls back to 'custom' for any other value", () => {
    expect(tierKeyForPriority(0)).toBe("custom");
    expect(tierKeyForPriority(137)).toBe("custom");
    expect(tierKeyForPriority(99)).toBe("custom");
    expect(tierKeyForPriority(600)).toBe("custom");
  });
});

describe("tierLabelForPriority", () => {
  it("returns the tier label for exact tier values", () => {
    expect(tierLabelForPriority(10)).toBe("Low");
    expect(tierLabelForPriority(100)).toBe("Normal");
    expect(tierLabelForPriority(200)).toBe("High");
    expect(tierLabelForPriority(500)).toBe("Urgent");
  });
  it("returns 'Custom' for any other value", () => {
    expect(tierLabelForPriority(137)).toBe("Custom");
    expect(tierLabelForPriority(0)).toBe("Custom");
  });
});
