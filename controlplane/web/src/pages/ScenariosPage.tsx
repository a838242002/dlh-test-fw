import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/gen";
import { TargetPicker } from "../components/TargetPicker";

type Scenario = components["schemas"]["Scenario"];

export function ScenariosPage() {
  const [items, setItems] = useState<Scenario[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [submitTarget, setSubmitTarget] = useState<Record<string, string>>({});
  const [submitting, setSubmitting] = useState<string | null>(null);

  useEffect(() => {
    api.GET("/api/scenarios", {}).then(({ data, error }) => {
      if (error) setError(JSON.stringify(error));
      else setItems(data?.items ?? []);
    });
  }, []);

  const handleRun = async (s: Scenario) => {
    setSubmitting(s.id);
    try {
      const targetId = submitTarget[s.id] || undefined;
      const { data, error } = await api.POST("/api/runs", {
        body: { scenarioId: s.id, targetId },
      });
      if (error) {
        alert(`Submit failed: ${JSON.stringify(error)}`);
      } else if (data?.id) {
        window.location.href = `/runs/${data.id}`;
      }
    } finally {
      setSubmitting(null);
    }
  };

  if (error) return <p className="text-rose-700">Error: {error}</p>;
  if (!items) return <p>Loading…</p>;
  return (
    <section>
      <h1 className="mb-4 text-xl font-semibold">Scenarios</h1>
      <ul className="grid grid-cols-1 gap-3 md:grid-cols-2 lg:grid-cols-3">
        {items.map((s) => (
          <li key={s.id} className="rounded border border-slate-200 bg-white p-4">
            <div className="font-medium">{s.displayName}</div>
            {s.targetType && <div className="text-xs text-slate-500">{s.targetType}</div>}
            {s.description && <p className="mt-2 text-sm text-slate-700">{s.description}</p>}
            <div className="mt-3 flex items-center gap-2">
              <TargetPicker
                value={submitTarget[s.id] ?? ""}
                onChange={(v) => setSubmitTarget((r) => ({ ...r, [s.id]: v }))}
                filterType={s.targetType ?? undefined}
              />
              <button
                onClick={() => handleRun(s)}
                disabled={submitting === s.id}
                className="rounded bg-emerald-600 px-3 py-1 text-xs font-medium text-white hover:bg-emerald-700"
              >
                {submitting === s.id ? "submitting…" : "Run"}
              </button>
            </div>
          </li>
        ))}
      </ul>
    </section>
  );
}
