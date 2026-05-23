export interface ParsedThreshold {
  metric: string;
  value: number;
  bound: string;
  passed: boolean;
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
