import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { ArrowLeft } from "lucide-react";
import { toast } from "sonner";
import { api } from "../api/client";
import type { components } from "../api/gen";
import { ErrorState } from "@/components/ErrorState";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { TIERS, tierForPriority } from "@/lib/tier";

type SP = components["schemas"]["ScenarioPriority"];

export function DefaultPrioritiesPage() {
  const [items, setItems] = useState<SP[] | null>(null);
  const [error, setError] = useState<unknown>(null);

  const reload = useCallback(() => {
    api.GET("/api/scenario-priorities", {}).then(({ data, error: e }) => {
      if (e) setError(e);
      else { setItems((data?.items ?? []) as SP[]); setError(null); }
    });
  }, []);

  useEffect(() => { reload(); }, [reload]);

  const save = async (scenario: string, priority: number) => {
    const { error: e } = await api.PUT("/api/scenario-priorities/{id}", {
      params: { path: { id: scenario } },
      body: { priority },
    });
    if (e) toast.error("Save failed", { description: JSON.stringify(e) });
    else { toast.success(`${scenario} → priority ${priority}`); reload(); }
  };

  if (error) return <ErrorState message="Failed to load priorities" details={error} />;
  if (!items) return <div className="space-y-4"><Skeleton className="h-8 w-64" /><Skeleton className="h-40 w-full" /></div>;

  return (
    <section className="space-y-5">
      <Link to="/queue" className="inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground">
        <ArrowLeft className="h-4 w-4" /> Queue
      </Link>
      <h1 className="text-lg font-semibold">Default priorities</h1>
      <Card>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Scenario</TableHead>
                <TableHead>Tiers</TableHead>
                <TableHead>Effective</TableHead>
                <TableHead>Status</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((sp) => (
                <PriorityRow key={sp.scenario} sp={sp} onSave={save} />
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </section>
  );
}

function PriorityRow({ sp, onSave }: { sp: SP; onSave: (s: string, p: number) => void }) {
  const [raw, setRaw] = useState(String(sp.effective));
  const overridden = sp.override != null;
  const currentTier = tierForPriority(Number(raw));
  return (
    <TableRow>
      <TableCell className="font-medium">{sp.scenario}</TableCell>
      <TableCell>
        <div className="flex gap-1">
          {TIERS.map((t) => (
            <Button
              key={t.label}
              size="sm"
              variant={currentTier === t.label ? "default" : "outline"}
              onClick={() => { setRaw(String(t.value)); onSave(sp.scenario, t.value); }}
            >
              {t.label}
            </Button>
          ))}
        </div>
      </TableCell>
      <TableCell>
        <div className="flex items-center gap-2">
          <Input
            type="number"
            value={raw}
            onChange={(e) => setRaw(e.target.value)}
            className="h-8 w-[88px] tabular-nums"
          />
          <Button size="sm" variant="ghost" onClick={() => onSave(sp.scenario, Number(raw))}>Save</Button>
        </div>
      </TableCell>
      <TableCell className="text-xs text-muted-foreground">
        {overridden
          ? <span>overridden · baked {sp.baked}</span>
          : <span>= baked default ({sp.baked})</span>}
      </TableCell>
    </TableRow>
  );
}
