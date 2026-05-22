import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/gen";

type Target = components["schemas"]["Target"];

export function TargetsPage() {
  const [items, setItems] = useState<Target[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [testing, setTesting] = useState<string | null>(null);
  const [results, setResults] = useState<Record<string, string>>({});

  const reload = () =>
    api.GET("/api/targets", {}).then(({ data, error }) => {
      if (error) setError(JSON.stringify(error));
      else setItems(data?.items ?? []);
    });

  useEffect(() => {
    reload();
  }, []);

  const testConn = async (id: string) => {
    setTesting(id);
    try {
      const { data, error } = await api.POST("/api/targets/{id}/test", {
        params: { path: { id } },
      });
      if (error) {
        setResults((r) => ({ ...r, [id]: `error: ${JSON.stringify(error)}` }));
      } else {
        setResults((r) => ({
          ...r,
          [id]: data?.ok
            ? `OK (${Math.round((data.latencyNanos ?? 0) / 1_000_000)} ms)`
            : `FAIL: ${data?.error ?? "unknown"}`,
        }));
      }
    } finally {
      setTesting(null);
    }
  };

  if (error) return <p className="text-rose-700">Error: {error}</p>;
  if (!items) return <p>Loading…</p>;
  return (
    <section>
      <h1 className="mb-4 text-xl font-semibold">Targets</h1>
      {items.length === 0 ? (
        <p className="text-slate-600">
          No targets registered. Targets are added by PR — see{" "}
          <code>docs/operations/register-target.md</code>.
        </p>
      ) : (
        <table className="w-full border-collapse text-sm">
          <thead>
            <tr className="border-b border-slate-200 text-left text-slate-600">
              <th className="py-2">ID</th>
              <th>Display Name</th>
              <th>Namespace</th>
              <th>Allowed Types</th>
              <th>Configured</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {items.map((t) => (
              <tr key={t.id} className="border-b border-slate-100">
                <td className="py-2">{t.id}</td>
                <td>{t.displayName ?? t.id}</td>
                <td>{t.namespace ?? "—"}</td>
                <td>{(t.allowedTargetTypes ?? []).join(", ") || "—"}</td>
                <td>
                  {t.configured ? (
                    <span className="text-emerald-700">✓</span>
                  ) : (
                    <span className="text-rose-700">✗</span>
                  )}
                </td>
                <td>
                  <button
                    onClick={() => testConn(t.id)}
                    disabled={testing === t.id}
                    className="rounded border border-slate-300 px-2 py-0.5 text-xs hover:bg-slate-100"
                  >
                    {testing === t.id ? "testing…" : "test"}
                  </button>
                  {results[t.id] && (
                    <span className="ml-2 text-xs text-slate-600">{results[t.id]}</span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}
