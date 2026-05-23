import { describe, it, expect } from "vitest";
import { deriveCategory, deriveTargetType, CATEGORIES } from "@/lib/category";

describe("deriveCategory", () => {
  it("matches explicit prefixes", () => {
    expect(deriveCategory("fixture-kafka-topic-seed")).toBe("fixture");
    expect(deriveCategory("util-write-slo")).toBe("util");
    expect(deriveCategory("load-k6-run")).toBe("load");
    expect(deriveCategory("verdict-slo-eval")).toBe("verdict");
    expect(deriveCategory("chaos-network-loss")).toBe("chaos");
  });
  it("falls back to chaos for unprefixed chaos scenarios", () => {
    expect(deriveCategory("mysql-pod-delete")).toBe("chaos");
    expect(deriveCategory("kafka-broker-partition")).toBe("chaos");
    expect(deriveCategory("doris-be-network-loss")).toBe("chaos");
  });
  it("falls back to other when nothing matches", () => {
    expect(deriveCategory("something-weird")).toBe("other");
  });
});

describe("deriveTargetType", () => {
  it("detects engine from id, else generic", () => {
    expect(deriveTargetType("mysql-pod-delete")).toBe("mysql");
    expect(deriveTargetType("fixture-kafka-topic-seed")).toBe("kafka");
    expect(deriveTargetType("doris-be-network-loss")).toBe("doris");
    expect(deriveTargetType("load-k6-run")).toBe("generic");
  });
});

describe("CATEGORIES", () => {
  it("is ordered chaos→fixture→load→verdict→util→other", () => {
    expect(CATEGORIES.map((c) => c.key)).toEqual(["chaos", "fixture", "load", "verdict", "util", "other"]);
  });
});
