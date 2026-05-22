import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../api/client";
import type { components } from "../api/gen";
import { StatusBadge } from "../components/StatusBadge";

type Run = components["schemas"]["Run"];

export function RunsPage() {
  const [items, setItems] = useState<Run[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api.GET("/api/runs", { params: { query: { limit: 100 } } }).then(({ data, error }) => {
      if (error) setError(JSON.stringify(error));
      else setItems(data?.items ?? []);
    });
  }, []);

  if (error) return <p className="text-rose-700">Error: {error}</p>;
  if (!items) return <p>Loading…</p>;
  return (
    <section>
      <h1 className="mb-4 text-xl font-semibold">Runs</h1>
      <table className="w-full border-collapse text-sm">
        <thead>
          <tr className="border-b border-slate-200 text-left text-slate-600">
            <th className="py-2">Scenario</th>
            <th>Target</th>
            <th>Status</th>
            <th>Started</th>
            <th>Score</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {items.map((r) => (
            <tr key={r.id} className="border-b border-slate-100">
              <td className="py-2">{r.scenario}</td>
              <td>{r.target ?? "local"}</td>
              <td><StatusBadge status={String(r.status)} /></td>
              <td className="text-slate-600">{new Date(r.startedAt).toLocaleString()}</td>
              <td>{r.score?.toFixed(2) ?? "—"}</td>
              <td><Link className="text-blue-600 hover:underline" to={`/runs/${r.id}`}>view</Link></td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}
