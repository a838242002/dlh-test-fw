# Scenario Queue + Priority Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add per-target serialisation + priority queueing to scenario submissions, using Argo's native `spec.synchronization.semaphore` and `spec.priority` features, backed by a chart-managed ConfigMap. Surface "queued" state in `scripts/run-scenario.sh` via a one-shot probe.

**Architecture:** A new ConfigMap `dlh-scenario-locks` ships three per-target lock keys (`mysql`, `kafka`, `doris`, all count `"1"`). Each scenario YAML declares `spec.synchronization.semaphore.configMapKeyRef` against the relevant key and `spec.priority: 100`. The Argo controller (v3.5.12) handles admission and priority-aware acquisition. `scripts/run-scenario.sh` splits `argo submit --wait` into `argo submit` + `argo wait` so a one-line probe can detect and announce "queued" state between them. `--priority N` flows through via the existing `"$@"` forwarding from Plan 9.

**Tech Stack:** Argo Workflows v3.5.12 (in-cluster), Argo CLI v4.0.5, Helm v4.2.0, kubectl, bash. No new images, no new RBAC, no new make targets.

**Reference spec:** `docs/superpowers/specs/2026-05-18-scenario-queue-design.md`. Re-read the architecture diagram, the per-file changes, and the risk register before starting.

**Branch & worktree:** Per `CLAUDE.md` conventions, create a feature worktree at `../dlh-test-fw-plan11` on branch `feat/plan11-scenario-queue` before Task 2. Task 1 runs from the main worktree.

---

## File Structure

**New files:**
- `helm/dlh-test-fw/templates/scenario-locks-configmap.yaml` — ConfigMap `dlh-scenario-locks` with three per-target keys.

**Modified files:**
- `scenarios/mysql-pod-delete.yaml` — add `spec.priority` and `spec.synchronization.semaphore`.
- `scenarios/kafka-broker-partition.yaml` — same pattern, `key: kafka`.
- `scenarios/doris-be-network-loss.yaml` — same pattern, `key: doris`.
- `scripts/run-scenario.sh` — split `argo submit --wait` into `argo submit` + `argo wait` with a one-shot probe between them.

**Unchanged:** WorkflowTemplates, fixture/chaos/util/verdict templates, dashboards, fixture images, verdict-job/, RBAC, Makefile.

---

## Task 1: Baseline — verify cluster + Argo version + Plan 9 still green

This task makes no commits. It confirms the cluster supports priority-aware semaphores and that Plan 9 behaviour is intact before we layer on top.

**Files:** None modified.

Work from: `/Users/allen/repo/dlh-test-fw` (main worktree, branch `main`).

- [ ] **Step 1: Confirm clean tree + recent state**

```bash
git status
git log --first-parent --oneline -5
```

Expected: clean tree on `main`; recent commits include `497324a` (scenario queue spec) and `864e959` (README CI badge) or newer.

- [ ] **Step 2: Confirm Argo workflow-controller is v3.5.x**

```bash
kubectl -n dlh-test-fw get deploy dlh-argo-workflows-workflow-controller \
  -o jsonpath='{.spec.template.spec.containers[0].image}{"\n"}'
argo version --short
```

Expected: image tag `v3.5.12` (or any v3.5.x+). CLI `v4.0.5`. If the controller is v3.4.x or older, STOP and report — priority-aware semaphore queueing requires v3.5+ (the spec's first risk item).

- [ ] **Step 3: Confirm existing scenarios still pass**

```bash
make run-mysql
```

Use Bash timeout ≥ 600000 ms (10 min). Expected: `Final phase: Succeeded`.

```bash
make run-kafka
```

Expected: `Final phase: Succeeded`.

These confirm Plan 9 + Plan 10 baseline is intact. If either fails, fix it before adding semaphores — running a queue on top of broken scenarios is misery.

- [ ] **Step 4: Confirm no existing synchronization usage**

```bash
grep -rn 'synchronization\|semaphore\|mutex\|priority' scenarios/ helm/dlh-test-fw/files/workflowtemplates/ helm/dlh-test-fw/templates/ 2>&1
```

Expected: no matches. Greenfield — we're not stepping on prior config.

- [ ] **Step 5: Verify no commit**

```bash
git status
```

Expected: clean. No files modified in this task.

---

## Task 2: ConfigMap `dlh-scenario-locks` + worktree

**Files:**
- Create worktree at `../dlh-test-fw-plan11` on branch `feat/plan11-scenario-queue`
- Create: `helm/dlh-test-fw/templates/scenario-locks-configmap.yaml`

- [ ] **Step 1: Create the feature worktree**

From `/Users/allen/repo/dlh-test-fw`:

```bash
git worktree add ../dlh-test-fw-plan11 -b feat/plan11-scenario-queue main
cd ../dlh-test-fw-plan11
git status
```

Expected: on `feat/plan11-scenario-queue`, working tree clean.

All subsequent steps in this and later tasks operate from `/Users/allen/repo/dlh-test-fw-plan11`.

- [ ] **Step 2: Write `helm/dlh-test-fw/templates/scenario-locks-configmap.yaml`**

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

- [ ] **Step 3: Render + lint**

```bash
helm template dlh helm/dlh-test-fw | awk '/^# Source:.*scenario-locks-configmap.yaml/,/^---$/' | head -20
helm lint helm/dlh-test-fw
```

Expected: rendered output shows the ConfigMap with three keys (`mysql`, `kafka`, `doris`), each value `"1"`. Lint: `0 chart(s) failed`.

- [ ] **Step 4: Deploy to live cluster**

```bash
helm upgrade dlh helm/dlh-test-fw -n dlh-test-fw
kubectl -n dlh-test-fw get cm dlh-scenario-locks -o jsonpath='{.data}' | jq
```

Expected:

```json
{
  "doris": "1",
  "kafka": "1",
  "mysql": "1"
}
```

- [ ] **Step 5: Commit**

```bash
git add helm/dlh-test-fw/templates/scenario-locks-configmap.yaml
git commit -m "feat(chart): add dlh-scenario-locks ConfigMap (per-target semaphore keys)"
```

---

## Task 3: Add synchronization + priority to `mysql-pod-delete.yaml`; verify same-target queueing

**Files:**
- Modify: `scenarios/mysql-pod-delete.yaml`

- [ ] **Step 1: Add `spec.priority` and `spec.synchronization` to the scenario**

Edit `scenarios/mysql-pod-delete.yaml`. Find the `spec:` block. After `serviceAccountName: argo-workflow` and BEFORE `entrypoint: main`, insert two new keys (`priority` and `synchronization`). Final shape:

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
    # ===== scenario identity =====
    - { name: scenario_label,    value: mysql-pod-delete }
    ...   # everything below this line is unchanged from Plan 9
```

Do NOT touch the `arguments.parameters`, `templates`, or any other section.

- [ ] **Step 2: Confirm only one scenario file changed**

```bash
git diff --stat scenarios/
```

Expected: exactly one file shown — `scenarios/mysql-pod-delete.yaml` — with a small +N/-0 diff (≈ 6 added lines, 0 removed).

- [ ] **Step 3: Submit one mysql workflow with short timings (faster turnaround for queue testing)**

We'll deliberately shorten this run so the next test (queueing) takes minutes not 10s of minutes.

```bash
scripts/run-scenario.sh scenarios/mysql-pod-delete.yaml -p load_duration=60s -p chaos_duration=30s &
SUBMIT1_PID=$!
sleep 5
wf1=$(kubectl -n dlh-test-fw get workflow -o name \
        | grep -E 'mysql-pod-delete-[0-9]{8}-[0-9]{6}$' \
        | sort | tail -1 | sed 's|.*/||')
echo "wf1=$wf1"
```

Expected: `wf1` is set; `kubectl -n dlh-test-fw get wf $wf1` shows `Running`.

- [ ] **Step 4: Verify `wf1` holds the mysql semaphore**

```bash
kubectl -n dlh-test-fw get wf "$wf1" -o jsonpath='{.status.synchronization}' | jq
```

Expected JSON contains `.semaphore.holding[0].semaphore` ending in `/dlh-scenario-locks/mysql` and `.holders` lists `$wf1`.

- [ ] **Step 5: Submit a SECOND mysql workflow while the first is still running**

```bash
scripts/run-scenario.sh scenarios/mysql-pod-delete.yaml -p load_duration=30s -p chaos_duration=10s &
SUBMIT2_PID=$!
sleep 5
wf2=$(kubectl -n dlh-test-fw get workflow -o name \
        | grep -E 'mysql-pod-delete-[0-9]{8}-[0-9]{6}$' \
        | sort | tail -1 | sed 's|.*/||')
echo "wf2=$wf2"
# wf2 should be a DIFFERENT name from wf1 (different timestamp; if same-second collision, sleep 1 and re-submit)
[[ "$wf2" != "$wf1" ]] && echo "wf2 distinct from wf1 OK"
```

Expected: `wf2` is a new workflow name (different timestamp from `wf1`).

- [ ] **Step 6: Verify `wf2` is Pending + blocked on the mysql semaphore**

```bash
kubectl -n dlh-test-fw get wf "$wf2" -o jsonpath='{.status.phase}{"\n"}'
kubectl -n dlh-test-fw get wf "$wf2" -o jsonpath='{.status.synchronization}' | jq
```

Expected:
- `.status.phase`: `Pending`
- `.status.synchronization.semaphore.waiting[0].semaphore` ends in `/dlh-scenario-locks/mysql`
- `.status.synchronization.semaphore.holders` is empty or absent (because `wf2` is waiting, not holding)

- [ ] **Step 7: Wait for both to finish; verify second ran AFTER first**

Wait for both background `run-scenario.sh` invocations to exit:

```bash
wait $SUBMIT1_PID
echo "submit1 exit=$?"
wait $SUBMIT2_PID
echo "submit2 exit=$?"
```

Use Bash timeout ≥ 900000 ms (15 min). Expected: both exit 0.

```bash
kubectl -n dlh-test-fw get wf "$wf1" -o jsonpath='{.status.phase}: started {.status.startedAt}, finished {.status.finishedAt}{"\n"}'
kubectl -n dlh-test-fw get wf "$wf2" -o jsonpath='{.status.phase}: started {.status.startedAt}, finished {.status.finishedAt}{"\n"}'
```

Expected: both `Succeeded`. `$wf2`'s `startedAt` is at or after `$wf1`'s `finishedAt` (timestamps demonstrate serialisation, not parallelism). If `wf2` started before `wf1` finished, semaphore failed to serialise — STOP and report BLOCKED with the timestamps.

- [ ] **Step 8: Commit**

```bash
git add scenarios/mysql-pod-delete.yaml
git commit -m "feat(scenarios): mysql-pod-delete acquires dlh-scenario-locks[mysql] (priority 100)"
```

---

## Task 4: Add synchronization + priority to kafka + doris; verify cross-target parallelism

**Files:**
- Modify: `scenarios/kafka-broker-partition.yaml`
- Modify: `scenarios/doris-be-network-loss.yaml`

- [ ] **Step 1: Edit `scenarios/kafka-broker-partition.yaml`**

Find the `spec:` block. After `serviceAccountName: argo-workflow` and BEFORE `entrypoint: main`, insert:

```yaml
  priority: 100
  synchronization:
    semaphore:
      configMapKeyRef:
        name: dlh-scenario-locks
        key: kafka
```

Final shape of the top of `spec:`:

```yaml
spec:
  serviceAccountName: argo-workflow
  priority: 100
  synchronization:
    semaphore:
      configMapKeyRef:
        name: dlh-scenario-locks
        key: kafka
  entrypoint: main
  arguments:
    parameters:
    # ===== scenario identity =====
    - { name: scenario_label,    value: kafka-broker-partition }
    ...
```

- [ ] **Step 2: Edit `scenarios/doris-be-network-loss.yaml`**

Same pattern with `key: doris` and `priority: 100`. (Doris is NO-GO; this keeps the scenario YAML internally consistent for when it's revived.)

```yaml
spec:
  serviceAccountName: argo-workflow
  priority: 100
  synchronization:
    semaphore:
      configMapKeyRef:
        name: dlh-scenario-locks
        key: doris
  entrypoint: main
```

- [ ] **Step 3: Submit mysql AND kafka in parallel; verify both Running, no blocking**

```bash
scripts/run-scenario.sh scenarios/mysql-pod-delete.yaml -p load_duration=60s -p chaos_duration=30s &
PID_MYSQL=$!
scripts/run-scenario.sh scenarios/kafka-broker-partition.yaml -p load_duration=60s -p chaos_duration=30s &
PID_KAFKA=$!
sleep 8

wf_mysql=$(kubectl -n dlh-test-fw get workflow -o name \
             | grep -E 'mysql-pod-delete-[0-9]{8}-[0-9]{6}$' \
             | sort | tail -1 | sed 's|.*/||')
wf_kafka=$(kubectl -n dlh-test-fw get workflow -o name \
             | grep -E 'kafka-broker-partition-[0-9]{8}-[0-9]{6}$' \
             | sort | tail -1 | sed 's|.*/||')
echo "wf_mysql=$wf_mysql"
echo "wf_kafka=$wf_kafka"

kubectl -n dlh-test-fw get wf "$wf_mysql" -o jsonpath='{.status.phase}{"\n"}'
kubectl -n dlh-test-fw get wf "$wf_kafka" -o jsonpath='{.status.phase}{"\n"}'
```

Expected: both `Running`. Neither in `Pending`. They're on different semaphore keys (mysql vs kafka) → no contention.

- [ ] **Step 4: Verify both hold their own semaphores**

```bash
kubectl -n dlh-test-fw get wf "$wf_mysql" -o jsonpath='{.status.synchronization.semaphore.holding[0].semaphore}{"\n"}'
kubectl -n dlh-test-fw get wf "$wf_kafka" -o jsonpath='{.status.synchronization.semaphore.holding[0].semaphore}{"\n"}'
```

Expected: first ends `/dlh-scenario-locks/mysql`, second ends `/dlh-scenario-locks/kafka`.

- [ ] **Step 5: Wait for both to finish**

```bash
wait $PID_MYSQL ; echo "mysql exit=$?"
wait $PID_KAFKA ; echo "kafka exit=$?"
```

Use Bash timeout ≥ 900000 ms (15 min). Expected: both exit 0.

```bash
kubectl -n dlh-test-fw get wf "$wf_mysql" -o jsonpath='{.status.phase}{"\n"}'
kubectl -n dlh-test-fw get wf "$wf_kafka" -o jsonpath='{.status.phase}{"\n"}'
```

Expected: both `Succeeded`.

- [ ] **Step 6: Dry-run doris scenario (NOT live; Doris target is NO-GO)**

```bash
kubectl create --dry-run=client -f scenarios/doris-be-network-loss.yaml -o yaml | grep -A6 '^spec:' | head -12
```

Expected: dry-run prints YAML; `spec.priority: 100` and `spec.synchronization.semaphore.configMapKeyRef.key: doris` both visible in the output. No schema errors.

- [ ] **Step 7: Commit**

```bash
git add scenarios/kafka-broker-partition.yaml scenarios/doris-be-network-loss.yaml
git commit -m "feat(scenarios): kafka + doris acquire per-target dlh-scenario-locks keys"
```

---

## Task 5: Add one-shot queue probe to `run-scenario.sh`

**Files:**
- Modify: `scripts/run-scenario.sh`

- [ ] **Step 1: Read current `scripts/run-scenario.sh` (so the edit is precise)**

The Plan 9 form uses `argo submit ... --wait` as one call. We're splitting it into `argo submit` + `argo wait` so a probe can run between them.

- [ ] **Step 2: Rewrite the script**

Overwrite `scripts/run-scenario.sh` with this exact content:

```bash
#!/usr/bin/env bash
# Submit a scenario Workflow and wait for it to finish.
#
# Replaces metadata.generateName: <prefix>- with metadata.name: <prefix>-YYYYMMDD-HHMMSS
# so the run is sortable + easy to find in `kubectl get workflow` and Grafana.
#
# Usage:
#   scripts/run-scenario.sh scenarios/<name>.yaml [argo-submit-args...]
#
# Examples:
#   scripts/run-scenario.sh scenarios/mysql-pod-delete.yaml
#   scripts/run-scenario.sh scenarios/mysql-pod-delete.yaml -p vus=50 -p mysql_op_mix=read:100
#   scripts/run-scenario.sh scenarios/mysql-pod-delete.yaml --priority 200
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: $0 scenarios/<name>.yaml [argo-submit-args...]" >&2
  exit 2
fi

file=$1; shift

prefix=$(awk '/^[[:space:]]*generateName:/ { sub(/.*generateName: */, ""); sub(/-$/, ""); print; exit }' "$file")
if [[ -z "$prefix" ]]; then
  echo "error: $file has no metadata.generateName line to derive a prefix from" >&2
  exit 1
fi
ts=$(date -u +%Y%m%d-%H%M%S)
name="${prefix}-${ts}"

rendered=$(mktemp)
trap 'rm -f "$rendered"' EXIT
sed "s|^\([[:space:]]*\)generateName:.*|\1name: ${name}|" "$file" > "$rendered"

echo "Submitting workflow: $name"
argo submit -n dlh-test-fw "$rendered" "$@" >/dev/null

# One-shot probe: if the workflow is queued behind a semaphore, surface it.
# (Sleep gives the controller a moment to annotate .status.synchronization.)
sleep 2
phase=$(kubectl -n dlh-test-fw get workflow "$name" -o jsonpath='{.status.phase}' 2>/dev/null || echo "")
if [[ "$phase" == "Pending" ]]; then
  blocked=$(kubectl -n dlh-test-fw get workflow "$name" \
              -o jsonpath='{.status.synchronization.semaphore.waiting[0].semaphore}' 2>/dev/null || echo "")
  if [[ -n "$blocked" ]]; then
    prio=$(kubectl -n dlh-test-fw get workflow "$name" \
             -o jsonpath='{.spec.priority}' 2>/dev/null || echo "default")
    echo "Queued: waiting for semaphore ${blocked} (priority ${prio})"
  fi
fi

argo wait -n dlh-test-fw "$name" || true
status=$(kubectl -n dlh-test-fw get workflow "$name" -o jsonpath='{.status.phase}')
echo "Final phase: $status"
echo "Report artifact: argo get -n dlh-test-fw $name  # see artifact section, or:"
echo "                 kubectl -n dlh-test-fw exec deploy/dlh-minio -- mc cat \"local/artifacts/${name}/${name}-main-*/verdict/report.json\" | jq ."
[[ "$status" == "Succeeded" ]]
```

Key changes vs Plan 9 form:
- `argo submit ... "$@"` (no `--wait`) replaces `argo submit ... --wait "$@"`.
- New probe block between submit and wait.
- `argo wait` replaces the implicit wait that was inside `argo submit --wait`.

- [ ] **Step 3: Sanity check — no-queue path still works**

```bash
make run-mysql
```

Use Bash timeout ≥ 600000 ms (10 min). Expected: standard output with `Submitting workflow: mysql-pod-delete-...`, then NO `Queued:` line (because nothing else is running), then `Final phase: Succeeded`.

- [ ] **Step 4: Queue-path test — submit one mysql, then a second mysql, verify the second prints the queued message**

```bash
# First in background (deliberately long-ish so we have time to submit a second).
scripts/run-scenario.sh scenarios/mysql-pod-delete.yaml -p load_duration=90s -p chaos_duration=30s &
PID1=$!
sleep 4   # let the first acquire the semaphore

# Second, capturing its stdout. This one should print "Queued: ..."
OUT=$(scripts/run-scenario.sh scenarios/mysql-pod-delete.yaml -p load_duration=30s -p chaos_duration=10s)
echo "--- second invocation output ---"
echo "$OUT"

wait $PID1
echo "$OUT" | grep -E 'Queued: waiting for semaphore .*/mysql \(priority 100\)' && echo "Queue message verified"
```

Use Bash timeout ≥ 900000 ms (15 min) for the whole step. Expected: the `grep` matches; the `OUT` variable contains both `Submitting workflow:`, `Queued: waiting for semaphore .../dlh-scenario-locks/mysql (priority 100)`, and `Final phase: Succeeded`.

If `Queued:` line is missing, the most likely cause is the 2-second sleep being too short — bump to `sleep 3` in the script and retry. Document the bump if needed.

- [ ] **Step 5: Commit**

```bash
git add scripts/run-scenario.sh
git commit -m "feat(scripts): one-shot probe in run-scenario.sh — print Queued message when semaphore-blocked"
```

---

## Task 6: Verify priority override end-to-end

**Files:** None modified. This is a verification task.

- [ ] **Step 1: Submit three mysql runs with deliberate priorities; first holds the slot**

We'll use short timings so all three finish within ~5 min, and we'll submit them rapidly so the queueing order is predictable.

```bash
# Slot holder (priority 100, default): this one starts immediately.
scripts/run-scenario.sh scenarios/mysql-pod-delete.yaml \
    -p load_duration=120s -p chaos_duration=60s &
PID_A=$!
sleep 4

# Two queued behind A, with explicit priorities.
scripts/run-scenario.sh scenarios/mysql-pod-delete.yaml --priority 50 \
    -p load_duration=30s -p chaos_duration=10s &
PID_B=$!
sleep 1

scripts/run-scenario.sh scenarios/mysql-pod-delete.yaml --priority 200 \
    -p load_duration=30s -p chaos_duration=10s &
PID_C=$!
sleep 4

# Identify the three workflows by name (latest 3 mysql-pod-delete- runs).
mapfile -t wfs < <(kubectl -n dlh-test-fw get workflow -o name \
                     | grep -E 'mysql-pod-delete-[0-9]{8}-[0-9]{6}$' \
                     | sort | tail -3 | sed 's|.*/||')
echo "wf A (priority 100): ${wfs[0]}"
echo "wf B (priority 50):  ${wfs[1]}"
echo "wf C (priority 200): ${wfs[2]}"
```

Note: `wfs[0]` is the oldest (=A), `wfs[2]` is the newest (=C). Confirm via priority:

```bash
for w in "${wfs[@]}"; do
  prio=$(kubectl -n dlh-test-fw get wf "$w" -o jsonpath='{.spec.priority}')
  echo "$w priority=$prio"
done
```

Expected: oldest is `100`, middle is `50`, newest is `200`.

- [ ] **Step 2: While A is running, check B and C are Pending and the queue order**

```bash
sleep 5
for w in "${wfs[@]}"; do
  phase=$(kubectl -n dlh-test-fw get wf "$w" -o jsonpath='{.status.phase}')
  prio=$(kubectl -n dlh-test-fw get wf "$w" -o jsonpath='{.spec.priority}')
  echo "$w  phase=$phase  priority=$prio"
done
```

Expected: A is `Running`, B and C are `Pending`.

- [ ] **Step 3: Wait for all three to finish**

```bash
wait $PID_A; echo "A exit=$?"
wait $PID_C; echo "C exit=$?"
wait $PID_B; echo "B exit=$?"
```

Use Bash timeout ≥ 1200000 ms (20 min). The wait order here doesn't matter — `wait <pid>` blocks until that specific child exits regardless of the order they actually finish.

Expected: all three exit 0.

- [ ] **Step 4: Verify the run order matches priority (C ran second, B ran third)**

```bash
for w in "${wfs[@]}"; do
  prio=$(kubectl -n dlh-test-fw get wf "$w" -o jsonpath='{.spec.priority}')
  start=$(kubectl -n dlh-test-fw get wf "$w" -o jsonpath='{.status.startedAt}')
  finish=$(kubectl -n dlh-test-fw get wf "$w" -o jsonpath='{.status.finishedAt}')
  echo "$w  priority=$prio  started=$start  finished=$finish"
done
```

Expected:
- A (priority 100) started first, finished first.
- C (priority 200) started SECOND (immediately after A finished — priority 200 beat priority 50).
- B (priority 50) started LAST.

The startedAt ordering must be A < C < B (chronologically). If C started after B, priority queueing failed — STOP and report BLOCKED with the timestamps.

- [ ] **Step 5: No commit**

This task is verification; no source changes. Proceed.

---

## Task 7: FINDINGS update + merge to main + cleanup + tag

**Files:**
- Modify: `docs/FINDINGS.md` (append a Plan 11 section).

- [ ] **Step 1: Append Plan 11 section to `docs/FINDINGS.md`**

Append at the end of the file:

```markdown
## Plan 11 — Scenario queue + priority (2026-05-18)

- Per-target serialisation lives in ConfigMap `dlh-scenario-locks` with keys
  `mysql`, `kafka`, `doris`, each value `"1"` (max concurrent workflows per
  key). Raise counts in the ConfigMap if a target gains capacity; no
  scenario-side change required.
- Each scenario declares `spec.priority: 100` (default) and
  `spec.synchronization.semaphore.configMapKeyRef` against its target's key.
  Different-target scenarios run in parallel; same-target serialises.
- Argo controller v3.5.12 in-cluster handles priority-aware acquisition
  order: (priority desc, creationTimestamp asc). v3.4.x and older are FIFO
  only — pin the chart's argo-workflows subchart ≥ 3.5.
- Submit-time priority override flows through `scripts/run-scenario.sh`
  via `"$@"` forwarding (Plan 9 contract). Example:
  `scripts/run-scenario.sh scenarios/mysql-pod-delete.yaml --priority 200`.
- `run-scenario.sh` split: `argo submit` + `argo wait` with a 2-second
  sleep + one-shot probe between them. The probe prints
  `Queued: waiting for semaphore <name> (priority N)` exactly when the
  workflow is `Pending` with `.status.synchronization.semaphore.waiting`
  populated. Silent otherwise.
- Pitfall: the 2-second probe sleep is a heuristic for the controller's
  annotation latency. If observed to be too short under load, bump it
  (verified locally as sufficient at v3.5.12).
- Cluster usage: queued workflows sit in `Pending` and consume zero pod
  resources — only controller bookkeeping. Long queues are safe up to
  ~20 entries; revisit at higher fan-in.
```

- [ ] **Step 2: Final suite re-run for the merge log**

```bash
make run-mysql
make run-kafka
./scripts/verify-templates.sh
```

Sequential. Use Bash timeout ≥ 900000 ms (15 min) per `make` command. Expected:
- mysql Succeeded
- kafka Succeeded
- `PASS: all 11 WorkflowTemplates present`

- [ ] **Step 3: Commit FINDINGS update**

```bash
git add docs/FINDINGS.md
git commit -m "docs(findings): record Plan 11 scenario queue + priority"
```

- [ ] **Step 4: Merge to main with --no-ff**

From the **main worktree**:

```bash
cd /Users/allen/repo/dlh-test-fw
git status            # should be clean
git checkout main
git pull origin main  # in case main moved
git merge --no-ff feat/plan11-scenario-queue -m "$(cat <<'EOF'
Merge feat/plan11-scenario-queue: per-target scenario serialisation + priority

Plan 11 adds Argo native semaphore-based queueing to the scenario catalog.
Each scenario YAML acquires a per-target slot from ConfigMap
dlh-scenario-locks (keys mysql/kafka/doris, all count=1). Different-target
scenarios run in parallel; same-target submissions serialise.

- spec.priority: 100 default in every scenario; --priority N at submit time
  overrides (Argo CLI native flag, flows through Plan 9's "$@" forwarding).
- Argo v3.5.12 in-cluster: priority-aware queue (priority desc,
  creationTimestamp asc).
- scripts/run-scenario.sh now splits argo submit + argo wait so a one-shot
  probe can print "Queued: waiting for semaphore <name> (priority N)" when
  the submitted workflow is blocked. Silent when not blocked.
- Verified: same-target serialisation (two mysql runs), different-target
  parallelism (mysql + kafka), priority order (200 > 100 > 50 across three
  queued mysql runs).
- Doris scenario stays NO-GO but YAML shape is updated for parity.
EOF
)"
git log --first-parent --oneline -6
```

Expected: merge commit at top; `--first-parent` log shows Plan 11 as one merge boundary.

- [ ] **Step 5: Push everything**

```bash
git push origin main
```

Expected: push succeeds. The push to main also fires the GitHub Actions CI workflow (Plan 10) — watch it briefly:

```bash
sleep 10
gh run list --branch main --limit 2
```

Expected: latest run on main is `in_progress` or `success`. If `failure`, STOP and report — main is red.

- [ ] **Step 6: Clean up worktree + branch**

```bash
git worktree remove ../dlh-test-fw-plan11
git push origin --delete feat/plan11-scenario-queue 2>/dev/null || true
git branch -d feat/plan11-scenario-queue
git worktree list
```

Expected: only the main worktree remains; remote branch deleted (no-op if it was never pushed); local branch deleted.

- [ ] **Step 7: Tag**

```bash
git tag plan11-scenario-queue
git push origin plan11-scenario-queue
git log --first-parent --oneline -8
```

Expected: tag at the merge commit; push succeeds.

---

## Self-Review notes (author check, fresh-eyes pass)

- Spec section "Goals (in scope)" 1–5: covered by Task 3 (goal 1 — same-target serialisation), Task 4 (goal 2 — different-target parallelism), Tasks 3+4 (goal 3 — declared in YAML), Task 6 (goal 4 — submit-time override), Task 5 (goal 5 — queued UX line).
- Spec section "Architecture": Task 2 ships the CM; Tasks 3+4 wire the scenarios; Task 5 wires the script. All four files in the file-summary table get touched exactly once.
- Spec section "Per-file" YAMLs match the plan tasks' insertion content verbatim (priority + synchronization block at the same position under `spec:`).
- Spec section "Testing": each row mapped to a step in Tasks 2–6 — ConfigMap renders (Task 2 Step 3), live CM (Task 2 Step 4), same-target serialisation (Task 3 Steps 5–7), different-target parallelism (Task 4 Steps 3–5), priority ordering (Task 6 Steps 1–4), submit-time override (Task 6 implicit in priority test), queued-message UX (Task 5 Step 4).
- Spec section "Success criteria" 1–7: all verified inside Tasks 2–6.
- Spec section "Risks":
  - Argo v3.5 priority semantics — verified in Task 1 Step 2.
  - `argo submit --priority` precedence — implicit in Task 6 (priority 200 from CLI overrides YAML 100).
  - Sleep-based probe is racy — Task 5 Step 4 explicitly says to bump to `sleep 3` if needed.
  - `argo wait` vs `argo submit --wait` exit code drift — relied on Plan 9 baseline + Task 5 Step 3 sanity check.
  - ConfigMap key naming — accepted as-is; future scenarios pick their own key.
  - Doris NO-GO — Task 4 Step 6 dry-run only.
- Placeholder scan: no TBD/TODO. Conditional behaviour ("if the queued message is missing, bump sleep") is explicit, not deferred.
- Type consistency:
  - ConfigMap name `dlh-scenario-locks` used identically in spec, CM template, all three scenarios, and the merge commit body.
  - Keys `mysql`/`kafka`/`doris` consistent across CM data and per-scenario `key:` field.
  - Priority field `spec.priority` (numeric) consistent across all scenarios and CLI override.
  - Script field `.status.synchronization.semaphore.waiting[0].semaphore` used in both probe code (Task 5) and verification commands (Task 3 Step 6, Task 4 Step 4). Holders vs waiters distinguished correctly: holders for the running workflow, waiters for the queued one.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-05-18-03-scenario-queue.md`. Two execution options:

**1. Subagent-Driven (recommended)** — fresh subagent per task. Tasks 3, 4, and 6 each spend 10–20 minutes waiting on cluster workflows; subagents handle that politely in the background while preserving your own context.

**2. Inline Execution** — batch execution with checkpoints. Faster setup but your terminal sits on cluster waits.

Which approach?
