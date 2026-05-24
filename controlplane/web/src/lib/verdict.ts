export interface ParsedThreshold {
  metric: string;
  value: number;
  bound: string;
  passed: boolean;
  cmp?: string;        // "<" | ">" | ""
  boundValue?: number; // numeric bound
  window?: string;     // "chaos" | "recovery" | ""
}

export interface ParsedVerdict {
  overall: boolean;
  thresholds: ParsedThreshold[];
  rawPromQL?: { query: string; value: number; passed: boolean };
}

function num(v: unknown): number {
  return typeof v === "number" ? v : Number(v);
}

export function parseVerdict(raw: Record<string, unknown> | null | undefined): ParsedVerdict | null {
  if (!raw || typeof raw.overall !== "boolean") return null;

  const rawThresholds = Array.isArray(raw.thresholds) ? (raw.thresholds as Record<string, unknown>[]) : [];
  const thresholds: ParsedThreshold[] = rawThresholds.map((t) => {
    let bound = "—";
    if (typeof t.lt === "number") bound = `< ${t.lt}`;
    else if (typeof t.gt === "number") bound = `> ${t.gt}`;
    return {
      metric: String(t.metric ?? ""),
      value: num(t.value),
      bound,
      passed: Boolean(t.passed),
      cmp: typeof t.lt === "number" ? "<" : typeof t.gt === "number" ? ">" : "",
      boundValue: typeof t.lt === "number" ? t.lt : typeof t.gt === "number" ? t.gt : NaN,
      window: typeof t.window === "string" ? t.window : "",
    };
  });

  const result: ParsedVerdict = { overall: raw.overall, thresholds };

  if (typeof raw.raw_promql === "string" && raw.raw_promql !== "") {
    result.rawPromQL = {
      query: raw.raw_promql,
      value: num(raw.raw_promql_value),
      passed: Boolean(raw.raw_promql_pass),
    };
  }

  return result;
}

/** Format a metric value with units inferred from the metric name. */
export function formatMetricByName(metric: string, v: number): string {
  if (!Number.isFinite(v)) return String(v);
  const m = metric.toLowerCase();
  if (m.includes("latency") || m.includes("duration")) {
    if (v < 1e-4) return `${trim(v * 1e6)} µs`;
    if (v < 1) return `${trim(v * 1e3)} ms`;
    return `${trim(v)} s`;
  }
  if (m.includes("rate") || m.includes("error")) {
    return `${trim(v * 100)} %`;
  }
  return trim(v);
}

/** 3 significant figures, no scientific notation, trailing zeros trimmed. */
function trim(v: number): string {
  return parseFloat(v.toPrecision(3)).toString();
}
