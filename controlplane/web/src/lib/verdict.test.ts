import { describe, it, expect } from "vitest";
import { parseVerdict, formatMetricByName } from "@/lib/verdict";

describe("parseVerdict", () => {
  it("returns null for null/undefined", () => {
    expect(parseVerdict(null)).toBeNull();
    expect(parseVerdict(undefined)).toBeNull();
  });

  it("returns null when overall is not a boolean", () => {
    expect(parseVerdict({ thresholds: [] })).toBeNull();
  });

  it("parses overall and thresholds with lt bound", () => {
    const v = parseVerdict({
      overall: true,
      thresholds: [
        { metric: "p95-latency-chaos", value: 0.0000025, lt: 2.5, passed: true },
        { metric: "error-rate-recovery", value: 0.3, lt: 0.05, passed: false },
      ],
    });
    expect(v).not.toBeNull();
    expect(v!.overall).toBe(true);
    expect(v!.thresholds).toHaveLength(2);
    expect(v!.thresholds[0]).toEqual({
      metric: "p95-latency-chaos", value: 0.0000025, bound: "< 2.5", passed: true,
      cmp: "<", boundValue: 2.5, window: "",
    });
  });

  it("formats a gt bound", () => {
    const v = parseVerdict({ overall: false, thresholds: [{ metric: "throughput", value: 50, gt: 100, passed: false }] });
    expect(v!.thresholds[0].bound).toBe("> 100");
  });

  it("uses '—' bound when neither lt nor gt present", () => {
    const v = parseVerdict({ overall: true, thresholds: [{ metric: "x", value: 1, passed: true }] });
    expect(v!.thresholds[0].bound).toBe("—");
  });

  it("extracts raw_promql when present", () => {
    const v = parseVerdict({
      overall: true, thresholds: [],
      raw_promql: "up == 1", raw_promql_value: 1, raw_promql_pass: true,
    });
    expect(v!.rawPromQL).toEqual({ query: "up == 1", value: 1, passed: true });
  });

  it("omits rawPromQL when raw_promql is empty", () => {
    const v = parseVerdict({ overall: true, thresholds: [] });
    expect(v!.rawPromQL).toBeUndefined();
  });

  it("tolerates a missing thresholds array", () => {
    const v = parseVerdict({ overall: true });
    expect(v!.thresholds).toEqual([]);
  });
});

describe("formatMetricByName", () => {
  it("latency → time units", () => {
    expect(formatMetricByName("p95-latency-chaos", 3e-6)).toBe("3 µs");
    expect(formatMetricByName("p95-latency-chaos", 0.0004)).toBe("0.4 ms");
    expect(formatMetricByName("p95-latency-chaos", 2.5)).toBe("2.5 s");
  });
  it("rate/error → percent", () => {
    expect(formatMetricByName("error-rate-recovery", 0.2961)).toBe("29.6 %");
    expect(formatMetricByName("error-rate-recovery", 0.5)).toBe("50 %");
  });
  it("fallback → significant figures, no sci-notation", () => {
    expect(formatMetricByName("throughput", 1234.567)).toBe("1230");
  });
});
