import { useEffect, useState, type ReactNode } from "react";
import { Link, useParams } from "react-router-dom";
import { ArrowLeft, CheckCircle2, Circle, ExternalLink, Loader2, XCircle } from "lucide-react";
import { api } from "../api/client";
import type { components } from "../api/gen";
import { StatusBadge } from "@/components/StatusBadge";
import { CategoryIcon } from "@/components/CategoryIcon";
import { ErrorState } from "@/components/ErrorState";
import { VerdictView } from "@/components/VerdictView";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { relativeTime, formatDuration } from "@/lib/time";
import { deriveCategory } from "@/lib/category";
import { namedSteps } from "@/lib/steps";

type RunDetail = components["schemas"]["RunDetail"];

function StepIcon({ phase }: { phase: string }) {
  if (phase === "Succeeded") return <CheckCircle2 className="h-4 w-4 text-status-success" />;
  if (phase === "Failed" || phase === "Error") return <XCircle className="h-4 w-4 text-status-failed" />;
  if (phase === "Running") return <Loader2 className="h-4 w-4 text-status-running" />;
  return <Circle className="h-4 w-4 text-status-pending" />;
}

function Meta({ label, value, title, children }: { label: string; value?: string; title?: string; children?: ReactNode }) {
  return (
    <div>
      <div className="text-xs uppercase tracking-wide text-muted-foreground">{label}</div>
      <div className="font-medium" title={title}>{children ?? value}</div>
    </div>
  );
}

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
        const d = JSON.parse(e.data);
        if (d.phase) setLiveStatus(d.phase);
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
  const allSteps = run.steps ?? [];
  const visibleSteps = namedSteps(allSteps, run.id);
  const hidden = allSteps.length - visibleSteps.length;

  return (
    <section className="space-y-5">
      <Link to="/runs" className="inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground">
        <ArrowLeft className="h-4 w-4" /> Runs
      </Link>

      <div className="flex flex-wrap items-center gap-3">
        <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-muted">
          <CategoryIcon category={deriveCategory(run.scenario)} />
        </div>
        <h1 className="font-mono text-lg font-semibold">{run.id}</h1>
        <StatusBadge status={status} />
        {(run.argoUrl || (run.grafanaUrls && run.grafanaUrls.length > 0)) && (
          <div className="ml-auto flex flex-wrap items-center gap-2">
            {run.argoUrl && (
              <a href={run.argoUrl} target="_blank" rel="noreferrer"
                className="inline-flex h-8 items-center gap-1.5 rounded-md border border-input bg-background px-3 text-xs font-medium hover:bg-accent hover:text-accent-foreground">
                <ExternalLink className="h-3.5 w-3.5" /> Argo
              </a>
            )}
            {(run.grafanaUrls ?? []).map((g) => (
              <a key={g.url} href={g.url} target="_blank" rel="noreferrer"
                className="inline-flex h-8 items-center gap-1.5 rounded-md border border-input bg-background px-3 text-xs font-medium hover:bg-accent hover:text-accent-foreground">
                <ExternalLink className="h-3.5 w-3.5" /> {g.label}
              </a>
            ))}
          </div>
        )}
      </div>

      <div className="flex flex-wrap gap-x-10 gap-y-3 rounded-lg border bg-card px-5 py-4">
        <Meta label="Scenario" value={run.scenario} />
        <Meta label="Target" value={run.target || "local"} />
        <Meta label="Started" value={relativeTime(run.startedAt)} title={new Date(run.startedAt).toLocaleString()} />
        <Meta label="Duration" value={formatDuration(run.startedAt, run.finishedAt)} />
        <Meta label="Triggered by">
          {run.triggeredBy?.id ? (
            <Link to="/schedules" className="text-primary hover:underline">{run.triggeredBy.id}</Link>
          ) : (
            <span className="text-muted-foreground">manual</span>
          )}
        </Meta>
      </div>

      <Card>
        <CardHeader><CardTitle className="text-base">Verdict</CardTitle></CardHeader>
        <CardContent><VerdictView verdict={run.verdict} /></CardContent>
      </Card>

      {visibleSteps.length > 0 && (
        <Card>
          <CardHeader className="flex flex-row items-center justify-between">
            <CardTitle className="text-base">Steps</CardTitle>
            <span className="text-xs text-muted-foreground">
              {visibleSteps.length} steps{hidden > 0 ? " · group nodes hidden" : ""}
            </span>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Step</TableHead>
                  <TableHead>Phase</TableHead>
                  <TableHead>Duration</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {visibleSteps.map((s, i) => (
                  <TableRow key={i}>
                    <TableCell className="flex items-center gap-2 font-medium">
                      <StepIcon phase={s.phase} />
                      {s.name}
                    </TableCell>
                    <TableCell className="text-muted-foreground">{s.phase}</TableCell>
                    <TableCell className="text-muted-foreground">{formatDuration(s.startedAt, s.finishedAt)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}
    </section>
  );
}
