import { useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { api } from "../api/client";
import type { components } from "../api/gen";
import { StatusBadge } from "@/components/StatusBadge";
import { ErrorState } from "@/components/ErrorState";
import { VerdictView } from "@/components/VerdictView";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

type RunDetail = components["schemas"]["RunDetail"];

export function RunDetailPage() {
  const { id } = useParams<{ id: string }>();
  const [run, setRun] = useState<RunDetail | null>(null);
  const [liveStatus, setLiveStatus] = useState<string | null>(null);
  const [error, setError] = useState<unknown>(null);

  useEffect(() => {
    if (!id) return;
    api.GET("/api/runs/{id}", { params: { path: { id } } }).then(({ data, error }) => {
      if (error) setError(error);
      else setRun(data as RunDetail);
    });

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

  if (error) return <ErrorState message="Failed to load run" details={error} />;
  if (!run) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-40 w-full" />
      </div>
    );
  }

  const status = liveStatus ?? String(run.status ?? "Unknown");

  return (
    <section className="space-y-6">
      <header className="flex flex-wrap items-center gap-3">
        <h1 className="text-xl font-semibold">{run.id}</h1>
        <StatusBadge status={status} />
        {run.target && <span className="text-xs text-muted-foreground">target: {run.target}</span>}
        {run.triggeredBy?.id && (
          <Link to="/schedules" className="text-xs text-primary hover:underline">
            Triggered by schedule: {run.triggeredBy.id}
          </Link>
        )}
      </header>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Scenario</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-sm text-muted-foreground">{run.scenario}</p>
        </CardContent>
      </Card>

      {run.steps && run.steps.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Steps</CardTitle>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Step</TableHead>
                  <TableHead>Phase</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {run.steps.map((s, i) => (
                  <TableRow key={i}>
                    <TableCell>{s.name}</TableCell>
                    <TableCell className="text-muted-foreground">{s.phase}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Verdict</CardTitle>
        </CardHeader>
        <CardContent>
          <VerdictView verdict={run.verdict} />
        </CardContent>
      </Card>
    </section>
  );
}
