# Scenario Queue + Priority — Design Spec

**Date**: 2026-05-18
**Status**: Draft, awaiting user review
**Project**: dlh-test-fw
**Builds on**: Plan 9 (scenario optimization, `scripts/run-scenario.sh` already forwards extra args to `argo submit`).

## Why

When two operators trigger scenarios simultaneously (`make run-mysql` & `make run-kafka` from different terminals, or back-to-back `make run-mysql` calls), today's behaviour is "both run in parallel" with no admission control. Three real problems:

1. **Same-target overlap is incoherent.** Two `mysql-pod-delete` runs in flight share the mysql target — the second run's k6 sees a mysql that's already being chaos-killed by the first run, so its verdict is meaningless.
2. **Cluster contention.** Minikube has 6 CPUs / 12 GiB. Two parallel chaos+load workflows can starve each other.
3. **No way to express "this one matters more than that one"** when several runs are queued.

The fix needs to be:
- **Per-target serialisation** (mysql doesn't block kafka — they hit different namespaces and runners)
- **Priority-ordered** within a target's queue
- **Declarative**, not a wrapper script that races

## Goals (in scope)

1. Two scenarios targeting the same backend (e.g. two `mysql-*` submissions) serialise. One runs to completion before the next starts.
2. Scenarios targeting different backends run in parallel (`mysql-pod-delete` does not block `kafka-broker-partition`).
3. Each scenario YAML declares a default `spec.priority`. Higher = run first within the same per-target queue.
4. Submit-time override: `scripts/run-scenario.sh scenarios/<name>.yaml --priority 200` re-orders without editing the YAML.
5. `run-scenario.sh` prints a single one-shot status line when the submitted workflow is queued (not running yet). No polling, no log spam.

## Goals (out of scope, deferred)

- Cross-cluster / multi-target queue (a single semaphore for everything) — explicitly rejected in brainstorm.
- Per-chaos-kind queue (e.g. pod-delete vs network-loss share a different lane) — overkill until the catalog grows beyond one scenario per target.
- Queue position display ("position 3 of 4") — informative but adds complexity that isn't worth it for a 3-scenario catalog. Revisit when ≥ 3 scenarios per target exist.
- Streaming workflow state transitions in `run-scenario.sh` — duplicates `kubectl get wf -w`.
- Webhook-driven submission from external systems — no use case yet.
- A separate `scripts/queue-status.sh` showing all pending workflows + their semaphore holders — users can `kubectl get wf -n dlh-test-fw` and read `.status.synchronization` directly.

## Architecture

```
helm/dlh-test-fw/templates/
└── scenario-locks-configmap.yaml    NEW — ConfigMap dlh-scenario-locks
                                            ├ mysql: "1"
                                            ├ kafka: "1"
                                            └ doris: "1"

scenarios/
├── mysql-pod-delete.yaml              + spec.synchronization.semaphore -> mysql key
│                                       + spec.priority: 100
├── kafka-broker-partition.yaml        + spec.synchronization.semaphore -> kafka key
│                                       + spec.priority: 100
└── doris-be-network-loss.yaml         + spec.synchronization.semaphore -> doris key
                                        + spec.priority: 100

scripts/
└── run-scenario.sh                    Modified: 5-line one-shot probe after submit
                                       (no other behavioural change)
```

How it works at runtime:
1. `make run-mysql` (or any path that calls `run-scenario.sh`) submits a Workflow with `spec.synchronization.semaphore.configMapKeyRef = dlh-scenario-locks[mysql]` and `spec.priority = 100` (or whatever `--priority` was passed).
2. Argo controller (v3.5.12) sees the semaphore declaration on admission. If the slot for `mysql` is free, the Workflow transitions Pending → Running immediately. If held, the Workflow stays Pending with `.status.synchronization.semaphore.blocked = [list of holders]`.
3. When two or more Workflows are blocked on the same semaphore key, the controller releases them in (priority desc, creationTimestamp asc) order — this is Argo's documented behaviour since v3.5.
4. `argo submit --wait` does not return until the Workflow reaches a terminal phase (`Succeeded`/`Failed`/`Error`), so the user's terminal waits whether the run is queued or actively running.
5. `run-scenario.sh`'s new probe runs ~2 seconds after `argo submit` returns control (i.e. after the controller has had a chance to annotate the workflow); if it sees `phase: Pending` + `.status.synchronization.semaphore.holding` is empty (i.e. we're blocked, not holding), it prints one line: `Queued: waiting for semaphore <CM>[<key>] (priority N)`. Then it waits silently for the workflow to finish.

## File-by-file

### NEW: `helm/dlh-test-fw/templates/scenario-locks-configmap.yaml`

```yaml
# Per-target scenario semaphore counts. Each key's value is the maximum
# number of concurrent workflows allowed for that target. count=1 means
# strict serialisation; future per-target tuning is a one-line edit.
#
# Scenarios reference this CM via:
#   spec.synchronization.semaphore.configMapKeyRef:
#     name: dlh-scenario-locks
#     key:  mysql          # or kafka, doris, ...
apiVersion: v1
kind: ConfigMap
metadata:
  name: dlh-scenario-locks
  namespace: {{ include "dlh.namespace" . }}
  labels:
    {{- include "dlh.labels" . | nindent 4 }}
data:
  mysql: "1"
  kafka: "1"
  doris: "1"
```

The CM lives in the `dlh-test-fw` namespace alongside `dlh-slos` and the WorkflowTemplates. Helm-managed; raise/lower the counts here when a target gets more capacity.

### MODIFIED: `scenarios/mysql-pod-delete.yaml`

Add two top-level fields under `spec:` (alongside the existing `serviceAccountName`, `entrypoint`, `arguments`). Position: after `serviceAccountName`, before `entrypoint`:

```yaml
spec:
  serviceAccountName: argo-workflow
  priority: 100
  synchronization:
    semaphore:
      configMapKeyRef:
        name: dlh-scenario-locks
        key: mysql
  entrypoint: main
  arguments:
    parameters:
      ...
```

### MODIFIED: `scenarios/kafka-broker-partition.yaml`

Identical pattern with `key: kafka` and `priority: 100`.

### MODIFIED: `scenarios/doris-be-network-loss.yaml`

Identical pattern with `key: doris` and `priority: 100`. (Doris is NO-GO but the spec stays consistent for when it's revived.)

### MODIFIED: `scripts/run-scenario.sh`

Add a one-shot probe between `argo submit --wait` and the existing `status=$(kubectl get workflow ... .status.phase)` line. Drop-in:

```bash
echo "Submitting workflow: $name"
argo submit -n dlh-test-fw "$rendered" "$@" >/dev/null   # no --wait here
# One-shot probe: if Pending and blocked on a semaphore, surface it.
sleep 2
phase=$(kubectl -n dlh-test-fw get workflow "$name" -o jsonpath='{.status.phase}' 2>/dev/null || echo "")
if [[ "$phase" == "Pending" ]]; then
  blocked=$(kubectl -n dlh-test-fw get workflow "$name" \
            -o jsonpath='{.status.synchronization.semaphore.blocked[0]}' 2>/dev/null || echo "")
  if [[ -n "$blocked" ]]; then
    prio=$(kubectl -n dlh-test-fw get workflow "$name" \
           -o jsonpath='{.spec.priority}' 2>/dev/null || echo "default")
    echo "Queued: waiting for semaphore ${blocked} (priority ${prio})"
  fi
fi
# Resume waiting via argo wait (separates submit + wait so the probe sits between).
argo wait -n dlh-test-fw "$name" || true
status=$(kubectl -n dlh-test-fw get workflow "$name" -o jsonpath='{.status.phase}')
echo "Final phase: $status"
echo "Report artifact: argo get -n dlh-test-fw $name  # or:"
echo "                 kubectl -n dlh-test-fw exec deploy/dlh-minio -- mc cat \"local/artifacts/${name}/${name}-main-*/verdict/report.json\" | jq ."
[[ "$status" == "Succeeded" ]]
```

Key change: split `argo submit --wait` into `argo submit` + `argo wait` so the probe can run in between. Other behaviour (timestamped name, `-p key=value` forwarding, `--priority N` forwarding) is unchanged because `"$@"` still passes through.

## How `--priority` flows through

`scripts/run-scenario.sh scenarios/mysql-pod-delete.yaml --priority 200 -p vus=5`:

1. Script rewrites `generateName → name: mysql-pod-delete-YYYYMMDD-HHMMSS`.
2. `argo submit -n dlh-test-fw <rendered> --priority 200 -p vus=5` — Argo CLI's `--priority` overrides the YAML's `spec.priority: 100` to `200`. `-p vus=5` overrides the workflow parameter.
3. Argo controller sees the Workflow with `spec.priority: 200`. If queued, it sits ahead of any `priority: 100` peers in the same per-target lane.

Argo's `--priority` is documented (since v2.5) and confirmed against v3.5.12 (in-cluster) + CLI v4.0.5.

## Testing

| Element | How |
|---|---|
| ConfigMap renders | `helm template dlh helm/dlh-test-fw \| grep -A8 'name: dlh-scenario-locks'` shows three keys. |
| Live CM exists post-upgrade | `kubectl -n dlh-test-fw get cm dlh-scenario-locks -o jsonpath='{.data}' \| jq` returns `{"mysql":"1","kafka":"1","doris":"1"}`. |
| Same-target serialisation | Submit two `mysql-pod-delete` runs back-to-back (one terminal: `make run-mysql`, another terminal immediately after). Confirm the second is `Pending` with `.status.synchronization.semaphore.blocked` populated for the duration of the first. Second starts within seconds of first finishing. |
| Different-target parallelism | Submit `make run-mysql` and `make run-kafka` simultaneously. Both reach `Running` without either blocking the other. `kubectl get wf -w` shows them both in `Running` at the same time. |
| Priority ordering | Submit three `mysql-pod-delete` runs in this order: A (priority 100), B (`--priority 50`), C (`--priority 200`). After A finishes, C starts next (priority 200 wins). B starts last (lowest). |
| Submit-time override | `scripts/run-scenario.sh scenarios/mysql-pod-delete.yaml --priority 200`; verify `kubectl get wf <name> -o jsonpath='{.spec.priority}'` returns `200`. |
| Queued-message UX | While `make run-mysql` is running, in another terminal `scripts/run-scenario.sh scenarios/mysql-pod-delete.yaml`. Verify the second invocation prints `Queued: waiting for semaphore mysql (priority 100)` on the line after `Submitting workflow:`. |
| `argo wait` still gates exit code | Submit a run that fails (e.g. force chaos to a non-existent target). Verify the script exits non-zero. |

## Success criteria

1. `dlh-scenario-locks` ConfigMap exists with three keys (`mysql`, `kafka`, `doris`), all value `"1"`.
2. All three scenario YAMLs declare `spec.synchronization.semaphore` referencing the correct per-target key and `spec.priority: 100`.
3. Same-target serialisation: two concurrent `mysql-pod-delete` runs do NOT overlap (the second is Pending while the first is Running).
4. Different-target parallelism: a concurrent `mysql-pod-delete` + `kafka-broker-partition` BOTH reach Running without contention.
5. Priority override at submit time changes acquisition order (verified via the three-run experiment in the testing table).
6. `scripts/run-scenario.sh` prints `Queued: waiting for semaphore <name> (priority N)` exactly once when the submission is blocked, and is silent when not.
7. No regression: an unblocked `make run-mysql` still completes end-to-end with `Final phase: Succeeded` and `dlh-slo-<wf>` CM rendered correctly (Plan 9 behaviour preserved).

## Risks

- **Argo v3.5 priority semantics.** Argo's documentation says priority-aware semaphore queueing was added in v3.5; v3.4 had FIFO-only. Server is v3.5.12 → covered. If the cluster is ever downgraded to v3.4.x, priority becomes a no-op (still FIFO). Mitigation: pin chart's argo-workflows subchart version to ≥ 3.5 (already at 0.42.7 which ships 3.5.12).
- **`argo submit --priority` precedence.** When both the YAML and the CLI flag specify a priority, the CLI flag wins (verified in CLI v4.0.5). Document in spec; no code change needed.
- **Pending workflows tie up Argo controller queue depth.** Argo workflow-controller defaults to processing up to N workflows per worker. A long queue of Pending workflows shouldn't hurt — they're not consuming pod resources, just controller bookkeeping. Mitigation: monitor `workflow-controller` Pending count if it ever grows past ~20.
- **Sleep-based probe is racy.** The 2-second sleep before reading `.status.synchronization` is a heuristic. If Argo controller is slow to annotate, the probe may see `phase=""` or `phase=Running` even when in reality the workflow is about to be blocked. Worst case: the script stays silent when it could have warned. Not a correctness issue, just degraded UX in pathological lag. Mitigation: bump to `sleep 3` if observed in CI; alternative is a 5-iteration retry loop (rejected as over-engineering).
- **`argo wait` vs `argo submit --wait` exit-code drift.** Plan 9 originally used `argo submit --wait`. We split it into `argo submit` + `argo wait` so the probe can sit between. Both forms produce the same exit code on terminal state in CLI v4.0.5 — verified during Plan 9 baseline. Document; retest in plan task 1.
- **ConfigMap key naming.** `mysql`, `kafka`, `doris` are short and target-aligned. If a future scenario targets a different mysql instance (e.g. `mysql-replica`), the key may need to disambiguate. Out of scope; revisit when it happens.
- **Doris still NO-GO.** The `doris` key is provisioned, but no live verification is possible. Same disposition as Plan 9: the YAML is correct, just not exercised.

## File summary

| Path | Change |
|---|---|
| `helm/dlh-test-fw/templates/scenario-locks-configmap.yaml` | NEW |
| `scenarios/mysql-pod-delete.yaml` | MODIFIED (+ `spec.priority`, `spec.synchronization`) |
| `scenarios/kafka-broker-partition.yaml` | MODIFIED (+ `spec.priority`, `spec.synchronization`) |
| `scenarios/doris-be-network-loss.yaml` | MODIFIED (+ `spec.priority`, `spec.synchronization`) |
| `scripts/run-scenario.sh` | MODIFIED (split submit+wait, add probe) |

No other files. No new make targets. No new images. No RBAC changes — `argo-workflow` SA already has `get configmaps` per Plan 4/5 backfill; reading a workflow's own status is implicit.
