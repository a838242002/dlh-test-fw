import { sparklinePoints } from "@/lib/sparkline";

export function Sparkline({ values, className }: { values: number[]; className?: string }) {
  const pts = sparklinePoints(values, 120, 24);
  if (!pts) return null;
  return (
    <svg viewBox="0 0 120 24" className={className} preserveAspectRatio="none" aria-hidden>
      <polyline
        points={pts}
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}
