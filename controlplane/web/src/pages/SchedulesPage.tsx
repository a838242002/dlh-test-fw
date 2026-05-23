import { useEffect, useState } from "react";
import { toast } from "sonner";
import { api } from "../api/client";
import type { components } from "../api/gen";
import { PageHeader } from "@/components/PageHeader";
import { EmptyState } from "@/components/EmptyState";
import { ErrorState } from "@/components/ErrorState";
import { relativeTime } from "@/lib/time";
import { Card } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger,
} from "@/components/ui/dialog";
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription,
  AlertDialogFooter, AlertDialogHeader, AlertDialogTitle, AlertDialogTrigger,
} from "@/components/ui/alert-dialog";

type Schedule = components["schemas"]["Schedule"];

export function SchedulesPage() {
  const [items, setItems] = useState<Schedule[] | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [createOpen, setCreateOpen] = useState(false);

  const [newId, setNewId] = useState("");
  const [newScenario, setNewScenario] = useState("");
  const [newTarget, setNewTarget] = useState("");
  const [newCron, setNewCron] = useState("");
  const [newTimezone, setNewTimezone] = useState("");

  const reload = () =>
    api.GET("/api/schedules", {}).then(({ data, error }) => {
      if (error) setError(error);
      else setItems(data?.items ?? []);
    });

  useEffect(() => {
    reload();
  }, []);

  const doPause = async (id: string) => {
    setBusy(id);
    try {
      await api.POST("/api/schedules/{id}/pause", { params: { path: { id } } });
      toast.success(`Paused ${id}`);
      await reload();
    } finally {
      setBusy(null);
    }
  };
  const doResume = async (id: string) => {
    setBusy(id);
    try {
      await api.POST("/api/schedules/{id}/resume", { params: { path: { id } } });
      toast.success(`Resumed ${id}`);
      await reload();
    } finally {
      setBusy(null);
    }
  };
  const doDelete = async (id: string) => {
    setBusy(id);
    try {
      await api.DELETE("/api/schedules/{id}", { params: { path: { id } } });
      toast.success(`Deleted ${id}`);
      await reload();
    } finally {
      setBusy(null);
    }
  };

  const doCreate = async () => {
    if (!newId || !newScenario || !newCron) {
      toast.error("id, scenario, and cron are required");
      return;
    }
    setBusy("__create__");
    try {
      const body: components["schemas"]["CreateScheduleRequest"] = {
        id: newId,
        scenarioId: newScenario,
        cron: newCron,
        ...(newTarget ? { targetId: newTarget } : {}),
        ...(newTimezone ? { timezone: newTimezone } : {}),
      };
      const { error } = await api.POST("/api/schedules", { body });
      if (error) {
        toast.error("Create failed", { description: JSON.stringify(error) });
        return;
      }
      toast.success(`Created ${newId}`);
      setNewId(""); setNewScenario(""); setNewTarget(""); setNewCron(""); setNewTimezone("");
      setCreateOpen(false);
      await reload();
    } finally {
      setBusy(null);
    }
  };

  if (error) return <ErrorState message="Failed to load schedules" details={error} />;

  const createButton = (
    <Dialog open={createOpen} onOpenChange={setCreateOpen}>
      <DialogTrigger asChild>
        <Button size="sm">+ New schedule</Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New schedule</DialogTitle>
          <DialogDescription>Create a recurring CronWorkflow for a scenario.</DialogDescription>
        </DialogHeader>
        <div className="grid gap-2">
          <Input placeholder="id (e.g. nightly-mysql)" value={newId} onChange={(e) => setNewId(e.target.value)} />
          <Input placeholder="scenario (e.g. mysql-pod-delete)" value={newScenario} onChange={(e) => setNewScenario(e.target.value)} />
          <Input placeholder="target (optional)" value={newTarget} onChange={(e) => setNewTarget(e.target.value)} />
          <Input placeholder="cron (e.g. 0 2 * * *)" value={newCron} onChange={(e) => setNewCron(e.target.value)} />
          <Input placeholder="timezone (e.g. Asia/Tokyo)" value={newTimezone} onChange={(e) => setNewTimezone(e.target.value)} />
        </div>
        <DialogFooter>
          <Button disabled={busy === "__create__"} onClick={doCreate}>
            {busy === "__create__" ? "Creating…" : "Create"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );

  return (
    <section>
      <PageHeader title="Schedules" action={createButton} />
      {!items ? (
        <p className="text-muted-foreground">Loading…</p>
      ) : items.length === 0 ? (
        <EmptyState message="No schedules yet" hint='Click "+ New schedule" to create one.' />
      ) : (
        <Card>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>ID</TableHead>
                <TableHead>Scenario</TableHead>
                <TableHead>Target</TableHead>
                <TableHead>Cron</TableHead>
                <TableHead>Last Fired</TableHead>
                <TableHead>Active</TableHead>
                <TableHead>Status</TableHead>
                <TableHead></TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((s) => (
                <TableRow key={s.id}>
                  <TableCell className="font-mono text-xs">{s.id}</TableCell>
                  <TableCell>{s.scenario}</TableCell>
                  <TableCell>{s.target ?? "local"}</TableCell>
                  <TableCell className="font-mono text-xs">{s.cron}</TableCell>
                  <TableCell className="text-xs text-muted-foreground" title={s.lastScheduledAt ? new Date(s.lastScheduledAt).toLocaleString() : undefined}>
                    {s.lastScheduledAt ? relativeTime(s.lastScheduledAt) : "—"}
                  </TableCell>
                  <TableCell>{s.activeCount ?? 0}</TableCell>
                  <TableCell>
                    {s.suspended ? (
                      <Badge variant="outline" className="bg-status-pending/15 text-status-pending">paused</Badge>
                    ) : (
                      <Badge variant="outline" className="bg-status-success/15 text-status-success">active</Badge>
                    )}
                  </TableCell>
                  <TableCell className="space-x-1 whitespace-nowrap">
                    {s.suspended ? (
                      <Button variant="outline" size="sm" disabled={busy === s.id} onClick={() => doResume(s.id)}>
                        resume
                      </Button>
                    ) : (
                      <Button variant="outline" size="sm" disabled={busy === s.id} onClick={() => doPause(s.id)}>
                        pause
                      </Button>
                    )}
                    <AlertDialog>
                      <AlertDialogTrigger asChild>
                        <Button variant="outline" size="sm" disabled={busy === s.id} className="text-destructive">
                          delete
                        </Button>
                      </AlertDialogTrigger>
                      <AlertDialogContent>
                        <AlertDialogHeader>
                          <AlertDialogTitle>Delete schedule "{s.id}"?</AlertDialogTitle>
                          <AlertDialogDescription>
                            This removes the CronWorkflow. In-flight runs are not affected.
                          </AlertDialogDescription>
                        </AlertDialogHeader>
                        <AlertDialogFooter>
                          <AlertDialogCancel>Cancel</AlertDialogCancel>
                          <AlertDialogAction onClick={() => doDelete(s.id)}>Delete</AlertDialogAction>
                        </AlertDialogFooter>
                      </AlertDialogContent>
                    </AlertDialog>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Card>
      )}
    </section>
  );
}
