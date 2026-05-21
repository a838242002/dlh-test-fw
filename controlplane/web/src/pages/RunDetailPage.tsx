import { useEffect, useState } from "react";
import { useParams } from "react-router-dom";
import { api } from "../api/client";
import type { components } from "../api/gen";
import { StatusBadge } from "../components/StatusBadge";

type RunDetail = components["schemas"]["RunDetail"];

export function RunDetailPage() {
  const { id } = useParams<{ id: string }>();
  const [run, setRun] = useState<RunDetail | null>(null);
  const [liveStatus, setLiveStatus] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!id) return;
    api.GET("/api/runs/{id}", { params: { path: { id } } }).then(({ data, error }) => {
      if (error) setError(JSON.stringify(error));
      else setRun(data as RunDetail);
    });

    // SSE
    const es = new EventSource(`/api/runs/${id}/events`);
    const onEvent = (e: MessageEvent) => {
      try {
        const data = JSON.parse(e.data);
        if (data.phase) setLiveStatus(data.phase);
      } catch {
        /* ignore */
      }
    };
    es.addEventListener("snapshot", onEvent);
    es.addEventListener("MODIFIED", onEvent);
    es.addEventListener("ADDED", onEvent);
    es.addEventListener("DELETED", onEvent);
    return () => es.close();
  }, [id]);

  if (error) return <p className="text-rose-700">Error: {error}</p>;
  if (!run) return <p>Loading…</p>;
  const status = liveStatus ?? String(run.status ?? "Unknown");
  return (
    <section className="space-y-6">
      <header className="flex items-baseline gap-3">
        <h1 className="text-xl font-semibold">{run.id}</h1>
        <StatusBadge status={status} />
      </header>
      <div>
        <h2 className="mb-2 font-medium">Scenario</h2>
        <p className="text-sm text-slate-700">{run.scenario}</p>
      </div>
      {run.steps && (
        <div>
          <h2 className="mb-2 font-medium">Steps</h2>
          <ul className="space-y-1 text-sm">
            {run.steps.map((s, i) => (
              <li key={i} className="flex justify-between border-b border-slate-100 py-1">
                <span>{s.name}</span>
                <span className="text-slate-600">{s.phase}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
      {run.verdict && (
        <div>
          <h2 className="mb-2 font-medium">Verdict</h2>
          <pre className="overflow-auto rounded border border-slate-200 bg-slate-50 p-3 text-xs">
            {JSON.stringify(run.verdict, null, 2)}
          </pre>
        </div>
      )}
    </section>
  );
}
