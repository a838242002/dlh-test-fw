export type Verdict = "pass" | "fail" | null;

/** Run.score is 1.0 (PASS), 0.0 (FAIL), or null (no verdict report). */
export function verdictFromScore(score: number | null | undefined): Verdict {
  if (score == null) return null;
  return score >= 1 ? "pass" : "fail";
}
