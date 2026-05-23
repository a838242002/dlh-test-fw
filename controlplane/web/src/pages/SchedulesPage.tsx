import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/gen";

type Schedule = components["schemas"]["Schedule"];

export function SchedulesPage() {
  const [items, setItems] = useState<Schedule[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);

  // Inline-create form state.
  const [createOpen, setCreateOpen] = useState(false);
  const [newId, setNewId] = useState("");
  const [newScenario, setNewScenario] = useState("");
  const [newTarget, setNewTarget] = useState("");
  const [newCron, setNewCron] = useState("");
  const [newTimezone, setNewTimezone] = useState("");

  const reload = () =>
    api.GET("/api/schedules", {}).then(({ data, error }) => {
      if (error) setError(JSON.stringify(error));
      else setItems(data?.items ?? []);
    });

  useEffect(() => {
    reload();
  }, []);

  const doPause = async (id: string) => {
    setBusy(id);
    try {
      await api.POST("/api/schedules/{id}/pause", { params: { path: { id } } });
      await reload();
    } finally {
      setBusy(null);
    }
  };
  const doResume = async (id: string) => {
    setBusy(id);
    try {
      await api.POST("/api/schedules/{id}/resume", { params: { path: { id } } });
      await reload();
    } finally {
      setBusy(null);
    }
  };
  const doDelete = async (id: string) => {
    if (!confirm(`Delete schedule "${id}"?`)) return;
    setBusy(id);
    try {
      await api.DELETE("/api/schedules/{id}", { params: { path: { id } } });
      await reload();
    } finally {
      setBusy(null);
    }
  };

  const doCreate = async () => {
    if (!newId || !newScenario || !newCron) {
      alert("id, scenario, cron required");
      return;
    }
    setBusy("__create__");
    try {
      const body: any = { id: newId, scenarioId: newScenario, cron: newCron };
      if (newTarget) body.targetId = newTarget;
      if (newTimezone) body.timezone = newTimezone;
      const { error } = await api.POST("/api/schedules", { body });
      if (error) {
        alert("Failed: " + JSON.stringify(error));
        return;
      }
      setNewId("");
      setNewScenario("");
      setNewTarget("");
      setNewCron("");
      setNewTimezone("");
      setCreateOpen(false);
      await reload();
    } finally {
      setBusy(null);
    }
  };

  if (error) return <p className="text-rose-700">Error: {error}</p>;
  if (!items) return <p>Loading…</p>;
  return (
    <section className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold">Schedules</h1>
        <button
          onClick={() => setCreateOpen(!createOpen)}
          className="rounded bg-emerald-600 px-3 py-1 text-xs font-medium text-white hover:bg-emerald-700"
        >
          {createOpen ? "Cancel" : "+ New schedule"}
        </button>
      </div>

      {createOpen && (
        <div className="rounded border border-slate-200 bg-slate-50 p-3 text-sm">
          <div className="grid grid-cols-2 gap-2 md:grid-cols-3">
            <input
              placeholder="id (e.g. nightly-mysql)"
              value={newId}
              onChange={(e) => setNewId(e.target.value)}
              className="rounded border border-slate-300 bg-white px-2 py-1"
            />
            <input
              placeholder="scenario (e.g. mysql-pod-delete)"
              value={newScenario}
              onChange={(e) => setNewScenario(e.target.value)}
              className="rounded border border-slate-300 bg-white px-2 py-1"
            />
            <input
              placeholder="target (optional)"
              value={newTarget}
              onChange={(e) => setNewTarget(e.target.value)}
              className="rounded border border-slate-300 bg-white px-2 py-1"
            />
            <input
              placeholder="cron (e.g. 0 2 * * *)"
              value={newCron}
              onChange={(e) => setNewCron(e.target.value)}
              className="rounded border border-slate-300 bg-white px-2 py-1"
            />
            <input
              placeholder="timezone (e.g. Asia/Tokyo)"
              value={newTimezone}
              onChange={(e) => setNewTimezone(e.target.value)}
              className="rounded border border-slate-300 bg-white px-2 py-1"
            />
            <button
              onClick={doCreate}
              disabled={busy === "__create__"}
              className="rounded bg-emerald-600 px-3 py-1 text-xs font-medium text-white hover:bg-emerald-700"
            >
              {busy === "__create__" ? "creating…" : "Create"}
            </button>
          </div>
        </div>
      )}

      {items.length === 0 ? (
        <p className="text-slate-600">No schedules yet. Click "+ New schedule".</p>
      ) : (
        <table className="w-full border-collapse text-sm">
          <thead>
            <tr className="border-b border-slate-200 text-left text-slate-600">
              <th className="py-2">ID</th>
              <th>Scenario</th>
              <th>Target</th>
              <th>Cron</th>
              <th>Last Fired</th>
              <th>Active</th>
              <th>Status</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {items.map((s) => (
              <tr key={s.id} className="border-b border-slate-100">
                <td className="py-2 font-mono text-xs">{s.id}</td>
                <td>{s.scenario}</td>
                <td>{s.target ?? "local"}</td>
                <td className="font-mono text-xs">{s.cron}</td>
                <td className="text-xs text-slate-600">
                  {s.lastScheduledAt ? new Date(s.lastScheduledAt).toLocaleString() : "—"}
                </td>
                <td>{s.activeCount ?? 0}</td>
                <td>
                  {s.suspended ? (
                    <span className="text-amber-700">paused</span>
                  ) : (
                    <span className="text-emerald-700">active</span>
                  )}
                </td>
                <td className="space-x-1">
                  {s.suspended ? (
                    <button
                      onClick={() => doResume(s.id)}
                      disabled={busy === s.id}
                      className="rounded border border-slate-300 px-2 py-0.5 text-xs hover:bg-slate-100"
                    >
                      resume
                    </button>
                  ) : (
                    <button
                      onClick={() => doPause(s.id)}
                      disabled={busy === s.id}
                      className="rounded border border-slate-300 px-2 py-0.5 text-xs hover:bg-slate-100"
                    >
                      pause
                    </button>
                  )}
                  <button
                    onClick={() => doDelete(s.id)}
                    disabled={busy === s.id}
                    className="rounded border border-rose-300 px-2 py-0.5 text-xs text-rose-700 hover:bg-rose-50"
                  >
                    delete
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}
