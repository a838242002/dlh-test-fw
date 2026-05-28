import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { Settings, X } from "lucide-react";
import { toast } from "sonner";
import { api } from "../api/client";
import type { components } from "../api/gen";
import { PageHeader } from "@/components/PageHeader";
import { InfoBand, Term } from "@/components/InfoBand";
import { ErrorState } from "@/components/ErrorState";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { relativeTime } from "@/lib/time";
import { PriorityChip } from "@/components/PriorityChip";
import { PriorityChipMenu } from "@/components/PriorityChipMenu";

type Queue = components["schemas"]["Queue"];
type Lane = components["schemas"]["QueueLane"];

const POLL_MS = 5000;

export function QueuePage() {
  const [queue, setQueue] = useState<Queue | null>(null);
  const [error, setError] = useState<unknown>(null);

  const reload = useCallback(() => {
    api.GET("/api/queue", {}).then(({ data, error: e }) => {
      if (e) setError(e);
      else { setQueue(data as Queue); setError(null); }
    });
  }, []);

  useEffect(() => {
    reload();
    const poll = setInterval(reload, POLL_MS);
    return () => clearInterval(poll);
  }, [reload]);

  const reprioritize = async (id: string, priority: number) => {
    const { error: e } = await api.POST("/api/runs/{id}/priority", {
      params: { path: { id } }, body: { priority },
    });
    if (e) toast.error("Reprioritize failed", { description: JSON.stringify(e) });
    else { toast.success(`Priority set to ${priority}`); reload(); }
  };
  const cancel = async (id: string) => {
    const { error: e } = await api.DELETE("/api/runs/{id}", { params: { path: { id } } });
    if (e) toast.error("Cancel failed", { description: JSON.stringify(e) });
    else { toast.success("Cancelled"); reload(); }
  };

  if (error) return <ErrorState message="Failed to load queue" details={error} />;
  if (!queue) return <div className="space-y-4"><Skeleton className="h-8 w-48" /><Skeleton className="h-40 w-full" /></div>;

  return (
    <section className="space-y-5">
      <PageHeader
        title="Queue"
        action={
          <Link to="/admin/priorities" className="inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground">
            <Settings className="h-4 w-4" /> Default priorities
          </Link>
        }
      />
      <InfoBand>
        <Term>1 slot</Term> per target type · releases by <Term>priority</Term> (high→low, then oldest) · types run <Term>in parallel</Term>
      </InfoBand>
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        {queue.lanes.map((lane) => (
          <LaneCard key={lane.key} lane={lane} onReprioritize={reprioritize} onCancel={cancel} />
        ))}
      </div>
    </section>
  );
}

function LaneCard({ lane, onReprioritize, onCancel }: { lane: Lane; onReprioritize: (id: string, priority: number) => void; onCancel: (id: string) => void }) {
  const idle = lane.running.length === 0 && lane.pending.length === 0;
  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between">
        <CardTitle className="text-base capitalize">{lane.key}</CardTitle>
        <span className={cn(
          "rounded-full px-2 py-0.5 text-xs tabular-nums",
          lane.running.length > 0 ? "bg-status-running/15 text-status-running" : "bg-muted text-muted-foreground"
        )}>{lane.running.length}/{lane.slots} slot</span>
      </CardHeader>
      <CardContent className="space-y-3">
        {idle && <div className="rounded-md border border-dashed py-6 text-center text-sm text-muted-foreground">Idle</div>}
        {lane.running.length > 0 && (
          <div>
            <div className="mb-1 text-xs font-medium uppercase tracking-wide text-muted-foreground">Running</div>
            {lane.running.map((e) => (
              <div key={e.id} className="flex items-center justify-between rounded-md bg-status-running/10 px-2.5 py-1.5 text-sm">
                <span className="flex items-center gap-2">
                  <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-status-running" />
                  <Link to={`/runs/${e.id}`} className="hover:underline">{e.scenario}</Link>
                </span>
                <span className="flex items-center gap-2 text-xs text-muted-foreground">
                  <PriorityChip priority={e.priority ?? null} />
                  <span>· {relativeTime(e.submittedAt)}</span>
                </span>
              </div>
            ))}
          </div>
        )}
        {lane.pending.length > 0 && (
          <div>
            <div className="mb-1 text-xs font-medium uppercase tracking-wide text-muted-foreground">Queued · release order</div>
            <div className="space-y-1">
              {lane.pending.map((e, i) => (
                <div key={e.id} className="flex items-center justify-between rounded-md border px-2.5 py-1.5 text-sm">
                  <span className="flex items-center gap-2">
                    <span className="font-mono text-xs text-muted-foreground">#{i + 1}</span>
                    {i === 0 && <span className="rounded bg-primary/15 px-1.5 py-0.5 text-[10px] font-semibold text-primary">NEXT</span>}
                    <Link to={`/runs/${e.id}`} className="hover:underline">{e.scenario}</Link>
                  </span>
                  <span className="flex items-center gap-2 text-xs text-muted-foreground">
                    <span title={new Date(e.submittedAt).toLocaleString()}>{relativeTime(e.submittedAt)}</span>
                    <PriorityChipMenu value={e.priority ?? null} onChange={(p) => onReprioritize(e.id, p)} align="end" />
                    <Button size="sm" variant="ghost" title="Cancel" onClick={() => onCancel(e.id)}>
                      <X className="h-3.5 w-3.5" />
                    </Button>
                  </span>
                </div>
              ))}
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
