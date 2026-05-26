import { useCallback, useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { AlertTriangle, ArrowUpDown, Search } from "lucide-react";
import { api } from "../api/client";
import type { components } from "../api/gen";
import { StatusBadge } from "@/components/StatusBadge";
import { StatPanel, type Stat } from "@/components/StatPanel";
import { TargetGlyph } from "@/components/TargetGlyph";
import { VerdictPill } from "@/components/VerdictPill";
import { PageHeader } from "@/components/PageHeader";
import { EmptyState } from "@/components/EmptyState";
import { ErrorState } from "@/components/ErrorState";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { computeStats } from "@/lib/stats";
import { relativeTime, formatDuration } from "@/lib/time";
import { filterRuns, sortRuns, type RunFilter, type RunSort } from "@/lib/runsFilter";

type Run = components["schemas"]["Run"];
type Schedule = components["schemas"]["Schedule"];

const POLL_MS = 5000;
const ANY = "__any__";

export function RunsPage() {
  const navigate = useNavigate();
  const [runs, setRuns] = useState<Run[] | null>(null);
  const [schedules, setSchedules] = useState<Schedule[]>([]);
  const [error, setError] = useState<unknown>(null);
  const [secondsAgo, setSecondsAgo] = useState(0);

  const [search, setSearch] = useState("");
  const [status, setStatus] = useState("");
  const [category, setCategory] = useState("");
  const [timeRange, setTimeRange] = useState<RunFilter["timeRange"]>("");
  const [failedOnly, setFailedOnly] = useState(false);
  const [sort, setSort] = useState<RunSort>({ key: "started", dir: "desc" });

  const reload = useCallback(() => {
    api.GET("/api/runs", {}).then(({ data, error: e }) => {
      if (e) {
        setError(e);
        return;
      }
      setRuns(data?.items ?? []);
      setSecondsAgo(0);
      setError(null);
    });
    api.GET("/api/schedules", {}).then(({ data }) => setSchedules(data?.items ?? []));
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

  const stats = runs ? computeStats(runs, schedules) : null;

  const visible = useMemo(() => {
    if (!runs) return [];
    return sortRuns(filterRuns(runs, { search, status, category, timeRange, failedOnly }), sort);
  }, [runs, search, status, category, timeRange, failedOnly, sort]);

  if (error) return <ErrorState message="Failed to load runs" details={error} />;

  const toggleSort = (key: RunSort["key"]) =>
    setSort((s) => (s.key === key ? { key, dir: s.dir === "desc" ? "asc" : "desc" } : { key, dir: "desc" }));

  const statItems: Stat[] = [
    {
      label: "Pass rate · 7d",
      value: stats == null ? "—" : stats.passRate7d == null ? "—" : `${Math.round(stats.passRate7d * 100)}%`,
      accent: "success",
    },
    { label: "Runs today", value: stats == null ? "—" : stats.runsToday },
    { label: "Running now", value: stats == null ? "—" : stats.runningNow, accent: "running" },
    { label: "Active schedules", value: stats == null ? "—" : stats.activeSchedules },
  ];

  return (
    <section>
      <PageHeader title="Runs" />

      <div className="mb-5">
        <StatPanel stats={statItems} />
      </div>

      <Card>
        <div className="flex flex-wrap items-center gap-2 border-b px-4 py-3">
          <div className="relative">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <Input value={search} onChange={(e) => setSearch(e.target.value)} placeholder="Search scenario…" className="h-8 w-[200px] pl-8" />
          </div>
          <Select value={status === "" ? ANY : status} onValueChange={(v) => setStatus(v === ANY ? "" : v)}>
            <SelectTrigger className="h-8 w-[140px]"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value={ANY}>Any status</SelectItem>
              <SelectItem value="Succeeded">Succeeded</SelectItem>
              <SelectItem value="Failed">Failed</SelectItem>
              <SelectItem value="Running">Running</SelectItem>
            </SelectContent>
          </Select>
          <Select value={category === "" ? ANY : category} onValueChange={(v) => setCategory(v === ANY ? "" : v)}>
            <SelectTrigger className="h-8 w-[150px]"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value={ANY}>Any category</SelectItem>
              <SelectItem value="chaos">chaos</SelectItem>
              <SelectItem value="fixture">fixture</SelectItem>
              <SelectItem value="load">load</SelectItem>
              <SelectItem value="verdict">verdict</SelectItem>
              <SelectItem value="util">util</SelectItem>
            </SelectContent>
          </Select>
          <Select value={timeRange === "" ? ANY : timeRange} onValueChange={(v) => setTimeRange((v === ANY ? "" : v) as RunFilter["timeRange"])}>
            <SelectTrigger className="h-8 w-[130px]"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value={ANY}>All time</SelectItem>
              <SelectItem value="24h">Last 24h</SelectItem>
              <SelectItem value="7d">Last 7d</SelectItem>
            </SelectContent>
          </Select>
          <Button
            type="button"
            variant="outline"
            size="sm"
            className={failedOnly ? "border-status-failed/40 bg-status-failed/10 text-status-failed" : ""}
            onClick={() => setFailedOnly((f) => !f)}
          >
            <AlertTriangle className="h-3.5 w-3.5" /> Failed only
          </Button>
          {runs && <span className="ml-auto text-xs text-muted-foreground">● live · updated {secondsAgo}s ago</span>}
        </div>

        {!runs ? (
          <div className="space-y-2 p-4">
            {Array.from({ length: 5 }).map((_, i) => (
              <Skeleton key={i} className="h-8 w-full" />
            ))}
          </div>
        ) : visible.length === 0 ? (
          <div className="p-4">
            <EmptyState
              message={runs.length === 0 ? "No runs yet" : "No matching runs"}
              hint={runs.length === 0 ? "Submit a scenario from the Scenarios page." : "Adjust the filters above."}
            />
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Scenario</TableHead>
                <TableHead>Target</TableHead>
                <TableHead>Status</TableHead>
                <TableHead className="text-right">Priority</TableHead>
                <TableHead>
                  <button className="inline-flex items-center gap-1 uppercase" onClick={() => toggleSort("started")}>
                    Started <ArrowUpDown className="h-3 w-3" />
                  </button>
                </TableHead>
                <TableHead className="text-right">
                  <button className="inline-flex items-center gap-1 uppercase" onClick={() => toggleSort("duration")}>
                    Duration <ArrowUpDown className="h-3 w-3" />
                  </button>
                </TableHead>
                <TableHead>Verdict</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {visible.map((r) => (
                <TableRow key={r.id} className="cursor-pointer" onClick={() => navigate(`/runs/${r.id}`)}>
                  <TableCell>
                    <div className="flex items-center gap-2.5">
                      <TargetGlyph scenario={r.scenario} />
                      <span className="font-medium">{r.scenario}</span>
                    </div>
                  </TableCell>
                  <TableCell className="text-muted-foreground">{r.target || "local"}</TableCell>
                  <TableCell><StatusBadge status={String(r.status)} /></TableCell>
                  <TableCell className="text-right font-mono tabular-nums text-muted-foreground">{r.priority ?? "—"}</TableCell>
                  <TableCell className="text-muted-foreground tabular-nums" title={new Date(r.startedAt).toLocaleString()}>
                    {relativeTime(r.startedAt)}
                  </TableCell>
                  <TableCell className="text-right font-mono tabular-nums text-muted-foreground">{formatDuration(r.startedAt, r.finishedAt)}</TableCell>
                  <TableCell><VerdictPill score={r.score} /></TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Card>
    </section>
  );
}
