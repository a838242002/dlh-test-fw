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

export type TierKey = "low" | "normal" | "high" | "urgent" | "custom";

// Explicit TierLabel → TierKey map. `satisfies` keeps the type system honest:
// renaming or adding a tier label will fail to compile until this map is
// updated, instead of silently producing an invalid key via a cast.
const TIER_KEY_MAP = {
  Low: "low",
  Normal: "normal",
  High: "high",
  Urgent: "urgent",
} as const satisfies Record<TierLabel, Exclude<TierKey, "custom">>;

/** Returns the bare tier key for an exact-match priority value, else "custom". */
export function tierKeyForPriority(priority: number): TierKey {
  const label = tierForPriority(priority);
  return label ? TIER_KEY_MAP[label] : "custom";
}

/** Returns the human label ("Low" | "Normal" | "High" | "Urgent" | "Custom"). */
export function tierLabelForPriority(priority: number): TierLabel | "Custom" {
  return tierForPriority(priority) ?? "Custom";
}
