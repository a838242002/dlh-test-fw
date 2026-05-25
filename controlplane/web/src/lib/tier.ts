// Named priority tiers — pure UI sugar over the underlying integer.
// The raw int is always authoritative; tiers are exact-match labels.
export const TIERS = [
  { label: "Low", value: 10 },
  { label: "Normal", value: 100 },
  { label: "High", value: 200 },
  { label: "Urgent", value: 500 },
] as const;

export type TierLabel = (typeof TIERS)[number]["label"];

/** Returns the tier label whose value exactly equals priority, else null. */
export function tierForPriority(priority: number): TierLabel | null {
  const t = TIERS.find((t) => t.value === priority);
  return t ? t.label : null;
}

/** Returns the priority value for a tier label, else null. */
export function priorityForTier(label: string): number | null {
  const t = TIERS.find((t) => t.label === label);
  return t ? t.value : null;
}
