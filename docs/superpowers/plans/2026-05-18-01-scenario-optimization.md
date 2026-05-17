# Scenario Optimization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extract inline `write-slo` and `ensure-load-table` boilerplate from scenarios into reusable `util/` WorkflowTemplates backed by a chart-managed SLO template library, and add submit-time `-p` override support to `scripts/run-scenario.sh`.

**Architecture:** SLO YAML templates ship as files under `helm/dlh-test-fw/files/slos/*.yaml`, are bundled into a single ConfigMap `dlh-slos` via `Files.Glob`, and rendered per-run by the new `util-write-slo` WorkflowTemplate using a sed-based `${VAR}` substitution loop driven by a `slo_vars` multiline parameter on each scenario. A second new WorkflowTemplate `util-ensure-mysql-table` parameterises the CREATE TABLE prep step. Scenarios are rewritten so every tunable (load, chaos, workload, SLO thresholds, table schema) lives in a single top-level `arguments.parameters` block surfaceable via `argo submit -p`.

**Tech Stack:** Helm 3, Argo Workflows v3, kubectl, bash, sed, mysql client image, minikube (local dev cluster), VictoriaMetrics (read-side verification).

**Reference spec:** `docs/superpowers/specs/2026-05-18-scenario-optimization-design.md`. Re-read for the SLO template body, the exact `util-write-slo` shell script, and the risk register before starting.

**Branch & worktree:** Per `CLAUDE.md` conventions, create a feature worktree at `../dlh-test-fw-plan9` on branch `feat/plan9-scenario-optimization` before Task 1. All commits land there; merge to main with `--no-ff` after Task 8 passes.

---

## File Structure

**New files:**
- `helm/dlh-test-fw/files/slos/pod-delete.yaml` — SLO template (latency+error pair, `${VAR}` placeholders)
- `helm/dlh-test-fw/files/slos/network-loss.yaml` — same shape as pod-delete, kept separate for future divergence
- `helm/dlh-test-fw/templates/slos-configmap.yaml` — Helm wrapper turning `files/slos/*.yaml` into ConfigMap `dlh-slos`
- `helm/dlh-test-fw/files/workflowtemplates/util/write-slo.yaml` — new WorkflowTemplate `util-write-slo`
- `helm/dlh-test-fw/files/workflowtemplates/util/ensure-mysql-table.yaml` — new WorkflowTemplate `util-ensure-mysql-table`

**Modified files:**
- `scenarios/mysql-pod-delete.yaml` — full rewrite to new shape
- `scenarios/kafka-broker-partition.yaml` — full rewrite to new shape
- `scenarios/doris-be-network-loss.yaml` — full rewrite to new shape (not run live)
- `scripts/run-scenario.sh` — switch from `kubectl create | argo wait` to `argo submit --wait` and forward extra args (`-p key=value`) through
- `scripts/verify-templates.sh` — add the two new WT names to `EXPECTED`

**Unchanged:** `dlh-k6` image, runners, `verdict-job/`, dashboards, fixture-images, chaos WTs.

---

## Task 1: Prep worktree + verify baseline

**Files:**
- Create worktree at `../dlh-test-fw-plan9`

- [ ] **Step 1: Create the feature worktree**

Run from `/Users/allen/repo/dlh-test-fw`:

```bash
git worktree add ../dlh-test-fw-plan9 -b feat/plan9-scenario-optimization main
cd ../dlh-test-fw-plan9
git status
```

Expected: `On branch feat/plan9-scenario-optimization`, working tree clean.

All subsequent tasks operate from `/Users/allen/repo/dlh-test-fw-plan9`.

- [ ] **Step 2: Verify cluster baseline is healthy**

```bash
kubectl -n dlh-test-fw get workflowtemplate
./scripts/verify-templates.sh
kubectl -n dlh-test-fw get sa argo-workflow -o yaml | grep -A2 secrets || true
kubectl -n dlh-test-fw auth can-i create configmaps --as=system:serviceaccount:dlh-test-fw:argo-workflow
```

Expected:
- 9 WTs present (`fixture-*`, `chaos-*`, `load-k6-run`, `verdict-slo-eval`)
- `verify-templates.sh` reports `PASS: all 9 WorkflowTemplates present`
- `auth can-i` returns `yes` (confirms SLO CM RBAC already in place from Plan 4/5)

If any of these fail, STOP and fix the underlying issue — do not proceed.

- [ ] **Step 3: Verify `argo submit --wait` semantics on the installed CLI**

```bash
argo version --short
# Submit an existing Phase 2 scenario unchanged to confirm --wait works.
ts=$(date -u +%Y%m%d-%H%M%S)
name="baseline-mysql-${ts}"
sed "s|^\([[:space:]]*\)generateName:.*|\1name: ${name}|" scenarios/mysql-pod-delete.yaml > /tmp/wf.yaml
argo submit -n dlh-test-fw /tmp/wf.yaml --wait
echo "exit=$?"
kubectl -n dlh-test-fw get workflow "$name" -o jsonpath='{.status.phase}{"\n"}'
```

Expected: `argo submit --wait` blocks until terminal state, prints status, exits 0 on Succeeded. Phase printed by kubectl matches `Succeeded`.

If `argo submit --wait` is missing or behaves differently than `argo wait`, note it in `FINDINGS.md` and adjust Task 7 accordingly (fall back to `argo submit` + `argo wait` two-call form).

- [ ] **Step 4: Commit baseline notes (no code changes yet)**

No commit — this task is verification only. Proceed to Task 2.

---

## Task 2: SLO template library + chart-side ConfigMap

**Files:**
- Create: `helm/dlh-test-fw/files/slos/pod-delete.yaml`
- Create: `helm/dlh-test-fw/files/slos/network-loss.yaml`
- Create: `helm/dlh-test-fw/templates/slos-configmap.yaml`

- [ ] **Step 1: Write `files/slos/pod-delete.yaml`**

Create the file with this exact content:

```yaml
# Variables (all required — no defaults supported by util-write-slo):
#   ${LATENCY_METRIC}     latency Trend metric base name (no _p95 suffix), e.g. k6_dlh_mysql_query_duration_seconds
#   ${OPS_COUNTER}        denominator counter for error-rate, e.g. k6_dlh_mysql_queries_total_total
#   ${ERR_KIND_PATTERN}   regex on dlh_app_errors_total kind label, e.g. mysql.*
#   ${P95_LT}             latency budget in seconds during chaos window
#   ${ERR_LT}             max acceptable error rate during recovery window
thresholds:
- metric: p95-latency-chaos
  query: avg(${LATENCY_METRIC}_p95{dlh_workflow="{{workflow.name}}"})
  lt: ${P95_LT}
  window: chaos
- metric: error-rate-recovery
  query: sum(rate(k6_dlh_app_errors_total_total{kind=~"${ERR_KIND_PATTERN}",dlh_workflow="{{workflow.name}}"}[30s])) / clamp_min(sum(rate(${OPS_COUNTER}{dlh_workflow="{{workflow.name}}"}[30s])), 1e-9)
  lt: ${ERR_LT}
  window: recovery
```

- [ ] **Step 2: Write `files/slos/network-loss.yaml`**

Copy `pod-delete.yaml` verbatim (intentional — divergence comes later if needed). Same content, same five `${VAR}` placeholders, same `{{workflow.name}}` references.

- [ ] **Step 3: Write `templates/slos-configmap.yaml`**

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: dlh-slos
  namespace: {{ include "dlh.namespace" . }}
  labels:
    {{- include "dlh.labels" . | nindent 4 }}
data:
{{- range $path, $_ := .Files.Glob "files/slos/*.yaml" }}
  {{ base $path }}: |
{{ $.Files.Get $path | indent 4 }}
{{- end }}
```

- [ ] **Step 4: Render the chart and confirm CM contains both keys**

```bash
helm template dlh helm/dlh-test-fw | awk '/^# Source:.*slos-configmap.yaml/,/^---/' | head -80
```

Expected: ConfigMap `dlh-slos` with two `data:` keys (`pod-delete.yaml`, `network-loss.yaml`), each value containing the full SLO body including the `${VAR}` markers and `{{workflow.name}}` literals (Helm must NOT interpret the `{{...}}` because `Files.Get` returns raw bytes).

If `{{workflow.name}}` got eaten by Helm tpl, the inclusion is wrong — `Files.Get` should be raw; only `tpl` invocations process templates.

- [ ] **Step 5: Helm lint**

```bash
helm lint helm/dlh-test-fw
```

Expected: `0 chart(s) failed` (existing INFO messages are fine).

- [ ] **Step 6: Apply to the live cluster and verify the CM**

```bash
helm upgrade dlh helm/dlh-test-fw -n dlh-test-fw
kubectl -n dlh-test-fw get cm dlh-slos -o jsonpath='{.data}' | jq 'keys'
```

Expected: `["network-loss.yaml", "pod-delete.yaml"]` (alphabetical order).

```bash
kubectl -n dlh-test-fw get cm dlh-slos -o jsonpath='{.data.pod-delete\.yaml}' | head -20
```

Expected: file contents with `${LATENCY_METRIC}` and `{{workflow.name}}` literals intact.

- [ ] **Step 7: Commit**

```bash
git add helm/dlh-test-fw/files/slos/ helm/dlh-test-fw/templates/slos-configmap.yaml
git commit -m "feat(slos): ship pod-delete + network-loss SLO templates via dlh-slos ConfigMap"
```

---

## Task 3: util-write-slo WorkflowTemplate

**Files:**
- Create: `helm/dlh-test-fw/files/workflowtemplates/util/write-slo.yaml`

Refer to spec section "util-write-slo" for the canonical script body — copy it verbatim. The escaping pattern is `{{`{{argo-template}}`}}` because `dlh-workflowtemplates.yaml` runs `tpl ($.Files.Get $path) $`.

- [ ] **Step 1: Write the WT file**

Create `helm/dlh-test-fw/files/workflowtemplates/util/write-slo.yaml`:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: WorkflowTemplate
metadata:
  name: util-write-slo
  labels:
    dlh.category: util
spec:
  entrypoint: main
  serviceAccountName: argo-workflow
  templates:
  - name: main
    inputs:
      parameters:
      - name: slo_name        # SLO library entry, e.g. "pod-delete" — resolves to dlh-slos[slo_name + ".yaml"]
      - name: slo_vars        # multi-line KEY=VAL block, one per ${VAR} the template references
    script:
      image: alpine/k8s:1.30.0
      command: [bash]
      source: |
        set -euo pipefail

        # 1. Read SLO template from chart-provisioned dlh-slos CM.
        TPL=$(kubectl -n {{`{{workflow.namespace}}`}} get cm dlh-slos \
                -o jsonpath="{.data.{{`{{inputs.parameters.slo_name}}`}}\.yaml}")
        if [[ -z "$TPL" ]]; then
          echo "ERROR: SLO library entry '{{`{{inputs.parameters.slo_name}}`}}.yaml' not in dlh-slos CM" >&2
          kubectl -n {{`{{workflow.namespace}}`}} get cm dlh-slos -o jsonpath='{.data}' | jq 'keys' >&2 || true
          exit 1
        fi

        # 2. Apply scenario-supplied ${VAR} → VAL substitutions.
        VARS=$(cat <<'EOF'
        {{`{{inputs.parameters.slo_vars}}`}}
        EOF
        )
        RENDERED="$TPL"
        while IFS='=' read -r K V; do
          [[ -z "$K" || "$K" =~ ^# ]] && continue
          K=$(printf '%s' "$K" | tr -d '[:space:]')
          [[ -z "$K" ]] && continue
          if [[ "$V" == *'|'* ]]; then
            echo "ERROR: slo_vars value for $K contains '|' which conflicts with sed separator" >&2
            exit 1
          fi
          RENDERED=$(printf '%s' "$RENDERED" | sed "s|\${$K}|$V|g")
        done <<< "$VARS"

        # 3. Defensive sed on {{workflow.name}} (Argo already rendered it in source:, but the SLO
        # template body was read at runtime so its literal placeholder still needs replacing).
        RENDERED=$(printf '%s' "$RENDERED" | sed "s|{{`{{workflow.name}}`}}|{{`{{workflow.name}}`}}|g")

        # 4. Fail-fast on any unresolved ${VAR} markers.
        if printf '%s' "$RENDERED" | grep -qE '\$\{[A-Z_]+\}'; then
          echo "ERROR: unresolved variables in rendered SLO:" >&2
          printf '%s' "$RENDERED" | grep -E '\$\{[A-Z_]+\}' >&2
          exit 1
        fi

        # 5. Write the per-workflow CM (verdict-slo-eval mounts this).
        kubectl -n {{`{{workflow.namespace}}`}} create configmap dlh-slo-{{`{{workflow.name}}`}} \
          --from-literal=slo.yaml="$RENDERED" \
          --dry-run=client -o yaml | kubectl apply -f -
```

- [ ] **Step 2: Render + lint**

```bash
helm template dlh helm/dlh-test-fw | grep -A2 "name: util-write-slo"
helm lint helm/dlh-test-fw
```

Expected: `util-write-slo` appears in rendered output; lint passes.

- [ ] **Step 3: Deploy and verify the WT registers**

```bash
helm upgrade dlh helm/dlh-test-fw -n dlh-test-fw
kubectl -n dlh-test-fw get workflowtemplate util-write-slo
```

Expected: WT shown, AGE less than a minute.

- [ ] **Step 4: Direct unit test — happy path**

Submit a one-shot test workflow that invokes `util-write-slo` directly with a sample slo_vars block:

```bash
cat > /tmp/test-write-slo.yaml <<'EOF'
apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
  generateName: test-write-slo-
  namespace: dlh-test-fw
spec:
  serviceAccountName: argo-workflow
  entrypoint: main
  templates:
  - name: main
    steps:
    - - name: w
        templateRef: { name: util-write-slo, template: main }
        arguments:
          parameters:
          - { name: slo_name, value: pod-delete }
          - name: slo_vars
            value: |
              LATENCY_METRIC=k6_dlh_mysql_query_duration_seconds
              OPS_COUNTER=k6_dlh_mysql_queries_total_total
              ERR_KIND_PATTERN=mysql.*
              P95_LT=1.0
              ERR_LT=0.05
EOF
ts=$(date -u +%Y%m%d-%H%M%S)
sed "s|generateName: test-write-slo-|name: test-write-slo-${ts}|" /tmp/test-write-slo.yaml \
  | kubectl create -f -
argo wait -n dlh-test-fw "test-write-slo-${ts}"
kubectl -n dlh-test-fw get workflow "test-write-slo-${ts}" -o jsonpath='{.status.phase}{"\n"}'
kubectl -n dlh-test-fw get cm "dlh-slo-test-write-slo-${ts}" -o jsonpath='{.data.slo\.yaml}'
```

Expected:
- Workflow phase: `Succeeded`
- Rendered slo.yaml contains `avg(k6_dlh_mysql_query_duration_seconds_p95{dlh_workflow="test-write-slo-<ts>"})`
- No `${...}` or `{{workflow.name}}` literals remain

- [ ] **Step 5: Direct unit test — fail-fast on missing variable**

Submit the same workflow with `P95_LT` deliberately omitted:

```bash
ts=$(date -u +%Y%m%d-%H%M%S)
cat > /tmp/test-write-slo-bad.yaml <<EOF
apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
  name: test-write-slo-bad-${ts}
  namespace: dlh-test-fw
spec:
  serviceAccountName: argo-workflow
  entrypoint: main
  templates:
  - name: main
    steps:
    - - name: w
        templateRef: { name: util-write-slo, template: main }
        arguments:
          parameters:
          - { name: slo_name, value: pod-delete }
          - name: slo_vars
            value: |
              LATENCY_METRIC=k6_dlh_mysql_query_duration_seconds
              OPS_COUNTER=k6_dlh_mysql_queries_total_total
              ERR_KIND_PATTERN=mysql.*
              ERR_LT=0.05
EOF
kubectl create -f /tmp/test-write-slo-bad.yaml
argo wait -n dlh-test-fw "test-write-slo-bad-${ts}" || true
kubectl -n dlh-test-fw get workflow "test-write-slo-bad-${ts}" -o jsonpath='{.status.phase}{"\n"}'
argo logs -n dlh-test-fw "test-write-slo-bad-${ts}" 2>&1 | tail -20
```

Expected: phase `Failed`; logs contain `unresolved variables in rendered SLO:` and a line with `${P95_LT}`.

- [ ] **Step 6: Cleanup test artifacts**

```bash
kubectl -n dlh-test-fw delete workflow -l 'workflows.argoproj.io/workflow' --field-selector "metadata.name=test-write-slo-${ts}" 2>/dev/null || true
kubectl -n dlh-test-fw delete workflow "test-write-slo-${ts}" "test-write-slo-bad-${ts}" 2>/dev/null || true
kubectl -n dlh-test-fw delete cm "dlh-slo-test-write-slo-${ts}" 2>/dev/null || true
```

(Don't fail the task if these are already gone.)

- [ ] **Step 7: Commit**

```bash
git add helm/dlh-test-fw/files/workflowtemplates/util/write-slo.yaml
git commit -m "feat(workflowtemplates): add util-write-slo with sed-based \${VAR} substitution and fail-fast on unresolved markers"
```

---

## Task 4: util-ensure-mysql-table WorkflowTemplate

**Files:**
- Create: `helm/dlh-test-fw/files/workflowtemplates/util/ensure-mysql-table.yaml`

- [ ] **Step 1: Write the WT file**

```yaml
apiVersion: argoproj.io/v1alpha1
kind: WorkflowTemplate
metadata:
  name: util-ensure-mysql-table
  labels:
    dlh.category: util
spec:
  entrypoint: main
  serviceAccountName: argo-workflow
  templates:
  - name: main
    inputs:
      parameters:
      - name: db_host
      - name: db
      - name: user
      - name: password
      - name: table
      - name: schema_sql       # full column definition, e.g. "id BIGINT AUTO_INCREMENT PRIMARY KEY, ts DATETIME NOT NULL"
    script:
      image: mysql:8.0
      command: [bash]
      source: |
        set -euo pipefail
        mysql -h {{`{{inputs.parameters.db_host}}`}} -P 3306 \
          -u {{`{{inputs.parameters.user}}`}} -p{{`{{inputs.parameters.password}}`}} \
          {{`{{inputs.parameters.db}}`}} -e "
            CREATE TABLE IF NOT EXISTS {{`{{inputs.parameters.table}}`}} (
              {{`{{inputs.parameters.schema_sql}}`}}
            ) ENGINE=InnoDB;"
```

- [ ] **Step 2: Render + lint + deploy**

```bash
helm lint helm/dlh-test-fw
helm upgrade dlh helm/dlh-test-fw -n dlh-test-fw
kubectl -n dlh-test-fw get workflowtemplate util-ensure-mysql-table
```

Expected: WT registered.

- [ ] **Step 3: Direct unit test**

```bash
ts=$(date -u +%Y%m%d-%H%M%S)
cat > /tmp/test-ensure-table.yaml <<EOF
apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
  name: test-ensure-table-${ts}
  namespace: dlh-test-fw
spec:
  serviceAccountName: argo-workflow
  entrypoint: main
  templates:
  - name: main
    steps:
    - - name: w
        templateRef: { name: util-ensure-mysql-table, template: main }
        arguments:
          parameters:
          - { name: db_host,    value: "mysql.mysql-sys.svc.cluster.local" }
          - { name: db,         value: "dlh" }
          - { name: user,       value: "root" }
          - { name: password,   value: "dlh-mysql-dev" }
          - { name: table,      value: "dlh_load_test" }
          - { name: schema_sql, value: "id BIGINT AUTO_INCREMENT PRIMARY KEY, ts DATETIME NOT NULL" }
EOF
kubectl create -f /tmp/test-ensure-table.yaml
argo wait -n dlh-test-fw "test-ensure-table-${ts}"
kubectl -n dlh-test-fw get workflow "test-ensure-table-${ts}" -o jsonpath='{.status.phase}{"\n"}'
kubectl -n mysql-sys exec deploy/mysql -- mysql -uroot -pdlh-mysql-dev dlh -e "DESCRIBE dlh_load_test"
kubectl -n mysql-sys exec deploy/mysql -- mysql -uroot -pdlh-mysql-dev dlh -e "DROP TABLE dlh_load_test"
kubectl -n dlh-test-fw delete workflow "test-ensure-table-${ts}"
```

Expected: phase `Succeeded`; `DESCRIBE` shows two columns (`id`, `ts`).

(If the mysql Deployment name differs, substitute the actual one — check with `kubectl -n mysql-sys get deploy`.)

- [ ] **Step 4: Commit**

```bash
git add helm/dlh-test-fw/files/workflowtemplates/util/ensure-mysql-table.yaml
git commit -m "feat(workflowtemplates): add util-ensure-mysql-table for parameterised CREATE TABLE prep"
```

---

## Task 5: Update verify-templates.sh

**Files:**
- Modify: `scripts/verify-templates.sh`

- [ ] **Step 1: Add the two new WT names to `EXPECTED`**

Edit `scripts/verify-templates.sh`. The new `EXPECTED` array must include both new names. Final form:

```bash
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
  util-write-slo
  util-ensure-mysql-table
)
```

Also update the final PASS message to count 11:

```bash
echo "PASS: all 11 WorkflowTemplates present"
```

- [ ] **Step 2: Run the verifier**

```bash
./scripts/verify-templates.sh
```

Expected: 11 `OK` lines, final `PASS: all 11 WorkflowTemplates present`.

- [ ] **Step 3: Commit**

```bash
git add scripts/verify-templates.sh
git commit -m "chore(scripts): include util-write-slo and util-ensure-mysql-table in verify-templates"
```

---

## Task 6: Rewrite mysql-pod-delete scenario

**Files:**
- Modify (full rewrite): `scenarios/mysql-pod-delete.yaml`

- [ ] **Step 1: Replace the scenario with the new shape**

Overwrite `scenarios/mysql-pod-delete.yaml` with this exact body (copied from spec, with the inline `write-slo` / `ensure-load-table` template blocks deleted entirely):

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
    # ===== scenario identity =====
    - { name: scenario_label,    value: mysql-pod-delete }
    # ===== SLO =====
    - { name: slo_name,          value: pod-delete }
    - name: slo_vars
      value: |
        LATENCY_METRIC=k6_dlh_mysql_query_duration_seconds
        OPS_COUNTER=k6_dlh_mysql_queries_total_total
        ERR_KIND_PATTERN=mysql.*
        P95_LT=1.0
        ERR_LT=0.05
    # ===== load shape =====
    - { name: vus,               value: "10" }
    - { name: load_duration,     value: 180s }
    # ===== chaos shape =====
    - { name: chaos_start_after, value: 30s }
    - { name: chaos_duration,    value: 60s }
    - { name: chaos_interval,    value: "10" }
    - { name: chaos_force,       value: "true" }
    # ===== workload (mysql-specific) =====
    - { name: mysql_dsn,         value: "root:dlh-mysql-dev@tcp(mysql.mysql-sys.svc.cluster.local:3306)/dlh" }
    - { name: mysql_op_mix,      value: "read:70,write:30" }
    - { name: mysql_read_sql,    value: "SELECT NOW()" }
    - { name: mysql_write_sql,   value: "INSERT INTO dlh_load(ts) VALUES(NOW())" }
    - { name: mysql_sleep_ms,    value: "50" }
    # ===== table prep =====
    - { name: load_table_name,   value: "dlh_load" }
    - { name: load_table_schema, value: "id BIGINT AUTO_INCREMENT PRIMARY KEY, ts DATETIME NOT NULL" }

  templates:
  - name: main
    steps:
    - - name: prep-slo
        templateRef: { name: util-write-slo, template: main }
        arguments:
          parameters:
          - { name: slo_name, value: "{{workflow.parameters.slo_name}}" }
          - { name: slo_vars, value: "{{workflow.parameters.slo_vars}}" }
    - - name: load-fixture
        templateRef: { name: fixture-minio-load-mysql, template: main }
        arguments:
          parameters:
          - { name: uri,                value: "s3://fixtures/mysql-users.sql" }
          - { name: db_host,            value: "mysql.mysql-sys.svc.cluster.local" }
          - { name: credentials_secret, value: "mysql-creds" }
    - - name: prep-table
        templateRef: { name: util-ensure-mysql-table, template: main }
        arguments:
          parameters:
          - { name: db_host,    value: "mysql.mysql-sys.svc.cluster.local" }
          - { name: db,         value: "dlh" }
          - { name: user,       value: "root" }
          - { name: password,   value: "dlh-mysql-dev" }
          - { name: table,      value: "{{workflow.parameters.load_table_name}}" }
          - { name: schema_sql, value: "{{workflow.parameters.load_table_schema}}" }
    - - name: chaos
        templateRef: { name: chaos-pod-delete, template: main }
        continueOn: { failed: true }
        arguments:
          parameters:
          - { name: target_namespace,    value: "mysql-sys" }
          - { name: target_pod_selector, value: "app=mysql" }
          - { name: duration,            value: "{{workflow.parameters.chaos_duration}}" }
          - { name: interval,            value: "{{workflow.parameters.chaos_interval}}" }
          - { name: force,               value: "{{workflow.parameters.chaos_force}}" }
      - name: load
        templateRef: { name: load-k6-run, template: main }
        arguments:
          parameters:
          - { name: script_path,    value: "/scripts/runners/mysql.js" }
          - { name: vus,            value: "{{workflow.parameters.vus}}" }
          - { name: duration,       value: "{{workflow.parameters.load_duration}}" }
          - { name: scenario_label, value: "{{workflow.parameters.scenario_label}}" }
          - name: env_map
            value: |
              MYSQL_DSN={{workflow.parameters.mysql_dsn}}
              MYSQL_OP_MIX={{workflow.parameters.mysql_op_mix}}
              MYSQL_READ_SQL={{workflow.parameters.mysql_read_sql}}
              MYSQL_WRITE_SQL={{workflow.parameters.mysql_write_sql}}
              MYSQL_SLEEP_MS={{workflow.parameters.mysql_sleep_ms}}
    - - name: verdict
        templateRef: { name: verdict-slo-eval, template: main }
        arguments:
          parameters:
          - { name: slo_yaml,           value: "(unused — read from CM)" }
          - { name: chaos_result_name,  value: "{{steps.chaos.outputs.parameters.chaos_result_name}}-pod-delete" }
          - { name: load_start_ts,      value: "{{steps.load.startedAt}}" }
          - { name: chaos_start_after,  value: "{{workflow.parameters.chaos_start_after}}" }
          - { name: chaos_duration,     value: "{{workflow.parameters.chaos_duration}}" }
          - { name: load_duration,      value: "{{workflow.parameters.load_duration}}" }
          - { name: metrics_namespace,  value: "{{workflow.parameters.scenario_label}}" }
          - { name: workflow_name,      value: "{{workflow.name}}" }
```

- [ ] **Step 2: Submit and wait**

```bash
make run-mysql
```

(That target invokes `./scripts/run-scenario.sh scenarios/mysql-pod-delete.yaml` — the script still works in its pre-Task-7 form for the no-override path.)

Expected: workflow reaches `Succeeded`. `run-scenario.sh` final line prints `Final phase: Succeeded` and exits 0.

- [ ] **Step 3: Verify rendered SLO CM**

```bash
wf=$(kubectl -n dlh-test-fw get workflow -l workflows.argoproj.io/workflow --sort-by=.metadata.creationTimestamp -o name | tail -1 | sed 's|.*/||')
# fallback: pick the latest mysql-pod-delete-* workflow
wf=$(kubectl -n dlh-test-fw get workflow -o name | grep mysql-pod-delete | sort | tail -1 | sed 's|.*/||')
kubectl -n dlh-test-fw get cm "dlh-slo-${wf}" -o jsonpath='{.data.slo\.yaml}'
```

Expected: rendered SLO contains
- `avg(k6_dlh_mysql_query_duration_seconds_p95{dlh_workflow="<wf>"})`
- `lt: 1.0`
- `lt: 0.05`
- `kind=~"mysql.*"`
- NO `${...}` or `{{workflow.name}}` literals remain.

- [ ] **Step 4: Verify verdict succeeded and dashboards still populate**

```bash
kubectl -n dlh-test-fw exec deploy/dlh-minio -- mc cat "local/artifacts/${wf}/${wf}-main-*/verdict/report.json" | jq .
```

Expected: `verdict: PASS` (or at minimum, valid JSON with both thresholds evaluated — chaos can naturally produce FAIL but the *shape* must match Phase 2).

Open Grafana (`make platform-verify` shows the URL or use `kubectl port-forward svc/dlh-grafana 3001:80`), pick the `dlh-mysql` dashboard with workflow `<wf>`, and confirm panels populate. Note: this is a visual check — the task can complete as long as PromQL series exist (next sub-step verifies):

```bash
curl -s "http://localhost:8428/api/v1/query?query=k6_dlh_mysql_query_duration_seconds_p95{dlh_workflow=\"${wf}\"}" | jq '.data.result | length'
```

(Use whatever VM port-forward the local env exposes — typical `kubectl -n dlh-test-fw port-forward svc/dlh-victoriametrics 8428:8428`.)

Expected: length > 0.

- [ ] **Step 5: Commit**

```bash
git add scenarios/mysql-pod-delete.yaml
git commit -m "feat(scenarios): rewrite mysql-pod-delete to use util-write-slo + util-ensure-mysql-table"
```

---

## Task 7: Rewrite kafka-broker-partition + doris-be-network-loss scenarios

**Files:**
- Modify (full rewrite): `scenarios/kafka-broker-partition.yaml`
- Modify (full rewrite): `scenarios/doris-be-network-loss.yaml`

- [ ] **Step 1: Replace kafka-broker-partition.yaml**

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
    # ===== scenario identity =====
    - { name: scenario_label,    value: kafka-broker-partition }
    # ===== SLO =====
    - { name: slo_name,          value: pod-delete }
    - name: slo_vars
      value: |
        LATENCY_METRIC=k6_dlh_kafka_produce_duration_seconds
        OPS_COUNTER=k6_dlh_kafka_messages_produced_total_total
        ERR_KIND_PATTERN=kafka-.*
        P95_LT=2.0
        ERR_LT=0.10
    # ===== load shape =====
    - { name: vus,               value: "5" }
    - { name: load_duration,     value: 180s }
    # ===== chaos shape =====
    - { name: chaos_start_after, value: 30s }
    - { name: chaos_duration,    value: 60s }
    - { name: broker_id,         value: "0" }
    # ===== workload (kafka-specific) =====
    - { name: kafka_bootstrap,   value: "kafka.kafka-sys.svc.cluster.local:9092" }
    - { name: kafka_topic,       value: "dlh-load" }
    - { name: kafka_op,          value: "produce" }
    - { name: kafka_message_size, value: "256" }

  templates:
  - name: main
    steps:
    - - name: prep-slo
        templateRef: { name: util-write-slo, template: main }
        arguments:
          parameters:
          - { name: slo_name, value: "{{workflow.parameters.slo_name}}" }
          - { name: slo_vars, value: "{{workflow.parameters.slo_vars}}" }
    # Topic creation is implicit: Writer has autoCreateTopic=true; no prep-table step.
    - - name: chaos
        templateRef: { name: chaos-kafka-broker-partition, template: main }
        continueOn: { failed: true }
        arguments:
          parameters:
          - { name: kafka_namespace, value: "kafka-sys" }
          - { name: broker_id,       value: "{{workflow.parameters.broker_id}}" }
          - { name: duration,        value: "{{workflow.parameters.chaos_duration}}" }
      - name: load
        templateRef: { name: load-k6-run, template: main }
        arguments:
          parameters:
          - { name: script_path,    value: "/scripts/runners/kafka.js" }
          - { name: vus,            value: "{{workflow.parameters.vus}}" }
          - { name: duration,       value: "{{workflow.parameters.load_duration}}" }
          - { name: scenario_label, value: "{{workflow.parameters.scenario_label}}" }
          - name: env_map
            value: |
              KAFKA_BOOTSTRAP={{workflow.parameters.kafka_bootstrap}}
              KAFKA_TOPIC={{workflow.parameters.kafka_topic}}
              KAFKA_OP={{workflow.parameters.kafka_op}}
              KAFKA_MESSAGE_SIZE={{workflow.parameters.kafka_message_size}}
    - - name: verdict
        templateRef: { name: verdict-slo-eval, template: main }
        arguments:
          parameters:
          - { name: slo_yaml,           value: "(unused — read from CM)" }
          - { name: chaos_result_name,  value: "{{steps.chaos.outputs.parameters.chaos_result_name}}-pod-network-partition" }
          - { name: load_start_ts,      value: "{{steps.load.startedAt}}" }
          - { name: chaos_start_after,  value: "{{workflow.parameters.chaos_start_after}}" }
          - { name: chaos_duration,     value: "{{workflow.parameters.chaos_duration}}" }
          - { name: load_duration,      value: "{{workflow.parameters.load_duration}}" }
          - { name: metrics_namespace,  value: "{{workflow.parameters.scenario_label}}" }
          - { name: workflow_name,      value: "{{workflow.name}}" }
```

Note: the kafka scenario also uses the `pod-delete` SLO library entry — the template body is identical to `network-loss` so reusing avoids duplication. (Spec leaves both library files in place for future divergence, but scenarios pick whichever SLO matches their failure mode.) The kafka chaos is a network partition, so `network-loss` is also valid; sticking with `pod-delete` because both entries are byte-identical right now and `pod-delete` is the more "default" name. Document this choice in the commit body.

- [ ] **Step 2: Run kafka scenario end-to-end**

```bash
make run-kafka
```

Expected: `Final phase: Succeeded`.

- [ ] **Step 3: Verify kafka SLO CM rendered**

```bash
wf=$(kubectl -n dlh-test-fw get workflow -o name | grep kafka-broker-partition | sort | tail -1 | sed 's|.*/||')
kubectl -n dlh-test-fw get cm "dlh-slo-${wf}" -o jsonpath='{.data.slo\.yaml}'
```

Expected: rendered SLO uses `k6_dlh_kafka_produce_duration_seconds_p95`, `lt: 2.0`, `lt: 0.10`, `kind=~"kafka-.*"`, no unresolved `${...}` or `{{workflow.name}}`.

- [ ] **Step 4: Replace doris-be-network-loss.yaml**

```yaml
# DEFERRED: requires Doris target (see targets/doris/README.md).
# Committed to document the chaos+load+verdict pattern for a Doris BE.
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
    # ===== scenario identity =====
    - { name: scenario_label,    value: doris-be-network-loss }
    # ===== SLO =====
    - { name: slo_name,          value: network-loss }
    - name: slo_vars
      value: |
        LATENCY_METRIC=k6_http_req_duration
        OPS_COUNTER=k6_http_reqs_total
        ERR_KIND_PATTERN=doris.*
        P95_LT=3.0
        ERR_LT=0.10
    # ===== load shape =====
    - { name: vus,               value: "5" }
    - { name: load_duration,     value: 180s }
    # ===== chaos shape =====
    - { name: chaos_start_after, value: 30s }
    - { name: chaos_duration,    value: 60s }
    - { name: loss_percent,      value: "50" }

  templates:
  - name: main
    steps:
    - - name: prep-slo
        templateRef: { name: util-write-slo, template: main }
        arguments:
          parameters:
          - { name: slo_name, value: "{{workflow.parameters.slo_name}}" }
          - { name: slo_vars, value: "{{workflow.parameters.slo_vars}}" }
    - - name: load-fixture
        templateRef: { name: fixture-minio-load-doris, template: main }
        arguments:
          parameters:
          - { name: uri,                            value: "s3://fixtures/doris-rows.csv" }
          - { name: fe_host,                        value: "doris-fe-0.doris-fe.doris-sys.svc.cluster.local:8030" }
          - { name: stream_load_credentials_secret, value: "doris-creds" }
    - - name: chaos
        templateRef: { name: chaos-network-loss, template: main }
        continueOn: { failed: true }
        arguments:
          parameters:
          - { name: target_namespace,    value: "doris-sys" }
          - { name: target_pod_selector, value: "app=doris-be" }
          - { name: loss_percent,        value: "{{workflow.parameters.loss_percent}}" }
          - { name: duration,            value: "{{workflow.parameters.chaos_duration}}" }
      - name: load
        templateRef: { name: load-k6-run, template: main }
        arguments:
          parameters:
          - { name: script_path,    value: "/scripts/runners/doris.js" }
          - { name: vus,            value: "{{workflow.parameters.vus}}" }
          - { name: duration,       value: "{{workflow.parameters.load_duration}}" }
          - { name: scenario_label, value: "{{workflow.parameters.scenario_label}}" }
          - { name: env_map,        value: "" }
    - - name: verdict
        templateRef: { name: verdict-slo-eval, template: main }
        arguments:
          parameters:
          - { name: slo_yaml,          value: "(unused — read from CM)" }
          - { name: chaos_result_name, value: "{{steps.chaos.outputs.parameters.chaos_result_name}}-pod-network-loss" }
          - { name: load_start_ts,     value: "{{steps.load.startedAt}}" }
          - { name: chaos_start_after, value: "{{workflow.parameters.chaos_start_after}}" }
          - { name: chaos_duration,    value: "{{workflow.parameters.chaos_duration}}" }
          - { name: load_duration,     value: "{{workflow.parameters.load_duration}}" }
          - { name: metrics_namespace, value: "{{workflow.parameters.scenario_label}}" }
          - { name: workflow_name,     value: "{{workflow.name}}" }
```

- [ ] **Step 5: Lint doris scenario (not run live — Doris is NO-GO)**

```bash
kubectl create --dry-run=client -f scenarios/doris-be-network-loss.yaml -o name
```

Expected: server-side dry-run prints `workflow.argoproj.io/doris-be-network-loss-...` (or client-side accepts the YAML). No schema errors.

- [ ] **Step 6: Commit**

```bash
git add scenarios/kafka-broker-partition.yaml scenarios/doris-be-network-loss.yaml
git commit -m "feat(scenarios): rewrite kafka-broker-partition + doris-be-network-loss to new shape

Kafka and Doris both reference util-write-slo with scenario-local slo_vars; doris
remains deferred (NO-GO target) but YAML shape matches the new contract."
```

---

## Task 8: run-scenario.sh `-p` forwarding + override end-to-end test

**Files:**
- Modify: `scripts/run-scenario.sh`

- [ ] **Step 1: Rewrite the script to forward extra args to `argo submit`**

Overwrite `scripts/run-scenario.sh`:

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
#   scripts/run-scenario.sh scenarios/mysql-pod-delete.yaml -p chaos_duration=120s
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
argo submit -n dlh-test-fw "$rendered" --wait "$@" || true
status=$(kubectl -n dlh-test-fw get workflow "$name" -o jsonpath='{.status.phase}')
echo "Final phase: $status"
echo "Report artifact: argo get -n dlh-test-fw $name  # see artifact section, or:"
echo "                 kubectl -n dlh-test-fw exec deploy/dlh-minio -- mc cat \"local/artifacts/${name}/${name}-main-*/verdict/report.json\" | jq ."
[[ "$status" == "Succeeded" ]]
```

Key differences from the old script:
- Switched from `kubectl create -f | argo wait` to `argo submit --wait` (Task 1 verified `--wait` works).
- All trailing args (`"$@"`) are forwarded to `argo submit`, enabling `-p key=value`, `--parameter-file`, etc.
- Removed the optional positional `override-name` second arg (unused in tree; conflicts with passing flags). If we need it back, prefer `-l name=...` via argo CLI.

- [ ] **Step 2: Sanity check — no-override run still works**

```bash
make run-mysql
```

Expected: `Final phase: Succeeded`.

- [ ] **Step 3: Override run — change vus and op mix**

```bash
./scripts/run-scenario.sh scenarios/mysql-pod-delete.yaml -p vus=5 -p mysql_op_mix=read:100
```

Expected: `Final phase: Succeeded`.

- [ ] **Step 4: Verify overrides reached the runner**

```bash
wf=$(kubectl -n dlh-test-fw get workflow -o name | grep mysql-pod-delete | sort | tail -1 | sed 's|.*/||')
# k6 env CM is created by load-k6-run as dlh-k6-env-<wf>
kubectl -n dlh-test-fw get cm "dlh-k6-env-${wf}" -o jsonpath='{.data}' | tr ',' '\n'
```

Expected: `MYSQL_OP_MIX=read:100`.

```bash
# Verify k6 saw the new VU count by querying VM (k6_vus is emitted as a gauge).
curl -s "http://localhost:8428/api/v1/query?query=max_over_time(k6_vus{dlh_workflow=\"${wf}\"}[10m])" | jq '.data.result[0].value[1]'
```

(Use the local VM port-forward; spin one up if not running: `kubectl -n dlh-test-fw port-forward svc/dlh-victoriametrics 8428:8428 &`.)

Expected: `"5"`.

- [ ] **Step 5: Negative test — fail-fast on missing slo_vars entry**

Submit a scenario with a deliberately-broken slo_vars (omit `P95_LT`):

```bash
./scripts/run-scenario.sh scenarios/mysql-pod-delete.yaml -p slo_vars="LATENCY_METRIC=k6_dlh_mysql_query_duration_seconds
OPS_COUNTER=k6_dlh_mysql_queries_total_total
ERR_KIND_PATTERN=mysql.*
ERR_LT=0.05"
```

Expected: `Final phase: Failed`.

```bash
wf=$(kubectl -n dlh-test-fw get workflow -o name | grep mysql-pod-delete | sort | tail -1 | sed 's|.*/||')
argo logs -n dlh-test-fw "$wf" 2>&1 | grep -A2 'unresolved variables'
```

Expected: log contains `unresolved variables in rendered SLO:` followed by `${P95_LT}`.

- [ ] **Step 6: Commit**

```bash
git add scripts/run-scenario.sh
git commit -m "feat(scripts): forward extra args from run-scenario.sh to argo submit (enables -p overrides)"
```

---

## Task 9: Merge to main + FINDINGS update

**Files:**
- Modify: `spikes/k6-vm-remote-write/FINDINGS.md` (append a section)

- [ ] **Step 1: Append a Plan 9 section to FINDINGS.md**

Add at the end of the file:

```markdown
## Plan 9 — Scenario optimization (2026-05-18)

- SLO templates live in a single ConfigMap `dlh-slos` keyed by filename
  (`pod-delete.yaml`, `network-loss.yaml`). util-write-slo reads the entry by
  `slo_name` parameter, applies `${VAR}` substitutions from a `slo_vars`
  multi-line KEY=VAL block, and writes `dlh-slo-<workflow.name>`.
- Two-layer substitution: `${VAR}` is filled by a bash sed loop; `{{workflow.name}}`
  is rendered by Argo into the script source AND defensively re-substituted at
  runtime so it works whether the literal appears in the script body or the CM
  body.
- Sed separator is `|`; util-write-slo fail-fasts if any slo_vars value contains
  `|`, or if any `${VAR}` markers remain after substitution.
- `scripts/run-scenario.sh` now forwards extra args to `argo submit`, so any
  scenario parameter is overridable at submit time: `-p vus=50`, `-p chaos_duration=120s`.
- The optional second-positional `override-name` arg of the old run-scenario.sh
  is removed; nothing in-tree used it.
- Doris scenario YAML is rewritten to the new shape but still deferred (NO-GO
  target — no live run).
```

- [ ] **Step 2: Run the full scenario suite once more for the merge log**

```bash
make run-mysql
make run-kafka
./scripts/verify-templates.sh
```

Expected: both runs `Succeeded`; verify-templates `PASS: all 11 WorkflowTemplates present`.

- [ ] **Step 3: Commit FINDINGS update**

```bash
git add spikes/k6-vm-remote-write/FINDINGS.md
git commit -m "docs(findings): record Plan 9 scenario optimization (util WTs + slo_vars + run-scenario -p)"
```

- [ ] **Step 4: Merge to main with --no-ff**

From `/Users/allen/repo/dlh-test-fw` (main worktree):

```bash
cd /Users/allen/repo/dlh-test-fw
git fetch  # noop locally but harmless
git checkout main
git merge --no-ff feat/plan9-scenario-optimization -m "Merge feat/plan9-scenario-optimization: extract util-write-slo + util-ensure-mysql-table, surface scenario params via argo submit -p

Plan 9 lifts inline write-slo and ensure-load-table heredocs out of scenarios
into two reusable WorkflowTemplates backed by a chart-managed SLO template
library (\`files/slos/*.yaml\` → ConfigMap \`dlh-slos\`). Every tunable in
scenarios (load shape, chaos shape, workload params, SLO thresholds) now lives
in a single top-level arguments.parameters block, surfaceable at submit time
via \`scripts/run-scenario.sh ... -p key=value\`.

- 2 new WTs: util-write-slo, util-ensure-mysql-table (11 WTs total)
- 2 new SLO library entries: pod-delete.yaml, network-loss.yaml
- mysql-pod-delete + kafka-broker-partition: rewritten and verified end-to-end
- doris-be-network-loss: rewritten to new shape (NO-GO target, not run live)
- Fail-fast: util-write-slo errors on unresolved \${VAR} markers or sed-unsafe
  values containing '|'."
git log --first-parent --oneline -5
```

Expected: merge commit visible at top; `--first-parent` log shows the plan as one commit.

- [ ] **Step 5: Clean up the worktree**

```bash
git worktree remove ../dlh-test-fw-plan9
git branch -d feat/plan9-scenario-optimization
git worktree list
```

Expected: only the main worktree remains; branch deleted (refuses with `-d` if anything's unmerged — should not happen).

- [ ] **Step 6: Tag the milestone**

```bash
git tag -f plan9-scenario-optimization
git log --first-parent --oneline -8
```

Expected: tag at the merge commit.

---

## Self-Review notes (author check, fresh-eyes pass)

- Spec section "Goals (in scope)" items 1–4: covered by Tasks 2, 3, 4, 6, 7, 8 respectively.
- Spec section "Testing" matrix: every row mapped to a verification step (Task 2 step 6, Task 3 step 4-5, Task 4 step 3, Task 6 step 3-4, Task 8 step 3-5).
- Spec section "Success criteria" 1–7: all verified inside Tasks 2–8.
- Spec section "Risks":
  - argo submit --wait semantics — verified in Task 1 step 3 with explicit fallback note.
  - sed separator clash with `|` — covered by util-write-slo source itself.
  - `{{workflow.name}}` inside SLO library — defensive sed in util-write-slo step 3.
  - Per-workflow CM proliferation — NOT explicitly added as ownerReferences in this plan; the spec mentions it as a verification item but `kubectl create configmap --dry-run | kubectl apply` doesn't set ownerReferences. Acknowledge: cleanup will rely on namespace-level GC or explicit `kubectl delete cm dlh-slo-*` until a follow-up plan adds owner refs (out of scope for Plan 9 — flagged as known limitation in FINDINGS).
  - `load_table_schema` with commas — passed whole-string into a single bash variable substitution; verified by Task 6 end-to-end run executing INSERTs successfully.
- Placeholder scan: no TBD/TODO; all commands and YAML are concrete.
- Type consistency: `slo_name`, `slo_vars`, `load_table_name`, `load_table_schema` parameter names are identical across spec, scenarios, util WT inputs, and verification queries.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-05-18-01-scenario-optimization.md`. Two execution options:

**1. Subagent-Driven (recommended)** — fresh subagent per task, review between tasks, fast iteration on a live minikube cluster.

**2. Inline Execution** — execute tasks in this session using executing-plans, batch execution with checkpoints.

Which approach?
