/** Render a metric/threshold number readably: sci-notation at the extremes, trimmed 4 sig-figs otherwise. */
export function formatMetricValue(v: number): string {
  if (!Number.isFinite(v)) return String(v);
  if (v === 0) return "0";
  const abs = Math.abs(v);
  if (abs < 1e-3 || abs >= 1e6) return v.toExponential(2);
  return parseFloat(v.toPrecision(4)).toString();
}
