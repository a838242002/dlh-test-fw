import { AlertTriangle } from "lucide-react";

export function ErrorState({ message, details }: { message: string; details?: unknown }) {
  const detailText =
    details == null ? "" : typeof details === "string" ? details : JSON.stringify(details, null, 2);
  return (
    <div className="rounded-lg border border-destructive/40 bg-destructive/10 p-4">
      <div className="flex items-center gap-2 font-medium text-destructive">
        <AlertTriangle className="h-4 w-4" />
        {message}
      </div>
      {detailText && (
        <details className="mt-2 text-sm text-muted-foreground">
          <summary className="cursor-pointer">Details</summary>
          <pre className="mt-2 overflow-auto whitespace-pre-wrap text-xs">{detailText}</pre>
        </details>
      )}
    </div>
  );
}
