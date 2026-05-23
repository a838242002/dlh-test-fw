import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { toast } from "sonner";
import { api } from "../api/client";
import type { components } from "../api/gen";
import { TargetPicker } from "@/components/TargetPicker";
import { PageHeader } from "@/components/PageHeader";
import { EmptyState } from "@/components/EmptyState";
import { ErrorState } from "@/components/ErrorState";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";

type Scenario = components["schemas"]["Scenario"];

export function ScenariosPage() {
  const navigate = useNavigate();
  const [items, setItems] = useState<Scenario[] | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [submitTarget, setSubmitTarget] = useState<Record<string, string>>({});
  const [submitting, setSubmitting] = useState<string | null>(null);

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
      if (error) {
        toast.error("Submit failed", { description: JSON.stringify(error) });
      } else if (data?.id) {
        toast.success(`Run ${data.id} submitted`);
        navigate(`/runs/${data.id}`);
      }
    } finally {
      setSubmitting(null);
    }
  };

  if (error) return <ErrorState message="Failed to load scenarios" details={error} />;

  return (
    <section>
      <PageHeader title="Scenarios" />
      {!items ? (
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2 lg:grid-cols-3">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} className="h-40 w-full" />
          ))}
        </div>
      ) : items.length === 0 ? (
        <EmptyState message="No scenarios available" />
      ) : (
        <ul className="grid grid-cols-1 gap-3 md:grid-cols-2 lg:grid-cols-3">
          {items.map((s) => (
            <li key={s.id}>
              <Card className="flex h-full flex-col">
                <CardHeader>
                  <CardTitle className="text-base">{s.displayName}</CardTitle>
                  {s.targetType && <CardDescription>{s.targetType}</CardDescription>}
                </CardHeader>
                <CardContent className="flex flex-1 flex-col justify-between gap-3">
                  {s.description && <p className="text-sm text-muted-foreground">{s.description}</p>}
                  <div className="flex items-center gap-2">
                    <TargetPicker
                      value={submitTarget[s.id] ?? ""}
                      onChange={(v) => setSubmitTarget((r) => ({ ...r, [s.id]: v }))}
                      filterType={s.targetType ?? undefined}
                    />
                    <Button size="sm" disabled={submitting === s.id} onClick={() => handleRun(s)}>
                      {submitting === s.id ? "Submitting…" : "Run"}
                    </Button>
                  </div>
                </CardContent>
              </Card>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
