import { describe, it, expect } from "vitest";
import { formatMetricValue } from "@/lib/format";

describe("formatMetricValue", () => {
  it("uses scientific notation for very small/large magnitudes", () => {
    expect(formatMetricValue(0.0000034999847412105)).toBe("3.50e-6");
    expect(formatMetricValue(2_500_000)).toBe("2.50e+6");
  });
  it("rounds mid-range to <=4 significant figures and trims zeros", () => {
    expect(formatMetricValue(0.295585588666926)).toBe("0.2956");
    expect(formatMetricValue(2.5)).toBe("2.5");
    expect(formatMetricValue(1)).toBe("1");
  });
  it("handles 0 and non-finite", () => {
    expect(formatMetricValue(0)).toBe("0");
    expect(formatMetricValue(NaN)).toBe("NaN");
  });
});
