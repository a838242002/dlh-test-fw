import { describe, it, expect } from "vitest";
import { relativeTime, formatDuration } from "@/lib/time";

const NOW = new Date("2026-05-23T12:00:00Z").getTime();
const ago = (ms: number) => new Date(NOW - ms).toISOString();

describe("relativeTime", () => {
  it("formats seconds/minutes/hours/days", () => {
    expect(relativeTime(ago(5_000), NOW)).toBe("5s ago");
    expect(relativeTime(ago(90_000), NOW)).toBe("1m ago");
    expect(relativeTime(ago(2 * 3_600_000), NOW)).toBe("2h ago");
    expect(relativeTime(ago(3 * 86_400_000), NOW)).toBe("3d ago");
  });
  it("clamps future to 0s and handles invalid input", () => {
    expect(relativeTime(new Date(NOW + 10_000).toISOString(), NOW)).toBe("0s ago");
    expect(relativeTime("not-a-date", NOW)).toBe("—");
  });
});

describe("formatDuration", () => {
  it("returns — when either endpoint is missing/invalid", () => {
    expect(formatDuration("2026-05-23T12:00:00Z", undefined)).toBe("—");
    expect(formatDuration(undefined, "2026-05-23T12:00:00Z")).toBe("—");
    expect(formatDuration("bad", "2026-05-23T12:00:00Z")).toBe("—");
  });
  it("formats s / m s / h m", () => {
    const s = "2026-05-23T12:00:00Z";
    expect(formatDuration(s, "2026-05-23T12:00:45Z")).toBe("45s");
    expect(formatDuration(s, "2026-05-23T12:04:16Z")).toBe("4m 16s");
    expect(formatDuration(s, "2026-05-23T13:05:00Z")).toBe("1h 5m");
  });
});
