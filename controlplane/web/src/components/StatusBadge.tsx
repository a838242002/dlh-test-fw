const colors: Record<string, string> = {
  Pending: "bg-slate-200 text-slate-800",
  Running: "bg-blue-100 text-blue-800",
  Succeeded: "bg-emerald-100 text-emerald-800",
  Failed: "bg-rose-100 text-rose-800",
  Error: "bg-rose-100 text-rose-800",
  Unknown: "bg-slate-100 text-slate-700",
};

export function StatusBadge({ status }: { status: string }) {
  return (
    <span className={`inline-block rounded px-2 py-0.5 text-xs font-medium ${colors[status] ?? colors.Unknown}`}>
      {status}
    </span>
  );
}
