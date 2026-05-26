// Pure SVG-polyline math for a tiny trend sparkline. Returns a `points`
// string ("x,y x,y …") mapping values across [0,w] x [0,h], y-inverted
// (higher value = higher on screen). Flat series draw a mid line.
export function sparklinePoints(values: number[], w: number, h: number): string {
  if (values.length < 2) return "";
  const min = Math.min(...values);
  const max = Math.max(...values);
  const span = max - min;
  const stepX = w / (values.length - 1);
  return values
    .map((v, i) => {
      const x = Math.round(i * stepX);
      const y = span === 0 ? h / 2 : h - ((v - min) / span) * h;
      return `${x},${Math.round(y)}`;
    })
    .join(" ");
}
