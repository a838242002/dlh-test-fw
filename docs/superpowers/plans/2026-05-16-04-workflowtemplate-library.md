# Plan 4 — WorkflowTemplate Library + Fixture Images Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Produce the reusable `WorkflowTemplate` library (fixture × 3, chaos × 4, load × 1, verdict × 1) plus three fixture container images (`dlh-fixture-mysql`, `dlh-fixture-doris`, `dlh-fixture-kafka`). After this plan, an engineer can write a `scenarios/<name>.yaml` Workflow that does `templateRef:` against any of these and have it execute end-to-end on the minikube platform stood up in Plan 2 using the verdict binary built in Plan 3.

**Architecture:** Source-of-truth WorkflowTemplate YAMLs live under `helm/dlh-test-fw/files/workflowtemplates/<category>/<name>.yaml` so the umbrella chart can glob them in via `{{ .Files.Glob }}`. Each template is parameterised with explicit `inputs.parameters` matching the table in the spec's "WorkflowTemplate 函式庫" section. Fixture container images are built from `fixture-images/<target>/Dockerfile` and loaded into minikube via `minikube image load`. Chaos templates wrap Litmus `ChaosEngine`; the `chaos/from-hub` variant fetches experiments dynamically from `chaos-hub`. The `verdict/slo-eval` template runs the `dlh-verdict:0.1.0` container produced by Plan 3, passes Argo's `{{steps.load.startedAt}}` as `--load-start-ts`, and emits `report.json` + `report.html` as Argo artifacts.

**Tech Stack:** Argo Workflows v3 (`argoproj.io/v1alpha1`), Litmus `ChaosEngine` (`litmuschaos.io/v1alpha1`), k6-operator `TestRun` (`k6.io/v1alpha1`, confirmed by Plan 1 FINDINGS), shell + `mc` + `mysql` / `curl` / `kcat`, Helm `.Files.Glob`.

**Prerequisites:** Plans 1, 2, 3 complete. `dlh-verdict:0.1.0` image loaded into minikube. Platform pods Ready.

**Out of scope:** Example scenarios + Grafana dashboards (Plan 5).

---

## File Structure

```
helm/dlh-test-fw/
├── templates/
│   └── dlh-workflowtemplates.yaml          # MODIFIED: glob workflowtemplates files
└── files/
    └── workflowtemplates/
        ├── fixture/
        │   ├── minio-load-mysql.yaml
        │   ├── minio-load-doris.yaml
        │   └── kafka-topic-seed.yaml
        ├── chaos/
        │   ├── pod-delete.yaml
        │   ├── network-loss.yaml
        │   ├── kafka-broker-partition.yaml
        │   └── from-hub.yaml
        ├── load/
        │   └── k6-run.yaml
        └── verdict/
            └── slo-eval.yaml
fixture-images/
├── mysql/Dockerfile
├── doris/Dockerfile
└── kafka/Dockerfile
scripts/
└── verify-templates.sh                      # confirms all 9 WTs registered
Makefile                                     # MODIFIED: add fixture-images target
```

Responsibilities:
- `helm/dlh-test-fw/templates/dlh-workflowtemplates.yaml` — Helm template that emits one `WorkflowTemplate` per YAML file under `files/workflowtemplates/`.
- Each `files/workflowtemplates/<cat>/<name>.yaml` — a single `WorkflowTemplate` resource. Plain YAML with Helm `{{ }}` substitutions where we need values from `values.yaml` (e.g. VM URL, verdict image tag, chaos hub URL).
- `fixture-images/<target>/Dockerfile` — alpine + `mc` (MinIO client) + target-specific CLI (mysql / curl / kcat).
- `scripts/verify-templates.sh` — `kubectl -n dlh-test-fw get workflowtemplate` and asserts all nine expected names are present.

---

## Task 1: Chart wiring — glob WorkflowTemplate files

**Files:**
- Modify: `helm/dlh-test-fw/templates/dlh-workflowtemplates.yaml`

- [ ] **Step 1: Replace the placeholder with a glob template**

```yaml
{{- range $path, $_ := .Files.Glob "files/workflowtemplates/**/*.yaml" }}
---
{{ tpl ($.Files.Get $path) $ }}
{{- end }}
```

- [ ] **Step 2: helm lint with current (still empty) glob to confirm no syntax error**

```bash
cd /Users/allen/repo/dlh-test-fw
helm lint helm/dlh-test-fw
```

Expected: clean (or "no matches for glob" warning — acceptable until we add files).

- [ ] **Step 3: Commit**

```bash
git add helm/dlh-test-fw/templates/dlh-workflowtemplates.yaml
git commit -m "chart: glob WorkflowTemplate files into release"
```

---

## Task 2: Fixture image — mysql

**Files:**
- Create: `fixture-images/mysql/Dockerfile`

- [ ] **Step 1: Write the Dockerfile**

```dockerfile
FROM alpine:3.20
RUN apk add --no-cache mysql-client bash ca-certificates \
 && wget -q https://dl.min.io/client/mc/release/linux-amd64/mc -O /usr/local/bin/mc \
 && chmod +x /usr/local/bin/mc
WORKDIR /work
ENTRYPOINT ["/bin/bash", "-c"]
```

- [ ] **Step 2: Build + load into minikube**

```bash
docker build -t dlh-fixture-mysql:0.1.0 fixture-images/mysql
minikube image load dlh-fixture-mysql:0.1.0
minikube image ls | grep dlh-fixture-mysql
```

Expected: image listed.

- [ ] **Step 3: Smoke-test the image inside minikube**

```bash
kubectl -n dlh-test-fw run mysql-fixture-smoke --rm -it --restart=Never \
  --image=dlh-fixture-mysql:0.1.0 --image-pull-policy=Never -- \
  'mc --version && mysql --version'
```

Expected: prints `mc version …` and `mysql Ver …`.

- [ ] **Step 4: Commit**

```bash
git add fixture-images/mysql/Dockerfile
git commit -m "fixture-image: mysql (alpine + mc + mysql client)"
```

---

## Task 3: Fixture image — doris

**Files:**
- Create: `fixture-images/doris/Dockerfile`

- [ ] **Step 1: Write the Dockerfile**

```dockerfile
FROM alpine:3.20
RUN apk add --no-cache curl bash ca-certificates \
 && wget -q https://dl.min.io/client/mc/release/linux-amd64/mc -O /usr/local/bin/mc \
 && chmod +x /usr/local/bin/mc
WORKDIR /work
ENTRYPOINT ["/bin/bash", "-c"]
```

(Doris ingestion is HTTP Stream Load → only `curl` needed.)

- [ ] **Step 2: Build + load**

```bash
docker build -t dlh-fixture-doris:0.1.0 fixture-images/doris
minikube image load dlh-fixture-doris:0.1.0
minikube image ls | grep dlh-fixture-doris
```

- [ ] **Step 3: Smoke**

```bash
kubectl -n dlh-test-fw run doris-fixture-smoke --rm -it --restart=Never \
  --image=dlh-fixture-doris:0.1.0 --image-pull-policy=Never -- \
  'mc --version && curl --version | head -1'
```

- [ ] **Step 4: Commit**

```bash
git add fixture-images/doris/Dockerfile
git commit -m "fixture-image: doris (alpine + mc + curl for Stream Load)"
```

---

## Task 4: Fixture image — kafka

**Files:**
- Create: `fixture-images/kafka/Dockerfile`

- [ ] **Step 1: Write the Dockerfile**

```dockerfile
FROM alpine:3.20
RUN apk add --no-cache kcat bash ca-certificates \
 && wget -q https://dl.min.io/client/mc/release/linux-amd64/mc -O /usr/local/bin/mc \
 && chmod +x /usr/local/bin/mc
WORKDIR /work
ENTRYPOINT ["/bin/bash", "-c"]
```

- [ ] **Step 2: Build + load**

```bash
docker build -t dlh-fixture-kafka:0.1.0 fixture-images/kafka
minikube image load dlh-fixture-kafka:0.1.0
minikube image ls | grep dlh-fixture-kafka
```

- [ ] **Step 3: Smoke**

```bash
kubectl -n dlh-test-fw run kafka-fixture-smoke --rm -it --restart=Never \
  --image=dlh-fixture-kafka:0.1.0 --image-pull-policy=Never -- \
  'mc --version && kcat -V'
```

- [ ] **Step 4: Add Makefile target to rebuild all three**

Append to repo-root `Makefile`:

```makefile
.PHONY: fixture-images
fixture-images:
	for d in mysql doris kafka; do \
	  docker build -t dlh-fixture-$$d:0.1.0 fixture-images/$$d && \
	  minikube image load dlh-fixture-$$d:0.1.0 ; \
	done
```

- [ ] **Step 5: Commit**

```bash
git add fixture-images/kafka/Dockerfile Makefile
git commit -m "fixture-image: kafka (alpine + mc + kcat); make target for all 3"
```

---

## Task 5: WT — `fixture/minio-load-mysql`

**Files:**
- Create: `helm/dlh-test-fw/files/workflowtemplates/fixture/minio-load-mysql.yaml`

Inputs (per spec): `uri` (s3://...), `db_host`, `credentials_secret`. Outputs: `loaded_rows`, `duration_sec`.

- [ ] **Step 1: Write the WorkflowTemplate**

```yaml
apiVersion: argoproj.io/v1alpha1
kind: WorkflowTemplate
metadata:
  name: fixture-minio-load-mysql
  labels:
    dlh.category: fixture
spec:
  entrypoint: main
  templates:
  - name: main
    inputs:
      parameters:
      - name: uri                    # e.g. s3://fixtures/users.sql
      - name: db_host                # mysql.default.svc.cluster.local
      - name: credentials_secret     # K8s Secret with keys: user, password, database
    outputs:
      parameters:
      - name: loaded_rows
        valueFrom: { path: /tmp/loaded_rows }
      - name: duration_sec
        valueFrom: { path: /tmp/duration_sec }
    container:
      image: dlh-fixture-mysql:0.1.0
      imagePullPolicy: Never        # local minikube load
      env:
      - name: MINIO_USER
        valueFrom: { secretKeyRef: { name: minio-artifact-creds, key: accesskey } }
      - name: MINIO_PASS
        valueFrom: { secretKeyRef: { name: minio-artifact-creds, key: secretkey } }
      - name: DB_USER
        valueFrom: { secretKeyRef: { name: "{{inputs.parameters.credentials_secret}}", key: user } }
      - name: DB_PASS
        valueFrom: { secretKeyRef: { name: "{{inputs.parameters.credentials_secret}}", key: password } }
      - name: DB_NAME
        valueFrom: { secretKeyRef: { name: "{{inputs.parameters.credentials_secret}}", key: database } }
      args:
      - |
        set -euo pipefail
        start=$(date +%s)
        mc alias set dlh http://dlh-minio.{{ .Release.Namespace }}.svc.cluster.local:9000 "$MINIO_USER" "$MINIO_PASS"
        path=$(echo "{{`{{inputs.parameters.uri}}`}}" | sed 's|^s3://|dlh/|')
        mc cp "$path" /tmp/fixture.sql
        mysql -h "{{`{{inputs.parameters.db_host}}`}}" -u "$DB_USER" -p"$DB_PASS" "$DB_NAME" < /tmp/fixture.sql
        rows=$(mysql -h "{{`{{inputs.parameters.db_host}}`}}" -u "$DB_USER" -p"$DB_PASS" -N -e "SELECT ROW_COUNT()" "$DB_NAME" || echo 0)
        echo "$rows" > /tmp/loaded_rows
        echo $(( $(date +%s) - start )) > /tmp/duration_sec
```

**Helm `{{` escape note:** WorkflowTemplates use Argo's `{{inputs.parameters.X}}` syntax which collides with Helm. The pattern `{{`{{inputs.parameters.X}}`}}` wraps the inner braces in a Helm literal — Helm renders it to `{{inputs.parameters.X}}` which Argo then resolves at workflow time. Helm-side substitutions (e.g. `{{ .Release.Namespace }}`) are left bare.

- [ ] **Step 2: Render via helm template, inspect Argo-side syntax is intact**

```bash
helm template dlh helm/dlh-test-fw -f helm/dlh-test-fw/values.yaml \
  --show-only templates/dlh-workflowtemplates.yaml | grep -A2 'fixture-minio-load-mysql'
```

Expected: literal `{{inputs.parameters.uri}}` survives in rendered output.

- [ ] **Step 3: Apply (assumes platform is up; chart already installed)**

```bash
helm upgrade dlh helm/dlh-test-fw -n dlh-test-fw \
  -f helm/dlh-test-fw/values.yaml -f helm/dlh-test-fw/values-minikube.yaml --wait
kubectl -n dlh-test-fw get workflowtemplate fixture-minio-load-mysql
```

Expected: WorkflowTemplate exists.

- [ ] **Step 4: Commit**

```bash
git add helm/dlh-test-fw/files/workflowtemplates/fixture/minio-load-mysql.yaml
git commit -m "wt(fixture): minio-load-mysql"
```

---

## Task 6: WT — `fixture/minio-load-doris`

**Files:**
- Create: `helm/dlh-test-fw/files/workflowtemplates/fixture/minio-load-doris.yaml`

- [ ] **Step 1: Write the template**

```yaml
apiVersion: argoproj.io/v1alpha1
kind: WorkflowTemplate
metadata:
  name: fixture-minio-load-doris
  labels: { dlh.category: fixture }
spec:
  entrypoint: main
  templates:
  - name: main
    inputs:
      parameters:
      - name: uri
      - name: fe_host                                  # Doris FE host:port
      - name: stream_load_credentials_secret           # keys: user, password, database, table
    outputs:
      parameters:
      - name: loaded_rows
        valueFrom: { path: /tmp/loaded_rows }
      - name: duration_sec
        valueFrom: { path: /tmp/duration_sec }
    container:
      image: dlh-fixture-doris:0.1.0
      imagePullPolicy: Never
      env:
      - name: MINIO_USER
        valueFrom: { secretKeyRef: { name: minio-artifact-creds, key: accesskey } }
      - name: MINIO_PASS
        valueFrom: { secretKeyRef: { name: minio-artifact-creds, key: secretkey } }
      - name: DR_USER
        valueFrom: { secretKeyRef: { name: "{{inputs.parameters.stream_load_credentials_secret}}", key: user } }
      - name: DR_PASS
        valueFrom: { secretKeyRef: { name: "{{inputs.parameters.stream_load_credentials_secret}}", key: password } }
      - name: DR_DB
        valueFrom: { secretKeyRef: { name: "{{inputs.parameters.stream_load_credentials_secret}}", key: database } }
      - name: DR_TABLE
        valueFrom: { secretKeyRef: { name: "{{inputs.parameters.stream_load_credentials_secret}}", key: table } }
      args:
      - |
        set -euo pipefail
        start=$(date +%s)
        mc alias set dlh http://dlh-minio.{{ .Release.Namespace }}.svc.cluster.local:9000 "$MINIO_USER" "$MINIO_PASS"
        path=$(echo "{{`{{inputs.parameters.uri}}`}}" | sed 's|^s3://|dlh/|')
        mc cp "$path" /tmp/fixture.csv
        resp=$(curl -sS -u "$DR_USER:$DR_PASS" \
          -H 'expect:100-continue' \
          -H 'columns: <set in scenario or default>' \
          -H 'column_separator:,' \
          -T /tmp/fixture.csv \
          "http://{{`{{inputs.parameters.fe_host}}`}}/api/$DR_DB/$DR_TABLE/_stream_load")
        echo "$resp"
        rows=$(echo "$resp" | sed -n 's/.*"NumberLoadedRows":\([0-9]*\).*/\1/p')
        echo "${rows:-0}" > /tmp/loaded_rows
        echo $(( $(date +%s) - start )) > /tmp/duration_sec
```

- [ ] **Step 2: Helm upgrade + verify**

```bash
helm upgrade dlh helm/dlh-test-fw -n dlh-test-fw \
  -f helm/dlh-test-fw/values.yaml -f helm/dlh-test-fw/values-minikube.yaml --wait
kubectl -n dlh-test-fw get workflowtemplate fixture-minio-load-doris
```

- [ ] **Step 3: Commit**

```bash
git add helm/dlh-test-fw/files/workflowtemplates/fixture/minio-load-doris.yaml
git commit -m "wt(fixture): minio-load-doris via Stream Load HTTP API"
```

---

## Task 7: WT — `fixture/kafka-topic-seed`

**Files:**
- Create: `helm/dlh-test-fw/files/workflowtemplates/fixture/kafka-topic-seed.yaml`

- [ ] **Step 1: Write the template**

```yaml
apiVersion: argoproj.io/v1alpha1
kind: WorkflowTemplate
metadata:
  name: fixture-kafka-topic-seed
  labels: { dlh.category: fixture }
spec:
  entrypoint: main
  templates:
  - name: main
    inputs:
      parameters:
      - name: bootstrap
      - name: topic
      - name: seed_uri                # s3://fixtures/events.jsonl (one record per line)
    outputs:
      parameters:
      - name: produced_count
        valueFrom: { path: /tmp/produced_count }
    container:
      image: dlh-fixture-kafka:0.1.0
      imagePullPolicy: Never
      env:
      - name: MINIO_USER
        valueFrom: { secretKeyRef: { name: minio-artifact-creds, key: accesskey } }
      - name: MINIO_PASS
        valueFrom: { secretKeyRef: { name: minio-artifact-creds, key: secretkey } }
      args:
      - |
        set -euo pipefail
        mc alias set dlh http://dlh-minio.{{ .Release.Namespace }}.svc.cluster.local:9000 "$MINIO_USER" "$MINIO_PASS"
        path=$(echo "{{`{{inputs.parameters.seed_uri}}`}}" | sed 's|^s3://|dlh/|')
        mc cp "$path" /tmp/seed.jsonl
        count=$(wc -l < /tmp/seed.jsonl)
        kcat -b "{{`{{inputs.parameters.bootstrap}}`}}" -t "{{`{{inputs.parameters.topic}}`}}" -P -l /tmp/seed.jsonl
        echo "$count" > /tmp/produced_count
```

- [ ] **Step 2: Upgrade + verify**

```bash
helm upgrade dlh helm/dlh-test-fw -n dlh-test-fw -f helm/dlh-test-fw/values.yaml -f helm/dlh-test-fw/values-minikube.yaml --wait
kubectl -n dlh-test-fw get workflowtemplate fixture-kafka-topic-seed
```

- [ ] **Step 3: Commit**

```bash
git add helm/dlh-test-fw/files/workflowtemplates/fixture/kafka-topic-seed.yaml
git commit -m "wt(fixture): kafka-topic-seed via kcat -P"
```

---

## Task 8: WT — `chaos/pod-delete`

**Files:**
- Create: `helm/dlh-test-fw/files/workflowtemplates/chaos/pod-delete.yaml`

Wraps Litmus' `pod-delete` ChaosExperiment via a `ChaosEngine` CR. The template creates the CR, waits for the ChaosResult to appear, and outputs the ChaosResult name for verdict.

- [ ] **Step 1: Write the template**

```yaml
apiVersion: argoproj.io/v1alpha1
kind: WorkflowTemplate
metadata:
  name: chaos-pod-delete
  labels: { dlh.category: chaos }
spec:
  entrypoint: main
  serviceAccountName: argo-workflow
  templates:
  - name: main
    inputs:
      parameters:
      - name: target_namespace
      - name: target_pod_selector       # e.g. app=mysql
      - name: duration                  # seconds, e.g. "60"
      - name: interval                  # seconds between kills, e.g. "10"
      - name: force                     # "true" / "false"
    outputs:
      parameters:
      - name: chaos_result_name
        valueFrom: { path: /tmp/cr_name }
    resource:
      action: create
      successCondition: status.experimentStatus.verdict in (Pass, Fail, Stopped)
      manifest: |
        apiVersion: litmuschaos.io/v1alpha1
        kind: ChaosEngine
        metadata:
          generateName: dlh-pod-delete-
          namespace: {{`{{workflow.namespace}}`}}
        spec:
          appinfo:
            appns: {{`{{inputs.parameters.target_namespace}}`}}
            applabel: {{`{{inputs.parameters.target_pod_selector}}`}}
            appkind: deployment
          chaosServiceAccount: litmus
          experiments:
          - name: pod-delete
            spec:
              components:
                env:
                - name: TOTAL_CHAOS_DURATION
                  value: {{`"{{inputs.parameters.duration}}"`}}
                - name: CHAOS_INTERVAL
                  value: {{`"{{inputs.parameters.interval}}"`}}
                - name: FORCE
                  value: {{`"{{inputs.parameters.force}}"`}}
    # Argo "resource" templates can stash created object's name via outputs.parameters.jsonPath.
    # We extract the ChaosResult name (engine_name + "-pod-delete").
```

**Note:** Argo's `resource` template can populate outputs via `jsonPath`. Per Litmus convention the `ChaosResult` is named `<engine>-<experiment>`. Add an `outputs.parameters.jsonPath` for the engine name and then concatenate. Replace the `outputs:` block with:

```yaml
    outputs:
      parameters:
      - name: chaos_result_name
        valueFrom:
          jsonPath: '{.metadata.name}'
        # The post-processing concat happens in the calling scenario or verdict template:
        # the verdict step receives chaos_result_name and appends "-pod-delete".
```

Document this naming convention in the file header so Plan 5 scenarios get it right.

- [ ] **Step 2: Upgrade + verify**

```bash
helm upgrade dlh helm/dlh-test-fw -n dlh-test-fw -f helm/dlh-test-fw/values.yaml -f helm/dlh-test-fw/values-minikube.yaml --wait
kubectl -n dlh-test-fw get workflowtemplate chaos-pod-delete
```

- [ ] **Step 3: Commit**

```bash
git add helm/dlh-test-fw/files/workflowtemplates/chaos/pod-delete.yaml
git commit -m "wt(chaos): pod-delete wrapping Litmus ChaosEngine"
```

---

## Task 9: WT — `chaos/network-loss`

**Files:**
- Create: `helm/dlh-test-fw/files/workflowtemplates/chaos/network-loss.yaml`

Same shape as pod-delete; underlying experiment is `pod-network-loss`.

- [ ] **Step 1: Write the template**

```yaml
apiVersion: argoproj.io/v1alpha1
kind: WorkflowTemplate
metadata:
  name: chaos-network-loss
  labels: { dlh.category: chaos }
spec:
  entrypoint: main
  serviceAccountName: argo-workflow
  templates:
  - name: main
    inputs:
      parameters:
      - name: target_namespace
      - name: target_pod_selector
      - name: loss_percent
      - name: duration
    outputs:
      parameters:
      - name: chaos_result_name
        valueFrom:
          jsonPath: '{.metadata.name}'
    resource:
      action: create
      successCondition: status.experimentStatus.verdict in (Pass, Fail, Stopped)
      manifest: |
        apiVersion: litmuschaos.io/v1alpha1
        kind: ChaosEngine
        metadata:
          generateName: dlh-network-loss-
          namespace: {{`{{workflow.namespace}}`}}
        spec:
          appinfo:
            appns: {{`{{inputs.parameters.target_namespace}}`}}
            applabel: {{`{{inputs.parameters.target_pod_selector}}`}}
            appkind: deployment
          chaosServiceAccount: litmus
          experiments:
          - name: pod-network-loss
            spec:
              components:
                env:
                - name: TOTAL_CHAOS_DURATION
                  value: {{`"{{inputs.parameters.duration}}"`}}
                - name: NETWORK_PACKET_LOSS_PERCENTAGE
                  value: {{`"{{inputs.parameters.loss_percent}}"`}}
```

- [ ] **Step 2: Upgrade + verify**

```bash
helm upgrade dlh helm/dlh-test-fw -n dlh-test-fw -f helm/dlh-test-fw/values.yaml -f helm/dlh-test-fw/values-minikube.yaml --wait
kubectl -n dlh-test-fw get workflowtemplate chaos-network-loss
```

- [ ] **Step 3: Commit**

```bash
git add helm/dlh-test-fw/files/workflowtemplates/chaos/network-loss.yaml
git commit -m "wt(chaos): network-loss wrapping Litmus pod-network-loss"
```

---

## Task 10: WT — `chaos/kafka-broker-partition`

**Files:**
- Create: `helm/dlh-test-fw/files/workflowtemplates/chaos/kafka-broker-partition.yaml`

- [ ] **Step 1: Write the template**

```yaml
apiVersion: argoproj.io/v1alpha1
kind: WorkflowTemplate
metadata:
  name: chaos-kafka-broker-partition
  labels: { dlh.category: chaos }
spec:
  entrypoint: main
  serviceAccountName: argo-workflow
  templates:
  - name: main
    inputs:
      parameters:
      - name: kafka_namespace
      - name: broker_id
      - name: duration
    outputs:
      parameters:
      - name: chaos_result_name
        valueFrom:
          jsonPath: '{.metadata.name}'
    resource:
      action: create
      successCondition: status.experimentStatus.verdict in (Pass, Fail, Stopped)
      manifest: |
        apiVersion: litmuschaos.io/v1alpha1
        kind: ChaosEngine
        metadata:
          generateName: dlh-kafka-partition-
          namespace: {{`{{workflow.namespace}}`}}
        spec:
          appinfo:
            appns: {{`{{inputs.parameters.kafka_namespace}}`}}
            applabel: "kafka.broker.id={{`{{inputs.parameters.broker_id}}`}}"
            appkind: statefulset
          chaosServiceAccount: litmus
          experiments:
          - name: pod-network-partition
            spec:
              components:
                env:
                - name: TOTAL_CHAOS_DURATION
                  value: {{`"{{inputs.parameters.duration}}"`}}
                - name: POLICY_TYPES
                  value: "all"
```

- [ ] **Step 2: Upgrade + verify**

- [ ] **Step 3: Commit**

```bash
git add helm/dlh-test-fw/files/workflowtemplates/chaos/kafka-broker-partition.yaml
git commit -m "wt(chaos): kafka-broker-partition via Litmus pod-network-partition"
```

---

## Task 11: WT — `chaos/from-hub`

**Files:**
- Create: `helm/dlh-test-fw/files/workflowtemplates/chaos/from-hub.yaml`

Dynamically pulls a ChaosExperiment manifest from ChaosHub and applies it. `args_map` is a JSON-string of env var overrides.

- [ ] **Step 1: Write the template**

```yaml
apiVersion: argoproj.io/v1alpha1
kind: WorkflowTemplate
metadata:
  name: chaos-from-hub
  labels: { dlh.category: chaos }
spec:
  entrypoint: main
  serviceAccountName: argo-workflow
  templates:
  - name: main
    inputs:
      parameters:
      - name: hub_url                   # https://github.com/litmuschaos/chaos-charts.git
        value: "{{ .Values.platform.chaosHub.url }}"
      - name: hub_ref
        value: "{{ .Values.platform.chaosHub.ref }}"
      - name: experiment_name           # e.g. "pod-cpu-hog"
      - name: target_namespace
      - name: target_pod_selector
      - name: args_map                  # JSON: {"TOTAL_CHAOS_DURATION":"60", ...}
    outputs:
      parameters:
      - name: chaos_result_name
        valueFrom: { path: /tmp/cr_name }
    script:
      image: alpine/git:2.43.0
      command: [sh]
      source: |
        set -euo pipefail
        apk add --no-cache curl jq yq kubectl >/dev/null
        git clone --depth 1 --branch "{{`{{inputs.parameters.hub_ref}}`}}" \
          "{{`{{inputs.parameters.hub_url}}`}}" /tmp/hub
        exp="{{`{{inputs.parameters.experiment_name}}`}}"
        manifest=$(find /tmp/hub -path "*/$exp/experiment.yaml" | head -1)
        if [ -z "$manifest" ]; then echo "experiment $exp not found in hub" >&2; exit 1; fi
        kubectl apply -n {{`{{workflow.namespace}}`}} -f "$manifest"
        # Build ChaosEngine wrapping it.
        cat > /tmp/engine.yaml <<EOF
        apiVersion: litmuschaos.io/v1alpha1
        kind: ChaosEngine
        metadata:
          generateName: dlh-hub-${exp}-
          namespace: {{`{{workflow.namespace}}`}}
        spec:
          appinfo:
            appns: {{`{{inputs.parameters.target_namespace}}`}}
            applabel: {{`{{inputs.parameters.target_pod_selector}}`}}
            appkind: deployment
          chaosServiceAccount: litmus
          experiments:
          - name: ${exp}
            spec:
              components:
                env: []
        EOF
        # Inject args_map entries as env list.
        envs=$(echo '{{`{{inputs.parameters.args_map}}`}}' | jq -r 'to_entries | map({name: .key, value: (.value|tostring)})')
        yq -i ".spec.experiments[0].spec.components.env = $envs" /tmp/engine.yaml
        engine_name=$(kubectl create -n {{`{{workflow.namespace}}`}} -f /tmp/engine.yaml -o jsonpath='{.metadata.name}')
        echo "$engine_name-$exp" > /tmp/cr_name
        # Wait until ChaosResult reaches terminal verdict.
        for i in $(seq 1 60); do
          v=$(kubectl -n {{`{{workflow.namespace}}`}} get chaosresult "$engine_name-$exp" -o jsonpath='{.status.experimentStatus.verdict}' 2>/dev/null || true)
          if [ "$v" = "Pass" ] || [ "$v" = "Fail" ] || [ "$v" = "Stopped" ]; then exit 0; fi
          sleep 5
        done
        echo "timed out waiting for ChaosResult" >&2
        exit 1
```

- [ ] **Step 2: Upgrade + verify**

```bash
helm upgrade dlh helm/dlh-test-fw -n dlh-test-fw -f helm/dlh-test-fw/values.yaml -f helm/dlh-test-fw/values-minikube.yaml --wait
kubectl -n dlh-test-fw get workflowtemplate chaos-from-hub
```

- [ ] **Step 3: Commit**

```bash
git add helm/dlh-test-fw/files/workflowtemplates/chaos/from-hub.yaml
git commit -m "wt(chaos): from-hub dynamically loads experiments from ChaosHub"
```

---

## Task 12: WT — `load/k6-run`

**Files:**
- Create: `helm/dlh-test-fw/files/workflowtemplates/load/k6-run.yaml`

Reuses the k6 TestRun CRD pattern verified in Plan 1. Wraps it in an Argo `resource` template.

- [ ] **Step 1: Write the template**

```yaml
apiVersion: argoproj.io/v1alpha1
kind: WorkflowTemplate
metadata:
  name: load-k6-run
  labels: { dlh.category: load }
spec:
  entrypoint: main
  serviceAccountName: argo-workflow
  templates:
  - name: main
    inputs:
      parameters:
      - name: script_configmap        # name of ConfigMap containing script.js
      - name: vus
      - name: duration                # e.g. "180s"
      - name: env_map                 # JSON string of extra env vars (may be "{}")
      - name: scenario_label          # used as scenario= tag on prom-rw output
    outputs:
      parameters:
      - name: metrics_namespace
        value: "{{`{{inputs.parameters.scenario_label}}`}}"
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
            configMap:
              name: {{`{{inputs.parameters.script_configmap}}`}}
              file: script.js
          arguments: >-
            --tag scenario={{`{{inputs.parameters.scenario_label}}`}}
            --out experimental-prometheus-rw
          runner:
            env:
            - name: K6_PROMETHEUS_RW_SERVER_URL
              value: {{ .Values.platform.vm.remoteWriteUrl }}
            - name: K6_PROMETHEUS_RW_PUSH_INTERVAL
              value: "5s"
            - name: K6_PROMETHEUS_RW_TREND_STATS
              value: "p(95),p(99),min,max,avg"
            - name: VUS
              value: {{`"{{inputs.parameters.vus}}"`}}
            - name: DURATION
              value: {{`"{{inputs.parameters.duration}}"`}}
```

**Note:** `env_map` is accepted as a parameter for future flexibility but not yet wired into the runner (k6-operator chart 3.x has limited support for dynamic env injection from a JSON string; punt to scenario-level extension). Document this in a comment.

- [ ] **Step 2: Upgrade + verify**

```bash
helm upgrade dlh helm/dlh-test-fw -n dlh-test-fw -f helm/dlh-test-fw/values.yaml -f helm/dlh-test-fw/values-minikube.yaml --wait
kubectl -n dlh-test-fw get workflowtemplate load-k6-run
```

- [ ] **Step 3: Commit**

```bash
git add helm/dlh-test-fw/files/workflowtemplates/load/k6-run.yaml
git commit -m "wt(load): k6-run with prom-rw output (matches spike findings)"
```

---

## Task 13: WT — `verdict/slo-eval`

**Files:**
- Create: `helm/dlh-test-fw/files/workflowtemplates/verdict/slo-eval.yaml`

Runs the verdict binary built in Plan 3. SLO YAML is passed as a literal parameter (Argo will mount it into a file) for simplicity; in production it would come from the scenario file embedded as ConfigMap.

- [ ] **Step 1: Write the template**

```yaml
apiVersion: argoproj.io/v1alpha1
kind: WorkflowTemplate
metadata:
  name: verdict-slo-eval
  labels: { dlh.category: verdict }
spec:
  entrypoint: main
  serviceAccountName: argo-workflow
  templates:
  - name: main
    inputs:
      parameters:
      - name: slo_yaml                  # full YAML string
      - name: chaos_result_name         # output from chaos step
      - name: load_start_ts             # "{{steps.load.startedAt}}" from caller
      - name: chaos_start_after         # e.g. "30s"
      - name: chaos_duration            # e.g. "60s"
      - name: load_duration             # e.g. "180s"
      - name: metrics_namespace         # scenario label
      - name: workflow_name             # "{{workflow.name}}"
    outputs:
      artifacts:
      - name: report
        path: /tmp/verdict
        archive:
          none: {}
    container:
      image: {{ .Values.platform.verdict.image }}:{{ .Values.platform.verdict.tag }}
      imagePullPolicy: Never
      args:
      - -slo-yaml=/etc/slo/slo.yaml
      - -chaos-result-name={{`{{inputs.parameters.chaos_result_name}}`}}
      - -load-start-ts={{`{{inputs.parameters.load_start_ts}}`}}
      - -chaos-start-after={{`{{inputs.parameters.chaos_start_after}}`}}
      - -chaos-duration={{`{{inputs.parameters.chaos_duration}}`}}
      - -load-duration={{`{{inputs.parameters.load_duration}}`}}
      - -prom-url={{ .Values.platform.vm.queryUrl }}
      - -workflow-name={{`{{inputs.parameters.workflow_name}}`}}
      - -artifact-dir=/tmp/verdict
      - -namespace={{`{{workflow.namespace}}`}}
      volumeMounts:
      - name: slo
        mountPath: /etc/slo
    volumes:
    - name: slo
      configMap:
        name: dlh-slo-{{`{{workflow.name}}`}}    # created by a sibling step in scenario (Plan 5)
```

**Note:** This template assumes a per-workflow ConfigMap `dlh-slo-<workflow>` containing `slo.yaml`. Plan 5's scenario template wraps `verdict-slo-eval` with a preceding "materialize SLO ConfigMap" step. Document this contract at the top of the file.

- [ ] **Step 2: Upgrade + verify**

```bash
helm upgrade dlh helm/dlh-test-fw -n dlh-test-fw -f helm/dlh-test-fw/values.yaml -f helm/dlh-test-fw/values-minikube.yaml --wait
kubectl -n dlh-test-fw get workflowtemplate verdict-slo-eval
```

- [ ] **Step 3: Commit**

```bash
git add helm/dlh-test-fw/files/workflowtemplates/verdict/slo-eval.yaml
git commit -m "wt(verdict): slo-eval runs dlh-verdict image with scenario inputs"
```

---

## Task 14: All-templates verification script

**Files:**
- Create: `scripts/verify-templates.sh`

- [ ] **Step 1: Write the script**

```bash
#!/usr/bin/env bash
set -euo pipefail
EXPECTED=(
  fixture-minio-load-mysql
  fixture-minio-load-doris
  fixture-kafka-topic-seed
  chaos-pod-delete
  chaos-network-loss
  chaos-kafka-broker-partition
  chaos-from-hub
  load-k6-run
  verdict-slo-eval
)
missing=0
for t in "${EXPECTED[@]}"; do
  if kubectl -n dlh-test-fw get workflowtemplate "$t" >/dev/null 2>&1; then
    echo "OK   $t"
  else
    echo "MISS $t" >&2
    missing=$((missing+1))
  fi
done
if (( missing > 0 )); then
  echo "FAIL: $missing WorkflowTemplates missing" >&2
  exit 1
fi
echo "PASS: all 9 WorkflowTemplates present"
```

- [ ] **Step 2: Make executable and add Makefile target**

```bash
chmod +x scripts/verify-templates.sh
```

Append to repo-root Makefile:

```makefile
.PHONY: verify-templates
verify-templates:
	./scripts/verify-templates.sh
```

- [ ] **Step 3: Run**

```bash
make verify-templates
```

Expected: `PASS: all 9 WorkflowTemplates present`.

- [ ] **Step 4: Commit**

```bash
git add scripts/verify-templates.sh Makefile
git commit -m "wt: verify-templates script asserts all 9 WTs registered"
```

---

## Definition of Done

- [ ] `make fixture-images` builds and loads all 3 fixture images into minikube.
- [ ] `helm upgrade dlh ...` installs all 9 WorkflowTemplates.
- [ ] `make verify-templates` passes.
- [ ] Each WorkflowTemplate's `inputs.parameters` matches the corresponding row of the spec's "WorkflowTemplate 函式庫" table.
- [ ] `verdict-slo-eval` references `dlh-verdict:0.1.0` (matches Plan 3's tag).
- [ ] `load-k6-run` references the exact env vars from `spikes/k6-vm-remote-write/FINDINGS.md`.

---

## Self-Review Notes

- **Spec coverage:** All four "WorkflowTemplate 函式庫" tables (Fixture, Chaos, Load, Verdict) implemented with parameter names matching the spec. The "chaos/from-hub dynamically loads from ChaosHub" requirement is implemented in Task 11 with `git clone` + `kubectl apply`. The Argo `steps.load.startedAt` integration is documented as the contract Plan 5 scenarios must satisfy when invoking `verdict-slo-eval`.
- **Placeholders:** None. Every template has full body.
- **Type consistency:**
  - `chaos_result_name` output appears identically in pod-delete (Task 8), network-loss (Task 9), kafka-broker-partition (Task 10), from-hub (Task 11), and is consumed as an input by verdict-slo-eval (Task 13). ✓
  - `scenario_label` produced by load-k6-run (Task 12) and consumed as `metrics_namespace` by verdict-slo-eval — names differ but the spec calls them differently ("metrics_namespace" in verdict, "scenario_label" in load). Plan 5 scenarios pass `load.outputs.parameters.metrics_namespace → verdict.inputs.parameters.metrics_namespace`; load's output is renamed `metrics_namespace` to match. ✓ (See Task 12 outputs.)
  - `verdict-slo-eval` requires a `dlh-slo-<workflow>` ConfigMap — Plan 5 must create it. Documented at top of Task 13's file as a "contract for caller".
  - Image tag `0.1.0` for `dlh-verdict` (Plan 3) and `dlh-fixture-*` (this plan) matches `helm/dlh-test-fw/values.yaml` (Plan 2). All three locations must move together if bumped.
