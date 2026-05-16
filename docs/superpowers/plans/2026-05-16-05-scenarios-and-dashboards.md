# Plan 5 — Sample Scenarios + Grafana Dashboards Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire the platform (Plans 1-4) into three runnable end-to-end scenarios — `mysql-pod-delete`, `doris-be-network-loss`, `kafka-broker-partition` — and ship the two Grafana dashboards the spec calls out (`dlh-run-detail`, `dlh-history`). After this plan, `kubectl apply -f scenarios/mysql-pod-delete.yaml && argo watch -n dlh-test-fw @latest` runs end-to-end and produces a verdict report viewable in the Argo UI and the dashboards in Grafana.

**Architecture:** Scenarios are top-level Argo `Workflow` resources (NOT WorkflowTemplates — they're concrete instances). Each scenario uses the three-step `- -` pattern described in the spec to enforce fixture → (chaos ∥ load) → verdict ordering. SLO YAML is materialised into a `ConfigMap` by an inline `script` step at the top of each scenario so `verdict-slo-eval` (Plan 4 Task 13) can consume it. Target systems (MySQL, Kafka, optionally Doris) are deployed via small Helm-released subcharts or stand-alone manifests in `targets/`. Grafana dashboards live as ConfigMaps labelled `dlh-dashboard` so the Grafana sidecar (Plan 2 values) auto-imports them.

**Tech Stack:** Argo Workflows v3, Litmus, k6-operator, Grafana 8.x with sidecar, MySQL 8 / Kafka KRaft / Doris 2.x (latter optional based on minikube memory), `argo` CLI for watching workflows.

**Prerequisites:** Plans 1-4 complete. Platform pods Ready. `make verify-templates` passes. `dlh-verdict:0.1.0` and `dlh-fixture-*:0.1.0` images loaded into minikube.

**Out of scope:** Phase 2 web UI for editing scenarios, multi-cluster, custom CRD.

---

## File Structure

```
scenarios/
├── mysql-pod-delete.yaml                    # ready-to-apply Argo Workflow
├── doris-be-network-loss.yaml               # ditto
├── kafka-broker-partition.yaml              # ditto
└── README.md                                # how to run / how to compose new ones
targets/
├── mysql/
│   ├── deploy.yaml                          # Deployment + Service + Secret
│   └── README.md
├── kafka/
│   ├── deploy.yaml                          # StatefulSet single-broker KRaft + Service
│   └── README.md
└── doris/
    ├── deploy.yaml                          # FE + BE minimal (optional)
    └── README.md
dashboards/grafana/
├── dlh-run-detail.json
└── dlh-history.json
helm/dlh-test-fw/templates/
└── dashboards-configmaps.yaml              # NEW: wraps the two JSON files in ConfigMaps
scripts/
└── run-scenario.sh                          # apply + watch + verify report
```

Responsibilities:
- `scenarios/*.yaml` — concrete Argo Workflows, fully self-contained (no `templateRef` lookup outside the registered library).
- `targets/<system>/deploy.yaml` — minimal install of the system under test; deliberately not productionised.
- `dashboards/grafana/*.json` — dashboard JSON. Tracked in repo for diffs; deployed via ConfigMaps.
- `helm/dlh-test-fw/templates/dashboards-configmaps.yaml` — Helm wraps each dashboard JSON file into a ConfigMap labelled `dlh-dashboard` so Grafana's sidecar imports them automatically (sidecar config set in Plan 2 `values.yaml`).
- `scripts/run-scenario.sh` — convenience runner.

---

## Task 1: MySQL target deployment

**Files:**
- Create: `targets/mysql/deploy.yaml`
- Create: `targets/mysql/README.md`

- [ ] **Step 1: Write the deployment**

```yaml
apiVersion: v1
kind: Namespace
metadata: { name: mysql-sys }
---
apiVersion: v1
kind: Secret
metadata: { name: mysql-creds, namespace: mysql-sys }
type: Opaque
stringData:
  user: root
  password: dlh-mysql-dev
  database: dlh
  MYSQL_ROOT_PASSWORD: dlh-mysql-dev
  MYSQL_DATABASE: dlh
---
apiVersion: apps/v1
kind: Deployment
metadata: { name: mysql, namespace: mysql-sys, labels: { app: mysql } }
spec:
  replicas: 1
  selector: { matchLabels: { app: mysql } }
  template:
    metadata: { labels: { app: mysql } }
    spec:
      containers:
      - name: mysql
        image: mysql:8.0
        envFrom: [{ secretRef: { name: mysql-creds } }]
        ports: [{ containerPort: 3306 }]
        resources:
          requests: { cpu: 200m, memory: 384Mi }
          limits:   { cpu: 500m, memory: 512Mi }
        readinessProbe:
          exec: { command: ["mysqladmin", "-uroot", "-p$MYSQL_ROOT_PASSWORD", "ping"] }
          initialDelaySeconds: 15
          periodSeconds: 5
---
apiVersion: v1
kind: Service
metadata: { name: mysql, namespace: mysql-sys }
spec:
  selector: { app: mysql }
  ports: [{ port: 3306, targetPort: 3306 }]
```

- [ ] **Step 2: Write `targets/mysql/README.md`**

```markdown
# MySQL target

A throwaway single-pod MySQL 8 for scenario testing.

    kubectl apply -f targets/mysql/deploy.yaml
    kubectl -n mysql-sys rollout status deploy/mysql

Credentials: see Secret `mysql-creds` in namespace `mysql-sys`.
```

- [ ] **Step 3: Apply + wait**

```bash
kubectl apply -f targets/mysql/deploy.yaml
kubectl -n mysql-sys rollout status deploy/mysql --timeout=180s
```

- [ ] **Step 4: Commit**

```bash
git add targets/mysql
git commit -m "target: mysql 8 single-pod for scenario testing"
```

---

## Task 2: Kafka target deployment

**Files:**
- Create: `targets/kafka/deploy.yaml`
- Create: `targets/kafka/README.md`

- [ ] **Step 1: Write the deployment (KRaft single-node)**

```yaml
apiVersion: v1
kind: Namespace
metadata: { name: kafka-sys }
---
apiVersion: apps/v1
kind: StatefulSet
metadata: { name: kafka, namespace: kafka-sys, labels: { app: kafka } }
spec:
  serviceName: kafka-headless
  replicas: 1
  selector: { matchLabels: { app: kafka } }
  template:
    metadata:
      labels:
        app: kafka
        kafka.broker.id: "0"           # selector for chaos-kafka-broker-partition
    spec:
      containers:
      - name: kafka
        image: bitnami/kafka:3.7
        env:
        - { name: KAFKA_CFG_NODE_ID,                value: "0" }
        - { name: KAFKA_CFG_PROCESS_ROLES,          value: "broker,controller" }
        - { name: KAFKA_CFG_LISTENERS,              value: "PLAINTEXT://:9092,CONTROLLER://:9093" }
        - { name: KAFKA_CFG_ADVERTISED_LISTENERS,   value: "PLAINTEXT://kafka.kafka-sys.svc.cluster.local:9092" }
        - { name: KAFKA_CFG_CONTROLLER_QUORUM_VOTERS, value: "0@kafka-0.kafka-headless.kafka-sys.svc.cluster.local:9093" }
        - { name: KAFKA_CFG_CONTROLLER_LISTENER_NAMES, value: "CONTROLLER" }
        - { name: KAFKA_CFG_INTER_BROKER_LISTENER_NAME, value: "PLAINTEXT" }
        - { name: ALLOW_PLAINTEXT_LISTENER,         value: "yes" }
        ports:
        - { containerPort: 9092, name: kafka }
        - { containerPort: 9093, name: controller }
        resources:
          requests: { cpu: 200m, memory: 512Mi }
          limits:   { cpu: 500m, memory: 1Gi }
        readinessProbe:
          tcpSocket: { port: 9092 }
          initialDelaySeconds: 20
          periodSeconds: 5
---
apiVersion: v1
kind: Service
metadata: { name: kafka, namespace: kafka-sys }
spec:
  selector: { app: kafka }
  ports: [{ port: 9092, targetPort: 9092 }]
---
apiVersion: v1
kind: Service
metadata: { name: kafka-headless, namespace: kafka-sys }
spec:
  clusterIP: None
  selector: { app: kafka }
  ports:
  - { port: 9092, name: kafka }
  - { port: 9093, name: controller }
```

- [ ] **Step 2: Apply + wait**

```bash
kubectl apply -f targets/kafka/deploy.yaml
kubectl -n kafka-sys rollout status statefulset/kafka --timeout=240s
```

- [ ] **Step 3: Quick produce/consume smoke**

```bash
kubectl -n kafka-sys run kcat-smoke --rm -it --restart=Never \
  --image=edenhill/kcat:1.7.1 -- \
  -b kafka.kafka-sys.svc.cluster.local:9092 -L
```

Expected: lists 0 topics (or whatever exists) without error.

- [ ] **Step 4: Commit**

```bash
git add targets/kafka
git commit -m "target: kafka 3.7 KRaft single-broker for scenario testing"
```

---

## Task 3: Doris target (optional, may skip on memory-tight workstation)

**Files:**
- Create: `targets/doris/deploy.yaml`
- Create: `targets/doris/README.md`

Spec calls Doris out as optional Phase 1; if minikube memory is tight, document skip and proceed.

- [ ] **Step 1: Decide based on `kubectl top nodes`**

```bash
kubectl top nodes
```

If available memory < 5 GiB, **skip this task** and write `targets/doris/README.md` saying "skipped on this workstation; see plan §Task 3 for revival steps". Commit only the README. **The doris scenario (Task 6) will then be marked SKIPPED in Definition of Done.**

- [ ] **Step 2 (if proceeding): Use the official `apache/doris` minimal images**

```yaml
apiVersion: v1
kind: Namespace
metadata: { name: doris-sys }
---
apiVersion: apps/v1
kind: StatefulSet
metadata: { name: doris-fe, namespace: doris-sys, labels: { app: doris-fe } }
spec:
  serviceName: doris-fe
  replicas: 1
  selector: { matchLabels: { app: doris-fe } }
  template:
    metadata: { labels: { app: doris-fe } }
    spec:
      containers:
      - name: fe
        image: apache/doris:2.1.5-fe
        ports:
        - { containerPort: 8030, name: http }
        - { containerPort: 9010, name: edit-log }
        resources:
          requests: { cpu: 500m, memory: 2Gi }
          limits:   { cpu: 1, memory: 2.5Gi }
        env: [{ name: FE_SERVERS, value: "fe1:doris-fe-0.doris-fe.doris-sys.svc.cluster.local:9010" }, { name: FE_ID, value: "1" }]
---
apiVersion: apps/v1
kind: StatefulSet
metadata: { name: doris-be, namespace: doris-sys, labels: { app: doris-be } }
spec:
  serviceName: doris-be
  replicas: 1
  selector: { matchLabels: { app: doris-be } }
  template:
    metadata: { labels: { app: doris-be } }
    spec:
      containers:
      - name: be
        image: apache/doris:2.1.5-be
        env: [{ name: FE_SERVERS, value: "fe1:doris-fe-0.doris-fe.doris-sys.svc.cluster.local:9010" }, { name: BE_ADDR, value: "doris-be-0.doris-be.doris-sys.svc.cluster.local:9050" }]
        resources:
          requests: { cpu: 500m, memory: 2Gi }
          limits:   { cpu: 1, memory: 2.5Gi }
---
apiVersion: v1
kind: Service
metadata: { name: doris-fe, namespace: doris-sys }
spec:
  selector: { app: doris-fe }
  clusterIP: None
  ports: [{ name: http, port: 8030 }, { name: edit-log, port: 9010 }]
---
apiVersion: v1
kind: Service
metadata: { name: doris-be, namespace: doris-sys }
spec:
  selector: { app: doris-be }
  clusterIP: None
  ports: [{ name: be, port: 9050 }, { name: webserver, port: 8040 }]
```

- [ ] **Step 3: Apply + wait OR document skip**

- [ ] **Step 4: Commit**

```bash
git add targets/doris
git commit -m "target: doris FE+BE minimal (or skipped — see README)"
```

---

## Task 4: Scenario — `mysql-pod-delete`

**Files:**
- Create: `scenarios/mysql-pod-delete.yaml`

The reference scenario. Demonstrates the canonical pattern.

- [ ] **Step 1: Write the scenario**

```yaml
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
    - name: load_duration
      value: 180s
    - name: chaos_start_after
      value: 30s
    - name: chaos_duration
      value: 60s
    - name: scenario_label
      value: mysql-pod-delete
  templates:
  - name: main
    steps:
    # Stage 0 — materialise SLO ConfigMap (consumed by verdict-slo-eval).
    - - name: prep-slo
        template: write-slo
    # Stage 1 — fixture must complete before chaos+load (fail-fast: no continueOn).
    - - name: load-fixture
        templateRef: { name: fixture-minio-load-mysql, template: main }
        arguments:
          parameters:
          - { name: uri, value: "s3://fixtures/mysql-users.sql" }
          - { name: db_host, value: "mysql.mysql-sys.svc.cluster.local" }
          - { name: credentials_secret, value: "mysql-creds" }
    # Stage 2 — chaos and load run in parallel; chaos failure does NOT abort load.
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
          - { name: script_configmap, value: "k6-script-mysql-pod-delete" }
          - { name: vus,              value: "10" }
          - { name: duration,         value: "{{workflow.parameters.load_duration}}" }
          - { name: env_map,          value: "{}" }
          - { name: scenario_label,   value: "{{workflow.parameters.scenario_label}}" }
    # Stage 3 — verdict (waits for stage 2 to finish).
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
      image: bitnami/kubectl:1.30
      command: [bash]
      source: |
        set -euo pipefail
        cat > /tmp/slo.yaml <<'EOF'
        thresholds:
        - metric: p95-latency-chaos
          query: histogram_quantile(0.95, sum(rate(k6_http_req_duration_seconds_bucket{scenario="mysql-pod-delete"}[1m])) by (le))
          lt: 1.0
          window: chaos
        - metric: error-rate-recovery
          query: sum(rate(k6_http_reqs_total{scenario="mysql-pod-delete",status!~"2.."}[30s])) / sum(rate(k6_http_reqs_total{scenario="mysql-pod-delete"}[30s]))
          lt: 0.05
          window: recovery
        EOF
        kubectl -n dlh-test-fw create configmap dlh-slo-{{workflow.name}} \
          --from-file=slo.yaml=/tmp/slo.yaml --dry-run=client -o yaml | kubectl apply -f -
```

**Note on k6 script ConfigMap:** This scenario references `k6-script-mysql-pod-delete` — we create it in Task 5 as a static ConfigMap (it doesn't change per-run). Plan 4's `load-k6-run` template expects the script to live in a pre-existing ConfigMap named via parameter.

- [ ] **Step 2: Write a placeholder k6 script ConfigMap**

```yaml
# scenarios/mysql-pod-delete-k6-script.yaml — applied once, lives forever
apiVersion: v1
kind: ConfigMap
metadata:
  name: k6-script-mysql-pod-delete
  namespace: dlh-test-fw
data:
  script.js: |
    import http from 'k6/http';
    import { sleep, check } from 'k6';
    export const options = {
      vus: parseInt(__ENV.VUS || '10'),
      duration: __ENV.DURATION || '180s',
      tags: { scenario: 'mysql-pod-delete' },
    };
    // Hits a thin HTTP proxy that fronts mysql; or, for spike-style use,
    // points at httpbin and treats it as a proxy stand-in.
    // For real workloads, replace this URL with a service that proxies SQL.
    export default function () {
      const r = http.get('http://httpbin.dlh-test-fw.svc.cluster.local/status/200');
      check(r, { 'status 2xx': (x) => x.status >= 200 && x.status < 300 });
      sleep(0.5);
    }
```

**Why httpbin not direct mysql:** k6 doesn't ship a MySQL driver out of the box (would need xk6-sql). For Phase 1 the spec accepts that scenarios validate the **platform mechanics**; a thin HTTP front for MySQL can be added later. Document this in `scenarios/README.md`.

- [ ] **Step 3: Apply the ConfigMap; do not run the workflow yet**

```bash
kubectl apply -f scenarios/mysql-pod-delete-k6-script.yaml
kubectl -n dlh-test-fw get cm k6-script-mysql-pod-delete
```

- [ ] **Step 4: Commit**

```bash
git add scenarios/mysql-pod-delete.yaml scenarios/mysql-pod-delete-k6-script.yaml
git commit -m "scenario: mysql-pod-delete reference scenario + k6 script CM"
```

---

## Task 5: Scenario — `kafka-broker-partition`

**Files:**
- Create: `scenarios/kafka-broker-partition.yaml`
- Create: `scenarios/kafka-broker-partition-k6-script.yaml`

- [ ] **Step 1: Write `scenarios/kafka-broker-partition.yaml`**

Same skeleton as Task 4 with these differences:
- Replace `fixture-minio-load-mysql` with `fixture-kafka-topic-seed` parameters.
- Replace `chaos-pod-delete` with `chaos-kafka-broker-partition`.
- Replace `{{...}}-pod-delete` suffix in `chaos_result_name` with `{{...}}-pod-network-partition`.
- Scenario label `kafka-broker-partition`.
- SLO targets `k6_kafka_*` metrics or proxy-style metrics; for Phase 1 reuse the http-style queries.

```yaml
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
    - - name: load-fixture
        templateRef: { name: fixture-kafka-topic-seed, template: main }
        arguments:
          parameters:
          - { name: bootstrap, value: "kafka.kafka-sys.svc.cluster.local:9092" }
          - { name: topic,     value: "events" }
          - { name: seed_uri,  value: "s3://fixtures/kafka-events.jsonl" }
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
          - { name: script_configmap, value: "k6-script-kafka-broker-partition" }
          - { name: vus,              value: "5" }
          - { name: duration,         value: "{{workflow.parameters.load_duration}}" }
          - { name: env_map,          value: "{}" }
          - { name: scenario_label,   value: "{{workflow.parameters.scenario_label}}" }
    - - name: verdict
        templateRef: { name: verdict-slo-eval, template: main }
        arguments:
          parameters:
          - { name: slo_yaml,          value: "(unused)" }
          - { name: chaos_result_name, value: "{{steps.chaos.outputs.parameters.chaos_result_name}}-pod-network-partition" }
          - { name: load_start_ts,     value: "{{steps.load.startedAt}}" }
          - { name: chaos_start_after, value: "{{workflow.parameters.chaos_start_after}}" }
          - { name: chaos_duration,    value: "{{workflow.parameters.chaos_duration}}" }
          - { name: load_duration,     value: "{{workflow.parameters.load_duration}}" }
          - { name: metrics_namespace, value: "{{workflow.parameters.scenario_label}}" }
          - { name: workflow_name,     value: "{{workflow.name}}" }

  - name: write-slo
    serviceAccountName: argo-workflow
    script:
      image: bitnami/kubectl:1.30
      command: [bash]
      source: |
        set -euo pipefail
        cat > /tmp/slo.yaml <<'EOF'
        thresholds:
        - metric: p95-latency-chaos
          query: histogram_quantile(0.95, sum(rate(k6_http_req_duration_seconds_bucket{scenario="kafka-broker-partition"}[1m])) by (le))
          lt: 2.0
          window: chaos
        EOF
        kubectl -n dlh-test-fw create configmap dlh-slo-{{workflow.name}} \
          --from-file=slo.yaml=/tmp/slo.yaml --dry-run=client -o yaml | kubectl apply -f -
```

- [ ] **Step 2: Write the matching k6 script ConfigMap**

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: k6-script-kafka-broker-partition
  namespace: dlh-test-fw
data:
  script.js: |
    import http from 'k6/http';
    import { sleep } from 'k6';
    export const options = {
      vus: parseInt(__ENV.VUS || '5'),
      duration: __ENV.DURATION || '180s',
      tags: { scenario: 'kafka-broker-partition' },
    };
    export default function () {
      http.get('http://httpbin.dlh-test-fw.svc.cluster.local/status/200');
      sleep(0.5);
    }
```

- [ ] **Step 3: Apply + commit**

```bash
kubectl apply -f scenarios/kafka-broker-partition-k6-script.yaml
git add scenarios/kafka-broker-partition.yaml scenarios/kafka-broker-partition-k6-script.yaml
git commit -m "scenario: kafka-broker-partition"
```

---

## Task 6: Scenario — `doris-be-network-loss`

**Files:**
- Create: `scenarios/doris-be-network-loss.yaml`
- Create: `scenarios/doris-be-network-loss-k6-script.yaml`

If Doris target was skipped in Task 3, **still write the scenario file** (it documents the pattern) but mark it as "deferred until target deployed" in `scenarios/README.md`.

- [ ] **Step 1: Write the scenario** (mirrors Task 5 with `chaos-network-loss` and Doris-specific fixture)

```yaml
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
    - - name: load-fixture
        templateRef: { name: fixture-minio-load-doris, template: main }
        arguments:
          parameters:
          - { name: uri,                              value: "s3://fixtures/doris-rows.csv" }
          - { name: fe_host,                          value: "doris-fe-0.doris-fe.doris-sys.svc.cluster.local:8030" }
          - { name: stream_load_credentials_secret,   value: "doris-creds" }
    - - name: chaos
        templateRef: { name: chaos-network-loss, template: main }
        continueOn: { failed: true }
        arguments:
          parameters:
          - { name: target_namespace,    value: "doris-sys" }
          - { name: target_pod_selector, value: "app=doris-be" }
          - { name: loss_percent,        value: "50" }
          - { name: duration,            value: "{{workflow.parameters.chaos_duration}}" }
      - name: load
        templateRef: { name: load-k6-run, template: main }
        arguments:
          parameters:
          - { name: script_configmap, value: "k6-script-doris-be-network-loss" }
          - { name: vus,              value: "5" }
          - { name: duration,         value: "{{workflow.parameters.load_duration}}" }
          - { name: env_map,          value: "{}" }
          - { name: scenario_label,   value: "{{workflow.parameters.scenario_label}}" }
    - - name: verdict
        templateRef: { name: verdict-slo-eval, template: main }
        arguments:
          parameters:
          - { name: slo_yaml,          value: "(unused)" }
          - { name: chaos_result_name, value: "{{steps.chaos.outputs.parameters.chaos_result_name}}-pod-network-loss" }
          - { name: load_start_ts,     value: "{{steps.load.startedAt}}" }
          - { name: chaos_start_after, value: "{{workflow.parameters.chaos_start_after}}" }
          - { name: chaos_duration,    value: "{{workflow.parameters.chaos_duration}}" }
          - { name: load_duration,     value: "{{workflow.parameters.load_duration}}" }
          - { name: metrics_namespace, value: "{{workflow.parameters.scenario_label}}" }
          - { name: workflow_name,     value: "{{workflow.name}}" }

  - name: write-slo
    serviceAccountName: argo-workflow
    script:
      image: bitnami/kubectl:1.30
      command: [bash]
      source: |
        set -euo pipefail
        cat > /tmp/slo.yaml <<'EOF'
        thresholds:
        - metric: p95-latency-chaos
          query: histogram_quantile(0.95, sum(rate(k6_http_req_duration_seconds_bucket{scenario="doris-be-network-loss"}[1m])) by (le))
          lt: 3.0
          window: chaos
        EOF
        kubectl -n dlh-test-fw create configmap dlh-slo-{{workflow.name}} \
          --from-file=slo.yaml=/tmp/slo.yaml --dry-run=client -o yaml | kubectl apply -f -
```

- [ ] **Step 2: Write `doris-be-network-loss-k6-script.yaml`** (same shape as Task 5 with `scenario: doris-be-network-loss`).

- [ ] **Step 3: If Doris deployed, apply both; otherwise commit only**

```bash
git add scenarios/doris-be-network-loss.yaml scenarios/doris-be-network-loss-k6-script.yaml
git commit -m "scenario: doris-be-network-loss (deferred run if target absent)"
```

---

## Task 7: scenarios/README.md

**Files:**
- Create: `scenarios/README.md`

- [ ] **Step 1: Write the README**

```markdown
# Scenarios

Concrete Argo Workflows that exercise the WorkflowTemplate library.

## Run one

    kubectl apply -f scenarios/mysql-pod-delete.yaml
    argo watch -n dlh-test-fw @latest

When the workflow finishes, the verdict step's exit code propagates to the Workflow
status: `Succeeded` = PASS, `Failed` = FAIL. The HTML report can be downloaded
from the Argo UI artifact viewer; the JSON report from the same place.

## SLO is embedded inline

Each scenario's first step (`prep-slo`) materialises a ConfigMap
`dlh-slo-<workflow-name>` containing the SLO YAML. The `verdict-slo-eval`
template mounts it. Edit the inline YAML in the scenario file to tune SLOs.

## k6 scripts live in static ConfigMaps

Per-scenario k6 scripts are static ConfigMaps (`k6-script-<scenario>`)
applied separately. They don't need to be reapplied per workflow run.

## Phase 1 caveat: HTTP proxy stand-in

k6 doesn't ship MySQL/Kafka/Doris drivers. Phase 1 scenarios point k6 at
the in-cluster `httpbin` Service as a stand-in to validate platform mechanics.
For real load testing, swap the URL in the k6 script for a service that
fronts the target system (or use xk6 extensions).

## Adding a new scenario

1. Pick one chaos + one fixture + one load template from the library.
2. Copy `mysql-pod-delete.yaml`; rename and rewire parameters.
3. Adjust the inline SLO YAML in the `write-slo` template.
4. Create a `k6-script-<name>` ConfigMap.
5. `kubectl apply` and `argo watch`.
```

- [ ] **Step 2: Commit**

```bash
git add scenarios/README.md
git commit -m "scenario: README for run/compose"
```

---

## Task 8: Grafana dashboard — `dlh-run-detail`

**Files:**
- Create: `dashboards/grafana/dlh-run-detail.json`

Spec calls for: k6 metric timeseries + chaos window overlay + verdict summary panel. We use templating variable `$workflow` (text input) that drives panel queries.

- [ ] **Step 1: Write a minimal but valid Grafana dashboard JSON**

```json
{
  "title": "DLH — Run Detail",
  "uid": "dlh-run",
  "schemaVersion": 39,
  "tags": ["dlh"],
  "timezone": "",
  "time": { "from": "now-30m", "to": "now" },
  "templating": {
    "list": [
      {
        "name": "workflow",
        "label": "Workflow",
        "type": "textbox",
        "current": { "text": "", "value": "" }
      },
      {
        "name": "scenario",
        "label": "Scenario label",
        "type": "textbox",
        "current": { "text": "", "value": "" }
      }
    ]
  },
  "panels": [
    {
      "type": "timeseries",
      "title": "k6 HTTP req rate",
      "targets": [
        {
          "expr": "sum by (status) (rate(k6_http_reqs_total{scenario=\"$scenario\"}[30s]))",
          "datasource": { "type": "prometheus", "uid": "VictoriaMetrics" }
        }
      ],
      "gridPos": { "x": 0, "y": 0, "w": 12, "h": 8 }
    },
    {
      "type": "timeseries",
      "title": "k6 p95 latency",
      "targets": [
        {
          "expr": "histogram_quantile(0.95, sum(rate(k6_http_req_duration_seconds_bucket{scenario=\"$scenario\"}[30s])) by (le))",
          "datasource": { "type": "prometheus", "uid": "VictoriaMetrics" }
        }
      ],
      "gridPos": { "x": 12, "y": 0, "w": 12, "h": 8 }
    },
    {
      "type": "stat",
      "title": "Verdict summary",
      "targets": [
        {
          "datasource": { "type": "yesoreyeram-infinity-datasource", "uid": "Infinity" },
          "type": "json",
          "source": "url",
          "url": "http://kubernetes.default.svc/api/v1/namespaces/dlh-test-fw/configmaps/dlh-result-$workflow",
          "root_selector": "data['result.json']"
        }
      ],
      "gridPos": { "x": 0, "y": 8, "w": 24, "h": 4 }
    }
  ]
}
```

**Note:** The verdict summary panel uses the Infinity datasource configured in Plan 2 to read the `dlh-result-<workflow>` ConfigMap. The exact URL pattern and `root_selector` may need tweaking after first run — document any tweaks in the dashboard's notes panel.

- [ ] **Step 2: Validate JSON parses**

```bash
jq . dashboards/grafana/dlh-run-detail.json > /dev/null && echo OK
```

Expected: `OK`.

- [ ] **Step 3: Commit**

```bash
git add dashboards/grafana/dlh-run-detail.json
git commit -m "dashboard: dlh-run-detail (k6 metrics + verdict summary)"
```

---

## Task 9: Grafana dashboard — `dlh-history`

**Files:**
- Create: `dashboards/grafana/dlh-history.json`

- [ ] **Step 1: Write `dlh-history.json`**

```json
{
  "title": "DLH — History",
  "uid": "dlh-history",
  "schemaVersion": 39,
  "tags": ["dlh"],
  "time": { "from": "now-7d", "to": "now" },
  "panels": [
    {
      "type": "table",
      "title": "Recent runs",
      "targets": [
        {
          "datasource": { "type": "yesoreyeram-infinity-datasource", "uid": "Infinity" },
          "type": "json",
          "source": "url",
          "url": "http://kubernetes.default.svc/api/v1/namespaces/dlh-test-fw/configmaps?labelSelector=app.kubernetes.io%2Fmanaged-by%3Ddlh-verdict",
          "root_selector": "items"
        }
      ],
      "gridPos": { "x": 0, "y": 0, "w": 24, "h": 12 }
    },
    {
      "type": "timeseries",
      "title": "Mean p95 latency over last 7d (all scenarios)",
      "targets": [
        {
          "expr": "avg by (scenario) (histogram_quantile(0.95, sum(rate(k6_http_req_duration_seconds_bucket[5m])) by (le, scenario)))",
          "datasource": { "type": "prometheus", "uid": "VictoriaMetrics" }
        }
      ],
      "gridPos": { "x": 0, "y": 12, "w": 24, "h": 8 }
    }
  ]
}
```

- [ ] **Step 2: Validate**

```bash
jq . dashboards/grafana/dlh-history.json > /dev/null && echo OK
```

- [ ] **Step 3: Commit**

```bash
git add dashboards/grafana/dlh-history.json
git commit -m "dashboard: dlh-history (recent verdicts + p95 trend)"
```

---

## Task 10: Wrap dashboards as ConfigMaps via Helm

**Files:**
- Create: `helm/dlh-test-fw/templates/dashboards-configmaps.yaml`
- Modify: Copy dashboard JSON into chart files for Helm to read.

Helm chart needs the JSON files accessible. We add them under `helm/dlh-test-fw/files/dashboards/` (mirror from `dashboards/grafana/`) — keep the canonical copy under `dashboards/grafana/` for visibility; chart copies are written via a small `make sync-dashboards` target.

- [ ] **Step 1: Create the chart-side dashboard dir**

```bash
mkdir -p helm/dlh-test-fw/files/dashboards
cp dashboards/grafana/*.json helm/dlh-test-fw/files/dashboards/
```

- [ ] **Step 2: Write the template**

```yaml
{{- range $path, $_ := .Files.Glob "files/dashboards/*.json" }}
{{- $name := base $path | trimSuffix ".json" }}
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: dlh-dashboard-{{ $name }}
  namespace: {{ include "dlh.namespace" $ }}
  labels:
    {{- include "dlh.labels" $ | nindent 4 }}
    dlh-dashboard: "true"          # picked up by grafana sidecar (label set in values.yaml)
data:
  {{ $name }}.json: |-
{{ $.Files.Get $path | indent 4 }}
{{- end }}
```

- [ ] **Step 3: Add a `make sync-dashboards` target**

Append to repo-root Makefile:

```makefile
.PHONY: sync-dashboards
sync-dashboards:
	cp dashboards/grafana/*.json helm/dlh-test-fw/files/dashboards/
```

- [ ] **Step 4: Upgrade chart**

```bash
helm upgrade dlh helm/dlh-test-fw -n dlh-test-fw \
  -f helm/dlh-test-fw/values.yaml -f helm/dlh-test-fw/values-minikube.yaml --wait
kubectl -n dlh-test-fw get cm -l dlh-dashboard=true
```

Expected: two ConfigMaps listed.

- [ ] **Step 5: Wait for Grafana sidecar to import**

```bash
kubectl -n dlh-test-fw logs deploy/dlh-grafana -c grafana-sc-dashboard --tail=50
```

Expected: log lines mentioning `dlh-run.json` and `dlh-history.json` being loaded. Then in Grafana UI, dashboards appear under "General" folder.

- [ ] **Step 6: Commit**

```bash
git add helm/dlh-test-fw/files/dashboards helm/dlh-test-fw/templates/dashboards-configmaps.yaml Makefile
git commit -m "chart: wrap grafana dashboards as labelled ConfigMaps for sidecar import"
```

---

## Task 11: scenario runner script

**Files:**
- Create: `scripts/run-scenario.sh`

- [ ] **Step 1: Write the script**

```bash
#!/usr/bin/env bash
set -euo pipefail
if [[ $# -ne 1 ]]; then
  echo "usage: $0 scenarios/<name>.yaml" >&2
  exit 2
fi
file=$1
name=$(kubectl create -f "$file" -o jsonpath='{.metadata.name}')
echo "Submitted workflow: $name"
argo wait -n dlh-test-fw "$name"
status=$(kubectl -n dlh-test-fw get workflow "$name" -o jsonpath='{.status.phase}')
echo "Final phase: $status"
echo "Report ConfigMap: kubectl -n dlh-test-fw get cm dlh-result-$name -o jsonpath='{.data.result\.json}' | jq ."
[[ "$status" == "Succeeded" ]]
```

- [ ] **Step 2: Make executable + add Makefile target**

```bash
chmod +x scripts/run-scenario.sh
```

Append to Makefile:

```makefile
.PHONY: run-mysql
run-mysql:
	./scripts/run-scenario.sh scenarios/mysql-pod-delete.yaml
```

- [ ] **Step 3: Commit**

```bash
git add scripts/run-scenario.sh Makefile
git commit -m "scenario: run-scenario.sh + make run-mysql target"
```

---

## Task 12: End-to-end smoke (mysql-pod-delete)

This is the climactic verification — proves the whole platform works.

- [ ] **Step 1: Pre-flight: upload a tiny mysql fixture to MinIO**

```bash
# Create a 2-row SQL fixture and push to s3://fixtures/mysql-users.sql.
kubectl -n dlh-test-fw run mc-seed --rm -it --restart=Never \
  --image=minio/mc:RELEASE.2024-11-21T17-21-54Z \
  --env="A=admin" --env="P=dlh-dev-secret-please-rotate" -- \
  /bin/sh -c '
    set -e
    mc alias set dlh http://dlh-minio.dlh-test-fw.svc.cluster.local:9000 "$A" "$P"
    echo "CREATE TABLE IF NOT EXISTS users (id INT PRIMARY KEY, name VARCHAR(64));" > /tmp/f.sql
    echo "INSERT INTO users VALUES (1,\"a\"),(2,\"b\");" >> /tmp/f.sql
    mc cp /tmp/f.sql dlh/fixtures/mysql-users.sql
    mc ls dlh/fixtures
  '
```

- [ ] **Step 2: Run the scenario**

```bash
make run-mysql
```

Expected: workflow reaches `Succeeded` within 5 minutes (load_duration=180s + scheduling overhead). If it doesn't:
- `argo logs -n dlh-test-fw @latest` to see step failures
- `kubectl -n dlh-test-fw describe workflow <name>` for orchestration issues
- `kubectl -n dlh-test-fw get chaosresult` to confirm Litmus actually ran chaos
- `kubectl -n dlh-test-fw get cm dlh-result-<workflow> -o yaml` to see verdict report

- [ ] **Step 3: Inspect the report**

```bash
WF=$(kubectl -n dlh-test-fw get wf --sort-by=.metadata.creationTimestamp -o name | tail -1 | cut -d/ -f2)
kubectl -n dlh-test-fw get cm "dlh-result-$WF" -o jsonpath='{.data.result\.json}' | jq .
```

Expected: JSON with `"overall": true|false`, threshold results, chaos verdict.

- [ ] **Step 4: Open Argo UI and confirm artifacts**

Visit `http://argo.dlh.local/workflows/dlh-test-fw/<wf-name>`. Click the `verdict` step. The Artifacts tab should list `report` (containing report.json + report.html). Open report.html in the artifact viewer — should show the PASS/FAIL banner.

- [ ] **Step 5: Open Grafana**

Visit `http://grafana.dlh.local`. Find dashboard "DLH — Run Detail". Set the `workflow` variable to the workflow name and `scenario` to `mysql-pod-delete`. Verify panels show k6 metrics and the verdict summary stat.

- [ ] **Step 6: No commit. (If you adjusted dashboard JSON for the Infinity URL pattern, sync via `make sync-dashboards`, helm upgrade, and commit.)**

---

## Task 13: End-to-end smoke (kafka-broker-partition)

- [ ] **Step 1: Upload kafka fixture**

```bash
kubectl -n dlh-test-fw run mc-seed --rm -it --restart=Never \
  --image=minio/mc:RELEASE.2024-11-21T17-21-54Z \
  --env="A=admin" --env="P=dlh-dev-secret-please-rotate" -- \
  /bin/sh -c '
    set -e
    mc alias set dlh http://dlh-minio.dlh-test-fw.svc.cluster.local:9000 "$A" "$P"
    printf "{\"id\":1}\n{\"id\":2}\n{\"id\":3}\n" > /tmp/e.jsonl
    mc cp /tmp/e.jsonl dlh/fixtures/kafka-events.jsonl
  '
```

- [ ] **Step 2: Run**

```bash
kubectl apply -f scenarios/kafka-broker-partition.yaml
./scripts/run-scenario.sh scenarios/kafka-broker-partition.yaml || true   # may need: kubectl create -f then argo wait
```

(Note: the runner script as written creates from file each invocation. Two `apply`s and a `wait` are equivalent — adjust as needed.)

- [ ] **Step 3: Inspect**

Same pattern as Task 12 Step 3.

- [ ] **Step 4: No commit unless issues surface that need code/config changes.**

---

## Task 14: Doris smoke (skip if target absent)

- [ ] **Step 1: If Doris target was deployed in Task 3, prep fixture + run scenario.**
- [ ] **Step 2: Otherwise mark in `scenarios/README.md` as deferred and commit that note.**

---

## Task 15: Final verification + tag

- [ ] **Step 1: Confirm all artifacts exist**

```bash
cd /Users/allen/repo/dlh-test-fw
test -f scenarios/mysql-pod-delete.yaml
test -f scenarios/kafka-broker-partition.yaml
test -f scenarios/doris-be-network-loss.yaml
test -f dashboards/grafana/dlh-run-detail.json
test -f dashboards/grafana/dlh-history.json
kubectl -n dlh-test-fw get cm -l dlh-dashboard=true | grep -c dlh-dashboard
echo "all present"
```

- [ ] **Step 2: At least mysql + kafka scenarios completed with `Succeeded` status at least once**

```bash
kubectl -n dlh-test-fw get wf | awk '$2 == "Succeeded"'
```

Expected: at least two rows.

- [ ] **Step 3: Tag the milestone**

```bash
git tag -a phase-1-mvp -m "Phase 1 MVP: chaos + load test platform end-to-end on minikube"
```

---

## Definition of Done

- [ ] All three scenario YAMLs exist and pass `kubectl apply --dry-run=server` (i.e. structurally valid against the cluster).
- [ ] Both dashboards exist as ConfigMaps with `dlh-dashboard=true` label; Grafana sidecar imported them.
- [ ] `make run-mysql` produces a `Succeeded` workflow with a populated `dlh-result-<wf>` ConfigMap and a `report.html` artifact in MinIO `artifacts` bucket.
- [ ] kafka scenario also runs at least once to Succeeded (or documented Failed with reason).
- [ ] doris scenario: deployed and run, OR documented as deferred in `scenarios/README.md`.
- [ ] Git tag `phase-1-mvp` exists.

---

## Self-Review Notes

- **Spec coverage:**
  - "Workflow 編排（順序保證）" two-dash pattern → Tasks 4-6 scenario steps. ✓
  - `continueOn: {failed: true}` on chaos, no `continueOn` on fixture (fail-fast) → Tasks 4-6. ✓
  - `load_start_ts = "{{steps.load.startedAt}}"` → Tasks 4-6 verdict step. ✓
  - 3 example scenarios named in spec → Tasks 4-6. ✓
  - 2 Grafana dashboards in spec → Tasks 8-9. ✓
  - "結果檢視路徑" (Argo UI / Grafana DLH Run Detail / DLH History / MinIO Console / `argo wait` exit code) → all reachable after Task 12. ✓
- **Placeholders:** None.
- **Type consistency:**
  - `chaos_result_name` from chaos step gets concatenated with the experiment-name suffix in verdict step arguments — names line up:
    - pod-delete → `-pod-delete` (Task 4) ✓
    - kafka-broker-partition uses underlying experiment `pod-network-partition` → `-pod-network-partition` (Task 5) ✓
    - network-loss uses `pod-network-loss` → `-pod-network-loss` (Task 6) ✓
  - `dlh-slo-{{workflow.name}}` ConfigMap name created in `write-slo` script matches what `verdict-slo-eval` (Plan 4 Task 13) mounts. ✓
  - `metrics_namespace` parameter naming is consistent between Plan 4's `load-k6-run` output and Plan 4's `verdict-slo-eval` input, and used identically in all three scenarios. ✓
  - Image tags `dlh-verdict:0.1.0`, `dlh-fixture-*:0.1.0` — all loaded into minikube in earlier plans; consumed here through the WorkflowTemplate library, not referenced directly in scenarios. ✓
- **Caveats deliberately accepted:** Phase 1 k6 scripts hit httpbin not the real target — documented in `scenarios/README.md`. Real load against MySQL/Kafka/Doris is a Phase 1.5 follow-up (k6 SQL/kafka extensions or HTTP proxy services in front of each target). The platform mechanics are fully exercised regardless.
