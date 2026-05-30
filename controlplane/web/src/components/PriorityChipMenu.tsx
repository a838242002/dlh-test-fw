import { useState } from "react";
import { ChevronDown } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { PriorityChip } from "./PriorityChip";
import { TIERS } from "@/lib/tier";

/**
 * Clickable priority chip with a tier menu. Picking a tier fires onChange with
 * the tier's integer value. The "Custom…" item swaps the menu body to an
 * inline number input (Enter commits, Escape cancels) so off-tier values stay
 * possible without a separate modal.
 *
 * When `disabled`, renders a read-only PriorityChip (no caret, no handler).
 * Used on the Queue page's RUNNING row, where the backend rejects reprioritize
 * on non-pending workflows.
 */
export function PriorityChipMenu({
  value,
  onChange,
  align = "start",
  disabled = false,
}: {
  value: number | null;
  onChange: (priority: number) => void;
  align?: "start" | "end";
  disabled?: boolean;
}) {
  const [customOpen, setCustomOpen] = useState(false);
  const [customDraft, setCustomDraft] = useState("");

  if (disabled) {
    return <PriorityChip priority={value} />;
  }

  const commitCustom = () => {
    const n = Number(customDraft);
    if (Number.isFinite(n) && customDraft.trim() !== "") {
      onChange(n);
    }
    setCustomOpen(false);
    setCustomDraft("");
  };

  return (
    <DropdownMenu onOpenChange={(open) => { if (!open) { setCustomOpen(false); setCustomDraft(""); } }}>
      <DropdownMenuTrigger className="inline-flex items-center gap-0.5 outline-none focus-visible:ring-2 focus-visible:ring-ring rounded-full">
        <PriorityChip priority={value} />
        <ChevronDown className="h-3 w-3 opacity-60" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align={align} className="min-w-[176px]">
        {TIERS.map((t) => (
          <DropdownMenuItem
            key={t.label}
            onSelect={() => { onChange(t.value); }}
            className="flex items-center justify-between gap-3"
          >
            <span>{t.label}</span>
            <PriorityChip priority={t.value} />
          </DropdownMenuItem>
        ))}
        <DropdownMenuSeparator />
        {!customOpen ? (
          <DropdownMenuItem
            onSelect={(e) => { e.preventDefault(); setCustomOpen(true); }}
            className="text-muted-foreground"
          >
            Custom…
          </DropdownMenuItem>
        ) : (
          <div className="flex items-center gap-1 p-1">
            <input
              autoFocus
              type="number"
              inputMode="numeric"
              step={1}
              value={customDraft}
              onChange={(e) => setCustomDraft(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") { e.preventDefault(); commitCustom(); }
                if (e.key === "Escape") { e.preventDefault(); setCustomOpen(false); setCustomDraft(""); }
              }}
              placeholder="int"
              className="h-7 w-24 rounded border border-border bg-background px-2 text-xs tabular-nums outline-none focus:ring-1 focus:ring-ring"
            />
          </div>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
