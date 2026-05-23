import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/gen";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";

type Target = components["schemas"]["Target"];

const LOCAL_VALUE = "__local__";

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
    api.GET("/api/targets", {}).then(({ data }) => setItems(data?.items ?? []));
  }, []);

  const filtered = (items ?? [])
    .filter((t) => t.configured)
    .filter(
      (t) => !filterType || !t.allowedTargetTypes?.length || t.allowedTargetTypes.includes(filterType)
    );

  return (
    <Select
      value={value === "" ? LOCAL_VALUE : value}
      onValueChange={(v) => onChange(v === LOCAL_VALUE ? "" : v)}
    >
      <SelectTrigger className="h-8 w-[200px] text-xs">
        <SelectValue placeholder="Select target" />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value={LOCAL_VALUE}>local — framework cluster</SelectItem>
        {filtered.map((t) => (
          <SelectItem key={t.id} value={t.id}>
            {t.displayName ?? t.id}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
