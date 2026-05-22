import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/gen";

type Target = components["schemas"]["Target"];

export function TargetPicker({
  value,
  onChange,
  filterType,
}: {
  value: string;
  onChange: (id: string) => void;
  filterType?: string;
}) {
  const [items, setItems] = useState<Target[] | null>(null);

  useEffect(() => {
    api.GET("/api/targets", {}).then(({ data }) => {
      setItems(data?.items ?? []);
    });
  }, []);

  if (!items) return <span className="text-xs text-slate-500">loading targets…</span>;
  const filtered = filterType
    ? items.filter(
        (t) =>
          !t.allowedTargetTypes?.length || t.allowedTargetTypes.includes(filterType)
      )
    : items;

  return (
    <select
      value={value}
      onChange={(e) => onChange(e.target.value)}
      className="rounded border border-slate-300 bg-white px-2 py-1 text-sm"
    >
      <option value="">(local — framework cluster)</option>
      {filtered
        .filter((t) => t.configured)
        .map((t) => (
          <option key={t.id} value={t.id}>
            {t.displayName ?? t.id}
          </option>
        ))}
    </select>
  );
}
