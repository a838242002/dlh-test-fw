# Plan 7 — Scripts + WT Migration + Scenario Rewrites Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Migrate the load step of every scenario from the `grafana/k6:0.50.0` + per-scenario k6 ConfigMap pattern to `ghcr.io/dlh/dlh-k6:0.1.0` (built in Plan 6) running the baked-in `/scripts/runners/<type>.js` against real MySQL, Kafka, and (if viable) Doris targets — driven by per-scenario env vars instead of per-scenario JS files.

**Architecture:** Change the `load/k6-run` WorkflowTemplate from a single-resource template to a two-step template: step 1 writes a per-workflow `dlh-k6-env-<workflow>` ConfigMap from the `env_map` parameter, step 2 creates the TestRun referencing that ConfigMap via `envFrom`. Three existing scenarios (mysql-pod-delete, kafka-broker-partition, doris-be-network-loss) are rewritten to pass `script_path` + `env_map` instead of `script_configmap`, with SLO queries updated to use the custom `dlh_<type>_*` metric names emitted by the new runner library. Doris is a time-boxed go/no-go inside this plan.

**Tech Stack:** Argo Workflows v0.42.7, k6-operator 4.4.1, k6 v1.6.1 (in `dlh-k6:0.1.0`), Helm 3, kubectl, bash. No new Go / no new container images.

**Prerequisites:**
- Plan 6 merged — `ghcr.io/dlh/dlh-k6:0.1.0` exists locally and is loaded into minikube. Verify with `minikube ssh -- docker images | grep dlh-k6`.
- `fixture-images/k6/lib/{common,mysql,kafka,doris}.js` and `fixture-images/k6/runners/{mysql,kafka,doris}.js` baked into the image at `/scripts/lib/` and `/scripts/runners/`.
- Minikube + platform release `dlh` are up in `dlh-test-fw` namespace.
- `targets/mysql/deploy.yaml` and `targets/kafka/deploy.yaml` exist (Plan 5 committed them); their target namespaces are `mysql-sys` and `kafka-sys`.
- Working in worktree `/Users/allen/repo/dlh-test-fw-phase2` on branch `feat/phase-2-scripts-dashboards`.

**Out of scope (Plan 8 owns):**
- Per-type Grafana dashboards (`dlh-mysql`, `dlh-kafka`, `dlh-doris`)
- Any changes to `dashboards/grafana/dlh-{run-detail,history}.json` (those keep working with the new metric names; Plan 8 adds the type-specific dashboards)
- Cross-linking between dashboards

---

## File structure

```
helm/dlh-test-fw/files/workflowtemplates/load/
└── k6-run.yaml                  ← MODIFIED: steps template (write-env + run-testrun), new param shape

scenarios/
├── mysql-pod-delete.yaml        ← MODIFIED: real MySQL via runners/mysql.js, env_map, SLO queries updated
├── kafka-broker-partition.yaml  ← MODIFIED: real Kafka via runners/kafka.js, env_map, SLO queries updated
├── doris-be-network-loss.yaml   ← MODIFIED if Task 4 = GO; touched only with README pointer if NO-GO
└── README.md                    ← MODIFIED: updated run instructions

scenarios/
├── mysql-pod-delete-k6-script.yaml      ← DELETED (script in image)
├── kafka-broker-partition-k6-script.yaml ← DELETED
└── doris-be-network-loss-k6-script.yaml ← DELETED

targets/
├── mysql/      ← verified, no changes expected
├── kafka/      ← verified, no changes expected
└── doris/      ← Task 4 either adds a deploy.yaml (GO) or refreshes README (NO-GO)

Makefile                         ← MODIFIED: add `run-kafka` (and `run-doris` if Task 4 = GO) targets

docs/FINDINGS.md  ← APPENDED: Plan 7 outcomes for Plan 8 consumption
```

Responsibilities at a glance:
- **`load/k6-run.yaml`** is the single contract change for every scenario; rewriting it once removes per-scenario k6 boilerplate.
- **Scenarios** keep their existing 4-stage shape (prep-slo → fixture → chaos+load → verdict); only the `load` step's arguments and the `write-slo` SLO queries change.
- **Doris go/no-go** is contained in Task 4 — its outcome decides whether Tasks 8/13 ship real Doris or just document it.

---

## Task 1: Empirical check — does k6 custom-Trend prom-rw emit `_p95` gauges?

This is the spec §"Open behavioural question" verification. The result decides PromQL shape for both this plan's SLO queries AND Plan 8's dashboards.

**Files:**
- No code changes — verification only. If the result is "NO", a comment is added in Task 5 to document the fallback chosen.

The hypothesis from the spec: `K6_PROMETHEUS_RW_TREND_STATS=p(95),p(99),min,max,avg` (set in `load/k6-run` WT) applies to every Trend metric, not just k6 built-ins. If yes, our custom `dlh_mysql_query_duration_seconds` Trend produces a `dlh_mysql_query_duration_seconds_p95` gauge in VM. If no, we'd need to compute averages from `_count` + `_sum`.

- [ ] **Step 1: Run a 30s probe scenario from outside Argo to test in isolation**

```bash
cd /Users/allen/repo/dlh-test-fw-phase2
cat <<'EOF' | kubectl -n dlh-test-fw apply -f -
apiVersion: k6.io/v1alpha1
kind: TestRun
metadata:
  name: trend-probe
spec:
  parallelism: 1
  script:
    localFile: /scripts/lib/smoke.js  # any baked-in script that runs default()
  arguments: >-
    --tag dlh_scenario=trend-probe
    --tag dlh_workflow=trend-probe
    --out experimental-prometheus-rw
  runner:
    image: ghcr.io/dlh/dlh-k6:0.1.0
    imagePullPolicy: Never
    env:
    - name: K6_PROMETHEUS_RW_SERVER_URL
      value: http://dlh-victoria-metrics-single-server.dlh-test-fw.svc.cluster.local:8428/api/v1/write
    - name: K6_PROMETHEUS_RW_PUSH_INTERVAL
      value: "3s"
    - name: K6_PROMETHEUS_RW_TREND_STATS
      value: "p(95),p(99),min,max,avg"
EOF
```

Wait for it to finish (smoke.js runs `iterations: 1` then exits):

```bash
kubectl -n dlh-test-fw wait --for=jsonpath='{.status.stage}'=finished testrun/trend-probe --timeout=120s
```

Expected: `testrun.k6.io/trend-probe condition met` within ~30s.

The smoke script only uses built-in counters, so this probe by itself doesn't fully answer. Skip ahead to step 2 for a probe that exercises a custom Trend.

- [ ] **Step 2: Write a custom-Trend probe script as a ConfigMap and re-run**

```bash
cat <<'EOF' | kubectl -n dlh-test-fw apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: trend-stat-probe
data:
  script.js: |
    import { Trend, Counter } from 'k6/metrics';
    const t = new Trend('dlh_probe_duration_seconds', true);
    const c = new Counter('dlh_probe_ops_total');
    export const options = { vus: 1, iterations: 50, tags: { dlh_scenario: 'trend-stat-probe' } };
    export default function () {
      const t0 = Date.now() / 1000;
      // simulate work — random sleep 1-10ms
      const ms = 1 + Math.random() * 9;
      const start = Date.now();
      while (Date.now() - start < ms) {}
      t.add(Date.now() / 1000 - t0);
      c.add(1);
    }
---
apiVersion: k6.io/v1alpha1
kind: TestRun
metadata:
  name: trend-stat-probe
spec:
  parallelism: 1
  script:
    configMap: { name: trend-stat-probe, file: script.js }
  arguments: >-
    --tag dlh_scenario=trend-stat-probe
    --tag dlh_workflow=trend-stat-probe
    --out experimental-prometheus-rw
  runner:
    image: ghcr.io/dlh/dlh-k6:0.1.0
    imagePullPolicy: Never
    env:
    - { name: K6_PROMETHEUS_RW_SERVER_URL, value: "http://dlh-victoria-metrics-single-server.dlh-test-fw.svc.cluster.local:8428/api/v1/write" }
    - { name: K6_PROMETHEUS_RW_PUSH_INTERVAL, value: "3s" }
    - { name: K6_PROMETHEUS_RW_TREND_STATS, value: "p(95),p(99),min,max,avg" }
EOF
```

Wait for finish:

```bash
kubectl -n dlh-test-fw wait --for=jsonpath='{.status.stage}'=finished testrun/trend-stat-probe --timeout=120s
```

- [ ] **Step 3: Query VM for the produced series**

```bash
PW=$(kubectl -n dlh-test-fw get secret grafana-admin-credentials -o jsonpath='{.data.admin-password}' | base64 -d)
kubectl -n dlh-test-fw port-forward svc/dlh-grafana 3000:80 >/tmp/gf.log 2>&1 &
PF=$!
sleep 3
echo "=== dlh_probe_* metric names in VM ==="
curl -s -u "admin:${PW}" -G "http://127.0.0.1:3000/api/datasources/proxy/uid/VictoriaMetrics/api/v1/label/__name__/values" | jq -r '.data[] | select(startswith("dlh_probe"))'
kill $PF
```

Expected (HYPOTHESIS YES):
```
dlh_probe_duration_seconds_avg
dlh_probe_duration_seconds_max
dlh_probe_duration_seconds_min
dlh_probe_duration_seconds_p95
dlh_probe_duration_seconds_p99
dlh_probe_ops_total
```

If you see `_p95` / `_p99` / `_avg` / `_max` / `_min` for `dlh_probe_duration_seconds`, the hypothesis is confirmed — use gauge form in Plan 7 SLO queries and Plan 8 dashboards.

If you only see `dlh_probe_duration_seconds_count` and `dlh_probe_duration_seconds_sum` (and `dlh_probe_ops_total`), the hypothesis is wrong — fallback queries must use `rate(<metric>_sum[w]) / rate(<metric>_count[w])` for averages and there is no exact `_p95` available.

- [ ] **Step 4: Record the outcome in FINDINGS.md (committed)**

Open `docs/FINDINGS.md` and append after the last section:

If HYPOTHESIS YES:

```markdown

## Plan 7 Task 1: k6 custom-Trend prom-rw emits `_p95` gauges (2026-05-17)

Verified empirically on `dlh-k6:0.1.0` (k6 v1.6.1) with
`K6_PROMETHEUS_RW_TREND_STATS=p(95),p(99),min,max,avg`. A custom
`new Trend('dlh_probe_duration_seconds', true)` produced the full
gauge family in VM:

    dlh_probe_duration_seconds_p95
    dlh_probe_duration_seconds_p99
    dlh_probe_duration_seconds_avg
    dlh_probe_duration_seconds_min
    dlh_probe_duration_seconds_max

**Implication:** Plan 7 SLO queries and Plan 8 dashboards use the
gauge form (`<metric>_p95`) directly — no `histogram_quantile()` needed,
no fallback to `rate(_sum)/rate(_count)`.
```

Otherwise (HYPOTHESIS NO — adjust the section text to describe the actual
observed series and the chosen fallback: `rate(<metric>_sum[30s]) / rate(<metric>_count[30s])`
for averages; for true percentiles consider Counter-based explicit histograms
or accept averages as an approximation).

- [ ] **Step 5: Tear down the probe resources**

```bash
kubectl -n dlh-test-fw delete testrun trend-probe trend-stat-probe --ignore-not-found
kubectl -n dlh-test-fw delete cm trend-stat-probe --ignore-not-found
```

- [ ] **Step 6: Commit FINDINGS update**

```bash
cd /Users/allen/repo/dlh-test-fw-phase2
git add docs/FINDINGS.md
git commit -m "findings: Plan 7 task 1 — k6 custom-Trend prom-rw emits _p95 (verified)"
```

---

## Task 2: Verify MySQL target is reachable from the cluster

**Files:**
- Verify-only; no changes expected. If the target isn't deployed yet, apply `targets/mysql/deploy.yaml`.

- [ ] **Step 1: Check namespace + pod state**

```bash
kubectl get ns mysql-sys 2>&1
kubectl -n mysql-sys get pod,svc 2>&1
```

Expected if already deployed: `mysql` Deployment 1/1 Ready, `mysql` Service on port 3306. If you see `Error from server (NotFound): namespaces "mysql-sys" not found`, proceed to Step 2; otherwise jump to Step 3.

- [ ] **Step 2: Apply the MySQL target (if missing)**

```bash
cd /Users/allen/repo/dlh-test-fw-phase2
kubectl apply -f targets/mysql/deploy.yaml
kubectl -n mysql-sys rollout status deploy/mysql --timeout=180s
```

Expected: `deployment "mysql" successfully rolled out`.

- [ ] **Step 3: Verify in-cluster reachability with the runner image**

```bash
kubectl -n dlh-test-fw run mysql-probe --rm -i --restart=Never \
  --image=ghcr.io/dlh/dlh-k6:0.1.0 --image-pull-policy=Never \
  --env MYSQL_DSN='root:dlh-mysql-dev@tcp(mysql.mysql-sys.svc.cluster.local:3306)/dlh' \
  --env DURATION=5s --env VUS=1 --env MYSQL_OP_MIX=read:100 \
  --command -- /usr/bin/k6 run /scripts/runners/mysql.js 2>&1 | tail -25
```

Expected: a k6 1-VU / 5s run summary. The runner connects to MySQL, runs `SELECT NOW()` per iteration, prints `iterations: <N>` (any N > 0) and exits 0. If you see `connect: connection refused` or DNS resolution errors, the target isn't reachable — fix before continuing.

- [ ] **Step 4: No commit — verification only.**

---

## Task 3: Verify Kafka target is reachable from the cluster

**Files:**
- Verify-only; no changes expected.

- [ ] **Step 1: Check namespace + pod state**

```bash
kubectl get ns kafka-sys 2>&1
kubectl -n kafka-sys get pod,svc 2>&1
```

Expected: `kafka-0` StatefulSet pod 1/1 Ready, `kafka` Service on 9092, `kafka-headless` Service on 9092. If missing, Step 2; otherwise Step 3.

- [ ] **Step 2: Apply the Kafka target (if missing)**

```bash
cd /Users/allen/repo/dlh-test-fw-phase2
kubectl apply -f targets/kafka/deploy.yaml
kubectl -n kafka-sys rollout status statefulset/kafka --timeout=300s
```

Expected: `statefulset rolling update complete 1 pods at revision ...`.

- [ ] **Step 3: Verify reachability with the kafka runner — produces 5 messages and exits**

```bash
kubectl -n dlh-test-fw run kafka-probe --rm -i --restart=Never \
  --image=ghcr.io/dlh/dlh-k6:0.1.0 --image-pull-policy=Never \
  --env KAFKA_BOOTSTRAP='kafka.kafka-sys.svc.cluster.local:9092' \
  --env KAFKA_TOPIC=dlh-probe \
  --env KAFKA_OP=produce --env VUS=1 --env DURATION=5s \
  --command -- /usr/bin/k6 run /scripts/runners/kafka.js 2>&1 | tail -25
```

Expected: k6 1-VU / 5s run prints a summary including `dlh_kafka_messages_produced_total` and exits 0. `connect: connection refused` means the broker isn't reachable — debug before continuing.

- [ ] **Step 4: No commit — verification only.**

---

## Task 4: Doris go/no-go spike (time-boxed: 60 minutes)

**Files:**
- Either Create: `targets/doris/deploy.yaml` (and refresh `targets/doris/README.md`) on GO
- Or Modify only: `targets/doris/README.md` (record NO-GO reason) on NO-GO

This task is bounded to 60 minutes of wall time. If the FE + BE aren't both Ready and `SHOW BACKENDS` doesn't list the BE as alive within that window, declare NO-GO and Tasks 8 + 13 skip the Doris scenario rewrite (leave the existing YAML as documentation, update `scenarios/README.md` to mark it deferred).

- [ ] **Step 1: Start a 60-minute timer (wall-clock)**

```bash
date -u "+spike start: %Y-%m-%dT%H:%M:%SZ"
```

Record this. The 60-minute cap starts now.

- [ ] **Step 2: Apply a minimal single-pod Doris deploy**

Doris's arm64 story improved in late 2025 with `apache/doris:doris-all-in-one-2.1.x`. Try that first.

```bash
cat > /tmp/doris-deploy.yaml <<'EOF'
apiVersion: v1
kind: Namespace
metadata:
  name: doris-sys
---
apiVersion: v1
kind: Service
metadata:
  name: doris-fe
  namespace: doris-sys
spec:
  selector: { app: doris }
  ports:
  - { name: query,    port: 9030, targetPort: 9030 }   # MySQL protocol
  - { name: http,     port: 8030, targetPort: 8030 }   # Stream Load
  - { name: be-thrift, port: 9020, targetPort: 9020 }
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: doris
  namespace: doris-sys
  labels: { app: doris }
spec:
  replicas: 1
  selector: { matchLabels: { app: doris } }
  template:
    metadata:
      labels: { app: doris }
    spec:
      containers:
      - name: doris
        image: apache/doris:doris-all-in-one-2.1.7
        env:
        - { name: FE_SERVERS, value: "fe1:127.0.0.1:9010" }
        - { name: FE_ID, value: "1" }
        ports:
        - containerPort: 8030
        - containerPort: 9030
        - containerPort: 9020
        resources:
          requests: { cpu: 500m, memory: 2Gi }
          limits:   { cpu: 2,    memory: 4Gi }
        readinessProbe:
          tcpSocket: { port: 9030 }
          initialDelaySeconds: 60
          periodSeconds: 10
EOF
kubectl apply -f /tmp/doris-deploy.yaml
```

- [ ] **Step 3: Wait up to 5 minutes for the pod to reach Ready**

```bash
kubectl -n doris-sys rollout status deploy/doris --timeout=300s
```

If this times out, capture `kubectl -n doris-sys describe pod` and the most recent log tail, then DECLARE NO-GO (jump to Step 6 NO-GO branch).

- [ ] **Step 4: Probe via MySQL protocol — `SHOW BACKENDS` should list one alive backend**

```bash
kubectl -n dlh-test-fw run doris-probe --rm -i --restart=Never \
  --image=mysql:8.0 --command -- \
  mysql -hdoris-fe.doris-sys.svc.cluster.local -P9030 -uroot -e "SHOW BACKENDS\G" 2>&1 | tail -20
```

Expected on GO: one backend row with `Alive: true`. On NO-GO: connection error or `Alive: false`.

- [ ] **Step 5: Probe Stream Load — POST a single CSV row and confirm `Status: Success`**

```bash
kubectl -n dlh-test-fw run doris-probe-sl --rm -i --restart=Never \
  --image=curlimages/curl:8.8.0 --command -- sh -c "
    curl -s -u root: -H 'Expect: 100-continue' -H 'columns:id,ts' -H 'format:csv' -H 'column_separator:,' \
      -T - http://doris-fe.doris-sys.svc.cluster.local:8030/api/dlh_probe/probe/_stream_load \
      <<< '1,2026-05-17 00:00:00'" 2>&1 | tail
```

You'll likely need to `CREATE DATABASE dlh_probe` and `CREATE TABLE` first via a quick mysql shell to make this succeed; if Stream Load returns `database not found`, run:

```bash
kubectl -n dlh-test-fw run doris-init --rm -i --restart=Never --image=mysql:8.0 --command -- \
  mysql -hdoris-fe.doris-sys.svc.cluster.local -P9030 -uroot -e "
    CREATE DATABASE IF NOT EXISTS dlh_probe;
    USE dlh_probe;
    CREATE TABLE IF NOT EXISTS probe (id BIGINT, ts DATETIME) DISTRIBUTED BY HASH(id) BUCKETS 1
      PROPERTIES('replication_num' = '1');
  "
```

Then re-run the Stream Load probe.

Expected on GO: response body contains `\"Status\": \"Success\"`.

- [ ] **Step 6: Decide GO or NO-GO**

**GO** if all of: Step 3 succeeded, Step 4 showed `Alive: true`, Step 5 Stream Load `Status: Success`. **NO-GO** otherwise, or if 60min has elapsed since Step 1.

**On GO:**

```bash
mkdir -p /Users/allen/repo/dlh-test-fw-phase2/targets/doris
cp /tmp/doris-deploy.yaml /Users/allen/repo/dlh-test-fw-phase2/targets/doris/deploy.yaml
cat > /Users/allen/repo/dlh-test-fw-phase2/targets/doris/README.md <<'EOF'
# Doris target — all-in-one (Plan 7)

Single-pod Doris 2.1.7 (FE + BE in one container, apache/doris:doris-all-in-one image).

## Apply

    kubectl apply -f targets/doris/deploy.yaml
    kubectl -n doris-sys rollout status deploy/doris --timeout=300s

## Smoke

    kubectl -n dlh-test-fw run doris-probe --rm -i --restart=Never --image=mysql:8.0 --command -- \
      mysql -hdoris-fe.doris-sys.svc.cluster.local -P9030 -uroot -e "SHOW BACKENDS\G"

## Notes

Memory requests are 2 GiB; the all-in-one image needs at least 4 GiB to be
stable. On minikube ensure the cluster has headroom (`minikube ssh -- free -h`).

The `dlh_probe.probe(id BIGINT, ts DATETIME)` schema used in this plan's
spike is the default schema the doris runner expects. The Plan 7
scenario writes CSV rows in that shape.
EOF
git add targets/doris/deploy.yaml targets/doris/README.md
git commit -m "target: doris all-in-one single-pod deploy (Plan 7 spike GO)"
```

**On NO-GO:**

```bash
cd /Users/allen/repo/dlh-test-fw-phase2
# preserve the timestamped failure reason in the existing README
cat >> targets/doris/README.md <<'EOF'

## Plan 7 spike attempt (2026-05-17): NO-GO

Tried `apache/doris:doris-all-in-one-2.1.7` on minikube. Result: <fill in
one sentence — pod stuck in CrashLoopBackOff / Stream Load returned X /
BE never reported Alive within 5 min / hit 60min wall-clock cap>.

Consequence for Phase 2: scenarios/doris-be-network-loss.yaml is NOT
migrated to a real-target shape; it remains the Phase 1 stub. Plan 8
ships `dlh-doris` dashboard with the same panel layout as the others
but no live data path until a future phase brings Doris up.
EOF
kubectl delete ns doris-sys --ignore-not-found  # clean up the spike
git add targets/doris/README.md
git commit -m "target: doris spike NO-GO — Plan 7 ships MySQL+Kafka only"
```

- [ ] **Step 7: Tear down ephemeral probe resources (either branch)**

```bash
kubectl -n doris-sys delete pod/doris-probe pod/doris-probe-sl pod/doris-init --ignore-not-found 2>&1
```

---

## Task 5: Rewrite `load/k6-run` WorkflowTemplate as a two-step template

**Files:**
- Modify: `helm/dlh-test-fw/files/workflowtemplates/load/k6-run.yaml`

Replace the single-resource template with a `steps:` template. Step 1 (`write-env`) is a `script:` template that parses the `env_map` parameter (KEY=VALUE lines) into a ConfigMap named `dlh-k6-env-{{workflow.name}}`. Step 2 (`run-testrun`) is the existing `resource:` template that creates the TestRun, pinned to `dlh-k6:0.1.0`, `script.localFile: {{inputs.parameters.script_path}}`, and `runner.envFrom` referencing the per-workflow ConfigMap.

- [ ] **Step 1: Replace the file**

```bash
cd /Users/allen/repo/dlh-test-fw-phase2
cat > helm/dlh-test-fw/files/workflowtemplates/load/k6-run.yaml <<'YAML'
# load/k6-run — runs a k6 TestRun using the dlh-k6 image (Plan 6) and one of
# the baked-in /scripts/runners/<type>.js scripts. Per-scenario env vars are
# delivered via a per-workflow ConfigMap that step 1 writes from the `env_map`
# input parameter (multi-line KEY=VALUE format).
#
# Per docs/FINDINGS.md:
# - dlh_scenario tag (not k6's reserved `scenario` label).
# - Plan 7 confirmed `_p95` gauges are emitted for custom Trend metrics
#   (`dlh_<type>_*` series produced by /scripts/lib/{mysql,kafka,doris}.js).
apiVersion: argoproj.io/v1alpha1
kind: WorkflowTemplate
metadata:
  name: load-k6-run
  labels:
    dlh.category: load
spec:
  entrypoint: main
  serviceAccountName: argo-workflow
  templates:
  # ---------------------------------------------------------------
  # main — two ordered steps
  # ---------------------------------------------------------------
  - name: main
    inputs:
      parameters:
      - name: script_path             # e.g. /scripts/runners/mysql.js
      - name: vus
      - name: duration                # e.g. "180s"
      - name: env_map                 # multi-line "KEY=VALUE\nKEY=VALUE" (may be empty)
      - name: scenario_label          # used as dlh_scenario= tag on prom-rw output
    outputs:
      parameters:
      - name: metrics_namespace
        value: {{`"{{inputs.parameters.scenario_label}}"`}}
    steps:
    - - name: write-env
        template: write-env-cm
        arguments:
          parameters:
          - { name: env_map, value: {{`"{{inputs.parameters.env_map}}"`}} }
    - - name: run-testrun
        template: run-testrun
        arguments:
          parameters:
          - { name: script_path,    value: {{`"{{inputs.parameters.script_path}}"`}} }
          - { name: vus,            value: {{`"{{inputs.parameters.vus}}"`}} }
          - { name: duration,       value: {{`"{{inputs.parameters.duration}}"`}} }
          - { name: scenario_label, value: {{`"{{inputs.parameters.scenario_label}}"`}} }
  # ---------------------------------------------------------------
  # write-env-cm — creates dlh-k6-env-<workflow> from env_map
  # ---------------------------------------------------------------
  - name: write-env-cm
    inputs:
      parameters:
      - name: env_map
    script:
      image: alpine/k8s:1.30.0
      command: [bash]
      source: |
        set -euo pipefail
        ENV_CONTENT=$(cat <<'EOF'
        {{`{{inputs.parameters.env_map}}`}}
        EOF
        )
        # Strip blank lines and lines that don't look like KEY=VALUE.
        FILTERED=$(printf '%s\n' "$ENV_CONTENT" | grep -E '^[A-Z_][A-Z0-9_]*=' || true)
        # Render --from-literal flags safely (preserves = and special chars in VALUE).
        ARGS=()
        while IFS= read -r line; do
          [[ -z "$line" ]] && continue
          KEY="${line%%=*}"
          VAL="${line#*=}"
          ARGS+=( "--from-literal=${KEY}=${VAL}" )
        done <<< "$FILTERED"
        kubectl -n {{`{{workflow.namespace}}`}} create configmap \
          dlh-k6-env-{{`{{workflow.name}}`}} "${ARGS[@]}" \
          --dry-run=client -o yaml | kubectl apply -f -
  # ---------------------------------------------------------------
  # run-testrun — creates the TestRun pointing at the baked-in script
  # ---------------------------------------------------------------
  - name: run-testrun
    inputs:
      parameters:
      - name: script_path
      - name: vus
      - name: duration
      - name: scenario_label
    resource:
      action: create
      successCondition: status.stage = finished
      failureCondition: status.stage = error
      manifest: |
        apiVersion: k6.io/v1alpha1
        kind: TestRun
        metadata:
          generateName: dlh-k6-
          namespace: {{`{{workflow.namespace}}`}}
          labels:
            dlh.scenario: {{`{{inputs.parameters.scenario_label}}`}}
        spec:
          parallelism: 1
          script:
            localFile: {{`{{inputs.parameters.script_path}}`}}
          arguments: >-
            --tag dlh_scenario={{`{{inputs.parameters.scenario_label}}`}}
            --tag dlh_workflow={{`{{workflow.name}}`}}
            --out experimental-prometheus-rw
          runner:
            image: ghcr.io/dlh/dlh-k6:0.1.0
            imagePullPolicy: Never
            env:
            - name: K6_PROMETHEUS_RW_SERVER_URL
              value: {{ .Values.platform.vm.remoteWriteUrl }}
            - name: K6_PROMETHEUS_RW_PUSH_INTERVAL
              value: "5s"
            - name: K6_PROMETHEUS_RW_TREND_STATS
              value: "p(95),p(99),min,max,avg"
            - name: SCENARIO_LABEL
              value: {{`"{{inputs.parameters.scenario_label}}"`}}
            - name: VUS
              value: {{`"{{inputs.parameters.vus}}"`}}
            - name: DURATION
              value: {{`"{{inputs.parameters.duration}}"`}}
            envFrom:
            - configMapRef:
                name: dlh-k6-env-{{`{{workflow.name}}`}}
YAML
```

- [ ] **Step 2: Helm-render the chart to confirm the file parses and references resolve**

```bash
cd /Users/allen/repo/dlh-test-fw-phase2
helm dependency update helm/dlh-test-fw 2>&1 | tail -3
helm template dlh helm/dlh-test-fw -n dlh-test-fw \
  -f helm/dlh-test-fw/values.yaml -f helm/dlh-test-fw/values-minikube.yaml 2>&1 \
  | grep -A2 "name: load-k6-run" | head -10
```

Expected: prints `name: load-k6-run` and a few lines below. No "error converting YAML to JSON" or "function ... not defined".

- [ ] **Step 3: Lint the chart**

```bash
helm lint helm/dlh-test-fw 2>&1 | tail -3
```

Expected: `1 chart(s) linted, 0 chart(s) failed` (one WARNING about icon is fine).

- [ ] **Step 4: Apply the chart so the new WT is registered**

```bash
helm upgrade --install dlh helm/dlh-test-fw -n dlh-test-fw \
  -f helm/dlh-test-fw/values.yaml -f helm/dlh-test-fw/values-minikube.yaml --timeout 5m 2>&1 | tail -3
```

Expected: `STATUS: deployed`.

- [ ] **Step 5: Verify the WT updated**

```bash
kubectl -n dlh-test-fw get workflowtemplate load-k6-run -o jsonpath='{.spec.entrypoint}{"\n"}{.spec.templates[*].name}{"\n"}'
```

Expected output:
```
main
main write-env-cm run-testrun
```

- [ ] **Step 6: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw-phase2
git add helm/dlh-test-fw/files/workflowtemplates/load/k6-run.yaml
git commit -m "wt(load/k6-run): two-step template — write env CM + TestRun on dlh-k6 image"
```

---

## Task 6: Rewrite `scenarios/mysql-pod-delete.yaml`

**Files:**
- Modify: `scenarios/mysql-pod-delete.yaml`

Switch the `load` step arguments to use `script_path` and `env_map`. Update the SLO queries to use the custom `dlh_mysql_query_duration_seconds_*` series emitted by `lib/mysql.js`.

- [ ] **Step 1: Replace the scenario file**

```bash
cd /Users/allen/repo/dlh-test-fw-phase2
cat > scenarios/mysql-pod-delete.yaml <<'YAML'
apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
  generateName: mysql-pod-delete-
  namespace: dlh-test-fw
spec:
  serviceAccountName: argo-workflow
  entrypoint: main
  arguments:
    parameters:
    - { name: load_duration,     value: 180s }
    - { name: chaos_start_after, value: 30s }
    - { name: chaos_duration,    value: 60s }
    - { name: scenario_label,    value: mysql-pod-delete }
  templates:
  - name: main
    steps:
    - - name: prep-slo
        template: write-slo
    - - name: load-fixture
        templateRef: { name: fixture-minio-load-mysql, template: main }
        arguments:
          parameters:
          - { name: uri,                value: "s3://fixtures/mysql-users.sql" }
          - { name: db_host,            value: "mysql.mysql-sys.svc.cluster.local" }
          - { name: credentials_secret, value: "mysql-creds" }
    - - name: chaos
        templateRef: { name: chaos-pod-delete, template: main }
        continueOn: { failed: true }
        arguments:
          parameters:
          - { name: target_namespace,    value: "mysql-sys" }
          - { name: target_pod_selector, value: "app=mysql" }
          - { name: duration,            value: "{{workflow.parameters.chaos_duration}}" }
          - { name: interval,            value: "10" }
          - { name: force,               value: "true" }
      - name: load
        templateRef: { name: load-k6-run, template: main }
        arguments:
          parameters:
          - { name: script_path,    value: "/scripts/runners/mysql.js" }
          - { name: vus,            value: "10" }
          - { name: duration,       value: "{{workflow.parameters.load_duration}}" }
          - { name: scenario_label, value: "{{workflow.parameters.scenario_label}}" }
          - name: env_map
            value: |
              MYSQL_DSN=root:dlh-mysql-dev@tcp(mysql.mysql-sys.svc.cluster.local:3306)/dlh
              MYSQL_OP_MIX=read:70,write:30
              MYSQL_READ_SQL=SELECT NOW()
              MYSQL_WRITE_SQL=INSERT INTO dlh_load(ts) VALUES(NOW())
              MYSQL_SLEEP_MS=50
    - - name: verdict
        templateRef: { name: verdict-slo-eval, template: main }
        arguments:
          parameters:
          - { name: slo_yaml,           value: "(unused — passed via ConfigMap)" }
          - { name: chaos_result_name,  value: "{{steps.chaos.outputs.parameters.chaos_result_name}}-pod-delete" }
          - { name: load_start_ts,      value: "{{steps.load.startedAt}}" }
          - { name: chaos_start_after,  value: "{{workflow.parameters.chaos_start_after}}" }
          - { name: chaos_duration,     value: "{{workflow.parameters.chaos_duration}}" }
          - { name: load_duration,      value: "{{workflow.parameters.load_duration}}" }
          - { name: metrics_namespace,  value: "{{workflow.parameters.scenario_label}}" }
          - { name: workflow_name,      value: "{{workflow.name}}" }

  - name: write-slo
    serviceAccountName: argo-workflow
    script:
      image: alpine/k8s:1.30.0
      command: [bash]
      source: |
        set -euo pipefail
        cat > /tmp/slo.yaml <<'EOF'
        thresholds:
        - metric: p95-query-latency-chaos
          query: avg(dlh_mysql_query_duration_seconds_p95{dlh_workflow="{{workflow.name}}"})
          lt: 1.0
          window: chaos
        - metric: error-rate-recovery
          query: sum(rate(dlh_app_errors_total{kind=~"mysql.*",dlh_workflow="{{workflow.name}}"}[30s])) / clamp_min(sum(rate(dlh_mysql_query_duration_seconds_count{dlh_workflow="{{workflow.name}}"}[30s])), 1e-9)
          lt: 0.05
          window: recovery
        EOF
        kubectl -n dlh-test-fw create configmap dlh-slo-{{workflow.name}} \
          --from-file=slo.yaml=/tmp/slo.yaml --dry-run=client -o yaml | kubectl apply -f -
YAML
```

- [ ] **Step 2: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw-phase2
git add scenarios/mysql-pod-delete.yaml
git commit -m "scenario(mysql-pod-delete): real MySQL via runners/mysql.js + env_map + updated SLOs"
```

---

## Task 7: Rewrite `scenarios/kafka-broker-partition.yaml`

**Files:**
- Modify: `scenarios/kafka-broker-partition.yaml`

Same shape as Task 6, scenario-specific changes only.

- [ ] **Step 1: Replace the scenario file**

```bash
cd /Users/allen/repo/dlh-test-fw-phase2
cat > scenarios/kafka-broker-partition.yaml <<'YAML'
apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
  generateName: kafka-broker-partition-
  namespace: dlh-test-fw
spec:
  serviceAccountName: argo-workflow
  entrypoint: main
  arguments:
    parameters:
    - { name: load_duration,     value: 180s }
    - { name: chaos_start_after, value: 30s }
    - { name: chaos_duration,    value: 60s }
    - { name: scenario_label,    value: kafka-broker-partition }
  templates:
  - name: main
    steps:
    - - name: prep-slo
        template: write-slo
    # Topic creation is implicit: kafka.js's Writer has autoCreateTopic=true and
    # the apache/kafka target accepts auto-create. No fixture step needed.
    - - name: chaos
        templateRef: { name: chaos-kafka-broker-partition, template: main }
        continueOn: { failed: true }
        arguments:
          parameters:
          - { name: kafka_namespace, value: "kafka-sys" }
          - { name: broker_id,       value: "0" }
          - { name: duration,        value: "{{workflow.parameters.chaos_duration}}" }
      - name: load
        templateRef: { name: load-k6-run, template: main }
        arguments:
          parameters:
          - { name: script_path,    value: "/scripts/runners/kafka.js" }
          - { name: vus,            value: "5" }
          - { name: duration,       value: "{{workflow.parameters.load_duration}}" }
          - { name: scenario_label, value: "{{workflow.parameters.scenario_label}}" }
          - name: env_map
            value: |
              KAFKA_BOOTSTRAP=kafka.kafka-sys.svc.cluster.local:9092
              KAFKA_TOPIC=dlh-load
              KAFKA_OP=produce
              KAFKA_MESSAGE_SIZE=256
    - - name: verdict
        templateRef: { name: verdict-slo-eval, template: main }
        arguments:
          parameters:
          - { name: slo_yaml,           value: "(unused — passed via ConfigMap)" }
          - { name: chaos_result_name,  value: "{{steps.chaos.outputs.parameters.chaos_result_name}}-pod-network-partition" }
          - { name: load_start_ts,      value: "{{steps.load.startedAt}}" }
          - { name: chaos_start_after,  value: "{{workflow.parameters.chaos_start_after}}" }
          - { name: chaos_duration,     value: "{{workflow.parameters.chaos_duration}}" }
          - { name: load_duration,      value: "{{workflow.parameters.load_duration}}" }
          - { name: metrics_namespace,  value: "{{workflow.parameters.scenario_label}}" }
          - { name: workflow_name,      value: "{{workflow.name}}" }

  - name: write-slo
    serviceAccountName: argo-workflow
    script:
      image: alpine/k8s:1.30.0
      command: [bash]
      source: |
        set -euo pipefail
        cat > /tmp/slo.yaml <<'EOF'
        thresholds:
        - metric: p95-produce-latency-chaos
          query: avg(dlh_kafka_produce_duration_seconds_p95{dlh_workflow="{{workflow.name}}"})
          lt: 2.0
          window: chaos
        - metric: produce-error-rate-recovery
          query: sum(rate(dlh_app_errors_total{kind="kafka-produce",dlh_workflow="{{workflow.name}}"}[30s])) / clamp_min(sum(rate(dlh_kafka_messages_produced_total{dlh_workflow="{{workflow.name}}"}[30s])), 1e-9)
          lt: 0.10
          window: recovery
        EOF
        kubectl -n dlh-test-fw create configmap dlh-slo-{{workflow.name}} \
          --from-file=slo.yaml=/tmp/slo.yaml --dry-run=client -o yaml | kubectl apply -f -
YAML
```

- [ ] **Step 2: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw-phase2
git add scenarios/kafka-broker-partition.yaml
git commit -m "scenario(kafka-broker-partition): real Kafka via runners/kafka.js + env_map + updated SLOs"
```

---

## Task 8: Rewrite `scenarios/doris-be-network-loss.yaml` (conditional on Task 4 outcome)

**Files (Task 4 = GO):**
- Modify: `scenarios/doris-be-network-loss.yaml`

**Files (Task 4 = NO-GO):**
- Modify: `scenarios/README.md` only — mark the scenario deferred and link to `targets/doris/README.md`.

### Branch A — Task 4 was GO

- [ ] **Step 1: Replace the scenario file**

```bash
cd /Users/allen/repo/dlh-test-fw-phase2
cat > scenarios/doris-be-network-loss.yaml <<'YAML'
apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
  generateName: doris-be-network-loss-
  namespace: dlh-test-fw
spec:
  serviceAccountName: argo-workflow
  entrypoint: main
  arguments:
    parameters:
    - { name: load_duration,     value: 180s }
    - { name: chaos_start_after, value: 30s }
    - { name: chaos_duration,    value: 60s }
    - { name: scenario_label,    value: doris-be-network-loss }
  templates:
  - name: main
    steps:
    - - name: prep-slo
        template: write-slo
    - - name: prep-schema
        template: ensure-table
    - - name: chaos
        templateRef: { name: chaos-network-loss, template: main }
        continueOn: { failed: true }
        arguments:
          parameters:
          - { name: target_namespace,    value: "doris-sys" }
          - { name: target_pod_selector, value: "app=doris" }
          - { name: loss_percent,        value: "30" }
          - { name: duration,            value: "{{workflow.parameters.chaos_duration}}" }
      - name: load
        templateRef: { name: load-k6-run, template: main }
        arguments:
          parameters:
          - { name: script_path,    value: "/scripts/runners/doris.js" }
          - { name: vus,            value: "5" }
          - { name: duration,       value: "{{workflow.parameters.load_duration}}" }
          - { name: scenario_label, value: "{{workflow.parameters.scenario_label}}" }
          - name: env_map
            value: |
              DORIS_FE_HOST=doris-fe.doris-sys.svc.cluster.local
              DORIS_FE_PORT=8030
              DORIS_QUERY_PORT=9030
              DORIS_DB=dlh_probe
              DORIS_TABLE=probe
              DORIS_USER=root
              DORIS_OP=stream_load
              DORIS_BATCH_ROWS=500
    - - name: verdict
        templateRef: { name: verdict-slo-eval, template: main }
        arguments:
          parameters:
          - { name: slo_yaml,           value: "(unused — passed via ConfigMap)" }
          - { name: chaos_result_name,  value: "{{steps.chaos.outputs.parameters.chaos_result_name}}-pod-network-loss" }
          - { name: load_start_ts,      value: "{{steps.load.startedAt}}" }
          - { name: chaos_start_after,  value: "{{workflow.parameters.chaos_start_after}}" }
          - { name: chaos_duration,     value: "{{workflow.parameters.chaos_duration}}" }
          - { name: load_duration,      value: "{{workflow.parameters.load_duration}}" }
          - { name: metrics_namespace,  value: "{{workflow.parameters.scenario_label}}" }
          - { name: workflow_name,      value: "{{workflow.name}}" }

  - name: write-slo
    serviceAccountName: argo-workflow
    script:
      image: alpine/k8s:1.30.0
      command: [bash]
      source: |
        set -euo pipefail
        cat > /tmp/slo.yaml <<'EOF'
        thresholds:
        - metric: p95-streamload-latency-chaos
          query: avg(dlh_doris_streamload_duration_seconds_p95{dlh_workflow="{{workflow.name}}"})
          lt: 5.0
          window: chaos
        - metric: streamload-error-rate-recovery
          query: sum(rate(dlh_app_errors_total{kind="doris-streamload",dlh_workflow="{{workflow.name}}"}[30s])) / clamp_min(sum(rate(dlh_doris_streamload_rows_total{dlh_workflow="{{workflow.name}}"}[30s])), 1e-9)
          lt: 0.10
          window: recovery
        EOF
        kubectl -n dlh-test-fw create configmap dlh-slo-{{workflow.name}} \
          --from-file=slo.yaml=/tmp/slo.yaml --dry-run=client -o yaml | kubectl apply -f -

  - name: ensure-table
    serviceAccountName: argo-workflow
    script:
      image: mysql:8.0
      command: [bash]
      source: |
        set -euo pipefail
        mysql -hdoris-fe.doris-sys.svc.cluster.local -P9030 -uroot -e "
          CREATE DATABASE IF NOT EXISTS dlh_probe;
          USE dlh_probe;
          CREATE TABLE IF NOT EXISTS probe (id BIGINT, ts DATETIME) DISTRIBUTED BY HASH(id) BUCKETS 1
            PROPERTIES('replication_num' = '1');"
YAML
git add scenarios/doris-be-network-loss.yaml
git commit -m "scenario(doris-be-network-loss): real Doris via runners/doris.js + env_map + updated SLOs"
```

### Branch B — Task 4 was NO-GO

- [ ] **Step 1: Update scenarios/README.md to mark Doris deferred (do not change the scenario YAML — keep it as documentation)**

```bash
cd /Users/allen/repo/dlh-test-fw-phase2
# Find the scenarios/README.md section listing scenarios and mark doris-be-network-loss as deferred
sed -i.bak 's|.*doris-be-network-loss.yaml.*|- **doris-be-network-loss.yaml** — **DEFERRED** (Plan 7 spike NO-GO; see `targets/doris/README.md`)|' scenarios/README.md
rm -f scenarios/README.md.bak
cat scenarios/README.md | grep -A1 -B1 doris
git add scenarios/README.md
git commit -m "scenarios: mark doris-be-network-loss DEFERRED per Plan 7 spike NO-GO"
```

---

## Task 9: Delete per-scenario k6 ConfigMap YAMLs

**Files:**
- Delete: `scenarios/mysql-pod-delete-k6-script.yaml`
- Delete: `scenarios/kafka-broker-partition-k6-script.yaml`
- Delete: `scenarios/doris-be-network-loss-k6-script.yaml` (regardless of Task 4 outcome — the file isn't used by the deferred scenario either)

Scripts now live in the `dlh-k6` image at `/scripts/runners/`.

- [ ] **Step 1: Remove all three files**

```bash
cd /Users/allen/repo/dlh-test-fw-phase2
git rm scenarios/mysql-pod-delete-k6-script.yaml \
       scenarios/kafka-broker-partition-k6-script.yaml \
       scenarios/doris-be-network-loss-k6-script.yaml
```

- [ ] **Step 2: Delete any matching in-cluster ConfigMaps (they're no longer applied)**

```bash
kubectl -n dlh-test-fw delete cm k6-script-mysql-pod-delete \
  k6-script-kafka-broker-partition k6-script-doris-be-network-loss --ignore-not-found
```

- [ ] **Step 3: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw-phase2
git commit -m "scenarios: remove per-scenario k6 ConfigMap YAMLs — scripts live in dlh-k6 image"
```

---

## Task 10: Add Makefile targets for kafka (+doris if GO)

**Files:**
- Modify: `Makefile` (repo root)

- [ ] **Step 1: Append `run-kafka` (and `run-doris` if Task 4 was GO)**

Open `Makefile` and add at the end:

```makefile

# --- Phase 2 scenarios ---
.PHONY: run-kafka

run-kafka:
	./scripts/run-scenario.sh scenarios/kafka-broker-partition.yaml
```

If Task 4 was GO, also add:

```makefile

.PHONY: run-doris

run-doris:
	./scripts/run-scenario.sh scenarios/doris-be-network-loss.yaml
```

- [ ] **Step 2: Verify both targets**

```bash
cd /Users/allen/repo/dlh-test-fw-phase2
make -n run-kafka
# If Task 4 = GO:
make -n run-doris
```

Expected: prints the `./scripts/run-scenario.sh ...` command. No "unknown target".

- [ ] **Step 3: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw-phase2
git add Makefile
git commit -m "make: add run-kafka (and run-doris if Plan 7 spike GO) targets"
```

---

## Task 11: End-to-end smoke — `make run-mysql`

**Files:**
- Verify-only.

This is the test that all the prior changes converge: a full scenario submits the workflow, k6 hits real MySQL, chaos pod-deletes the pod, verdict reads VM and produces a pass/fail.

- [ ] **Step 1: Run the scenario**

```bash
cd /Users/allen/repo/dlh-test-fw-phase2
make run-mysql 2>&1 | tail -10
```

Expected: `Final phase: Succeeded`, exit 0 from `run-scenario.sh`.

If the workflow fails, get the failure node:

```bash
WF=$(kubectl -n dlh-test-fw get workflow -o name --sort-by=.metadata.creationTimestamp | grep mysql-pod-delete | tail -1 | cut -d/ -f2)
kubectl -n dlh-test-fw get workflow "$WF" -o jsonpath='{.status.nodes}' | jq -r 'to_entries[] | select(.value.phase != "Succeeded") | "\(.value.displayName)\t\(.value.phase)\t\(.value.message // "")"'
```

Common failures and fixes:
- "Failed to apply manifest: ConfigMap dlh-k6-env-... not found" — Task 5 `write-env` script step failed. `kubectl -n dlh-test-fw logs <write-env-pod>` and inspect.
- k6 runner exits 1 with `dial tcp: connection refused` — mysql target is down (re-run Task 2 Step 1).
- Verdict step `Failed: ChaosResult ... Awaited` — chaos didn't actually run; check chaos-pod-delete WT logs.

- [ ] **Step 2: Confirm verdict metrics in VM**

```bash
WF=$(kubectl -n dlh-test-fw get workflow -o name --sort-by=.metadata.creationTimestamp | grep mysql-pod-delete | tail -1 | cut -d/ -f2)
PW=$(kubectl -n dlh-test-fw get secret grafana-admin-credentials -o jsonpath='{.data.admin-password}' | base64 -d)
kubectl -n dlh-test-fw port-forward svc/dlh-grafana 3000:80 >/tmp/gf.log 2>&1 &
sleep 3
echo "=== dlh_verdict_overall for $WF ==="
curl -s -u "admin:${PW}" -G "http://127.0.0.1:3000/api/datasources/proxy/uid/VictoriaMetrics/api/v1/query" \
  --data-urlencode "query=last_over_time(dlh_verdict_overall{dlh_workflow=\"$WF\"}[7d])" | jq '.data.result[0].value'
echo "=== dlh_mysql_query_duration_seconds_p95 for $WF ==="
curl -s -u "admin:${PW}" -G "http://127.0.0.1:3000/api/datasources/proxy/uid/VictoriaMetrics/api/v1/query" \
  --data-urlencode "query=last_over_time(dlh_mysql_query_duration_seconds_p95{dlh_workflow=\"$WF\"}[7d])" | jq '.data.result | length'
kill %1
```

Expected: `dlh_verdict_overall` value = `"1"` (pass) and the `dlh_mysql_query_duration_seconds_p95` series count ≥ 1.

- [ ] **Step 3: Confirm Argo artifact contains the verdict report**

```bash
WF=$(kubectl -n dlh-test-fw get workflow -o name --sort-by=.metadata.creationTimestamp | grep mysql-pod-delete | tail -1 | cut -d/ -f2)
kubectl -n dlh-test-fw exec deploy/dlh-minio -- mc cat "local/artifacts/${WF}-main-*/verdict/report.json" 2>/dev/null | jq '{overall, thresholds: [.thresholds[] | {metric, value, passed}]}'
```

Expected: JSON with `overall: true` (or false, but with real values — not zero) and 2 thresholds.

- [ ] **Step 4: No commit — verification only.**

---

## Task 12: End-to-end smoke — `make run-kafka`

**Files:**
- Verify-only.

- [ ] **Step 1: Run the scenario**

```bash
cd /Users/allen/repo/dlh-test-fw-phase2
make run-kafka 2>&1 | tail -10
```

Expected: `Final phase: Succeeded`.

- [ ] **Step 2: Confirm kafka metrics in VM**

```bash
WF=$(kubectl -n dlh-test-fw get workflow -o name --sort-by=.metadata.creationTimestamp | grep kafka-broker-partition | tail -1 | cut -d/ -f2)
PW=$(kubectl -n dlh-test-fw get secret grafana-admin-credentials -o jsonpath='{.data.admin-password}' | base64 -d)
kubectl -n dlh-test-fw port-forward svc/dlh-grafana 3000:80 >/tmp/gf.log 2>&1 &
sleep 3
for m in dlh_kafka_messages_produced_total dlh_kafka_produce_duration_seconds_p95 dlh_verdict_overall; do
  echo "--- $m ---"
  curl -s -u "admin:${PW}" -G "http://127.0.0.1:3000/api/datasources/proxy/uid/VictoriaMetrics/api/v1/query" \
    --data-urlencode "query=last_over_time(${m}{dlh_workflow=\"$WF\"}[7d])" | jq '.data.result | length'
done
kill %1
```

Expected: all three return ≥ 1.

- [ ] **Step 3: No commit — verification only.**

---

## Task 13: End-to-end smoke — `make run-doris` (skipped if Task 4 = NO-GO)

**Files:**
- Verify-only.

### Branch A — Task 4 was GO

- [ ] **Step 1: Run the scenario**

```bash
cd /Users/allen/repo/dlh-test-fw-phase2
make run-doris 2>&1 | tail -10
```

Expected: `Final phase: Succeeded`.

- [ ] **Step 2: Confirm doris metrics in VM**

```bash
WF=$(kubectl -n dlh-test-fw get workflow -o name --sort-by=.metadata.creationTimestamp | grep doris-be-network-loss | tail -1 | cut -d/ -f2)
PW=$(kubectl -n dlh-test-fw get secret grafana-admin-credentials -o jsonpath='{.data.admin-password}' | base64 -d)
kubectl -n dlh-test-fw port-forward svc/dlh-grafana 3000:80 >/tmp/gf.log 2>&1 &
sleep 3
for m in dlh_doris_streamload_rows_total dlh_doris_streamload_duration_seconds_p95 dlh_verdict_overall; do
  echo "--- $m ---"
  curl -s -u "admin:${PW}" -G "http://127.0.0.1:3000/api/datasources/proxy/uid/VictoriaMetrics/api/v1/query" \
    --data-urlencode "query=last_over_time(${m}{dlh_workflow=\"$WF\"}[7d])" | jq '.data.result | length'
done
kill %1
```

Expected: all three return ≥ 1.

### Branch B — Task 4 was NO-GO

Skip the scenario run. Verify the deferred state holds:

```bash
grep -i 'doris-be-network-loss' /Users/allen/repo/dlh-test-fw-phase2/scenarios/README.md
```

Expected: a line containing **DEFERRED**.

- [ ] **No commit either branch — verification only.**

---

## Task 14: FINDINGS append for Plan 8 consumption

**Files:**
- Modify: `docs/FINDINGS.md` (append a section)

Plan 8 (dashboards) reads FINDINGS to know which metric series are actually emitted, with which labels, and which scenarios are live.

- [ ] **Step 1: Append**

Open `docs/FINDINGS.md` and append at the end:

```markdown

## Plan 7 outcome — scripts + WT migration (2026-05-17)

`load/k6-run` is now a two-step template (write-env CM → run TestRun on
`dlh-k6:0.1.0`). Scenarios pass `script_path` + `env_map` (multi-line
KEY=VALUE) instead of `script_configmap`. Three `*-k6-script.yaml` files
deleted.

### Live scenarios and the metric series they emit

| Scenario | Runner | Metrics actually in VM | Verdict overall |
|---|---|---|---|
| `mysql-pod-delete` | `runners/mysql.js` | `dlh_mysql_query_duration_seconds_{p95,p99,avg,min,max,count,sum}` (tagged `op`); `dlh_app_errors_total{kind=~"mysql.*"}` | <fill: PASS or FAIL of the verification run> |
| `kafka-broker-partition` | `runners/kafka.js` | `dlh_kafka_produce_duration_seconds_{p95,...}` (tagged `topic`); `dlh_kafka_messages_produced_total{topic}`; `dlh_app_errors_total{kind="kafka-produce"}` | <fill> |
| `doris-be-network-loss` | `runners/doris.js` if GO; deferred if NO-GO | `dlh_doris_streamload_duration_seconds_{p95,...}` (tagged `db, table`); `dlh_doris_streamload_rows_total{db, table}`; `dlh_doris_query_duration_seconds_{p95,...}`; `dlh_app_errors_total{kind=~"doris-.*"}` — OR none if deferred | <fill or N/A> |

### Implications for Plan 8

- Type-specific dashboard PromQL uses the gauge form `<metric>_p95`
  directly (confirmed in Plan 7 Task 1 — k6 emits the full p95/p99/avg/min/max
  family for custom Trends when `K6_PROMETHEUS_RW_TREND_STATS` is set).
- Variable cascade: `$scenario` from `label_values(<marker_metric>, dlh_scenario)`
  where marker is `dlh_mysql_query_duration_seconds_count` for mysql,
  `dlh_kafka_messages_produced_total` for kafka,
  `dlh_doris_streamload_rows_total` for doris.
- `$workflow` from `label_values(<marker_metric>{dlh_scenario="$scenario"}, dlh_workflow)`.
- Existing `dlh-run-detail` dashboard's k6 panels still reference
  `k6_http_*` series — those are NO LONGER EMITTED by the new runners
  (real protocol tests, not HTTP). Plan 8 either drops those k6 panels
  from `dlh-run-detail` or replaces them with per-target equivalents.
```

- [ ] **Step 2: Fill in the verification verdict for each scenario from Tasks 11/12/13**

Replace each `<fill>` with the actual PASS/FAIL from the verification runs.

- [ ] **Step 3: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw-phase2
git add docs/FINDINGS.md
git commit -m "findings: Plan 7 outcomes — live scenarios + emitted metric series for Plan 8"
```

---

## Definition of Done

- [ ] `helm/dlh-test-fw/files/workflowtemplates/load/k6-run.yaml` is a two-step template (`write-env-cm` + `run-testrun`) pinned to `ghcr.io/dlh/dlh-k6:0.1.0` with `imagePullPolicy: Never`, accepting `script_path` instead of `script_configmap`.
- [ ] `scenarios/mysql-pod-delete.yaml` and `scenarios/kafka-broker-partition.yaml` use `script_path: /scripts/runners/<type>.js` + `env_map` and their `write-slo` queries reference `dlh_<type>_*` metric names.
- [ ] `scenarios/doris-be-network-loss.yaml` is either (GO) rewritten the same way OR (NO-GO) marked deferred in `scenarios/README.md` with the existing YAML preserved.
- [ ] All three `scenarios/*-k6-script.yaml` files are deleted.
- [ ] `make run-mysql` runs end-to-end against real MySQL and produces a verdict in MinIO with non-zero threshold values.
- [ ] `make run-kafka` runs end-to-end against real Kafka and produces a verdict.
- [ ] Doris: either `make run-doris` succeeds (GO branch) OR `scenarios/README.md` documents the deferred state with a NO-GO reason in `targets/doris/README.md`.
- [ ] `dlh_<type>_*` series are present in VictoriaMetrics for the workflows of every live scenario.
- [ ] `docs/FINDINGS.md` has a "Plan 7 outcome" section with the verdict-pass-rate of each scenario and the implications for Plan 8.
- [ ] Every task is its own atomic commit on `feat/phase-2-scripts-dashboards`.
- [ ] No changes to `dashboards/grafana/*.json`, no changes to `verdict-job/`, no changes outside `helm/dlh-test-fw/files/workflowtemplates/load/`, `scenarios/`, `targets/`, `Makefile`, `docs/FINDINGS.md`.

---

## Self-review notes

- **Spec coverage:** Implements spec §"load/k6-run WorkflowTemplate change" (Task 5), §"Example scenario YAML (post-migration)" (Tasks 6-8), §"Migration" steps 1-3 (Tasks 4, 5-8 land together), §"Doris caveat" (Task 4), §"Testing" Plan 7 row (Tasks 11-13 — three end-to-end smokes). Out of scope: dashboard panels (Plan 8 owns).

- **Placeholder scan:** Every code block contains the actual content. The only literal `<fill>` markers are inside FINDINGS template strings the engineer substitutes in-task (Task 14 Step 2). The plan instruction explicitly tells them to fill those.

- **Type consistency:** Metric names match across:
  - `runners/mysql.js` Trend `dlh_mysql_query_duration_seconds` ↔ Task 6 SLO query ↔ Task 11 verification ↔ Task 14 FINDINGS table.
  - `runners/kafka.js` Counter `dlh_kafka_messages_produced_total` + Trend `dlh_kafka_produce_duration_seconds` ↔ Task 7 ↔ Task 12 ↔ Task 14.
  - `runners/doris.js` Counter `dlh_doris_streamload_rows_total` + Trends `dlh_doris_streamload_duration_seconds`, `dlh_doris_query_duration_seconds` ↔ Task 8 ↔ Task 13 ↔ Task 14.
  - Common error counter `dlh_app_errors_total` tagged `kind` is the join key for error-rate SLOs across all scenarios.
  - WT input parameter name `script_path` consistent in Task 5, 6, 7, 8.
  - ConfigMap name pattern `dlh-k6-env-{{workflow.name}}` matches between `write-env-cm` and `run-testrun` (both in Task 5).
