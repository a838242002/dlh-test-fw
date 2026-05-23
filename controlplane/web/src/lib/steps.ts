/** True for Argo DAG/step-group placeholder nodes named like "[0]", "[12]". */
export function isGroupNode(name: string): boolean {
  return /^\[\d+\]$/.test(name.trim());
}

/** Keep only real named steps: drop bracketed group nodes and (optionally) the workflow root node. */
export function namedSteps<T extends { name: string }>(steps: T[], rootName?: string): T[] {
  return steps.filter((s) => !isGroupNode(s.name) && s.name !== rootName);
}
