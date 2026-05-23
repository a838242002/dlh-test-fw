export function relativeTime(iso: string, now: number = Date.now()): string {
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) return "—";
  const sec = Math.max(0, Math.floor((now - t) / 1000));
  if (sec < 60) return `${sec}s ago`;
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min}m ago`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr}h ago`;
  return `${Math.floor(hr / 24)}d ago`;
}

export function formatDuration(startIso?: string, endIso?: string): string {
  if (!startIso || !endIso) return "—";
  const start = new Date(startIso).getTime();
  const end = new Date(endIso).getTime();
  if (Number.isNaN(start) || Number.isNaN(end) || end < start) return "—";
  let s = Math.floor((end - start) / 1000);
  if (s < 60) return `${s}s`;
  let m = Math.floor(s / 60);
  s = s % 60;
  if (m < 60) return `${m}m ${s}s`;
  const h = Math.floor(m / 60);
  m = m % 60;
  return `${h}h ${m}m`;
}
