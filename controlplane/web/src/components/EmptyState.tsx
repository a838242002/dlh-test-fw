import { type ReactNode } from "react";
import { Inbox } from "lucide-react";

export function EmptyState({ message, hint }: { message: string; hint?: ReactNode }) {
  return (
    <div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-16 text-center">
      <Inbox className="mb-3 h-8 w-8 text-muted-foreground" />
      <p className="font-medium">{message}</p>
      {hint && <p className="mt-1 text-sm text-muted-foreground">{hint}</p>}
    </div>
  );
}
