import { CheckCircle2, XCircle } from "lucide-react";
import { parseVerdict } from "@/lib/verdict";
import { cn } from "@/lib/utils";
import { formatMetricValue } from "@/lib/format";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

export function VerdictView({ verdict }: { verdict: Record<string, unknown> | null | undefined }) {
  const parsed = parseVerdict(verdict);
  if (!parsed) {
    return <p className="text-sm text-muted-foreground">No verdict report yet.</p>;
  }
  return (
    <div className="space-y-4">
      <div
        className={cn(
          "flex items-center gap-2 rounded-lg border p-3 font-medium",
          parsed.overall
            ? "border-status-success/40 bg-status-success/10 text-status-success"
            : "border-status-failed/40 bg-status-failed/10 text-status-failed"
        )}
      >
        {parsed.overall ? <CheckCircle2 className="h-5 w-5" /> : <XCircle className="h-5 w-5" />}
        {parsed.overall ? "PASS" : "FAIL"}
      </div>

      {parsed.thresholds.length > 0 && (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Metric</TableHead>
              <TableHead>Value</TableHead>
              <TableHead>Bound</TableHead>
              <TableHead>Result</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {parsed.thresholds.map((t) => (
              <TableRow key={t.metric}>
                <TableCell className="font-medium">{t.metric}</TableCell>
                <TableCell className="font-mono text-xs">{formatMetricValue(t.value)}</TableCell>
                <TableCell className="font-mono text-xs">{t.bound}</TableCell>
                <TableCell className={t.passed ? "text-status-success" : "text-status-failed"}>
                  {t.passed ? "pass" : "fail"}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      {parsed.rawPromQL && (
        <div className="text-sm">
          <span className="text-muted-foreground">Raw PromQL: </span>
          <code className="font-mono text-xs">{parsed.rawPromQL.query}</code>{" "}
          <span className="font-mono text-xs text-muted-foreground">= {formatMetricValue(parsed.rawPromQL.value)}</span>{" "}
          <span className={parsed.rawPromQL.passed ? "text-status-success" : "text-status-failed"}>
            ({parsed.rawPromQL.passed ? "pass" : "fail"})
          </span>
        </div>
      )}

      <details className="text-sm text-muted-foreground">
        <summary className="cursor-pointer">View raw JSON</summary>
        <pre className="mt-2 overflow-auto rounded border bg-muted/40 p-3 text-xs">
          {JSON.stringify(verdict, null, 2)}
        </pre>
      </details>
    </div>
  );
}
