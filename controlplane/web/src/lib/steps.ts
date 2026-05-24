/** True for Argo DAG/step-group placeholder nodes named like "[0]", "[12]". */
export function isGroupNode(name: string): boolean {
  return /^\[\d+\]$/.test(name.trim());
}

/** Keep only real named steps: drop bracketed group nodes and (optionally) the workflow root node. */
export function namedSteps<T extends { name: string }>(steps: T[], rootName?: string): T[] {
  return steps.filter((s) => !isGroupNode(s.name) && s.name !== rootName);
}

export interface TimelineStep { name: string; startedAt?: string; finishedAt?: string }
export interface TimelineBar { name: string; offsetPct: number; widthPct: number; running: boolean }
export interface TimelineLayout { windowMs: number; startMs: number; bars: TimelineBar[] }

const MIN_VISIBLE_PCT = 0.7;

/** Compute bar offset/width (% of the run window) for a chronological timeline.
 *  `nowIso` (default: Date.now) bounds still-running steps. */
export function timelineLayout(steps: TimelineStep[], nowIso?: string): TimelineLayout {
  const now = nowIso ? Date.parse(nowIso) : Date.now();
  const starts = steps.map((s) => (s.startedAt ? Date.parse(s.startedAt) : now));
  const ends = steps.map((s) => (s.finishedAt ? Date.parse(s.finishedAt) : now));
  const startMs = Math.min(...starts, now);
  const endMs = Math.max(...ends, startMs + 1);
  const windowMs = Math.max(endMs - startMs, 1);
  const bars = steps.map((s, i) => {
    const offsetPct = ((starts[i] - startMs) / windowMs) * 100;
    const rawWidth = ((ends[i] - starts[i]) / windowMs) * 100;
    return { name: s.name, offsetPct, widthPct: Math.max(rawWidth, MIN_VISIBLE_PCT), running: !s.finishedAt };
  });
  return { windowMs, startMs, bars };
}

/** Map a verdict window (epoch ms) to offset/width % within the run window. */
export function windowBand(startMs: number, windowMs: number, fromMs: number, toMs: number) {
  return { offsetPct: ((fromMs - startMs) / windowMs) * 100, widthPct: ((toMs - fromMs) / windowMs) * 100 };
}
