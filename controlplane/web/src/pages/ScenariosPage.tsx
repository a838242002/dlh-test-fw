import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Play, Search } from "lucide-react";
import { toast } from "sonner";
import { api } from "../api/client";
import type { components } from "../api/gen";
import { TargetPicker } from "@/components/TargetPicker";
import { CategoryIcon } from "@/components/CategoryIcon";
import { PageHeader } from "@/components/PageHeader";
import { EmptyState } from "@/components/EmptyState";
import { ErrorState } from "@/components/ErrorState";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { CATEGORIES, deriveCategory, deriveTargetType, type CategoryKey } from "@/lib/category";

type Scenario = components["schemas"]["Scenario"];

export function ScenariosPage() {
  const navigate = useNavigate();
  const [items, setItems] = useState<Scenario[] | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [submitTarget, setSubmitTarget] = useState<Record<string, string>>({});
  const [submitting, setSubmitting] = useState<string | null>(null);
  const [search, setSearch] = useState("");

  useEffect(() => {
    api.GET("/api/scenarios", {}).then(({ data, error }) => {
      if (error) setError(error);
      else setItems(data?.items ?? []);
    });
  }, []);

  const handleRun = async (s: Scenario) => {
    setSubmitting(s.id);
    try {
      const targetId = submitTarget[s.id] || undefined;
      const { data, error } = await api.POST("/api/runs", { body: { scenarioId: s.id, targetId } });
      if (error) toast.error("Submit failed", { description: JSON.stringify(error) });
      else if (data?.id) {
        toast.success(`Run ${data.id} submitted`);
        navigate(`/runs/${data.id}`);
      }
    } finally {
      setSubmitting(null);
    }
  };

  const { grouped, total } = useMemo(() => {
    const q = search.trim().toLowerCase();
    const map = new Map<CategoryKey, Scenario[]>();
    let count = 0;
    for (const s of items ?? []) {
      if (q && !s.id.toLowerCase().includes(q)) continue;
      const key = deriveCategory(s.id);
      const arr = map.get(key) ?? [];
      arr.push(s);
      map.set(key, arr);
      count++;
    }
    return { grouped: map, total: count };
  }, [items, search]);

  if (error) return <ErrorState message="Failed to load scenarios" details={error} />;

  return (
    <section>
      <PageHeader
        title="Scenarios"
        action={
          <div className="relative">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <Input value={search} onChange={(e) => setSearch(e.target.value)} placeholder="Search scenarios…" className="h-8 w-[220px] pl-8" />
          </div>
        }
      />

      {!items ? (
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2 lg:grid-cols-3">
          {Array.from({ length: 6 }).map((_, i) => (
            <Skeleton key={i} className="h-28 w-full" />
          ))}
        </div>
      ) : items.length === 0 ? (
        <EmptyState message="No scenarios available" />
      ) : total === 0 ? (
        <EmptyState message="No matching scenarios" hint="Try a different search." />
      ) : (
        CATEGORIES.map((cat) => {
          const scns = grouped.get(cat.key);
          if (!scns || scns.length === 0) return null;
          return (
            <div key={cat.key} className="mb-6">
              <div className={cn("mb-2 flex items-center gap-2 font-semibold", cat.accent)}>
                <CategoryIcon category={cat.key} />
                {cat.label.toUpperCase()}
                <span className="rounded-full border bg-card px-2 text-xs font-medium text-muted-foreground">{scns.length}</span>
              </div>
              <ul className="grid grid-cols-1 gap-3 md:grid-cols-2 lg:grid-cols-3">
                {scns.map((s) => {
                  const tt = s.targetType ?? deriveTargetType(s.id);
                  return (
                    <li key={s.id}>
                      <Card className="h-full">
                        <CardContent className="flex h-full flex-col gap-3 p-4">
                          <div className="flex items-start gap-3">
                            <div className="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-lg bg-muted">
                              <CategoryIcon category={cat.key} />
                            </div>
                            <div className="min-w-0">
                              <div className="truncate font-semibold">{s.displayName}</div>
                              <div className="text-xs text-muted-foreground">{tt}</div>
                            </div>
                          </div>
                          <div className="mt-auto flex items-center gap-2">
                            <TargetPicker
                              value={submitTarget[s.id] ?? ""}
                              onChange={(v) => setSubmitTarget((r) => ({ ...r, [s.id]: v }))}
                              filterType={s.targetType ?? undefined}
                            />
                            <Button size="sm" disabled={submitting === s.id} onClick={() => handleRun(s)}>
                              <Play className="h-3.5 w-3.5" />
                              {submitting === s.id ? "Submitting…" : "Run"}
                            </Button>
                          </div>
                        </CardContent>
                      </Card>
                    </li>
                  );
                })}
              </ul>
            </div>
          );
        })
      )}
    </section>
  );
}
