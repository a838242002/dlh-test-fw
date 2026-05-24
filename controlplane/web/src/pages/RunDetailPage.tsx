import { useEffect, useState, type ReactNode } from "react";
import { Link, useParams } from "react-router-dom";
import { ArrowLeft, CheckCircle2, Circle, ExternalLink, Loader2, XCircle } from "lucide-react";
import { api, getAuthToken } from "../api/client";
import type { components } from "../api/gen";
import { StatusBadge } from "@/components/StatusBadge";
import { CategoryIcon } from "@/components/CategoryIcon";
import { ErrorState } from "@/components/ErrorState";
import { VerdictView } from "@/components/VerdictView";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { relativeTime, formatDuration } from "@/lib/time";
import { deriveCategory } from "@/lib/category";
import { namedSteps, timelineLayout, windowBand } from "@/lib/steps";

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

    const tok = getAuthToken();
    const es = new EventSource(`/api/runs/${id}/events${tok ? `?access_token=${encodeURIComponent(tok)}` : ""}`);
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

      {/* Title row: scenario name + status badge + external links */}
      <div className="flex flex-wrap items-center gap-3">
        <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-muted">
          <CategoryIcon category={deriveCategory(run.scenario)} />
        </div>
        <h1 className="text-lg font-semibold">{run.scenario}</h1>
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

      {/* Run-id subtitle + description */}
      <div className="ml-11 -mt-2 font-mono text-xs text-muted-foreground">{run.id}</div>
      {run.description && <p className="ml-11 max-w-2xl text-sm text-muted-foreground">{run.description}</p>}

      {/* Meta strip */}
      <div className="flex flex-wrap gap-x-10 gap-y-3 rounded-lg border bg-card px-5 py-4">
        <Meta label="Target" value={run.target || "local"} />
        <Meta label="Chaos · SLO" value={run.scenario.includes("-") ? run.scenario.split("-").slice(1).join("-") : "—"} />
        <Meta label="Priority" value={run.priority != null ? String(run.priority) : "—"} />
        <Meta label="Started" value={relativeTime(run.startedAt)} title={new Date(run.startedAt).toLocaleString()} />
        <Meta label="Duration" value={formatDuration(run.startedAt, run.finishedAt)} />
        <Meta label="Triggered by">
          {run.triggeredBy?.id ? (
            <Link to="/schedules" className="text-primary hover:underline">{run.triggeredBy.id}</Link>
          ) : (<span className="text-muted-foreground">manual</span>)}
        </Meta>
      </div>

      <Card>
        <CardHeader><CardTitle className="text-base">Verdict</CardTitle></CardHeader>
        <CardContent><VerdictView verdict={run.verdict} /></CardContent>
      </Card>

      {/* Steps: chronological timeline with chaos-window band + messages */}
      {visibleSteps.length > 0 && (() => {
        const lay = timelineLayout(visibleSteps, run.finishedAt ?? undefined);
        const v = run.verdict as Record<string, unknown> | undefined;
        const chaos =
          v?.chaos_window_start && v?.chaos_window_end
            ? windowBand(lay.startMs, lay.windowMs, Date.parse(v.chaos_window_start as string), Date.parse(v.chaos_window_end as string))
            : null;
        const kindOf = (name: string) =>
          name.includes("chaos") ? "bg-amber-500"
          : name.startsWith("load") || name.includes("testrun") ? "bg-blue-500"
          : name === "verdict" ? "bg-indigo-500"
          : "bg-slate-600";
        return (
          <Card>
            <CardHeader className="flex flex-row items-center justify-between">
              <CardTitle className="text-base">Steps</CardTitle>
              <span className="text-xs text-muted-foreground">{visibleSteps.length} steps · chronological{hidden > 0 ? " · group nodes hidden" : ""}</span>
            </CardHeader>
            <CardContent>
              {chaos && (
                <div className="relative mb-2 ml-[268px] h-4">
                  <div className="absolute inset-y-0 rounded border-x border-dashed border-amber-500/50 bg-amber-500/15 px-1 text-[10px] text-amber-400"
                       style={{ left: `${chaos.offsetPct}%`, width: `${chaos.widthPct}%` }}>chaos window</div>
                </div>
              )}
              <div className="space-y-1.5">
                {visibleSteps.map((s, i) => (
                  <div key={i} className="grid grid-cols-[180px_64px_1fr] items-center gap-3">
                    <span className="flex items-center gap-2 text-sm font-medium"><StepIcon phase={s.phase} />{s.name}</span>
                    <span className="font-mono text-xs text-muted-foreground">{formatDuration(s.startedAt, s.finishedAt)}</span>
                    <span className="relative h-3.5 rounded bg-muted">
                      <span className={`absolute top-0 h-3.5 rounded ${kindOf(s.name)} ${lay.bars[i].running ? "animate-pulse" : ""}`}
                            style={{ left: `${lay.bars[i].offsetPct}%`, width: `${lay.bars[i].widthPct}%` }} />
                    </span>
                  </div>
                ))}
              </div>
              {visibleSteps.some((s) => s.message) && (
                <div className="mt-3 space-y-1">
                  {visibleSteps.filter((s) => s.message).map((s, i) => (
                    <div key={i} className="rounded border border-status-failed/40 bg-status-failed/10 px-2.5 py-1.5 font-mono text-xs text-status-failed">
                      {s.name}: {s.message}
                    </div>
                  ))}
                </div>
              )}
            </CardContent>
          </Card>
        );
      })()}
    </section>
  );
}
