// Shared helpers imported by every runner.
// All metrics emitted with `dlh_` prefix to stay distinguishable from k6's
// built-in `k6_*` series. Per-sample dlh_scenario tag is set by buildOptions();
// dlh_workflow tag is added by the load/k6-run WorkflowTemplate's --tag flag.

import { Counter } from 'k6/metrics';

/**
 * Build the k6 options object with env-driven overrides.
 *
 *   scenarioLabel: string — used as the `dlh_scenario` tag
 *   vus: number           — default VUs (overridable via VUS env)
 *   duration: string      — default duration (overridable via DURATION env)
 *
 * Returns a plain object compatible with k6's `export const options = ...`.
 */
export function buildOptions({ scenarioLabel, vus, duration }) {
  return {
    vus: parseInt(__ENV.VUS || String(vus), 10),
    duration: __ENV.DURATION || duration,
    tags: { dlh_scenario: __ENV.SCENARIO_LABEL || scenarioLabel },
  };
}

/** Per-iteration error counter. Use `errCounter.add(1, { kind: '...' })`. */
export const errCounter = new Counter('dlh_app_errors_total');

/**
 * Parse a weighted op-mix spec like "read:70,write:30" into a picker.
 *
 * Returns { pick(): string } that returns one of the op names per call,
 * with frequency proportional to its weight. Weights need not sum to 100.
 *
 * Throws on empty input.
 */
export function parseOpMix(spec) {
  const entries = String(spec).split(',').map((s) => s.trim()).filter(Boolean);
  if (entries.length === 0) {
    throw new Error('parseOpMix: empty spec');
  }
  const parsed = entries.map((e) => {
    const [name, w] = e.split(':');
    return { name: name.trim(), weight: parseFloat(w || '1') };
  });
  const total = parsed.reduce((s, e) => s + e.weight, 0);
  return {
    pick() {
      let r = Math.random() * total;
      for (const e of parsed) {
        if ((r -= e.weight) <= 0) return e.name;
      }
      return parsed[parsed.length - 1].name;
    },
  };
}

/** Seconds since epoch as a float — for ad-hoc duration measurement. */
export function nowSec() {
  return Date.now() / 1000;
}
