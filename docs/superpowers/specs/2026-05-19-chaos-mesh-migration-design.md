# Chaos Mesh Migration — Design Spec

**Date**: 2026-05-19
**Status**: Draft, awaiting user review
**Project**: dlh-test-fw
**Supersedes**: implicit "remove Litmus ChaosCenter portal" idea (folded into this migration).

## Why

Three independent pressures point at the same fix:

1. **Operational baggage.** Litmus 3.x chart ships only the portal; we backfill chaos-operator in-tree. The portal needs MongoDB; Bitnami's 2025 secure-images migration broke MongoDB on arm64; we ship an in-tree replacement (`templates/mongodb.yaml`). The whole stack — Litmus + MongoDB + Bitnami + in-tree backfills — is ~5 pods, ~600 LOC of helm/values/templates, and a documented ongoing burden in FINDINGS.md.
2. **Capability ceiling.** Litmus hub's experiment catalog is limited. Future Phase 4+ scenarios (IO latency, clock skew, JVM faults, DNS errors, HTTP-layer faults) need either custom Litmus experiments or a swap to a richer primitive base.
3. **Ecosystem health.** Litmus 3.x has had rocky chart releases. Chaos Mesh (CNCF Incubating) has a single official chart, stable release cadence, and first-class Argo Workflows integration docs.

Chaos Mesh swap addresses all three in one cutover.

## Goals (in scope)

1. Replace Litmus chaos-operator + ChaosEngine + ChaosResult primitives with Chaos Mesh chaos-controller-manager + PodChaos / NetworkChaos / Schedule.
2. Rewrite the three existing chaos WorkflowTemplates (`chaos-pod-delete`, `chaos-network-loss`, `chaos-kafka-broker-partition`) to emit Chaos Mesh CRs.
3. Simplify `verdict-job` by deleting the `internal/chaosresult/` package — trust the Argo chaos step's `successCondition` as the only chaos-applied signal.
4. Delete all Litmus-specific in-tree templates (`litmus-chaos-operator.yaml`, `litmus-chaos-experiments.yaml`, `rbac-litmus-cluster-admin-lite.yaml`, `mongodb.yaml`).
5. Preserve the existing scenario contract (`scenarios/*.yaml` → workflow → verdict report). End users see no behaviour change beyond the chaos engine itself.
6. End state: zero Litmus / MongoDB pods, CRDs, deployments in cluster. `kubectl get pods | grep -E 'litmus|mongo'` returns empty.

## Goals (out of scope, deferred)

- New chaos kinds (IOChaos, TimeChaos, JVMChaos, etc.) — captured as future Phase 4 work; Plan 12 only migrates the three existing scenarios.
- Chaos Mesh dashboard UI — explicitly disabled (`chaos-mesh.dashboard.create=false`), same logic that removed the Litmus portal.
- Workflow-level orchestration via Chaos Mesh `Workflow` CR — we keep using Argo Workflows for orchestration.
- Side-by-side Litmus + Chaos Mesh coexistence — direct cutover chosen during brainstorm.
- Multi-cluster chaos federation — no use case.
- `chaos-from-hub` WT — Litmus-specific, deleted not migrated. If Phase 4 wants a "fetch chaos definition from a remote source" mechanism, we'll design fresh.

## Architecture

```
helm/dlh-test-fw/
├── Chart.yaml                              MODIFIED  remove `litmus`, add `chaos-mesh`
├── values.yaml                             MODIFIED  remove `litmus:` block, add `chaos-mesh:` block
├── files/workflowtemplates/chaos/
│   ├── pod-delete.yaml                     REWRITE   emit Schedule wrapping PodChaos
│   ├── network-loss.yaml                   REWRITE   emit NetworkChaos (action: loss)
│   ├── kafka-broker-partition.yaml         REWRITE   emit NetworkChaos (action: partition)
│   └── from-hub.yaml                       DELETE    Litmus-hub specific
├── templates/
│   ├── litmus-chaos-operator.yaml          DELETE
│   ├── litmus-chaos-experiments.yaml       DELETE
│   ├── rbac-litmus-cluster-admin-lite.yaml DELETE
│   └── mongodb.yaml                        DELETE
verdict-job/
├── internal/chaosresult/                   DELETE    ~87 LOC (chaosresult.go + _test.go)
├── cmd/verdict/main.go                     MODIFIED  -15 LOC (drop -chaos-result-name flag, drop GetVerdict)
├── internal/eval/eval.go                   MODIFIED  -10 LOC (drop ChaosVerdict field; overall = all SLO passed)
└── internal/report/report.go               MODIFIED  -5 LOC  (drop chaos_verdict from report.json)
scenarios/
├── mysql-pod-delete.yaml                   MODIFIED  drop `chaos_result_name` from verdict step
├── kafka-broker-partition.yaml             MODIFIED  same
└── doris-be-network-loss.yaml              MODIFIED  same
helm/dlh-test-fw/files/workflowtemplates/verdict/
└── slo-eval.yaml                           MODIFIED  drop `chaos_result_name` input parameter
scripts/
├── verify-templates.sh                     MODIFIED  WT list (chaos-from-hub gone; 10 WTs total)
└── platform-up.sh                          MODIFIED  drop `helm repo add litmuschaos`
docs/FINDINGS.md                            APPENDED  Plan 12 section
```

Net: 14 modified, 6 deleted, 0 new files (only new content is values.yaml's `chaos-mesh:` block and a Chart.yaml dependency entry). Codebase shrinks despite migrating to a new system — the Litmus + MongoDB + ChaosCenter baggage is heavier than the Chaos Mesh equivalent.

## Chaos primitive mapping

| Current Litmus | New Chaos Mesh | Notes |
|---|---|---|
| `ChaosEngine` + experiment `pod-delete` (duration=60s, interval=10s → 6 kills) | `Schedule` wrapping `PodChaos {action: pod-kill, mode: one}` with `schedule: "@every 10s"`, `historyLimit: 6` | Preserves the "stream of kills over duration" semantic. WT submits one Schedule CR; Chaos Mesh controller reconciles N PodChaos children. |
| `ChaosEngine` + experiment `pod-network-loss` (duration=60s, loss=50%) | `NetworkChaos {action: loss, duration: "60s", loss: {loss: "50"}}` | 1:1 mapping. NetworkChaos has a native `duration` field — no Schedule needed. |
| `ChaosEngine` + experiment `pod-network-partition` (duration=60s) | `NetworkChaos {action: partition, duration: "60s", direction: "both"}` | 1:1 mapping. |

**Selector translation:** Litmus's `appinfo.appns + applabel` → Chaos Mesh's `selector.namespaces + selector.labelSelectors`. Same target semantics, different field shape.

**Why Schedule for pod-kill but not for network chaos?** Chaos Mesh `PodChaos action: pod-kill` is a one-shot event with no `duration` field — once a pod is killed there's no "ongoing chaos" state to maintain. `Schedule` is Chaos Mesh's native mechanism for "re-apply chaos every N seconds". NetworkChaos in contrast represents a persistent fault state; its CR has a native `duration` field, so no wrapper is needed.

## Argo step success conditions (per WT)

### `chaos-pod-delete` WT

Submits a `Schedule` CR. The WT's `resource` step needs to:
- Return success when chaos has actually completed (Schedule stopped producing children AND last child finished).
- Not return prematurely while Chaos Mesh is still cycling through kills.

Mechanism:
- `successCondition: status.activeJobs == 0 && status.lastScheduleTime != ""` on the Schedule CR
- Plus an Argo-layer `sleep <chaos_duration>` step running in parallel, so the chaos step doesn't return before the configured chaos window has elapsed even if the Schedule's terminal state arrives early

The "sleep in parallel" pattern: the chaos WT's `entrypoint` becomes a `dag` of two nodes — `submit-schedule` (the resource step) and `sleep-duration` (a busybox sleep) — and the WT only returns when both finish.

### `chaos-network-loss` and `chaos-kafka-broker-partition` WTs

Submit `NetworkChaos` CRs directly. The NetworkChaos CR's lifecycle:
- Creation → Chaos Mesh injects via chaos-daemon → `.status.experiment.containerRecords[*].phase = Injected`
- After `duration` elapses → Chaos Mesh recovers → `.status.experiment.containerRecords[*].phase = Recovered`

Mechanism:
- `successCondition: status.experiment.containerRecords[0].phase == Recovered`
- `failureCondition: status.conditions[?(@.type=="AllInjected")].status == False` with an implicit Argo step timeout (3× duration)

No parallel sleep needed — `NetworkChaos.duration` handles the window timing natively.

## Verdict-job simplification

### Before (Litmus era)

```
verdict-job starts
├── 1. Read ChaosResult.status.experimentStatus.verdict = "Pass"/"Fail"
│       (internal/chaosresult/chaosresult.go does this)
├── 2. Run SLO eval (read VM, eval thresholds against chaos window)
└── overall = (chaosVerdict == "Pass") AND (all SLO thresholds passed)
```

### After (Chaos Mesh era)

```
verdict-job starts
├── Run SLO eval (read VM, eval thresholds against chaos window)
└── overall = (all SLO thresholds passed)
```

The chaos-applied signal is now encoded entirely in "the Argo chaos step Succeeded, therefore verdict-job got invoked". The chaos step's `successCondition` waits for Chaos Mesh CR phase to reach the terminal-recovered state; if chaos never injected, the Argo step fails, `continueOn: { failed: true }` still lets verdict run BUT the SLO eval will see no chaos impact and the operator should investigate the chaos step's logs — same diagnostic path as today, one less layer of redundant validation.

### Code deletions

| File | LOC change | Notes |
|---|---|---|
| `verdict-job/internal/chaosresult/chaosresult.go` | -42 | Whole file deleted |
| `verdict-job/internal/chaosresult/chaosresult_test.go` | -45 | Whole file deleted |
| `verdict-job/cmd/verdict/main.go` | -15 | Drop `-chaos-result-name` flag, drop `GetVerdict` call, drop the `chaosresult` import |
| `verdict-job/internal/eval/eval.go` | -10 | Drop `ChaosVerdict` field from `Result` struct, simplify `overall` computation |
| `verdict-job/internal/report/report.go` | -5 | Drop `chaos_verdict` from JSON output |
| `helm/dlh-test-fw/files/workflowtemplates/verdict/slo-eval.yaml` | -3 | Drop `chaos_result_name` input parameter + container arg |
| `helm/dlh-test-fw/files/workflowtemplates/chaos/{pod-delete,network-loss,kafka-broker-partition}.yaml` | -9 (3×3) | Drop `outputs.parameters.chaos_result_name` |
| `scenarios/{mysql-pod-delete,kafka-broker-partition,doris-be-network-loss}.yaml` | -3 | Drop `chaos_result_name` wiring in verdict step |

**Net delete: ~132 LOC across Go + YAML. Zero LOC added on the verdict side.**

## Chaos Mesh subchart selection

- Chart repo: `https://charts.chaos-mesh.org`
- Tentative chart: `chaos-mesh-helm` version pinning a 2.7.x release (latest stable as of 2026-05). Plan Task 1 confirms latest available and pins to a specific version + records appVersion in FINDINGS.

`values.yaml` `chaos-mesh:` block (approximate; Plan 12 finalises):

```yaml
chaos-mesh:
  controllerManager:
    replicaCount: 1
  chaosDaemon:
    # DaemonSet; injects network chaos via hostNetwork.
    runtime: containerd
    socketPath: /run/containerd/containerd.sock
  dashboard:
    create: false              # No UI; same logic as Litmus portal removal
  dnsServer:
    create: false              # We don't use DNSChaos
```

## CRD strategy

The live cluster already has `*.chaos-mesh.org` CRDs from a prior partial install — NOT managed by our Helm release. Plan 12 Task 1 explicitly compares the existing CRD versions against the chosen subchart's CRD versions:

- **If compatible** → chart install is a no-op for CRDs; proceed.
- **If incompatible** → clean and reinstall:
  ```bash
  kubectl delete crd $(kubectl get crd -o name | grep chaos-mesh.org)
  helm upgrade dlh helm/dlh-test-fw -n dlh-test-fw
  ```

Same Task 1 also deletes Litmus CRDs **after** the helm upgrade removes the litmus subchart (otherwise CRDs linger as orphaned resources):

```bash
kubectl delete crd \
  chaosengines.litmuschaos.io \
  chaosexperiments.litmuschaos.io \
  chaosresults.litmuschaos.io \
  eventtrackerpolicies.eventtracker.litmuschaos.io
```

Documented as an explicit branch in Plan 12 Task 1, not "discover at execution time".

## Example rewrites

### `chaos-pod-delete.yaml` (new shape — illustrative; Plan 12 finalises exact field wiring)

```yaml
apiVersion: argoproj.io/v1alpha1
kind: WorkflowTemplate
metadata:
  name: chaos-pod-delete
  labels:
    dlh.category: chaos
spec:
  entrypoint: main
  serviceAccountName: argo-workflow
  templates:
  - name: main
    inputs:
      parameters:
      - name: target_namespace
      - name: target_pod_selector      # e.g. "app=mysql" (Litmus form)
      - name: duration                 # e.g. "60s"
      - name: interval                 # e.g. "10s"
    dag:
      tasks:
      - name: submit-schedule
        template: submit
        arguments:
          parameters:
          - { name: target_namespace,    value: "{{inputs.parameters.target_namespace}}" }
          - { name: target_pod_selector, value: "{{inputs.parameters.target_pod_selector}}" }
          - { name: interval,            value: "{{inputs.parameters.interval}}" }
      - name: sleep-window
        template: sleep
        arguments:
          parameters:
          - { name: duration, value: "{{inputs.parameters.duration}}" }
        # Both tasks run in parallel; the DAG completes when both finish.
  - name: submit
    inputs:
      parameters:
      - name: target_namespace
      - name: target_pod_selector
      - name: interval
    resource:
      action: create
      successCondition: status.lastScheduleTime != "" && status.activeJobs == 0
      manifest: |
        apiVersion: chaos-mesh.org/v1alpha1
        kind: Schedule
        metadata:
          generateName: dlh-pod-kill-
          namespace: {{`{{workflow.namespace}}`}}
        spec:
          schedule: '@every {{`{{inputs.parameters.interval}}`}}'
          historyLimit: 10
          concurrencyPolicy: Forbid
          type: PodChaos
          podChaos:
            action: pod-kill
            mode: one
            selector:
              namespaces:
              - {{`{{inputs.parameters.target_namespace}}`}}
              labelSelectors:
                {{`{{inputs.parameters.target_pod_selector}}`}}
  - name: sleep
    inputs:
      parameters:
      - name: duration
    container:
      image: busybox:1.36
      command: [sh, -c]
      args: ["sleep ${DURATION%s}"]
      env:
      - name: DURATION
        value: "{{`{{inputs.parameters.duration}}`}}"
```

Note: `target_pod_selector` continues to use the Litmus form `app=mysql` at the scenario layer; the WT parses that into Chaos Mesh's `labelSelectors: { app: mysql }` map shape. This keeps the scenario YAMLs minimally changed and the WT absorbs the engine-specific shape.

### `chaos-network-loss.yaml` (new shape, simpler — no Schedule)

```yaml
spec:
  entrypoint: main
  serviceAccountName: argo-workflow
  templates:
  - name: main
    inputs:
      parameters:
      - name: target_namespace
      - name: target_pod_selector
      - name: loss_percent             # e.g. "50"
      - name: duration                 # e.g. "60s"
    resource:
      action: create
      successCondition: status.experiment.containerRecords[0].phase == Recovered
      failureCondition: status.conditions[?(@.type=="AllInjected")].status == False
      manifest: |
        apiVersion: chaos-mesh.org/v1alpha1
        kind: NetworkChaos
        metadata:
          generateName: dlh-network-loss-
          namespace: {{`{{workflow.namespace}}`}}
        spec:
          action: loss
          duration: '{{`{{inputs.parameters.duration}}`}}'
          mode: one
          selector:
            namespaces:
            - {{`{{inputs.parameters.target_namespace}}`}}
            labelSelectors:
              {{`{{inputs.parameters.target_pod_selector}}`}}
          loss:
            loss: '{{`{{inputs.parameters.loss_percent}}`}}'
            correlation: '0'
          direction: both
```

`chaos-kafka-broker-partition.yaml` follows the same shape with `action: partition` and a kafka-targeted selector. Plan 12 captures the full body.

## Testing

| Element | How |
|---|---|
| Chaos Mesh subchart installs cleanly | `helm template` renders controller-manager Deployment + chaos-daemon DaemonSet, no dashboard, no dnsServer. `kubectl rollout status deploy/chaos-controller-manager --timeout=180s` succeeds. |
| CRDs are Helm-managed | `kubectl get crd podchaos.chaos-mesh.org -o jsonpath='{.metadata.annotations.meta\.helm\.sh/release-name}'` returns `dlh`. |
| `chaos-pod-delete` WT works | `make run-mysql` produces a Workflow; chaos step submits one Schedule CR; Chaos Mesh controller emits N PodChaos children over the duration; mysql pod gets restarted N times during the chaos window. |
| `chaos-network-loss` WT works | Targeted standalone test (one-off scenario YAML against a sacrificial pod) shows NetworkChaos applied → loss > 50% from inside cluster, `duration` elapses, NetworkChaos recovered. |
| `chaos-kafka-broker-partition` WT works | `make run-kafka` produces a Workflow; kafka broker is partitioned from clients; k6 load runner sees produce errors during the chaos window. |
| verdict-job report shape | After `make run-mysql`, `report.json` has SLO threshold details but NO `chaos_verdict` field. Existing dashboards still populate. |
| Litmus is fully gone | `kubectl get pods -n dlh-test-fw \| grep -E 'litmus\|mongo\|chaos-operator-ce'` returns empty. `kubectl get crd \| grep litmus` returns empty. |
| Plan 9/10/11 still green | After helm upgrade: `make run-mysql && make run-kafka && ./scripts/verify-templates.sh`; CI on push to main passes (`helm`, `go`, `shellcheck`, `kubeconform` all green). |

## Success criteria

1. `helm/dlh-test-fw/Chart.yaml` declares `chaos-mesh` subchart; no `litmus` dependency.
2. Three chaos WTs emit Chaos Mesh CRs (Schedule wrapping PodChaos, NetworkChaos, NetworkChaos). Zero `ChaosEngine` references anywhere in the repo (`grep -rln ChaosEngine`).
3. `verdict-job/internal/chaosresult/` directory is gone; verdict report JSON has no `chaos_verdict` field; remaining 6 verdict-job test packages still pass (`go test ./...`).
4. `make run-mysql` and `make run-kafka` both Succeed end-to-end against Chaos Mesh.
5. No Litmus or MongoDB pods, deployments, CRDs, or service accounts remain in the `dlh-test-fw` namespace after merge.
6. `docs/FINDINGS.md` has a Plan 12 section documenting: Schedule-based pod-kill loop, Litmus retirement, dead config removed, the CRD compatibility branch.
7. `scripts/verify-templates.sh` updated to expect 10 WTs (3 chaos + 3 fixture + load-k6-run + verdict-slo-eval + util-write-slo + util-ensure-mysql-table); `chaos-from-hub` removed.
8. CI on `main` after merge: all 4 jobs green (`helm`, `go`, `shellcheck`, `kubeconform`).

## Risks

- **Existing `chaos-mesh.org` CRDs have unknown provenance.** Plan 12 Task 1 baseline strictly compares versions and cleans if mismatched. If the partial install left CRD instances around (Chaos Mesh CRs created but no controller running), those orphaned CRs get GC'd when CRDs are deleted — verify via `kubectl get podchaos.chaos-mesh.org -A` before nuking.
- **Schedule CR `successCondition` race conditions** on Chaos Mesh controller restarts mid-window. Mitigation: the parallel `sleep <duration>` step in the WT DAG ensures the chaos step never returns before the configured window elapses, regardless of CR state vagaries.
- **`mode: one` vs `mode: all`** for pod-kill. Litmus pod-delete picks one matching pod per kill cycle (random). Chaos Mesh `mode: one` matches. If a future scenario wants "kill ALL matching pods at once", that's a scenario-YAML change (set `mode: all` via parameter), no WT change.
- **No belt-and-braces chaos verdict** (Simplify decision). If a chaos WT silently fails to actually apply chaos but Argo step Succeeds (a corner case under Chaos Mesh's stricter CR state machine), SLO eval will see no impact and emit "PASS" — a false negative. Accepted trade-off; the simpler architecture is worth more than the rare belt-and-braces signal that was 90% redundant with the Argo success anyway.
- **`Schedule.spec.schedule: "@every Ns"` syntax compatibility.** Chaos Mesh documents `@every` as supported. Plan 12 Task 1 baseline explicitly verifies by submitting a no-op Schedule CR before any scenario rewrites land.
- **chaos-daemon DaemonSet on minikube driver.** chaos-daemon needs `hostNetwork: true` + privileged + host PID. Minikube's docker driver supports this; arm64 / Apple Silicon expected to work since chaos-mesh ships multi-arch images. Plan baseline confirms.
- **Plan 11 `dlh-scenario-locks` semaphore is unaffected.** Argo Workflow-level synchronisation is engine-agnostic; the chaos-engine swap doesn't touch the per-target queue.
- **CI's `kubeconform` job has `-skip CustomResourceDefinition,ChaosExperiment`.** After Plan 12, `ChaosExperiment` is dead — drop the skip. New CRD kinds from Chaos Mesh (`PodChaos`, `NetworkChaos`, `Schedule`) should be in the Datree CRDs catalog; verify and skip if missing.

## File summary

| Path | Change |
|---|---|
| `helm/dlh-test-fw/Chart.yaml` | MODIFIED — remove `litmus` dep, add `chaos-mesh` dep |
| `helm/dlh-test-fw/values.yaml` | MODIFIED — remove `litmus:` block, add `chaos-mesh:` block |
| `helm/dlh-test-fw/templates/litmus-chaos-operator.yaml` | DELETED |
| `helm/dlh-test-fw/templates/litmus-chaos-experiments.yaml` | DELETED |
| `helm/dlh-test-fw/templates/rbac-litmus-cluster-admin-lite.yaml` | DELETED |
| `helm/dlh-test-fw/templates/mongodb.yaml` | DELETED |
| `helm/dlh-test-fw/files/workflowtemplates/chaos/pod-delete.yaml` | REWRITE |
| `helm/dlh-test-fw/files/workflowtemplates/chaos/network-loss.yaml` | REWRITE |
| `helm/dlh-test-fw/files/workflowtemplates/chaos/kafka-broker-partition.yaml` | REWRITE |
| `helm/dlh-test-fw/files/workflowtemplates/chaos/from-hub.yaml` | DELETED |
| `helm/dlh-test-fw/files/workflowtemplates/verdict/slo-eval.yaml` | MODIFIED |
| `verdict-job/cmd/verdict/main.go` | MODIFIED |
| `verdict-job/internal/chaosresult/chaosresult.go` | DELETED |
| `verdict-job/internal/chaosresult/chaosresult_test.go` | DELETED |
| `verdict-job/internal/eval/eval.go` | MODIFIED |
| `verdict-job/internal/report/report.go` | MODIFIED |
| `scenarios/mysql-pod-delete.yaml` | MODIFIED |
| `scenarios/kafka-broker-partition.yaml` | MODIFIED |
| `scenarios/doris-be-network-loss.yaml` | MODIFIED |
| `scripts/verify-templates.sh` | MODIFIED |
| `scripts/platform-up.sh` | MODIFIED |
| `docs/FINDINGS.md` | APPENDED |
| `.github/workflows/ci.yml` | MODIFIED — update kubeconform `-skip` list (drop `ChaosExperiment`) |

Total: 14 modified, 6 deleted, 0 new files outside the chart's `chaos-mesh:` values block.

## Relationship to other plans

- **Plan 9** (util WTs + slo_vars + run-scenario.sh -p): unaffected. SLO library / util-write-slo / util-ensure-mysql-table are chaos-engine-agnostic.
- **Plan 10** (GitHub Actions CI): `kubeconform -skip` list updates as part of Plan 12 (drop `ChaosExperiment`, possibly add the Chaos Mesh CRDs if Datree's catalog lacks them — Plan baseline verifies).
- **Plan 11** (scenario queue + priority): unaffected. `spec.synchronization.semaphores` is an Argo Workflow concept; the chaos engine underneath is irrelevant.
