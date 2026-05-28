import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { ArrowLeft } from "lucide-react";
import { toast } from "sonner";
import { api } from "../api/client";
import type { components } from "../api/gen";
import { ErrorState } from "@/components/ErrorState";
import { PageHeader } from "@/components/PageHeader";
import { PriorityChip } from "@/components/PriorityChip";
import { SegmentedTiers } from "@/components/SegmentedTiers";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

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
      <PageHeader
        title="Default priorities"
        subtitle="Set the baked default each scenario uses when no priority is given on submit."
        action={
          <Link to="/queue" className="inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground">
            <ArrowLeft className="h-4 w-4" /> Queue
          </Link>
        }
      />
      <Card>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Scenario</TableHead>
                <TableHead>Priority</TableHead>
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
  const [customOpen, setCustomOpen] = useState(false);
  const [customDraft, setCustomDraft] = useState(String(sp.effective));
  const overridden = sp.override != null;

  const commitCustom = () => {
    const n = Number(customDraft);
    if (Number.isFinite(n) && customDraft.trim() !== "") {
      onSave(sp.scenario, n);
    }
    setCustomOpen(false);
  };

  return (
    <TableRow>
      <TableCell className="font-medium">{sp.scenario}</TableCell>
      <TableCell>
        <div className="flex items-center gap-3">
          <SegmentedTiers value={sp.effective} onPick={(p) => onSave(sp.scenario, p)} />
          {!customOpen ? (
            <button
              type="button"
              onClick={() => { setCustomDraft(String(sp.effective)); setCustomOpen(true); }}
              className="text-[11px] text-muted-foreground hover:text-foreground hover:underline"
            >
              Custom…
            </button>
          ) : (
            <input
              autoFocus
              type="number"
              inputMode="numeric"
              value={customDraft}
              onChange={(e) => setCustomDraft(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") { e.preventDefault(); commitCustom(); }
                if (e.key === "Escape") { e.preventDefault(); setCustomOpen(false); }
              }}
              onBlur={() => setCustomOpen(false)}
              placeholder="int"
              className="h-7 w-24 rounded border border-border bg-background px-2 text-xs tabular-nums outline-none focus:ring-1 focus:ring-ring"
            />
          )}
        </div>
      </TableCell>
      <TableCell>
        <div className="flex items-center gap-2">
          <PriorityChip priority={sp.effective} />
          <span className="text-xs text-muted-foreground">
            {overridden ? `overridden · baked ${sp.baked}` : "= baked default"}
          </span>
        </div>
      </TableCell>
    </TableRow>
  );
}
