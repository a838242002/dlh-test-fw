# Scenario Optimization — Design Spec

**Date**: 2026-05-18
**Status**: Draft, awaiting user review
**Project**: dlh-test-fw
**Builds on**: Phase 2 MVP (tag `phase-2-mvp`)

## Why

Phase 2's scenarios work end-to-end but suffer from two structural smells:

1. **Inline boilerplate that doesn't belong in scenarios.** Each scenario embeds a `write-slo` bash heredoc (YAML-in-bash-in-YAML, three layers of escaping) and mysql adds an `ensure-load-table` bash script. Adding a new scenario means copying these blocks verbatim; tuning an SLO threshold means editing a heredoc inside a workflow step.

2. **Scattered, hard-to-experiment parameters.** Tunable values live in three places: workflow `arguments.parameters` (vus, durations), `env_map` heredoc inside the load step (mysql DSN, op mix, SQL strings), and `write-slo` heredoc (thresholds, lt values). Running a variant ("what if vus=50?") requires editing the YAML in one of three locations and re-applying. There's no override path at submission time.

## Goals (in scope)

1. Extract `write-slo` and `ensure-load-table` into reusable `util/` WorkflowTemplates that scenarios reference by name.
2. Promote SLO threshold definitions into a chart-managed library (`files/slos/<chaos_type>.yaml`) with target-specific values supplied per-scenario via variable substitution.
3. Consolidate every tunable knob (load shape, chaos shape, target-specific workload params, SLO threshold values) into the scenario's top-level `arguments.parameters` block.
4. Enable submit-time override via `argo submit -p key=value`, surfaced through `run-scenario.sh -p`.

## Goals (out of scope, deferred)

- Workload library (`files/workloads/*`) — keep workload params scenario-local until 3+ scenarios genuinely share one workload pattern.
- New chaos types (cpu-stress, disk-pressure, etc.) — orthogonal milestone.
- Kafka topic seed / Doris ensure-table utilities — Kafka uses `autoCreateTopic=true`, Doris is still NO-GO. Add when a scenario actually needs them.
- `${VAR:-default}` syntax in SLO templates — sed-based envsubst doesn't support it; scenarios must supply every variable. The `util-write-slo` sanity check fail-fasts on unresolved `${VAR}` markers.
- Multi-line SLO override CLI flag — power users can use `argo submit -p slo_vars='...'` directly; not worth a dedicated flag.
- Dashboard / verdict-job / runners / fixture-images / chaos templates — no changes.

## Architecture

```
helm upgrade
  ├─ files/slos/*.yaml         → ConfigMap dlh-slos (one CM, all SLO templates as data keys)
  └─ files/workflowtemplates/util/*.yaml → 2 new WorkflowTemplates registered

scripts/run-scenario.sh mysql-pod-delete.yaml [-p vus=50 ...]
  ↓ argo submit -p vus=50 ...
Workflow starts
  ├─ prep-slo     templateRef util-write-slo
  │                 ├ reads dlh-slos[slo_name + ".yaml"] → SLO template (has ${VAR} + {{workflow.name}})
  │                 ├ substitutes ${VAR} via sed (loops slo_vars KEY=VAL block)
  │                 ├ substitutes {{workflow.name}} (already rendered by Argo, just defensive sed)
  │                 ├ fail-fast if any unresolved ${...} remains
  │                 └ writes dlh-slo-<workflow.name> CM
  ├─ load-fixture   existing
  ├─ prep-table     templateRef util-ensure-mysql-table  (parameterized CREATE TABLE)
  ├─ chaos || load  existing chaos-pod-delete + load-k6-run
  └─ verdict        existing verdict-slo-eval (consumes dlh-slo-<wf> CM via mount)
```

## File layout

```
helm/dlh-test-fw/
├── files/
│   ├── workflowtemplates/
│   │   └── util/                          ← NEW (chart auto-globs)
│   │       ├── write-slo.yaml
│   │       └── ensure-mysql-table.yaml
│   └── slos/                              ← NEW
│       ├── pod-delete.yaml                 # template, contains ${VAR} placeholders
│       └── network-loss.yaml               # same shape as pod-delete (kept separate for future divergence)
├── templates/
│   └── slos-configmap.yaml                ← NEW (Helm Files.Glob wraps files/slos/* into one CM `dlh-slos`)

scenarios/
├── mysql-pod-delete.yaml                   ← REWRITTEN: top params block, no inline write-slo / ensure-table
├── kafka-broker-partition.yaml             ← REWRITTEN
└── doris-be-network-loss.yaml              ← REWRITTEN (deferred for live run; YAML shape matches)

scripts/
└── run-scenario.sh                         ← MODIFIED: forwards -p flags to argo submit
```

No changes to: `dlh-k6` image, runners, `verdict-job/`, dashboards, fixture-images, chaos WorkflowTemplates.

## SLO library — template form

A two-layer substitution model:

| Layer | Syntax | Filled by |
|---|---|---|
| 2a — `${VAR}` | Scenario's `slo_vars` block, via sed loop in util-write-slo |
| 2b — `{{workflow.name}}` | Argo controller (template variable already rendered into the bash step's source) |

**`files/slos/pod-delete.yaml`**:

```yaml
# Variables (all required — no defaults):
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

**`files/slos/network-loss.yaml`** has identical structure; kept separate so future divergence (different metric pair, different window) doesn't require splitting later.

**Chart wrapper template `slos-configmap.yaml`**:

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

Each SLO file lands under its filename key (`pod-delete.yaml`, `network-loss.yaml`). util-write-slo references by that key.

## util-write-slo

```yaml
apiVersion: argoproj.io/v1alpha1
kind: WorkflowTemplate
metadata:
  name: util-write-slo
spec:
  entrypoint: main
  serviceAccountName: argo-workflow
  templates:
  - name: main
    inputs:
      parameters:
      - name: slo_name        # SLO library entry, e.g. "pod-delete"
      - name: slo_vars        # multi-line KEY=VAL block, one per ${VAR} the template references
    script:
      image: alpine/k8s:1.30.0
      command: [bash]
      source: |
        set -euo pipefail

        # 1. Read SLO template from chart-provisioned dlh-slos CM.
        TPL=$(kubectl -n {{`{{workflow.namespace}}`}} get cm dlh-slos \
                -o jsonpath="{.data.{{`{{inputs.parameters.slo_name}}`}}\\.yaml}")
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
          # `|` as sed separator; values must not contain `|` (sanity-checked below).
          if [[ "$V" == *'|'* ]]; then
            echo "ERROR: slo_vars value for $K contains '|' which conflicts with sed separator" >&2
            exit 1
          fi
          RENDERED=$(printf '%s' "$RENDERED" | sed "s|\${$K}|$V|g")
        done <<< "$VARS"

        # 3. {{workflow.name}} should already have been rendered by Argo into the source: above,
        # but be defensive in case the SLO template uses the literal placeholder.
        RENDERED=$(printf '%s' "$RENDERED" | sed "s|{{`{{workflow.name}}`}}|{{`{{workflow.name}}`}}|g")

        # 4. Fail-fast on any unresolved ${VAR} markers.
        if printf '%s' "$RENDERED" | grep -qE '\$\{[A-Z_]+\}'; then
          echo "ERROR: unresolved variables in rendered SLO:" >&2
          printf '%s' "$RENDERED" | grep -E '\$\{[A-Z_]+\}' >&2
          exit 1
        fi

        # 5. Write the per-workflow CM (verdict-slo-eval reads this via volume mount).
        kubectl -n {{`{{workflow.namespace}}`}} create configmap dlh-slo-{{`{{workflow.name}}`}} \
          --from-literal=slo.yaml="$RENDERED" \
          --dry-run=client -o yaml | kubectl apply -f -
```

Notes:
- Step 3 is defensive — Argo's templating already substituted `{{workflow.name}}` when it rendered `source:`, so the literal placeholder in the SLO template gets replaced via the SAME substitution. Either way, the final RENDERED ends up with the actual workflow name.
- Step 4 catches misspelled var names (`LATNECY_METRIC=...`) or vars the scenario forgot to supply.
- RBAC: `argo-workflow` SA already has `get/create/patch configmaps` per Plan 4's backfill. Verify in plan task 1.

## util-ensure-mysql-table

```yaml
apiVersion: argoproj.io/v1alpha1
kind: WorkflowTemplate
metadata:
  name: util-ensure-mysql-table
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
      - name: schema_sql       # column definition, e.g. "id BIGINT AUTO_INCREMENT PRIMARY KEY, ts DATETIME NOT NULL"
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

## Scenario after rewrite

`scenarios/mysql-pod-delete.yaml`:

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

`kafka-broker-partition.yaml` follows the same pattern. Differences:
- `slo_vars`: `LATENCY_METRIC=k6_dlh_kafka_produce_duration_seconds`, `OPS_COUNTER=k6_dlh_kafka_messages_produced_total_total`, `ERR_KIND_PATTERN=kafka-.*`, `P95_LT=2.0`, `ERR_LT=0.10`
- No `prep-table` step (kafka's Writer has `autoCreateTopic=true`)
- workload params: `kafka_bootstrap`, `kafka_topic`, `kafka_op`, `kafka_message_size`

`doris-be-network-loss.yaml` follows the same pattern with `slo_name: network-loss` and doris-specific vars; not run live (Doris NO-GO).

## run-scenario.sh

```bash
#!/usr/bin/env bash
# Submit a scenario with optional -p overrides.
#
# Usage:
#   scripts/run-scenario.sh scenarios/<name>.yaml [argo-submit-args...]
#
# Examples:
#   scripts/run-scenario.sh scenarios/mysql-pod-delete.yaml
#   scripts/run-scenario.sh scenarios/mysql-pod-delete.yaml -p vus=50 -p mysql_op_mix=read:100
#   scripts/run-scenario.sh scenarios/mysql-pod-delete.yaml -p chaos_duration=120s
set -euo pipefail

[[ $# -lt 1 ]] && { echo "usage: $0 scenarios/<name>.yaml [argo args...]" >&2; exit 2; }
file=$1; shift

prefix=$(awk '/^[[:space:]]*generateName:/ { sub(/.*generateName: */, ""); sub(/-$/, ""); print; exit }' "$file")
[[ -z "$prefix" ]] && { echo "no generateName: in $file" >&2; exit 1; }
ts=$(date -u +%Y%m%d-%H%M%S)
name="${prefix}-${ts}"

rendered=$(mktemp)
trap 'rm -f $rendered' EXIT
sed "s|^\([[:space:]]*\)generateName:.*|\1name: ${name}|" "$file" > "$rendered"

echo "Submitting workflow: $name"
argo submit -n dlh-test-fw "$rendered" --wait "$@" || true
status=$(kubectl -n dlh-test-fw get workflow "$name" -o jsonpath='{.status.phase}')
echo "Final phase: $status"
echo "Report: argo get -n dlh-test-fw $name"
echo "         kubectl -n dlh-test-fw exec deploy/dlh-minio -- mc cat \"local/artifacts/${name}/${name}-main-*/verdict/report.json\" | jq ."
[[ "$status" == "Succeeded" ]]
```

## Testing

| Element | How |
|---|---|
| chart provisions `dlh-slos` | `kubectl get cm dlh-slos -o jsonpath='{.data}' \| jq 'keys'` returns `["pod-delete.yaml", "network-loss.yaml"]` |
| util-write-slo + util-ensure-mysql-table | `kubectl get wt util-write-slo util-ensure-mysql-table` |
| Render correctness | Trigger any scenario; `kubectl get cm dlh-slo-<wf> -o jsonpath='{.data.slo\.yaml}'` returns YAML with all `${VAR}` resolved and `{{workflow.name}}` replaced with the actual name |
| Sanity check fires | Submit a scenario whose `slo_vars` omits `P95_LT`; prep-slo step Failed with stderr containing `unresolved variables` |
| Three scenarios end-to-end | `make run-mysql` PASS, `make run-kafka` PASS, doris YAML passes lint (not run live) |
| CLI override | `make run-mysql` then `scripts/run-scenario.sh scenarios/mysql-pod-delete.yaml -p vus=5 -p mysql_op_mix=read:100`; verify `dlh-k6-env-<wf>` CM data shows `MYSQL_OP_MIX=read:100`; verify `k6_vus{dlh_workflow=...}` = 5 in VM |
| No dashboard regression | `dlh-mysql`/`dlh-kafka` dashboards still populate after a run |

## Success criteria

1. `dlh-slos` ConfigMap exists with two data keys: `pod-delete.yaml`, `network-loss.yaml`.
2. Two new WorkflowTemplates `util-write-slo` + `util-ensure-mysql-table` registered.
3. Three scenarios rewritten to the new shape; `make run-mysql` + `make run-kafka` PASS end-to-end.
4. `dlh-slo-<wf>` ConfigMaps after each run contain fully-rendered PromQL (no `${...}` or `{{...}}` remaining).
5. Submit-time override: `scripts/run-scenario.sh scenarios/mysql-pod-delete.yaml -p vus=5 -p mysql_op_mix=read:100` produces a workflow where the corresponding env-CM and runner show the overridden values.
6. Negative test: a scenario with deliberately-missing `slo_vars` entry fails at prep-slo with `unresolved variables` in stderr.
7. No regression: existing Phase 2 dashboards still populate; no change in verdict metrics shape.

## Risks

- **`argo submit --wait` exit semantics differ across argo CLI 3.x vs 4.x.** The existing `run-scenario.sh` worked around this with `argo wait`. Verify in plan task 1 that `--wait` returns when workflow reaches a terminal phase and that `[[ "$status" == "Succeeded" ]]` gate stays correct.
- **sed-based envsubst can mangle values containing the separator (`|`)**. Mitigation: util-write-slo step 2 detects and fail-fasts.
- **`{{...}}` template syntax inside the SLO file vs Argo's template substitution.** The SLO file is read at runtime from a ConfigMap, so Argo never sees its `{{workflow.name}}`. The bash step substitutes it explicitly. Verify: util-write-slo renders correctly when the SLO template contains literal `{{workflow.name}}` strings.
- **Per-workflow CM proliferation**. Both `dlh-slo-<wf>` and `dlh-k6-env-<wf>` get ownerReferences pointing at the Workflow, so k8s GC reclaims them on workflow deletion. Verify in plan.
- **`load_table_schema` value contains commas**. The schema fragment `id BIGINT, ts DATETIME` is passed as a single workflow parameter and substituted into a SQL `CREATE TABLE` body — commas are fine because the substitution is whole-string (not CSV-split). Verify in plan task that the rendered SQL is valid.

## Post-Plan-9 amendments (2026-05-18, after live merge `4d68ea3`)

Two corrections to the verbatim `util-write-slo` script in the "util-write-slo" section above — both surfaced during Plan 9 implementation and verified live:

1. **Step 3 — `{{workflow.name}}` LHS must be built at runtime, not written as a literal.** The spec's claim that "Argo's templating already substituted `{{workflow.name}}` when it rendered `source:`" is correct for the *script body* but wrong for the **SLO template body**: `TPL` is loaded from the `dlh-slos` ConfigMap at runtime via `kubectl get cm`, so Argo never sees its `{{workflow.name}}` literal. A naive `sed "s|{{workflow.name}}|{{workflow.name}}|g"` is a no-op because Argo renders both sides identically to the actual workflow name. Fix: construct the LHS at bash runtime via octal so Argo can't see the `{{...}}` token in `source:`:
   ```bash
   WF_LIT=$(printf '\173\173workflow.name\175\175')
   RENDERED=$(printf '%s' "$RENDERED" | sed "s|${WF_LIT}|{{`{{workflow.name}}`}}|g")
   ```

2. **Step 4 — unresolved-marker regex must include digits.** Spec wrote `\${[A-Z_]+}` which never matches `${P95_LT}` (contains `9`). Use `\${[A-Z0-9_]+}`. Without this fix the fail-fast guard silently passes on missing numeric-threshold vars.

3. **`slo_vars` value guard widened.** Spec rejects `|` (sed separator). Plan 9 also rejects `&` and `\` because sed treats them specially in the replacement string (`&` = whole match; `\` starts back-references / escapes). Single error message: `"slo_vars value for $K contains a sed-unsafe character (|, &, or \\)"`.

4. **`dlh-slo-<wf>` `ownerReferences` set via `{{workflow.uid}}` + jq, matching Plan 7's `load/k6-run` env-CM pattern** (not via separate `kubectl patch`). RBAC unchanged — `argo-workflow` SA already had `create/get/update/patch configmaps`. Cascade GC verified end-to-end: deleting the Workflow auto-reclaims the SLO CM.

5. **`argo submit --wait` on CLI v4.0.5 accepts multi-line `-p key=value` values verbatim** (embedded literal newlines from quoted bash arg). No parameter-file workaround needed for `-p slo_vars=<multi-line>`.

6. **Pitfall logged in FINDINGS:** when picking "the latest scenario workflow" via lexical `sort`, filter to timestamped names with `grep -E '<prefix>-[0-9]{8}-[0-9]{6}$'` first — otherwise stray Phase-2 UUID-suffixed workflows sort after timestamped ones (letters > digits in ASCII).
