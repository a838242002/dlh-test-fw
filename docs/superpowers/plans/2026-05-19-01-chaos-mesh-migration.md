# Chaos Mesh Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace Litmus chaos-operator + ChaosEngine primitives with Chaos Mesh chaos-controller-manager + PodChaos/NetworkChaos/Schedule, delete the `verdict-job/internal/chaosresult/` package, and clear the Litmus + MongoDB + Bitnami baggage entirely. End state: zero Litmus pods/CRDs in cluster; scenarios still run end-to-end with the same contract.

**Architecture:** Phased cutover in one worktree. First install Chaos Mesh alongside the existing Litmus (no removal yet) and prove its primitives work standalone. Then simplify verdict-job to remove the Litmus-specific `chaosresult` package (verified against the OLD Litmus chaos WTs to isolate the change). Then rewrite the three chaos WTs to emit Chaos Mesh CRs (`Schedule` wrapping `PodChaos` for pod-kill; `NetworkChaos` direct for loss/partition). Finally remove Litmus entirely.

**Tech Stack:** Argo Workflows v3.6.10 (in-cluster), Helm v4.2.0, Go 1.26.3, Chaos Mesh v2.7.x (latest stable), kubectl, bash. No new images.

**Reference spec:** `docs/superpowers/specs/2026-05-19-chaos-mesh-migration-design.md`. Re-read the chaos primitive mapping, the verdict-job simplification deltas, the existing-CRD strategy, and the risk register before starting.

**Branch & worktree:** Per `CLAUDE.md` conventions, create a feature worktree at `../dlh-test-fw-plan12` on branch `feat/plan12-chaos-mesh-migration` before Task 2. Task 1 runs from the main worktree.

---

## File Structure

**New files:**
- (none outside the chart's `values.yaml` `chaos-mesh:` block and a `Chart.yaml` dependency entry)

**Modified files:**
- `helm/dlh-test-fw/Chart.yaml` — swap `litmus` for `chaos-mesh`
- `helm/dlh-test-fw/values.yaml` — swap `litmus:` for `chaos-mesh:` block
- `helm/dlh-test-fw/files/workflowtemplates/chaos/pod-delete.yaml` — rewrite (Schedule + PodChaos + sleep DAG)
- `helm/dlh-test-fw/files/workflowtemplates/chaos/network-loss.yaml` — rewrite (NetworkChaos)
- `helm/dlh-test-fw/files/workflowtemplates/chaos/kafka-broker-partition.yaml` — rewrite (NetworkChaos)
- `helm/dlh-test-fw/files/workflowtemplates/verdict/slo-eval.yaml` — drop `chaos_result_name` input
- `verdict-job/cmd/verdict/main.go` — drop `-chaos-result-name` flag
- `verdict-job/internal/eval/eval.go` — drop `ChaosVerdict` field
- `verdict-job/internal/report/report.go` — drop `chaos_verdict` JSON field
- `scenarios/mysql-pod-delete.yaml` — drop `chaos_result_name` arg + `chaos_force` arg
- `scenarios/kafka-broker-partition.yaml` — drop `chaos_result_name` arg
- `scenarios/doris-be-network-loss.yaml` — drop `chaos_result_name` arg
- `scripts/verify-templates.sh` — drop `chaos-from-hub`; total 10 WTs
- `scripts/platform-up.sh` — drop `helm repo add litmuschaos`
- `.github/workflows/ci.yml` — update kubeconform `-skip` list
- `docs/FINDINGS.md` — append Plan 12 section

**Deleted files:**
- `helm/dlh-test-fw/templates/litmus-chaos-operator.yaml`
- `helm/dlh-test-fw/templates/litmus-chaos-experiments.yaml`
- `helm/dlh-test-fw/templates/rbac-litmus-cluster-admin-lite.yaml`
- `helm/dlh-test-fw/templates/mongodb.yaml`
- `helm/dlh-test-fw/files/workflowtemplates/chaos/from-hub.yaml`
- `verdict-job/internal/chaosresult/chaosresult.go`
- `verdict-job/internal/chaosresult/chaosresult_test.go`

**Unchanged:** dashboards, fixture images, dlh-k6 image, util-write-slo, util-ensure-mysql-table, SLO library, scenario-locks ConfigMap, Plan 9/10/11 artefacts.

---

## Task 1: Baseline — verify Plan 9/10/11 green; map existing Chaos Mesh CRDs

This task makes no commits. It confirms the starting state (everything still works post-Plan-11) AND inspects the orphaned Chaos Mesh CRDs in cluster so Task 2 can decide whether to clean or keep them.

**Files:** None modified.

Work from: `/Users/allen/repo/dlh-test-fw` (main worktree, branch `main`).

- [ ] **Step 1: Confirm clean tree + recent state**

```bash
git status
git log --first-parent --oneline -5
```

Expected: clean tree on `main`; recent commits include `da9e605` (Chaos Mesh migration spec) or newer.

- [ ] **Step 2: Confirm Plan 9/10/11 baselines**

```bash
make run-mysql
```

Use Bash timeout ≥ 600000 ms (10 min). Expected: `Final phase: Succeeded`.

```bash
make run-kafka
./scripts/verify-templates.sh
```

Expected: `Final phase: Succeeded`; `PASS: all 11 WorkflowTemplates present`.

If anything fails, STOP and report BLOCKED — fix the existing problem before layering Chaos Mesh on top.

- [ ] **Step 3: Inspect orphaned Chaos Mesh CRDs**

```bash
# List all chaos-mesh.org CRDs and their provenance.
for crd in $(kubectl get crd -o name | grep chaos-mesh.org); do
  name=$(echo "$crd" | sed 's|.*/||')
  release=$(kubectl get "$crd" -o jsonpath='{.metadata.annotations.meta\.helm\.sh/release-name}' 2>/dev/null || echo "(none)")
  version=$(kubectl get "$crd" -o jsonpath='{.spec.versions[0].name}' 2>/dev/null || echo "?")
  echo "$name  release=$release  version=$version"
done
echo "---"
# Are there any chaos-mesh.org CR instances? (Orphaned data we'd nuke if cleaning CRDs.)
kubectl get $(kubectl get crd -o name | grep chaos-mesh.org | sed 's|.*/||' | paste -sd, -) -A 2>&1 | head -20
```

Expected outcome (record verbatim in your report; Plan 12 Task 2 decides what to do):
- CRD list: all `chaos-mesh.org/v1alpha1` (the standard Chaos Mesh CRD version).
- `release=` likely `(none)` for all — confirms they're not managed by our Helm release.
- CR instances: likely zero (nothing was creating Chaos Mesh CRs since no controller was running).

If a CR instance exists, capture its name + namespace + creation timestamp for later decision. Don't delete anything in this task.

- [ ] **Step 4: Inspect current Argo controller version (sanity)**

```bash
kubectl -n dlh-test-fw get deploy dlh-argo-workflows-workflow-controller \
  -o jsonpath='{.spec.template.spec.containers[0].image}{"\n"}'
```

Expected: `quay.io/argoproj/workflow-controller:v3.6.10` (set by Plan 11). Chaos Mesh's CRD `successCondition` JSONPath syntax works with v3.6+.

- [ ] **Step 5: Verify no commit**

```bash
git status
```

Expected: clean (untracked plan-doc file is fine; do NOT commit it here).

---

## Task 2: Worktree + install Chaos Mesh subchart alongside Litmus

**Files:**
- Create worktree at `../dlh-test-fw-plan12`
- Modify: `helm/dlh-test-fw/Chart.yaml`
- Modify: `helm/dlh-test-fw/values.yaml`

At the end of this task, the cluster has BOTH Litmus and Chaos Mesh installed. Litmus continues to handle the existing chaos WTs; Chaos Mesh's controller + DaemonSet are up and ready for Task 3 onwards.

- [ ] **Step 1: Create feature worktree**

From `/Users/allen/repo/dlh-test-fw`:

```bash
git worktree add ../dlh-test-fw-plan12 -b feat/plan12-chaos-mesh-migration main
cd ../dlh-test-fw-plan12
git status
```

Expected: on `feat/plan12-chaos-mesh-migration`, working tree clean.

All subsequent steps in this plan operate from `/Users/allen/repo/dlh-test-fw-plan12`.

- [ ] **Step 2: Find a Chaos Mesh chart version**

```bash
helm repo add chaos-mesh https://charts.chaos-mesh.org
helm repo update chaos-mesh
helm search repo chaos-mesh/chaos-mesh --versions | head -10
# Inspect appVersion of the latest stable:
helm show chart chaos-mesh/chaos-mesh | grep -E '^(name|version|appVersion):'
```

Pick the LATEST stable chart whose appVersion is `2.7.x` (or `2.8.x` if available and not RC). Document the chosen `version:` and `appVersion:` in your report.

- [ ] **Step 3: Inspect the chart's bundled CRDs**

```bash
# What CRD versions does this chart ship? We need to know if they're compatible with the orphans in cluster.
helm template chaos-mesh chaos-mesh/chaos-mesh --version <PICKED_VERSION> \
  | grep -E '^kind: CustomResourceDefinition|^  name: .*chaos-mesh.org' | head -30
```

Compare with the cluster's CRD versions from Task 1 Step 3.

- If both are `chaos-mesh.org/v1alpha1` → compatible; chart's CRD install is mostly no-op.
- If chart bundles a newer apiVersion that the existing CRDs don't have → clean and reinstall in Step 5.

Document the decision: KEEP existing CRDs or CLEAN them.

- [ ] **Step 4: Add the subchart to `Chart.yaml`**

Edit `helm/dlh-test-fw/Chart.yaml`. Under `dependencies:`, ADD (do not remove litmus yet):

```yaml
- name: chaos-mesh
  version: <PICKED_VERSION>          # e.g. 2.7.4
  repository: https://charts.chaos-mesh.org
```

Position: alphabetical placement is fine; existing entries are not strictly alphabetical so anywhere under `dependencies:` works.

- [ ] **Step 5: Add `chaos-mesh:` block to `values.yaml`**

Edit `helm/dlh-test-fw/values.yaml`. Add at the end (before any other top-level subchart key — order doesn't matter):

```yaml
chaos-mesh:
  controllerManager:
    replicaCount: 1
  chaosDaemon:
    # DaemonSet; injects network chaos via hostNetwork.
    runtime: containerd
    socketPath: /run/containerd/containerd.sock
  dashboard:
    create: false              # No UI; same logic as Litmus portal removal.
  dnsServer:
    create: false              # We don't use DNSChaos.
```

- [ ] **Step 6: Conditional CRD clean (only if Step 3 decided CLEAN)**

```bash
# Only if Task 2 Step 3 decided CRDs are incompatible:
kubectl delete crd $(kubectl get crd -o name | grep chaos-mesh.org)
# (Don't do this if Step 3 decided KEEP.)
```

Skip this step entirely if Step 3 chose KEEP.

- [ ] **Step 7: Helm dependency + install**

```bash
helm dependency update helm/dlh-test-fw
helm upgrade dlh helm/dlh-test-fw -n dlh-test-fw --timeout 5m
```

Use Bash timeout ≥ 600000 ms (10 min) — `helm upgrade --timeout 5m` lets Helm wait for rollout but caps wall-clock.

Expected: upgrade succeeds. New resources: `chaos-controller-manager` Deployment, `chaos-daemon` DaemonSet, possibly `chaos-mesh` ServiceAccount + RBAC.

- [ ] **Step 8: Verify Chaos Mesh is up**

```bash
kubectl -n dlh-test-fw rollout status deploy/chaos-controller-manager --timeout=180s
kubectl -n dlh-test-fw rollout status ds/chaos-daemon --timeout=180s
kubectl -n dlh-test-fw get pods | grep -E 'chaos-controller-manager|chaos-daemon' | head -10
# Verify CRDs are now Helm-managed:
kubectl get crd podchaos.chaos-mesh.org -o jsonpath='{.metadata.annotations.meta\.helm\.sh/release-name}{"\n"}'
```

Expected:
- controller-manager Ready (1/1)
- chaos-daemon Ready on every node (minikube: 1/1)
- `release-name` annotation on the CRD is `dlh` (chart now manages them).

- [ ] **Step 9: Confirm Plan 9/10/11 still pass with both engines installed**

```bash
make run-mysql
```

Use Bash timeout ≥ 600000 ms. Expected: `Final phase: Succeeded` — Litmus still drives chaos; Chaos Mesh is dormant.

- [ ] **Step 10: Commit**

```bash
git add helm/dlh-test-fw/Chart.yaml helm/dlh-test-fw/values.yaml
git commit -m "feat(chart): install chaos-mesh subchart alongside existing litmus

Phase 1 of Plan 12. Chaos Mesh controller + DaemonSet up; existing Litmus
chaos WTs continue to run. Plan-12 Task 3 smoke-tests Chaos Mesh primitives
before any WT rewrite."
```

`Chart.lock` and `charts/` are gitignored per project convention (Plan 11 confirmed).

---

## Task 3: Standalone Chaos Mesh smoke test (PodChaos + NetworkChaos + Schedule)

This task is verification-only — no commits. Prove the three Chaos Mesh primitives we'll use actually inject chaos and recover correctly, BEFORE wiring them through WTs.

**Files:** None modified.

- [ ] **Step 1: Smoke-test PodChaos (action: pod-kill, mode: one)**

```bash
cat > /tmp/smoke-podchaos.yaml <<'EOF'
apiVersion: chaos-mesh.org/v1alpha1
kind: PodChaos
metadata:
  generateName: smoke-pod-kill-
  namespace: dlh-test-fw
spec:
  action: pod-kill
  mode: one
  selector:
    namespaces: [mysql-sys]
    labelSelectors:
      app: mysql
EOF
name=$(kubectl create -f /tmp/smoke-podchaos.yaml -o jsonpath='{.metadata.name}')
echo "Submitted PodChaos: $name"
sleep 5
kubectl -n dlh-test-fw get podchaos "$name" -o jsonpath='{.status.experiment.containerRecords[0].phase}{"\n"}'
kubectl -n dlh-test-fw get podchaos "$name" -o yaml | head -50
```

Expected: `.status.experiment.containerRecords[0].phase` reaches `Injected` and shortly after `NotInjected` (pod-kill has no recovery — pod was killed). The mysql pod should restart within ~30s.

```bash
kubectl -n mysql-sys get pods | head
# Check restart count:
kubectl -n mysql-sys get pod -l app=mysql -o jsonpath='{.items[0].status.containerStatuses[0].restartCount}{"\n"}'
```

Expected: mysql pod has at least one restart count incremented from baseline. Note: minikube's mysql may not have a baseline restart count of 0; just confirm it bumped.

Cleanup:

```bash
kubectl -n dlh-test-fw delete podchaos "$name"
```

- [ ] **Step 2: Smoke-test NetworkChaos (action: loss)**

```bash
cat > /tmp/smoke-netchaos.yaml <<'EOF'
apiVersion: chaos-mesh.org/v1alpha1
kind: NetworkChaos
metadata:
  generateName: smoke-net-loss-
  namespace: dlh-test-fw
spec:
  action: loss
  duration: "20s"
  mode: one
  selector:
    namespaces: [mysql-sys]
    labelSelectors:
      app: mysql
  loss:
    loss: "50"
    correlation: "0"
  direction: both
EOF
name=$(kubectl create -f /tmp/smoke-netchaos.yaml -o jsonpath='{.metadata.name}')
echo "Submitted NetworkChaos: $name"

# Wait for Injected.
for i in {1..30}; do
  phase=$(kubectl -n dlh-test-fw get networkchaos "$name" -o jsonpath='{.status.experiment.containerRecords[0].phase}' 2>/dev/null)
  echo "[$i] phase=$phase"
  [[ "$phase" == "Injected" ]] && break
  sleep 2
done

# Wait for Recovered.
for i in {1..30}; do
  phase=$(kubectl -n dlh-test-fw get networkchaos "$name" -o jsonpath='{.status.experiment.containerRecords[0].phase}' 2>/dev/null)
  echo "[$i] phase=$phase"
  [[ "$phase" == "Recovered" ]] && break
  sleep 2
done

kubectl -n dlh-test-fw delete networkchaos "$name"
```

Expected: phase transitions `→ Injected → Recovered` within ~30s total. Some Argo controllers report `containerRecords` as an array; if `[0]` is `Recovered`, success.

- [ ] **Step 3: Smoke-test Schedule wrapping PodChaos**

```bash
cat > /tmp/smoke-schedule.yaml <<'EOF'
apiVersion: chaos-mesh.org/v1alpha1
kind: Schedule
metadata:
  generateName: smoke-pod-kill-sched-
  namespace: dlh-test-fw
spec:
  schedule: "@every 10s"
  historyLimit: 5
  concurrencyPolicy: Forbid
  type: PodChaos
  podChaos:
    action: pod-kill
    mode: one
    selector:
      namespaces: [mysql-sys]
      labelSelectors:
        app: mysql
EOF
name=$(kubectl create -f /tmp/smoke-schedule.yaml -o jsonpath='{.metadata.name}')
echo "Submitted Schedule: $name"

# Wait ~30s, then check Schedule has produced children.
sleep 35
kubectl -n dlh-test-fw get schedule "$name" -o jsonpath='{.status.lastScheduleTime}{"\t"}{.status.active}{"\n"}'
kubectl -n dlh-test-fw get podchaos -o name | grep -c "$(echo "$name" | sed 's/smoke-pod-kill-sched-//')"  # rough count of children
```

Expected: `lastScheduleTime` populated; at least 2 PodChaos children produced (30s / 10s interval).

Cleanup:

```bash
kubectl -n dlh-test-fw delete schedule "$name"
# Children may already be GC'd; clean any leftovers:
kubectl -n dlh-test-fw delete podchaos --field-selector metadata.namespace=dlh-test-fw 2>/dev/null || true
```

(Be careful with this last command — it deletes ALL PodChaos in the namespace. Only safe because we know no real workflows are using PodChaos yet at this point in the plan.)

- [ ] **Step 4: No commit**

This task is verification. If all three smokes pass, proceed to Task 4. If any fail, STOP and report BLOCKED — chart install isn't sufficient.

---

## Task 4: Simplify verdict-job + slo-eval WT + scenarios (drop chaos_result_name)

The verdict-job has `internal/chaosresult/` to read Litmus `ChaosResult.status.experimentStatus.verdict`. Plan 12 drops this entirely. The change must be atomic across:
- Go code (delete the package, drop the flag)
- slo-eval WT (drop the input param + container arg)
- All three scenarios (drop the wiring)

At the end of this task, Litmus chaos WTs still emit `outputs.parameters.chaos_result_name` (that's a Task 5/6 concern), but nothing reads it.

**Files:**
- Delete: `verdict-job/internal/chaosresult/chaosresult.go`
- Delete: `verdict-job/internal/chaosresult/chaosresult_test.go`
- Modify: `verdict-job/cmd/verdict/main.go`
- Modify: `verdict-job/internal/eval/eval.go`
- Modify: `verdict-job/internal/report/report.go`
- Modify: `helm/dlh-test-fw/files/workflowtemplates/verdict/slo-eval.yaml`
- Modify: `scenarios/mysql-pod-delete.yaml`
- Modify: `scenarios/kafka-broker-partition.yaml`
- Modify: `scenarios/doris-be-network-loss.yaml`

- [ ] **Step 1: Read current state of the Go files**

```bash
ls verdict-job/internal/chaosresult/
cat verdict-job/internal/chaosresult/chaosresult.go
head -50 verdict-job/cmd/verdict/main.go
head -50 verdict-job/internal/eval/eval.go
```

Note the current shape of `Result` struct in `eval.go` (specifically the `ChaosVerdict` field) and the chaos-related flags in `main.go`.

- [ ] **Step 2: Delete the `chaosresult` package**

```bash
rm -rf verdict-job/internal/chaosresult/
ls verdict-job/internal/  # confirm chaosresult/ gone
```

- [ ] **Step 3: Update `verdict-job/cmd/verdict/main.go`**

Read the current file. Remove three things:
1. The `-chaos-result-name` flag declaration (`flag.String("chaos-result-name", ...)`).
2. The `chaosresult` import.
3. The call to `chaosresult.GetVerdict(...)` and any place that uses its return value.

Where the return value `chaosVerdict` was passed to `eval.Run(...)`, drop that argument. The `eval.Run` signature changes in Step 4 to match.

After the edit, run:

```bash
cd verdict-job
go build ./...
cd -
```

Expected: build succeeds with no errors. If `eval.Run` now has a missing argument complaint, that's expected — Step 4 fixes it.

- [ ] **Step 4: Update `verdict-job/internal/eval/eval.go`**

Read the file. Find the `Result` struct. Remove the `ChaosVerdict string` field (and any methods that reference it).

Find the `Run(...)` function. Drop the `chaosVerdict string` parameter. Update the `overall` computation: replace `chaosVerdict == "Pass" && allPassed` with just `allPassed`.

After the edit:

```bash
cd verdict-job
go build ./...
cd -
```

Expected: build succeeds.

- [ ] **Step 5: Update `verdict-job/internal/report/report.go`**

Read the file. Remove `chaos_verdict` from the JSON output struct (e.g. drop a `ChaosVerdict string \`json:"chaos_verdict"\`` line). Drop any code that writes it.

- [ ] **Step 6: Run all verdict-job tests**

```bash
cd verdict-job
go vet ./...
go test ./...
cd -
```

Expected: `go vet` silent. `go test ./...` passes on all 6 remaining packages (chaosresult is gone; main has no test file). If a test in `eval/` or `report/` references `ChaosVerdict`, fix it (drop the assertion / fixture line) before moving on.

- [ ] **Step 7: Update `helm/dlh-test-fw/files/workflowtemplates/verdict/slo-eval.yaml`**

Read the file. Remove the `chaos_result_name` input parameter declaration AND the corresponding `-chaos-result-name=...` container argument.

After edit, run:

```bash
helm lint helm/dlh-test-fw
helm template dlh helm/dlh-test-fw | grep -A20 'name: verdict-slo-eval' | head -30
```

Expected: lint clean; rendered WT no longer has `chaos_result_name` anywhere.

- [ ] **Step 8: Update the three scenarios**

For each of `scenarios/mysql-pod-delete.yaml`, `scenarios/kafka-broker-partition.yaml`, `scenarios/doris-be-network-loss.yaml`:

In the verdict step's `arguments.parameters`, find the `chaos_result_name` line and DELETE it. Example diff for mysql:

```yaml
    - - name: verdict
        templateRef: { name: verdict-slo-eval, template: main }
        arguments:
          parameters:
          - { name: slo_yaml,           value: "(unused — read from CM)" }
-         - { name: chaos_result_name,  value: "{{steps.chaos.outputs.parameters.chaos_result_name}}-pod-delete" }
          - { name: load_start_ts,      value: "{{steps.load.startedAt}}" }
```

Do the same for kafka (`...chaos_result_name}}-pod-network-partition` line) and doris (`...chaos_result_name}}-pod-network-loss` line).

- [ ] **Step 9: Deploy + smoke test (still using Litmus chaos WTs)**

The chaos WTs haven't been touched yet; they still emit `outputs.parameters.chaos_result_name`. Argo doesn't error on unused outputs.

```bash
helm upgrade dlh helm/dlh-test-fw -n dlh-test-fw
# Build verdict-job + load into minikube:
cd verdict-job && make load-image && cd -
# Wait for new verdict image to propagate (imagePullPolicy: Never means pod uses last loaded):
# Actually `make load-image` only refreshes the local image; running workflows use whichever
# pod's already running. The next workflow will pick the new image automatically.

make run-mysql
```

Use Bash timeout ≥ 600000 ms. Expected: `Final phase: Succeeded`. The chaos still happens via Litmus, the verdict-job runs WITHOUT reading ChaosResult, and the report JSON has no `chaos_verdict` field.

```bash
wf=$(kubectl -n dlh-test-fw get workflow -o name | grep -E 'mysql-pod-delete-[0-9]{8}-[0-9]{6}$' | sort | tail -1 | sed 's|.*/||')
echo "wf=$wf"
kubectl -n dlh-test-fw exec deploy/dlh-minio -- mc cat "local/artifacts/${wf}/${wf}-main-*/verdict/report.json" | jq .
```

Expected: JSON output, no `chaos_verdict` key. Has `overall`, `thresholds`, etc.

- [ ] **Step 10: Commit**

```bash
git add verdict-job/cmd/verdict/main.go \
        verdict-job/internal/eval/eval.go \
        verdict-job/internal/report/report.go \
        helm/dlh-test-fw/files/workflowtemplates/verdict/slo-eval.yaml \
        scenarios/mysql-pod-delete.yaml \
        scenarios/kafka-broker-partition.yaml \
        scenarios/doris-be-network-loss.yaml
git rm -r verdict-job/internal/chaosresult/
git commit -m "feat(verdict): drop chaosresult package; trust Argo chaos step success

Plan 12 step 1: simplify verdict-job. The Litmus-specific ChaosResult
verdict read is removed; chaos completion is now signalled solely by the
Argo chaos step's successCondition. Net -130 LOC. End-to-end mysql
scenario still passes (Litmus chaos WTs unchanged; just unread)."
```

---

## Task 5: Rewrite `chaos-pod-delete` WT to emit Schedule + PodChaos

**Files:**
- Modify (full rewrite): `helm/dlh-test-fw/files/workflowtemplates/chaos/pod-delete.yaml`
- Modify: `scenarios/mysql-pod-delete.yaml` (drop unused `chaos_force` workflow parameter + step arg; convert `chaos_interval: "10"` → `"10s"`)

- [ ] **Step 1: Overwrite `helm/dlh-test-fw/files/workflowtemplates/chaos/pod-delete.yaml`**

Replace the entire file with:

```yaml
# Plan 12 (2026-05-19): rewritten to use Chaos Mesh Schedule + PodChaos
# instead of Litmus ChaosEngine. Schedule produces N PodChaos children
# at `interval` cadence; a parallel Argo sleep step ensures the chaos
# step doesn't return before the requested chaos window has elapsed,
# even if the Schedule's terminal-state JSONPath glitches.
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
      - name: target_pod_selector      # "key=value" form (Litmus convention preserved)
      - name: duration                 # e.g. "60s"
      - name: interval                 # e.g. "10s"
    dag:
      tasks:
      - name: submit-schedule
        template: submit
        arguments:
          parameters:
          - { name: target_namespace,    value: "{{`{{inputs.parameters.target_namespace}}`}}" }
          - { name: target_pod_selector, value: "{{`{{inputs.parameters.target_pod_selector}}`}}" }
          - { name: interval,            value: "{{`{{inputs.parameters.interval}}`}}" }
      - name: sleep-window
        template: sleep
        arguments:
          parameters:
          - { name: duration, value: "{{`{{inputs.parameters.duration}}`}}" }

  - name: submit
    inputs:
      parameters:
      - name: target_namespace
      - name: target_pod_selector
      - name: interval
    script:
      image: alpine/k8s:1.30.0
      command: [bash]
      source: |
        set -euo pipefail
        # Parse "key=value" selector into the labelSelectors map form.
        SEL='{{`{{inputs.parameters.target_pod_selector}}`}}'
        KEY="${SEL%%=*}"
        VAL="${SEL#*=}"
        cat <<EOF | kubectl apply -f -
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
                $KEY: $VAL
        EOF

  - name: sleep
    inputs:
      parameters:
      - name: duration
    container:
      image: busybox:1.36
      command: [sh, -c]
      args:
      - |
        # `{{duration}}` is "60s" — strip the trailing "s" for `sleep`.
        D='{{`{{inputs.parameters.duration}}`}}'
        sleep "${D%s}"
```

Notes:
- The `submit` template uses a `script` step (not the original `resource` action) because we need to parse the `key=value` selector into Chaos Mesh's `labelSelectors` map shape at runtime. A `resource` step doesn't allow shell logic in the manifest body.
- Schedule CR persists after the workflow finishes (Argo doesn't auto-delete it). Cleanup happens at scenario level — see Step 3 below.
- No `outputs.parameters.chaos_result_name` (Task 4 removed all consumers).

- [ ] **Step 2: Update `scenarios/mysql-pod-delete.yaml`**

Two coupled changes:

(a) Remove the `chaos_force` workflow parameter (no Chaos Mesh equivalent — pod-kill is force-equivalent by default):

```yaml
- { name: chaos_interval,    value: "10" }
- { name: chaos_force,       value: "true" }    # DELETE THIS LINE
```

(b) Convert `chaos_interval` from `"10"` to `"10s"`:

```yaml
- { name: chaos_interval,    value: "10s" }
```

(c) Update the chaos step args. Currently:

```yaml
- - name: chaos
    templateRef: { name: chaos-pod-delete, template: main }
    continueOn: { failed: true }
    arguments:
      parameters:
      - { name: target_namespace,    value: "mysql-sys" }
      - { name: target_pod_selector, value: "app=mysql" }
      - { name: duration,            value: "{{workflow.parameters.chaos_duration}}" }
      - { name: interval,            value: "{{workflow.parameters.chaos_interval}}" }
      - { name: force,               value: "{{workflow.parameters.chaos_force}}" }   # DELETE
```

Remove the `force` line. Final shape:

```yaml
- - name: chaos
    templateRef: { name: chaos-pod-delete, template: main }
    continueOn: { failed: true }
    arguments:
      parameters:
      - { name: target_namespace,    value: "mysql-sys" }
      - { name: target_pod_selector, value: "app=mysql" }
      - { name: duration,            value: "{{workflow.parameters.chaos_duration}}" }
      - { name: interval,            value: "{{workflow.parameters.chaos_interval}}" }
```

Note: `chaos_duration` in mysql scenario YAML is currently `"60s"` (Plan 9 form), so it already has the unit suffix Chaos Mesh wants.

- [ ] **Step 3: Ensure argo-workflow SA can create chaos-mesh.org resources**

Plan 4 backfilled RBAC for litmuschaos.io but not chaos-mesh.org. Check:

```bash
kubectl -n dlh-test-fw auth can-i create podchaos.chaos-mesh.org --as=system:serviceaccount:dlh-test-fw:argo-workflow
kubectl -n dlh-test-fw auth can-i create schedules.chaos-mesh.org --as=system:serviceaccount:dlh-test-fw:argo-workflow
kubectl -n dlh-test-fw auth can-i create networkchaos.chaos-mesh.org --as=system:serviceaccount:dlh-test-fw:argo-workflow
```

If any return `no`, edit `helm/dlh-test-fw/templates/rbac-argo-workflow-scenarios.yaml`. Add a rules entry:

```yaml
- apiGroups: ["chaos-mesh.org"]
  resources: ["podchaos", "networkchaos", "schedules"]
  verbs: ["create", "get", "list", "watch", "update", "patch", "delete"]
```

Then helm upgrade and re-check `auth can-i` returns `yes`.

- [ ] **Step 4: Deploy + smoke**

```bash
helm upgrade dlh helm/dlh-test-fw -n dlh-test-fw
kubectl -n dlh-test-fw get wt chaos-pod-delete -o yaml | head -30
```

Confirm the WT shows the new shape (Schedule + sleep DAG).

- [ ] **Step 5: Run mysql scenario end-to-end**

```bash
make run-mysql
```

Use Bash timeout ≥ 900000 ms (15 min). Expected: `Final phase: Succeeded`.

```bash
wf=$(kubectl -n dlh-test-fw get workflow -o name | grep -E 'mysql-pod-delete-[0-9]{8}-[0-9]{6}$' | sort | tail -1 | sed 's|.*/||')
# Inspect the chaos artefacts during the run (best done during, but here as post-mortem):
kubectl -n dlh-test-fw get schedule | head
kubectl -n dlh-test-fw get podchaos | head
```

Expected: at least one `Schedule` was created in `dlh-test-fw` and at least one (preferably ~6 for 60s/10s) `PodChaos` child was created during the run. The mysql pod was restarted multiple times.

```bash
# Verify verdict report JSON:
kubectl -n dlh-test-fw exec deploy/dlh-minio -- mc cat "local/artifacts/${wf}/${wf}-main-*/verdict/report.json" | jq .
```

Expected: report has SLO thresholds; no `chaos_verdict` field; `overall` reflects SLO outcomes only.

- [ ] **Step 6: Clean leftover Chaos Mesh CRs from smoke**

```bash
# Find any Schedule/PodChaos from the test run and delete (chaos-from-hub-style cleanup):
kubectl -n dlh-test-fw delete schedule --all 2>/dev/null || true
kubectl -n dlh-test-fw delete podchaos --all 2>/dev/null || true
```

(These deletions are safe at this point in the plan because the scenario workflow is finished and nothing else uses Chaos Mesh CRs yet.)

- [ ] **Step 7: Commit**

```bash
git add helm/dlh-test-fw/files/workflowtemplates/chaos/pod-delete.yaml \
        scenarios/mysql-pod-delete.yaml \
        helm/dlh-test-fw/templates/rbac-argo-workflow-scenarios.yaml
git commit -m "feat(chaos): rewrite chaos-pod-delete to use Chaos Mesh Schedule + PodChaos

Argo DAG: parallel submit-schedule + sleep-window. Schedule CR cycles N
PodChaos children at the configured interval. mysql scenario updated
(chaos_force dropped, chaos_interval gains 's' suffix). RBAC for
argo-workflow SA extended to chaos-mesh.org/{podchaos,networkchaos,schedules}."
```

---

## Task 6: Rewrite `chaos-network-loss` + `chaos-kafka-broker-partition` WTs

**Files:**
- Modify (full rewrite): `helm/dlh-test-fw/files/workflowtemplates/chaos/network-loss.yaml`
- Modify (full rewrite): `helm/dlh-test-fw/files/workflowtemplates/chaos/kafka-broker-partition.yaml`

- [ ] **Step 1: Overwrite `chaos/network-loss.yaml`**

```yaml
# Plan 12: NetworkChaos for pod-network-loss. CR has native duration —
# no Schedule wrapper needed (unlike pod-kill).
apiVersion: argoproj.io/v1alpha1
kind: WorkflowTemplate
metadata:
  name: chaos-network-loss
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
      - name: target_pod_selector      # "key=value" form
      - name: loss_percent             # e.g. "50"
      - name: duration                 # e.g. "60s"
    script:
      image: alpine/k8s:1.30.0
      command: [bash]
      source: |
        set -euo pipefail
        SEL='{{`{{inputs.parameters.target_pod_selector}}`}}'
        KEY="${SEL%%=*}"
        VAL="${SEL#*=}"

        # Submit and wait for terminal Recovered phase.
        NAME=$(cat <<EOF | kubectl create -f - -o jsonpath='{.metadata.name}'
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
              $KEY: $VAL
          loss:
            loss: '{{`{{inputs.parameters.loss_percent}}`}}'
            correlation: '0'
          direction: both
        EOF
        )
        echo "NetworkChaos created: $NAME"

        # Poll for Recovered. Worst-case = duration + small buffer.
        D='{{`{{inputs.parameters.duration}}`}}'
        DEADLINE=$(( $(date +%s) + ${D%s} + 30 ))
        while (( $(date +%s) < DEADLINE )); do
          phase=$(kubectl -n {{`{{workflow.namespace}}`}} get networkchaos "$NAME" \
                    -o jsonpath='{.status.experiment.containerRecords[0].phase}' 2>/dev/null || echo "")
          echo "[$(date +%H:%M:%S)] phase=$phase"
          if [[ "$phase" == "Recovered" ]]; then
            echo "Recovered."
            exit 0
          fi
          sleep 3
        done
        echo "ERROR: NetworkChaos did not reach Recovered within budget" >&2
        exit 1
```

- [ ] **Step 2: Overwrite `chaos/kafka-broker-partition.yaml`**

```yaml
# Plan 12: NetworkChaos action=partition for isolating a kafka broker pod.
apiVersion: argoproj.io/v1alpha1
kind: WorkflowTemplate
metadata:
  name: chaos-kafka-broker-partition
  labels:
    dlh.category: chaos
spec:
  entrypoint: main
  serviceAccountName: argo-workflow
  templates:
  - name: main
    inputs:
      parameters:
      - name: kafka_namespace          # e.g. "kafka-sys"
      - name: broker_id                # e.g. "0" — translates to label selector
      - name: duration                 # e.g. "60s"
    script:
      image: alpine/k8s:1.30.0
      command: [bash]
      source: |
        set -euo pipefail
        # apache/kafka KRaft single-broker target has pods labelled
        # `app=kafka,kafka-id=<N>`. Partition that one broker.
        BID='{{`{{inputs.parameters.broker_id}}`}}'

        NAME=$(cat <<EOF | kubectl create -f - -o jsonpath='{.metadata.name}'
        apiVersion: chaos-mesh.org/v1alpha1
        kind: NetworkChaos
        metadata:
          generateName: dlh-kafka-partition-
          namespace: {{`{{workflow.namespace}}`}}
        spec:
          action: partition
          duration: '{{`{{inputs.parameters.duration}}`}}'
          mode: all
          selector:
            namespaces:
            - {{`{{inputs.parameters.kafka_namespace}}`}}
            labelSelectors:
              app: kafka
              kafka-id: "$BID"
          direction: both
        EOF
        )
        echo "NetworkChaos partition created: $NAME"

        D='{{`{{inputs.parameters.duration}}`}}'
        DEADLINE=$(( $(date +%s) + ${D%s} + 30 ))
        while (( $(date +%s) < DEADLINE )); do
          phase=$(kubectl -n {{`{{workflow.namespace}}`}} get networkchaos "$NAME" \
                    -o jsonpath='{.status.experiment.containerRecords[0].phase}' 2>/dev/null || echo "")
          echo "[$(date +%H:%M:%S)] phase=$phase"
          [[ "$phase" == "Recovered" ]] && { echo "Recovered."; exit 0; }
          sleep 3
        done
        echo "ERROR: NetworkChaos partition did not reach Recovered within budget" >&2
        exit 1
```

Notes:
- Uses `mode: all` because we want to partition that specific broker pod (not pick "one of many").
- `kafka-id` label is the apache/kafka KRaft chart's pod label. Plan baseline can verify by `kubectl -n kafka-sys get pods --show-labels`. If the label is different (e.g. `statefulset.kubernetes.io/pod-name`), update the selector accordingly.

- [ ] **Step 3: Deploy + smoke**

```bash
helm upgrade dlh helm/dlh-test-fw -n dlh-test-fw
kubectl -n dlh-test-fw get wt chaos-network-loss chaos-kafka-broker-partition -o name
```

Expected: both WTs present.

- [ ] **Step 4: Confirm kafka pod labels for selector**

```bash
kubectl -n kafka-sys get pods --show-labels | head
```

If the broker pod's labels DON'T include `app=kafka` AND `kafka-id=0` (e.g. it's `app.kubernetes.io/name=kafka` or there's no kafka-id), update the WT's selector to match. Document any change in your report.

- [ ] **Step 5: Run kafka scenario end-to-end**

```bash
make run-kafka
```

Use Bash timeout ≥ 900000 ms (15 min). Expected: `Final phase: Succeeded`.

```bash
wf=$(kubectl -n dlh-test-fw get workflow -o name | grep -E 'kafka-broker-partition-[0-9]{8}-[0-9]{6}$' | sort | tail -1 | sed 's|.*/||')
kubectl -n dlh-test-fw exec deploy/dlh-minio -- mc cat "local/artifacts/${wf}/${wf}-main-*/verdict/report.json" | jq .
kubectl -n dlh-test-fw get networkchaos
```

Expected: report shows SLO thresholds evaluated; no `chaos_verdict` field; NetworkChaos CR(s) created and now in Recovered state.

- [ ] **Step 6: Clean leftover chaos CRs**

```bash
kubectl -n dlh-test-fw delete networkchaos --all 2>/dev/null || true
```

- [ ] **Step 7: Commit**

```bash
git add helm/dlh-test-fw/files/workflowtemplates/chaos/network-loss.yaml \
        helm/dlh-test-fw/files/workflowtemplates/chaos/kafka-broker-partition.yaml
git commit -m "feat(chaos): rewrite network-loss + kafka-broker-partition WTs to Chaos Mesh NetworkChaos

NetworkChaos has native duration; no Schedule wrapper needed. Both WTs
poll .status.experiment.containerRecords[0].phase for Recovered.
kafka-broker-partition uses mode=all with label selector for the specific
broker pod."
```

---

## Task 7: Remove Litmus entirely

**Files:**
- Modify: `helm/dlh-test-fw/Chart.yaml` — remove `litmus` dep
- Modify: `helm/dlh-test-fw/values.yaml` — remove `litmus:` block
- Delete: `helm/dlh-test-fw/templates/litmus-chaos-operator.yaml`
- Delete: `helm/dlh-test-fw/templates/litmus-chaos-experiments.yaml`
- Delete: `helm/dlh-test-fw/templates/rbac-litmus-cluster-admin-lite.yaml`
- Delete: `helm/dlh-test-fw/templates/mongodb.yaml`
- Delete: `helm/dlh-test-fw/files/workflowtemplates/chaos/from-hub.yaml`
- Modify: `scripts/platform-up.sh`

- [ ] **Step 1: Remove the `litmus` subchart from `Chart.yaml`**

Edit `helm/dlh-test-fw/Chart.yaml`. Under `dependencies:`, delete the entire entry:

```yaml
- name: litmus
  version: 3.28.0
  repository: https://litmuschaos.github.io/litmus-helm/
```

- [ ] **Step 2: Remove the `litmus:` block from `values.yaml`**

Edit `helm/dlh-test-fw/values.yaml`. Delete the entire `litmus:` top-level key and its nested content (the multi-paragraph comment + portal config + mongodb config + DB_SERVER + DBUSER + DBPASSWORD).

- [ ] **Step 3: Delete the Litmus in-tree templates and the Litmus-hub chaos WT**

```bash
git rm helm/dlh-test-fw/templates/litmus-chaos-operator.yaml \
       helm/dlh-test-fw/templates/litmus-chaos-experiments.yaml \
       helm/dlh-test-fw/templates/rbac-litmus-cluster-admin-lite.yaml \
       helm/dlh-test-fw/templates/mongodb.yaml \
       helm/dlh-test-fw/files/workflowtemplates/chaos/from-hub.yaml
```

- [ ] **Step 4: Drop `helm repo add litmuschaos` from `platform-up.sh`**

Edit `scripts/platform-up.sh`. Find the line:

```bash
helm repo add litmuschaos https://litmuschaos.github.io/litmus-helm/ || true
```

Delete it.

- [ ] **Step 5: Helm lint + dependency update**

```bash
helm dependency update helm/dlh-test-fw
helm lint helm/dlh-test-fw
```

Expected: lint clean; `Chart.lock` updated to no longer reference litmus.

- [ ] **Step 6: Deploy (this evicts the Litmus pods)**

```bash
helm upgrade dlh helm/dlh-test-fw -n dlh-test-fw --timeout 5m
```

Expected: upgrade succeeds; Helm removes the Litmus subchart's deployments + ChaosCenter + MongoDB pods.

Verify:

```bash
kubectl -n dlh-test-fw get pods | grep -iE 'litmus|mongo|chaos-operator-ce' && echo "STILL PRESENT" || echo "All gone"
kubectl -n dlh-test-fw get deploy | grep -iE 'litmus|mongo|chaos-operator'
```

Expected: `All gone`. No deployments named litmus-*, mongo-*, or chaos-operator-ce.

- [ ] **Step 7: Clean Litmus CRDs**

```bash
kubectl delete crd \
  chaosengines.litmuschaos.io \
  chaosexperiments.litmuschaos.io \
  chaosresults.litmuschaos.io \
  eventtrackerpolicies.eventtracker.litmuschaos.io 2>&1 || true

# Verify:
kubectl get crd | grep litmus && echo "STILL PRESENT" || echo "All Litmus CRDs gone"
```

Expected: `All Litmus CRDs gone`.

- [ ] **Step 8: Final scenario smoke (both must pass)**

```bash
make run-mysql
make run-kafka
```

Both with Bash timeout ≥ 900000 ms each. Expected: both `Final phase: Succeeded`. This proves the Chaos Mesh swap is complete and self-sufficient — no Litmus residue is needed.

- [ ] **Step 9: Commit**

```bash
git add helm/dlh-test-fw/Chart.yaml \
        helm/dlh-test-fw/values.yaml \
        scripts/platform-up.sh
git commit -m "feat(chart): remove litmus subchart + Litmus in-tree templates + MongoDB

Plan 12 phase 2: Litmus retired. Removes the entire ChaosCenter + MongoDB
stack and the in-tree backfills. Litmus CRDs cleaned from cluster
out-of-band. End-to-end scenarios now run purely on Chaos Mesh."
```

---

## Task 8: Update CI kubeconform + verify-templates.sh

**Files:**
- Modify: `scripts/verify-templates.sh`
- Modify: `.github/workflows/ci.yml`

- [ ] **Step 1: Update `scripts/verify-templates.sh`**

Edit the EXPECTED array. After Plan 11 it was 11 templates including `chaos-from-hub`. After Plan 12 it's 10 (we deleted `chaos-from-hub`):

```bash
EXPECTED=(
  fixture-minio-load-mysql
  fixture-minio-load-doris
  fixture-kafka-topic-seed
  chaos-pod-delete
  chaos-network-loss
  chaos-kafka-broker-partition
  load-k6-run
  verdict-slo-eval
  util-write-slo
  util-ensure-mysql-table
)
```

Update the final PASS message to `PASS: all 10 WorkflowTemplates present`.

```bash
./scripts/verify-templates.sh
```

Expected: `PASS: all 10 WorkflowTemplates present`.

- [ ] **Step 2: Update `.github/workflows/ci.yml` kubeconform `-skip` list**

The Plan 10 kubeconform job has `-skip CustomResourceDefinition,ChaosExperiment`. After Plan 12 there are no Litmus ChaosExperiment CRs in our rendered chart, but Chaos Mesh CRs (PodChaos, NetworkChaos, Schedule) might or might not be in Datree's CRDs catalog.

Verify locally first:

```bash
helm template dlh helm/dlh-test-fw > /tmp/rendered.yaml
kubeconform -skip CustomResourceDefinition -strict -summary \
  -schema-location default \
  -schema-location 'https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json' \
  /tmp/rendered.yaml
```

If this passes: update the CI to use `-skip CustomResourceDefinition` (drop `ChaosExperiment`).
If it fails: add the offending Kind(s) to the skip list. Common candidates: `PodChaos`, `NetworkChaos`, `Schedule`.

Then also run on scenarios:

```bash
kubeconform -skip CustomResourceDefinition -strict -summary \
  -schema-location default \
  -schema-location 'https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json' \
  scenarios/*.yaml
```

Document the FINAL `-skip` list (e.g. `CustomResourceDefinition` or `CustomResourceDefinition,PodChaos,NetworkChaos,Schedule`).

Edit `.github/workflows/ci.yml`. In the `kubeconform` job, update BOTH validation steps:

```yaml
- name: Validate rendered chart
  run: |
    kubeconform -skip <FINAL_LIST> -strict -summary \
      ...
```

(Apply the same `<FINAL_LIST>` to both the rendered-chart validation and the scenarios validation.)

Also update the comment above:

```yaml
# -skip CustomResourceDefinition: Datree catalog has no meta-schema for CRD-of-CRD.
# -skip <other Kinds if any>: Datree catalog lacks schema for these Chaos Mesh CRDs.
```

- [ ] **Step 3: Commit**

```bash
git add scripts/verify-templates.sh .github/workflows/ci.yml
git commit -m "chore: update verify-templates list + kubeconform skips for Plan 12

verify-templates.sh: 11 -> 10 WTs (chaos-from-hub gone).
CI kubeconform: drop ChaosExperiment from -skip list (Litmus CRs are gone);
add Chaos Mesh CRs that Datree's catalog lacks (if any)."
```

---

## Task 9: FINDINGS + final suite + merge + push + cleanup + tag

**Files:**
- Modify: `docs/FINDINGS.md` — append Plan 12 section

- [ ] **Step 1: Append Plan 12 section to `docs/FINDINGS.md`**

Read the current FINDINGS.md tail to match style. Append:

```markdown
## Plan 12 — Chaos Mesh migration (2026-05-19)

- Litmus retired entirely. ChaosCenter portal, in-tree MongoDB, in-tree
  chaos-operator backfill, in-tree ChaosExperiment CRs, in-tree
  cluster-admin-lite RBAC, and the chaos-from-hub WT all deleted in one
  cutover. Net file-count -6.
- Chaos engine is now `chaos-mesh` subchart (v2.7.x family, appVersion v2.7.x).
  Controller-manager Deployment + chaos-daemon DaemonSet. No dashboard, no
  DNS server, no portal-equivalent UI — same posture as the Litmus portal
  removal logic.
- Chaos primitive mapping:
  - `Litmus pod-delete (duration+interval)` -> `chaos-mesh.org/Schedule`
    wrapping `PodChaos {action: pod-kill, mode: one}` with
    `schedule: "@every <interval>"` and `historyLimit: 10`.
  - `Litmus pod-network-loss` -> `chaos-mesh.org/NetworkChaos
    {action: loss, duration: <s>, loss: {loss: <%>, correlation: "0"}}`.
  - `Litmus pod-network-partition` -> `chaos-mesh.org/NetworkChaos
    {action: partition, duration: <s>, direction: both}`.
- `chaos-pod-delete` WT uses an Argo DAG: parallel submit-schedule +
  sleep-window. The sleep ensures the chaos step does not return before
  the requested chaos window has elapsed, even if Schedule status JSONPath
  glitches on controller restart.
- `chaos-network-loss` / `chaos-kafka-broker-partition` WTs poll
  `.status.experiment.containerRecords[0].phase == Recovered` rather than
  using Argo's resource-step successCondition. This is because we use
  `script` steps (not `resource` steps) to do the `key=value -> labelSelectors`
  parsing at runtime; the polling is the equivalent terminal-state wait.
- `verdict-job/internal/chaosresult/` deleted (-130 LOC across Go + tests).
  `verdict-job/cmd/verdict/main.go` no longer takes `-chaos-result-name`.
  `verdict-job/internal/eval/eval.go` `Result.ChaosVerdict` field gone.
  `report.json` no longer has `chaos_verdict`. The chaos-applied signal is
  now ENTIRELY encoded in Argo chaos step success — false-negative risk
  documented in the spec (accepted trade-off).
- RBAC for `argo-workflow` ServiceAccount extended to
  `chaos-mesh.org/{podchaos,networkchaos,schedules}` in
  `templates/rbac-argo-workflow-scenarios.yaml`.
- Pitfall: `kafka-broker-partition` selector uses labels `app=kafka` +
  `kafka-id=<N>`. If apache/kafka KRaft chart labels change, this WT
  needs an update. Verified at Plan 12 Task 6.
- Pitfall: Chaos Mesh CRDs may have lived in cluster from a partial install
  before Plan 12. Task 2 handles compatibility check; if mismatched,
  delete + reinstall.
- CI kubeconform skip list adjusted: `ChaosExperiment` dropped (no Litmus
  CRs left). Chaos Mesh kinds (`PodChaos`, `NetworkChaos`, `Schedule`)
  resolve via Datree CRDs-catalog (or added to skip list if not — see
  the actual `.github/workflows/ci.yml`).
- Plan 11 `dlh-scenario-locks` semaphore unaffected — Argo Workflow
  synchronisation is chaos-engine-agnostic.
```

- [ ] **Step 2: Final suite re-run for the merge log**

```bash
make run-mysql
make run-kafka
./scripts/verify-templates.sh
```

Sequential. Bash timeout ≥ 900000 ms per `make`. Expected:
- mysql `Final phase: Succeeded`
- kafka `Final phase: Succeeded`
- verify-templates `PASS: all 10 WorkflowTemplates present`

- [ ] **Step 3: Commit FINDINGS**

```bash
git add docs/FINDINGS.md
git commit -m "docs(findings): record Plan 12 chaos mesh migration"
```

- [ ] **Step 4: Merge to main with --no-ff**

From the **main worktree**:

```bash
cd /Users/allen/repo/dlh-test-fw
git status   # should be clean (or only an untracked plan doc)
git checkout main
git pull origin main
git merge --no-ff feat/plan12-chaos-mesh-migration -m "$(cat <<'EOF'
Merge feat/plan12-chaos-mesh-migration: retire Litmus, adopt Chaos Mesh

Plan 12 replaces the Litmus chaos engine with Chaos Mesh end-to-end.

- helm/dlh-test-fw/Chart.yaml: -litmus 3.28.0, +chaos-mesh 2.7.x
- 6 files deleted: 4 in-tree templates (mongodb, litmus-chaos-operator,
  litmus-chaos-experiments, rbac-litmus-cluster-admin-lite), 1 chaos WT
  (chaos-from-hub), 1 Go package (verdict-job/internal/chaosresult/)
- 3 chaos WTs rewritten:
  * chaos-pod-delete: Argo DAG over Schedule-wrapped PodChaos + sleep
  * chaos-network-loss: NetworkChaos {action:loss}
  * chaos-kafka-broker-partition: NetworkChaos {action:partition,mode:all}
- verdict-job: -130 LOC. report.json no longer has chaos_verdict.
- 3 scenarios drop chaos_result_name (and mysql drops chaos_force).
- CI kubeconform skip list updated for Plan 12 CRD shape.

Verified live: same-target serialisation still works (Plan 11
unaffected), make run-mysql + make run-kafka both Succeeded end-to-end
against Chaos Mesh, Litmus pods and CRDs fully gone from cluster.

Spec: docs/superpowers/specs/2026-05-19-chaos-mesh-migration-design.md
Plan: docs/superpowers/plans/2026-05-19-01-chaos-mesh-migration.md
EOF
)"
git log --first-parent --oneline -8
```

- [ ] **Step 5: Push to remote + watch CI on main**

```bash
git push origin main
sleep 10
gh run list --branch main --limit 2
```

Expected: push succeeds; main-branch CI is in_progress or success. If `failure`, STOP and report — main red.

- [ ] **Step 6: Clean worktree + branch (local + remote)**

```bash
git worktree remove ../dlh-test-fw-plan12
git push origin --delete feat/plan12-chaos-mesh-migration 2>/dev/null || true
git branch -d feat/plan12-chaos-mesh-migration
git worktree list
```

Expected: only main worktree; remote branch deleted (no-op if never pushed); local branch deleted.

- [ ] **Step 7: Tag + push**

```bash
git tag plan12-chaos-mesh-migration
git push origin plan12-chaos-mesh-migration
git log --first-parent --oneline -10
```

---

## Self-Review notes (author check, fresh-eyes pass)

- **Spec coverage:**
  - Goal 1 (replace Litmus primitives): Tasks 2 (install Chaos Mesh) + 5/6 (rewrite WTs) + 7 (remove Litmus).
  - Goal 2 (rewrite 3 chaos WTs): Tasks 5 (pod-delete) + 6 (network-loss + kafka-broker-partition).
  - Goal 3 (simplify verdict-job): Task 4.
  - Goal 4 (delete Litmus in-tree templates): Task 7.
  - Goal 5 (preserve scenario contract): verified by `make run-mysql` + `make run-kafka` at each task's end.
  - Goal 6 (zero Litmus/MongoDB in cluster): Task 7 Steps 6/7 + Task 9 Step 2.
- **Spec out-of-scope items**: all explicitly excluded — no new chaos kinds, no dashboard, no side-by-side mode.
- **Spec testing matrix**: every row maps to a step in Tasks 2/3/4/5/6/7/8/9.
- **Spec success criteria**: 1 (Chart.yaml swap) Task 7; 2 (WTs emit Chaos Mesh CRs) Tasks 5+6; 3 (chaosresult gone) Task 4; 4 (mysql + kafka end-to-end) Tasks 7 + 9; 5 (no Litmus) Task 7; 6 (FINDINGS) Task 9; 7 (verify-templates 10 WTs) Task 8; 8 (CI green) Task 9 Step 5.
- **Spec risks**: every risk has a corresponding plan-task mitigation:
  - Orphan Chaos Mesh CRDs → Task 1 Step 3 (inspect) + Task 2 Step 6 (conditional clean)
  - Schedule successCondition race → Task 5 Step 1 (sleep DAG)
  - mode one vs all → captured in WTs explicitly
  - No belt-and-braces chaos verdict → Task 4 (intentional)
  - `@every Ns` syntax → Task 3 Step 3 (smoke verifies)
  - chaos-daemon on minikube → Task 2 Step 8 (rollout status)
  - Plan 11 semaphore → unaffected by design; Task 9 Step 2 confirms via successful scenario runs
  - CI kubeconform skip → Task 8
- **Placeholder scan**: no TBD/TODO/etc. Conditional steps ("if labels differ, update WT", "if Datree lacks schema, add to skip list") are explicit and bounded.
- **Type consistency**:
  - WT names match across tasks: `chaos-pod-delete`, `chaos-network-loss`, `chaos-kafka-broker-partition`.
  - Parameter names match: `target_namespace`, `target_pod_selector`, `duration`, `interval`, `loss_percent`, `kafka_namespace`, `broker_id`.
  - File paths match across task headers and step bodies.
  - 10 WTs in verify-templates matches: 3 chaos + 3 fixture + load-k6-run + verdict-slo-eval + 2 util.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-05-19-01-chaos-mesh-migration.md`. Two execution options:

**1. Subagent-Driven (recommended)** — fresh subagent per task. The migration has 9 tasks, several of which involve multi-minute cluster waits (Tasks 4/5/6 each run mysql or kafka scenarios); subagents handle those in the background while preserving your context.

**2. Inline Execution** — batch with checkpoints. Terminal sits on the cluster waits.

Which approach?