import { useCallback, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { api } from "../api/client";
import type { components } from "../api/gen";
import { StatusBadge } from "@/components/StatusBadge";
import { StatCard } from "@/components/StatCard";
import { PageHeader } from "@/components/PageHeader";
import { EmptyState } from "@/components/EmptyState";
import { ErrorState } from "@/components/ErrorState";
import { Card } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { computeStats } from "@/lib/stats";

type Run = components["schemas"]["Run"];
type Schedule = components["schemas"]["Schedule"];

const POLL_MS = 5000;

export function RunsPage() {
  const navigate = useNavigate();
  const [runs, setRuns] = useState<Run[] | null>(null);
  const [schedules, setSchedules] = useState<Schedule[]>([]);
  const [error, setError] = useState<unknown>(null);
  const [secondsAgo, setSecondsAgo] = useState(0);

  const reload = useCallback(() => {
    api.GET("/api/runs", {}).then(({ data: runsData, error: runsError }) => {
      if (runsError) {
        setError(runsError);
        return;
      }
      setRuns(runsData?.items ?? []);
      setSecondsAgo(0);
      setError(null);
    });
    api.GET("/api/schedules", {}).then(({ data: schedData }) => {
      setSchedules(schedData?.items ?? []);
    });
  }, []);

  useEffect(() => {
    reload();
    const poll = setInterval(reload, POLL_MS);
    const tick = setInterval(() => setSecondsAgo((n) => n + 1), 1000);
    return () => {
      clearInterval(poll);
      clearInterval(tick);
    };
  }, [reload]);

  if (error) return <ErrorState message="Failed to load runs" details={error} />;

  const stats = runs ? computeStats(runs, schedules) : null;

  return (
    <section>
      <PageHeader title="Runs" />

      <div className="mb-6 grid grid-cols-2 gap-3 md:grid-cols-4">
        <StatCard
          label="Pass rate (7d)"
          value={stats == null ? "—" : stats.passRate7d == null ? "—" : `${Math.round(stats.passRate7d * 100)}%`}
          accent="success"
        />
        <StatCard label="Runs today" value={stats == null ? "—" : String(stats.runsToday)} />
        <StatCard label="Running now" value={stats == null ? "—" : String(stats.runningNow)} accent="running" />
        <StatCard label="Active schedules" value={stats == null ? "—" : String(stats.activeSchedules)} />
      </div>

      <Card>
        <div className="flex items-center justify-between border-b px-4 py-3">
          <span className="font-medium">Recent runs</span>
          {runs && (
            <span className="text-xs text-muted-foreground">● live · updated {secondsAgo}s ago</span>
          )}
        </div>
        {!runs ? (
          <div className="space-y-2 p-4">
            {Array.from({ length: 4 }).map((_, i) => (
              <Skeleton key={i} className="h-8 w-full" />
            ))}
          </div>
        ) : runs.length === 0 ? (
          <div className="p-4">
            <EmptyState message="No runs yet" hint="Submit a scenario from the Scenarios page." />
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Scenario</TableHead>
                <TableHead>Target</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Started</TableHead>
                <TableHead>Score</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {runs.map((r) => (
                <TableRow
                  key={r.id}
                  className="cursor-pointer"
                  onClick={() => navigate(`/runs/${r.id}`)}
                >
                  <TableCell className="font-medium">{r.scenario}</TableCell>
                  <TableCell className="text-muted-foreground">{r.target || "local"}</TableCell>
                  <TableCell>
                    <StatusBadge status={String(r.status)} />
                  </TableCell>
                  <TableCell className="text-muted-foreground">
                    {new Date(r.startedAt).toLocaleString()}
                  </TableCell>
                  <TableCell>{r.score == null ? "—" : r.score.toFixed(2)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Card>
    </section>
  );
}
