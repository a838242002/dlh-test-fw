import { useEffect, useState } from "react";
import { toast } from "sonner";
import { api } from "../api/client";
import type { components } from "../api/gen";
import { PageHeader } from "@/components/PageHeader";
import { EmptyState } from "@/components/EmptyState";
import { ErrorState } from "@/components/ErrorState";
import { Card } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

type Target = components["schemas"]["Target"];

export function TargetsPage() {
  const [items, setItems] = useState<Target[] | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [testing, setTesting] = useState<string | null>(null);

  useEffect(() => {
    api.GET("/api/targets", {}).then(({ data, error }) => {
      if (error) setError(error);
      else setItems(data?.items ?? []);
    });
  }, []);

  const testConn = async (id: string) => {
    setTesting(id);
    try {
      const { data, error } = await api.POST("/api/targets/{id}/test", { params: { path: { id } } });
      if (error) {
        toast.error(`Test failed: ${id}`, { description: JSON.stringify(error) });
      } else if (data?.ok) {
        toast.success(`${id} OK (${Math.round((data.latencyNanos ?? 0) / 1_000_000)} ms)`);
      } else {
        toast.error(`${id} unreachable`, { description: data?.error ?? "unknown" });
      }
    } finally {
      setTesting(null);
    }
  };

  if (error) return <ErrorState message="Failed to load targets" details={error} />;

  return (
    <section>
      <PageHeader title="Targets" />
      {!items ? (
        <p className="text-muted-foreground">Loading…</p>
      ) : items.length === 0 ? (
        <EmptyState
          message="No targets registered"
          hint={<>Targets are added by PR — see <code>docs/operations/register-target.md</code>.</>}
        />
      ) : (
        <Card>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>ID</TableHead>
                <TableHead>Display Name</TableHead>
                <TableHead>Namespace</TableHead>
                <TableHead>Allowed Types</TableHead>
                <TableHead>Configured</TableHead>
                <TableHead></TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((t) => (
                <TableRow key={t.id}>
                  <TableCell className="font-medium">{t.id}</TableCell>
                  <TableCell>{t.displayName ?? t.id}</TableCell>
                  <TableCell className="text-muted-foreground">{t.namespace ?? "—"}</TableCell>
                  <TableCell>{(t.allowedTargetTypes ?? []).join(", ") || "—"}</TableCell>
                  <TableCell>
                    {t.configured ? (
                      <Badge className="bg-status-success/15 text-status-success" variant="outline">
                        configured
                      </Badge>
                    ) : (
                      <Badge variant="outline" className="bg-status-failed/15 text-status-failed">
                        missing
                      </Badge>
                    )}
                  </TableCell>
                  <TableCell>
                    <Button variant="outline" size="sm" disabled={testing === t.id} onClick={() => testConn(t.id)}>
                      {testing === t.id ? "Testing…" : "Test"}
                    </Button>
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
