# Plan 8 — Per-Type Grafana Dashboards Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship three per-type Grafana dashboards — `dlh-mysql`, `dlh-kafka`, `dlh-doris` — sourced from the custom Trend/Counter metrics emitted by the Plan 7 runners and the verdict gauges from Plan 5. The `dlh-doris` dashboard ships with the same panel skeleton as the others but reports "No data" until a future phase brings Doris up (Plan 7 spike was NO-GO).

**Architecture:** Three new JSON files under `dashboards/grafana/` + their chart-embed copies under `helm/dlh-test-fw/files/dashboards/`. The chart's existing `templates/dashboards-configmaps.yaml` auto-globs every JSON there and wraps it as a `dlh-dashboard=true` ConfigMap; Grafana's sidecar provisioner picks them up automatically — no chart template change needed. Existing `dlh-run-detail` dashboard gets three cross-link buttons to the new per-type dashboards (the only modification to existing dashboards).

**Tech Stack:** Grafana 8.x (sidecar-provisioned dashboards), VictoriaMetrics PromQL, Helm `Files.Glob`, jq for JSON validation, kubectl, bash. No new container images, no chart template changes beyond auto-globbed ConfigMaps.

**Prerequisites:**
- Plan 7 merged (commits up to `6fc68d2`). The `feat/phase-2-scripts-dashboards` worktree is at `/Users/allen/repo/dlh-test-fw-phase2`.
- Phase 1 dashboards (`dlh-run-detail`, `dlh-history`) deployed and working — Plan 8 follows their datasource-UID-pinning and `last_over_time(...[7d])` conventions exactly.
- `make run-mysql` and `make run-kafka` have produced VM series (otherwise the variable cascade has nothing to populate and you can't visually verify the dashboards).
- `helm dependency update helm/dlh-test-fw` has been run in this worktree (Plan 7 task 5 did it; if the `helm/dlh-test-fw/charts/` dir is empty, re-run).

**Out of scope (deferred or owned by future work):**
- Bringing Doris up on minikube (still NO-GO per Plan 7 task 4). The `dlh-doris` dashboard ships as a panel template; rerunning it once Doris is alive should populate it without dashboard changes.
- Target-side observability (mysql_exporter, kafka_exporter, Doris `/metrics`) — load-side metrics only.
- Any change to `dlh-history.json`, `dlh-run-detail.json` panels except the three new top-of-dashboard cross-link buttons.
- Any change to `verdict-job/`, the chart templates other than the auto-globbed ConfigMaps, scenarios, runners, or fixture images.

---

## Metric naming reference (from Plan 7 FINDINGS)

These are the EXACT series names that VictoriaMetrics actually serves. The `k6_` prefix is added by k6's prometheus-remote-write output for every CUSTOM metric (Trend or Counter). Counter `_total` names get a second `_total` appended on export. Verdict metrics are pushed directly to VM by the Go binary (NOT through k6), so they keep their original names.

| Source | Code name (Trend / Counter) | Series in VM |
|---|---|---|
| `lib/mysql.js` Trend | `dlh_mysql_query_duration_seconds` | `k6_dlh_mysql_query_duration_seconds_{p95,p99,avg,min,max}` (tagged `op`) |
| `lib/mysql.js` Counter | `dlh_mysql_queries_total` | `k6_dlh_mysql_queries_total_total` (tagged `op`) |
| `lib/kafka.js` Trend | `dlh_kafka_produce_duration_seconds` | `k6_dlh_kafka_produce_duration_seconds_{p95,...}` (tagged `topic`) |
| `lib/kafka.js` Counter | `dlh_kafka_messages_produced_total` | `k6_dlh_kafka_messages_produced_total_total` (tagged `topic`) |
| `lib/doris.js` Trend | `dlh_doris_streamload_duration_seconds` | `k6_dlh_doris_streamload_duration_seconds_{p95,...}` (tagged `db`, `table`) |
| `lib/doris.js` Counter | `dlh_doris_streamload_rows_total` | `k6_dlh_doris_streamload_rows_total_total` (tagged `db`, `table`) |
| `lib/doris.js` Trend (query op) | `dlh_doris_query_duration_seconds` | `k6_dlh_doris_query_duration_seconds_{p95,...}` |
| `lib/common.js` Counter | `dlh_app_errors_total` | `k6_dlh_app_errors_total_total` (tagged `kind`) |
| Plan 3 verdict-job Gauges | `dlh_verdict_{overall,chaos_pass,threshold_pass,threshold_value}` | `dlh_verdict_*` (NO prefix — pushed directly to VM, not through k6) |
| k6 built-ins | `k6_vus`, `k6_iterations`, etc. | `k6_vus`, `k6_iterations_total`, ... |

Every series carries `dlh_scenario` and `dlh_workflow` labels (set by k6 `--tag` and by verdict-job's metric push). Dashboards filter on those.

---

## File structure

```
dashboards/grafana/
├── dlh-mysql.json                    ← NEW
├── dlh-kafka.json                    ← NEW
├── dlh-doris.json                    ← NEW
├── dlh-run-detail.json               ← MODIFIED: add 3 cross-link buttons (top of dashboard)
└── dlh-history.json                  ← unchanged

helm/dlh-test-fw/files/dashboards/
├── dlh-mysql.json                    ← NEW (mirror of source)
├── dlh-kafka.json                    ← NEW
├── dlh-doris.json                    ← NEW
├── dlh-run-detail.json               ← MIRRORED after Task 8 edit
└── dlh-history.json                  ← unchanged

spikes/k6-vm-remote-write/FINDINGS.md ← APPENDED: Plan 8 wrap-up + Phase 2 milestone summary
```

The chart's `templates/dashboards-configmaps.yaml` (Phase 1) does `Files.Glob "files/dashboards/*.json"` already — no template change needed. Adding a JSON there = new ConfigMap on `helm upgrade` = sidecar auto-imports = dashboard appears in Grafana.

Each new dashboard follows the same panel-grid shape (mirrors `dlh-run-detail`'s recently-tightened layout):

```
y=0,  h=8  ┌── <type>-specific timeseries A ──┬── B ──┬── C ──┐ (w=8 each)
y=8,  h=5  ├──── Verdict — overall (w=12) ────┴── Verdict — chaos (w=12) ───┤
y=13, h=8  └────────────── Verdict — SLO thresholds table (w=24) ──────────┘
```

What varies per dashboard is just the three timeseries panels in the first row (query rate / latency / error rate, with per-type metric names) and the marker metric used for the `$scenario` variable cascade.

---

## Task 1: Verify the assumptions hold (live VM has the series)

Before authoring 3 dashboards on the assumption certain series exist, prove they do. If anything is absent, fix the underlying issue OR adjust the panel queries — don't paper over.

**Files:** verify-only.

- [ ] **Step 1: Port-forward Grafana and probe VM for every metric name the dashboards will use**

```bash
cd /Users/allen/repo/dlh-test-fw-phase2
PW=$(kubectl -n dlh-test-fw get secret grafana-admin-credentials -o jsonpath='{.data.admin-password}' | base64 -d)
kubectl -n dlh-test-fw port-forward svc/dlh-grafana 3000:80 >/tmp/gf.log 2>&1 &
PF=$!
sleep 3

echo "=== mysql series ==="
for m in k6_dlh_mysql_query_duration_seconds_p95 k6_dlh_mysql_queries_total_total ; do
  CT=$(curl -s -u "admin:${PW}" -G "http://127.0.0.1:3000/api/datasources/proxy/uid/VictoriaMetrics/api/v1/series" --data-urlencode "match[]=${m}" | jq '.data | length')
  echo "  $m: ${CT} series"
done
echo "=== kafka series ==="
for m in k6_dlh_kafka_produce_duration_seconds_p95 k6_dlh_kafka_messages_produced_total_total ; do
  CT=$(curl -s -u "admin:${PW}" -G "http://127.0.0.1:3000/api/datasources/proxy/uid/VictoriaMetrics/api/v1/series" --data-urlencode "match[]=${m}" | jq '.data | length')
  echo "  $m: ${CT} series"
done
echo "=== shared series ==="
for m in k6_dlh_app_errors_total_total dlh_verdict_overall dlh_verdict_threshold_pass dlh_verdict_threshold_value dlh_verdict_chaos_pass k6_vus ; do
  CT=$(curl -s -u "admin:${PW}" -G "http://127.0.0.1:3000/api/datasources/proxy/uid/VictoriaMetrics/api/v1/series" --data-urlencode "match[]=${m}" | jq '.data | length')
  echo "  $m: ${CT} series"
done
kill $PF
```

Expected: each name returns at least 1 series for mysql + kafka + shared. The doris series (`k6_dlh_doris_*`) WILL return 0 — that's by design (Doris NO-GO).

If any of the mysql/kafka/shared names returns 0, STOP and find out why before authoring dashboards against them.

- [ ] **Step 2: No commit — verification only.**

---

## Task 2: Author `dashboards/grafana/dlh-mysql.json`

**Files:**
- Create: `dashboards/grafana/dlh-mysql.json`

Six panels: 3 timeseries (top row, w=8 each) + 2 verdict stats + 1 threshold table (mirrors run-detail's revised layout).

- [ ] **Step 1: Write the file**

```bash
cd /Users/allen/repo/dlh-test-fw-phase2
cat > dashboards/grafana/dlh-mysql.json <<'JSON'
{
  "title": "DLH — MySQL",
  "uid": "dlh-mysql",
  "schemaVersion": 39,
  "tags": ["dlh"],
  "timezone": "",
  "time": { "from": "now-7d", "to": "now" },
  "links": [
    { "title": "Open in Run Detail", "type": "dashboards", "tags": [], "asDropdown": false, "includeVars": true, "keepTime": true, "url": "/d/dlh-run/", "targetBlank": false }
  ],
  "templating": {
    "list": [
      {
        "name": "scenario",
        "label": "Scenario",
        "type": "query",
        "datasource": { "type": "prometheus", "uid": "VictoriaMetrics" },
        "query": { "query": "label_values(k6_dlh_mysql_queries_total_total, dlh_scenario)", "refId": "StandardVariableQuery" },
        "refresh": 2,
        "sort": 1,
        "includeAll": false,
        "multi": false
      },
      {
        "name": "workflow",
        "label": "Workflow",
        "type": "query",
        "datasource": { "type": "prometheus", "uid": "VictoriaMetrics" },
        "query": { "query": "label_values(k6_dlh_mysql_queries_total_total{dlh_scenario=\"$scenario\"}, dlh_workflow)", "refId": "StandardVariableQuery" },
        "refresh": 2,
        "sort": 1,
        "includeAll": false,
        "multi": false
      }
    ]
  },
  "panels": [
    {
      "type": "timeseries",
      "title": "Query rate by op",
      "description": "k6 issue rate per MySQL op (read/write/...) — series tagged `op` by lib/mysql.js.",
      "targets": [
        { "expr": "sum by (op) (rate(k6_dlh_mysql_queries_total_total{dlh_scenario=\"$scenario\",dlh_workflow=\"$workflow\"}[30s]))", "datasource": { "type": "prometheus", "uid": "VictoriaMetrics" }, "legendFormat": "{{op}}" }
      ],
      "fieldConfig": { "defaults": { "unit": "reqps" } },
      "gridPos": { "x": 0, "y": 0, "w": 8, "h": 8 }
    },
    {
      "type": "timeseries",
      "title": "Query p95 latency",
      "description": "Per-op p95 latency, gauge emitted by k6 prom-rw from the lib/mysql.js Trend.",
      "targets": [
        { "expr": "k6_dlh_mysql_query_duration_seconds_p95{dlh_scenario=\"$scenario\",dlh_workflow=\"$workflow\"}", "datasource": { "type": "prometheus", "uid": "VictoriaMetrics" }, "legendFormat": "{{op}}" }
      ],
      "fieldConfig": { "defaults": { "unit": "s" } },
      "gridPos": { "x": 8, "y": 0, "w": 8, "h": 8 }
    },
    {
      "type": "timeseries",
      "title": "Errors by kind",
      "description": "App-level errors from common.js dlh_app_errors_total, filtered to mysql-* kinds.",
      "targets": [
        { "expr": "sum by (kind) (rate(k6_dlh_app_errors_total_total{kind=~\"mysql.*\",dlh_workflow=\"$workflow\"}[30s]))", "datasource": { "type": "prometheus", "uid": "VictoriaMetrics" }, "legendFormat": "{{kind}}" }
      ],
      "fieldConfig": { "defaults": { "unit": "ops" } },
      "gridPos": { "x": 16, "y": 0, "w": 8, "h": 8 }
    },
    {
      "type": "stat",
      "title": "Verdict — overall",
      "description": "Pushed by verdict-job at end-of-run. Survives 5min VM lookback-delta via last_over_time(...[7d]).",
      "targets": [
        { "expr": "last_over_time(dlh_verdict_overall{dlh_workflow=\"$workflow\"}[7d])", "datasource": { "type": "prometheus", "uid": "VictoriaMetrics" }, "instant": true }
      ],
      "fieldConfig": {
        "defaults": {
          "mappings": [
            { "type": "value", "options": { "0": { "text": "FAIL", "color": "red" }, "1": { "text": "PASS", "color": "green" } } }
          ],
          "color": { "mode": "thresholds" },
          "thresholds": { "mode": "absolute", "steps": [ { "color": "red", "value": null }, { "color": "green", "value": 1 } ] }
        }
      },
      "options": { "colorMode": "background", "graphMode": "none", "textMode": "value", "reduceOptions": { "calcs": ["lastNotNull"], "fields": "", "values": false } },
      "gridPos": { "x": 0, "y": 8, "w": 12, "h": 5 }
    },
    {
      "type": "stat",
      "title": "Verdict — chaos",
      "targets": [
        { "expr": "last_over_time(dlh_verdict_chaos_pass{dlh_workflow=\"$workflow\"}[7d])", "datasource": { "type": "prometheus", "uid": "VictoriaMetrics" }, "instant": true }
      ],
      "fieldConfig": {
        "defaults": {
          "mappings": [
            { "type": "value", "options": { "0": { "text": "NOT PASS", "color": "red" }, "1": { "text": "PASS", "color": "green" } } }
          ],
          "color": { "mode": "thresholds" },
          "thresholds": { "mode": "absolute", "steps": [ { "color": "red", "value": null }, { "color": "green", "value": 1 } ] }
        }
      },
      "options": { "colorMode": "background", "graphMode": "none", "textMode": "value", "reduceOptions": { "calcs": ["lastNotNull"], "fields": "", "values": false } },
      "gridPos": { "x": 12, "y": 8, "w": 12, "h": 5 }
    },
    {
      "type": "table",
      "title": "Verdict — SLO thresholds",
      "description": "Per-threshold pass/fail + measured value, joined by threshold name.",
      "targets": [
        { "expr": "last_over_time(dlh_verdict_threshold_pass{dlh_workflow=\"$workflow\"}[7d])", "datasource": { "type": "prometheus", "uid": "VictoriaMetrics" }, "format": "table", "instant": true, "refId": "A" },
        { "expr": "last_over_time(dlh_verdict_threshold_value{dlh_workflow=\"$workflow\"}[7d])", "datasource": { "type": "prometheus", "uid": "VictoriaMetrics" }, "format": "table", "instant": true, "refId": "B" }
      ],
      "transformations": [
        { "id": "joinByField", "options": { "byField": "name", "mode": "outer" } },
        {
          "id": "organize",
          "options": {
            "excludeByName": {
              "Time": true, "Time 1": true, "Time 2": true,
              "__name__": true, "__name__ 1": true, "__name__ 2": true,
              "dlh_workflow": true, "dlh_workflow 1": true, "dlh_workflow 2": true,
              "dlh_scenario": true, "dlh_scenario 1": true, "dlh_scenario 2": true
            },
            "renameByName": { "name": "threshold", "Value #A": "pass", "Value #B": "value" },
            "indexByName": { "name": 0, "Value #A": 1, "Value #B": 2 }
          }
        }
      ],
      "fieldConfig": {
        "defaults": {},
        "overrides": [
          {
            "matcher": { "id": "byName", "options": "pass" },
            "properties": [
              { "id": "mappings", "value": [ { "type": "value", "options": { "0": { "text": "FAIL", "color": "red" }, "1": { "text": "PASS", "color": "green" } } } ] },
              { "id": "custom.cellOptions", "value": { "type": "color-background" } }
            ]
          }
        ]
      },
      "gridPos": { "x": 0, "y": 13, "w": 24, "h": 8 }
    }
  ]
}
JSON
```

- [ ] **Step 2: Validate JSON syntax**

```bash
cd /Users/allen/repo/dlh-test-fw-phase2
jq empty dashboards/grafana/dlh-mysql.json && echo "valid"
```

Expected: prints `valid`. If it errors, fix the JSON and re-validate.

- [ ] **Step 3: Mirror to chart-embed location**

```bash
cd /Users/allen/repo/dlh-test-fw-phase2
cp dashboards/grafana/dlh-mysql.json helm/dlh-test-fw/files/dashboards/dlh-mysql.json
diff -q dashboards/grafana/dlh-mysql.json helm/dlh-test-fw/files/dashboards/dlh-mysql.json && echo "in sync"
```

Expected: `in sync` (no diff).

- [ ] **Step 4: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw-phase2
git add dashboards/grafana/dlh-mysql.json helm/dlh-test-fw/files/dashboards/dlh-mysql.json
git commit -m "dashboard: dlh-mysql — query rate / p95 / errors / verdict (Plan 8)"
```

---

## Task 3: Apply chart and verify `dlh-mysql` provisions cleanly

**Files:** verify-only.

- [ ] **Step 1: Helm upgrade so the new ConfigMap lands**

```bash
cd /Users/allen/repo/dlh-test-fw-phase2
helm upgrade --install dlh helm/dlh-test-fw -n dlh-test-fw \
  -f helm/dlh-test-fw/values.yaml -f helm/dlh-test-fw/values-minikube.yaml --timeout 5m 2>&1 | tail -3
```

Expected: `STATUS: deployed`.

- [ ] **Step 2: ConfigMap exists with the sidecar label**

```bash
kubectl -n dlh-test-fw get cm dlh-dashboard-dlh-mysql --show-labels 2>&1 | head -3
```

Expected: a row with `dlh-dashboard=true` label.

- [ ] **Step 3: Sidecar imported it**

```bash
kubectl -n dlh-test-fw logs deploy/dlh-grafana -c grafana-sc-dashboard --tail=5 2>&1 | grep -i "dlh-mysql" | tail -3
```

Expected: a line containing `Writing /tmp/dashboards/dlh-mysql.json (ascii)` (the sidecar's import log).

- [ ] **Step 4: Probe Grafana for the dashboard + at least one panel's query**

```bash
PW=$(kubectl -n dlh-test-fw get secret grafana-admin-credentials -o jsonpath='{.data.admin-password}' | base64 -d)
kubectl -n dlh-test-fw port-forward svc/dlh-grafana 3000:80 >/tmp/gf.log 2>&1 &
sleep 3
curl -s -u "admin:${PW}" "http://127.0.0.1:3000/api/dashboards/uid/dlh-mysql" | jq '.dashboard | {title, uid, panels: (.panels | length)}'
WF=$(kubectl -n dlh-test-fw get workflow -o name --sort-by=.metadata.creationTimestamp | grep mysql-pod-delete | tail -1 | cut -d/ -f2)
echo "checking against workflow: $WF"
curl -s -u "admin:${PW}" -G "http://127.0.0.1:3000/api/datasources/proxy/uid/VictoriaMetrics/api/v1/query" \
  --data-urlencode "query=k6_dlh_mysql_query_duration_seconds_p95{dlh_workflow=\"$WF\"}" | jq '.data.result | length'
kill %1
```

Expected:
- Dashboard returns `title: "DLH — MySQL"`, `uid: "dlh-mysql"`, `panels: 6`.
- Probe query returns at least 1 series for the latest mysql workflow.

- [ ] **Step 5: No commit — verification only.**

---

## Task 4: Author `dashboards/grafana/dlh-kafka.json`

**Files:**
- Create: `dashboards/grafana/dlh-kafka.json`

Same skeleton, kafka-specific top row (produce rate / produce p95 / errors).

- [ ] **Step 1: Write the file**

```bash
cd /Users/allen/repo/dlh-test-fw-phase2
cat > dashboards/grafana/dlh-kafka.json <<'JSON'
{
  "title": "DLH — Kafka",
  "uid": "dlh-kafka",
  "schemaVersion": 39,
  "tags": ["dlh"],
  "timezone": "",
  "time": { "from": "now-7d", "to": "now" },
  "links": [
    { "title": "Open in Run Detail", "type": "dashboards", "tags": [], "asDropdown": false, "includeVars": true, "keepTime": true, "url": "/d/dlh-run/", "targetBlank": false }
  ],
  "templating": {
    "list": [
      {
        "name": "scenario",
        "label": "Scenario",
        "type": "query",
        "datasource": { "type": "prometheus", "uid": "VictoriaMetrics" },
        "query": { "query": "label_values(k6_dlh_kafka_messages_produced_total_total, dlh_scenario)", "refId": "StandardVariableQuery" },
        "refresh": 2,
        "sort": 1,
        "includeAll": false,
        "multi": false
      },
      {
        "name": "workflow",
        "label": "Workflow",
        "type": "query",
        "datasource": { "type": "prometheus", "uid": "VictoriaMetrics" },
        "query": { "query": "label_values(k6_dlh_kafka_messages_produced_total_total{dlh_scenario=\"$scenario\"}, dlh_workflow)", "refId": "StandardVariableQuery" },
        "refresh": 2,
        "sort": 1,
        "includeAll": false,
        "multi": false
      }
    ]
  },
  "panels": [
    {
      "type": "timeseries",
      "title": "Produce rate by topic",
      "description": "Messages produced per second, tagged by topic.",
      "targets": [
        { "expr": "sum by (topic) (rate(k6_dlh_kafka_messages_produced_total_total{dlh_scenario=\"$scenario\",dlh_workflow=\"$workflow\"}[30s]))", "datasource": { "type": "prometheus", "uid": "VictoriaMetrics" }, "legendFormat": "{{topic}}" }
      ],
      "fieldConfig": { "defaults": { "unit": "mps" } },
      "gridPos": { "x": 0, "y": 0, "w": 8, "h": 8 }
    },
    {
      "type": "timeseries",
      "title": "Produce p95 latency",
      "description": "Per-topic p95 produce latency from lib/kafka.js Trend.",
      "targets": [
        { "expr": "k6_dlh_kafka_produce_duration_seconds_p95{dlh_scenario=\"$scenario\",dlh_workflow=\"$workflow\"}", "datasource": { "type": "prometheus", "uid": "VictoriaMetrics" }, "legendFormat": "{{topic}}" }
      ],
      "fieldConfig": { "defaults": { "unit": "s" } },
      "gridPos": { "x": 8, "y": 0, "w": 8, "h": 8 }
    },
    {
      "type": "timeseries",
      "title": "Errors by kind",
      "description": "App-level errors filtered to kafka-* kinds.",
      "targets": [
        { "expr": "sum by (kind) (rate(k6_dlh_app_errors_total_total{kind=~\"kafka.*\",dlh_workflow=\"$workflow\"}[30s]))", "datasource": { "type": "prometheus", "uid": "VictoriaMetrics" }, "legendFormat": "{{kind}}" }
      ],
      "fieldConfig": { "defaults": { "unit": "ops" } },
      "gridPos": { "x": 16, "y": 0, "w": 8, "h": 8 }
    },
    {
      "type": "stat",
      "title": "Verdict — overall",
      "targets": [
        { "expr": "last_over_time(dlh_verdict_overall{dlh_workflow=\"$workflow\"}[7d])", "datasource": { "type": "prometheus", "uid": "VictoriaMetrics" }, "instant": true }
      ],
      "fieldConfig": {
        "defaults": {
          "mappings": [
            { "type": "value", "options": { "0": { "text": "FAIL", "color": "red" }, "1": { "text": "PASS", "color": "green" } } }
          ],
          "color": { "mode": "thresholds" },
          "thresholds": { "mode": "absolute", "steps": [ { "color": "red", "value": null }, { "color": "green", "value": 1 } ] }
        }
      },
      "options": { "colorMode": "background", "graphMode": "none", "textMode": "value", "reduceOptions": { "calcs": ["lastNotNull"], "fields": "", "values": false } },
      "gridPos": { "x": 0, "y": 8, "w": 12, "h": 5 }
    },
    {
      "type": "stat",
      "title": "Verdict — chaos",
      "targets": [
        { "expr": "last_over_time(dlh_verdict_chaos_pass{dlh_workflow=\"$workflow\"}[7d])", "datasource": { "type": "prometheus", "uid": "VictoriaMetrics" }, "instant": true }
      ],
      "fieldConfig": {
        "defaults": {
          "mappings": [
            { "type": "value", "options": { "0": { "text": "NOT PASS", "color": "red" }, "1": { "text": "PASS", "color": "green" } } }
          ],
          "color": { "mode": "thresholds" },
          "thresholds": { "mode": "absolute", "steps": [ { "color": "red", "value": null }, { "color": "green", "value": 1 } ] }
        }
      },
      "options": { "colorMode": "background", "graphMode": "none", "textMode": "value", "reduceOptions": { "calcs": ["lastNotNull"], "fields": "", "values": false } },
      "gridPos": { "x": 12, "y": 8, "w": 12, "h": 5 }
    },
    {
      "type": "table",
      "title": "Verdict — SLO thresholds",
      "targets": [
        { "expr": "last_over_time(dlh_verdict_threshold_pass{dlh_workflow=\"$workflow\"}[7d])", "datasource": { "type": "prometheus", "uid": "VictoriaMetrics" }, "format": "table", "instant": true, "refId": "A" },
        { "expr": "last_over_time(dlh_verdict_threshold_value{dlh_workflow=\"$workflow\"}[7d])", "datasource": { "type": "prometheus", "uid": "VictoriaMetrics" }, "format": "table", "instant": true, "refId": "B" }
      ],
      "transformations": [
        { "id": "joinByField", "options": { "byField": "name", "mode": "outer" } },
        {
          "id": "organize",
          "options": {
            "excludeByName": {
              "Time": true, "Time 1": true, "Time 2": true,
              "__name__": true, "__name__ 1": true, "__name__ 2": true,
              "dlh_workflow": true, "dlh_workflow 1": true, "dlh_workflow 2": true,
              "dlh_scenario": true, "dlh_scenario 1": true, "dlh_scenario 2": true
            },
            "renameByName": { "name": "threshold", "Value #A": "pass", "Value #B": "value" },
            "indexByName": { "name": 0, "Value #A": 1, "Value #B": 2 }
          }
        }
      ],
      "fieldConfig": {
        "defaults": {},
        "overrides": [
          {
            "matcher": { "id": "byName", "options": "pass" },
            "properties": [
              { "id": "mappings", "value": [ { "type": "value", "options": { "0": { "text": "FAIL", "color": "red" }, "1": { "text": "PASS", "color": "green" } } } ] },
              { "id": "custom.cellOptions", "value": { "type": "color-background" } }
            ]
          }
        ]
      },
      "gridPos": { "x": 0, "y": 13, "w": 24, "h": 8 }
    }
  ]
}
JSON
```

- [ ] **Step 2: Validate + mirror + commit**

```bash
cd /Users/allen/repo/dlh-test-fw-phase2
jq empty dashboards/grafana/dlh-kafka.json && echo "valid"
cp dashboards/grafana/dlh-kafka.json helm/dlh-test-fw/files/dashboards/dlh-kafka.json
diff -q dashboards/grafana/dlh-kafka.json helm/dlh-test-fw/files/dashboards/dlh-kafka.json && echo "in sync"
git add dashboards/grafana/dlh-kafka.json helm/dlh-test-fw/files/dashboards/dlh-kafka.json
git commit -m "dashboard: dlh-kafka — produce rate / p95 / errors / verdict (Plan 8)"
```

---

## Task 5: Apply chart and verify `dlh-kafka` provisions cleanly

**Files:** verify-only.

- [ ] **Step 1: helm upgrade + sidecar + Grafana API probe**

```bash
cd /Users/allen/repo/dlh-test-fw-phase2
helm upgrade --install dlh helm/dlh-test-fw -n dlh-test-fw \
  -f helm/dlh-test-fw/values.yaml -f helm/dlh-test-fw/values-minikube.yaml --timeout 5m 2>&1 | tail -3
kubectl -n dlh-test-fw logs deploy/dlh-grafana -c grafana-sc-dashboard --tail=10 2>&1 | grep -i "dlh-kafka" | tail -1

PW=$(kubectl -n dlh-test-fw get secret grafana-admin-credentials -o jsonpath='{.data.admin-password}' | base64 -d)
kubectl -n dlh-test-fw port-forward svc/dlh-grafana 3000:80 >/tmp/gf.log 2>&1 &
sleep 3
curl -s -u "admin:${PW}" "http://127.0.0.1:3000/api/dashboards/uid/dlh-kafka" | jq '.dashboard | {title, uid, panels: (.panels | length)}'
WF=$(kubectl -n dlh-test-fw get workflow -o name --sort-by=.metadata.creationTimestamp | grep kafka-broker-partition | tail -1 | cut -d/ -f2)
echo "checking against workflow: $WF"
curl -s -u "admin:${PW}" -G "http://127.0.0.1:3000/api/datasources/proxy/uid/VictoriaMetrics/api/v1/query" \
  --data-urlencode "query=k6_dlh_kafka_messages_produced_total_total{dlh_workflow=\"$WF\"}" | jq '.data.result | length'
kill %1
```

Expected: dashboard returns title `DLH — Kafka`, 6 panels; probe returns ≥1 series.

- [ ] **Step 2: No commit — verification only.**

---

## Task 6: Author `dashboards/grafana/dlh-doris.json` (template-only — Doris is NO-GO)

**Files:**
- Create: `dashboards/grafana/dlh-doris.json`

Same skeleton, doris-specific top row (Stream Load rate / streamload p95 / errors). Will render with "No data" panels until Doris is brought up; that's the intended state.

- [ ] **Step 1: Write the file**

```bash
cd /Users/allen/repo/dlh-test-fw-phase2
cat > dashboards/grafana/dlh-doris.json <<'JSON'
{
  "title": "DLH — Doris",
  "uid": "dlh-doris",
  "schemaVersion": 39,
  "tags": ["dlh"],
  "timezone": "",
  "time": { "from": "now-7d", "to": "now" },
  "links": [
    { "title": "Open in Run Detail", "type": "dashboards", "tags": [], "asDropdown": false, "includeVars": true, "keepTime": true, "url": "/d/dlh-run/", "targetBlank": false }
  ],
  "templating": {
    "list": [
      {
        "name": "scenario",
        "label": "Scenario",
        "type": "query",
        "datasource": { "type": "prometheus", "uid": "VictoriaMetrics" },
        "query": { "query": "label_values(k6_dlh_doris_streamload_rows_total_total, dlh_scenario)", "refId": "StandardVariableQuery" },
        "refresh": 2,
        "sort": 1,
        "includeAll": false,
        "multi": false
      },
      {
        "name": "workflow",
        "label": "Workflow",
        "type": "query",
        "datasource": { "type": "prometheus", "uid": "VictoriaMetrics" },
        "query": { "query": "label_values(k6_dlh_doris_streamload_rows_total_total{dlh_scenario=\"$scenario\"}, dlh_workflow)", "refId": "StandardVariableQuery" },
        "refresh": 2,
        "sort": 1,
        "includeAll": false,
        "multi": false
      }
    ]
  },
  "panels": [
    {
      "type": "timeseries",
      "title": "Stream Load rate (rows/sec)",
      "description": "Rows accepted per second per (db, table). Empty until Doris is alive (Plan 7 spike NO-GO; see targets/doris/README.md).",
      "targets": [
        { "expr": "sum by (db, table) (rate(k6_dlh_doris_streamload_rows_total_total{dlh_scenario=\"$scenario\",dlh_workflow=\"$workflow\"}[30s]))", "datasource": { "type": "prometheus", "uid": "VictoriaMetrics" }, "legendFormat": "{{db}}.{{table}}" }
      ],
      "fieldConfig": { "defaults": { "unit": "rps" } },
      "gridPos": { "x": 0, "y": 0, "w": 8, "h": 8 }
    },
    {
      "type": "timeseries",
      "title": "Stream Load p95 latency",
      "description": "Per-batch p95 latency from lib/doris.js Trend.",
      "targets": [
        { "expr": "k6_dlh_doris_streamload_duration_seconds_p95{dlh_scenario=\"$scenario\",dlh_workflow=\"$workflow\"}", "datasource": { "type": "prometheus", "uid": "VictoriaMetrics" }, "legendFormat": "{{db}}.{{table}}" }
      ],
      "fieldConfig": { "defaults": { "unit": "s" } },
      "gridPos": { "x": 8, "y": 0, "w": 8, "h": 8 }
    },
    {
      "type": "timeseries",
      "title": "Errors by kind",
      "description": "App-level errors filtered to doris-* kinds (streamload / query).",
      "targets": [
        { "expr": "sum by (kind) (rate(k6_dlh_app_errors_total_total{kind=~\"doris.*\",dlh_workflow=\"$workflow\"}[30s]))", "datasource": { "type": "prometheus", "uid": "VictoriaMetrics" }, "legendFormat": "{{kind}}" }
      ],
      "fieldConfig": { "defaults": { "unit": "ops" } },
      "gridPos": { "x": 16, "y": 0, "w": 8, "h": 8 }
    },
    {
      "type": "stat",
      "title": "Verdict — overall",
      "targets": [
        { "expr": "last_over_time(dlh_verdict_overall{dlh_workflow=\"$workflow\"}[7d])", "datasource": { "type": "prometheus", "uid": "VictoriaMetrics" }, "instant": true }
      ],
      "fieldConfig": {
        "defaults": {
          "mappings": [
            { "type": "value", "options": { "0": { "text": "FAIL", "color": "red" }, "1": { "text": "PASS", "color": "green" } } }
          ],
          "color": { "mode": "thresholds" },
          "thresholds": { "mode": "absolute", "steps": [ { "color": "red", "value": null }, { "color": "green", "value": 1 } ] }
        }
      },
      "options": { "colorMode": "background", "graphMode": "none", "textMode": "value", "reduceOptions": { "calcs": ["lastNotNull"], "fields": "", "values": false } },
      "gridPos": { "x": 0, "y": 8, "w": 12, "h": 5 }
    },
    {
      "type": "stat",
      "title": "Verdict — chaos",
      "targets": [
        { "expr": "last_over_time(dlh_verdict_chaos_pass{dlh_workflow=\"$workflow\"}[7d])", "datasource": { "type": "prometheus", "uid": "VictoriaMetrics" }, "instant": true }
      ],
      "fieldConfig": {
        "defaults": {
          "mappings": [
            { "type": "value", "options": { "0": { "text": "NOT PASS", "color": "red" }, "1": { "text": "PASS", "color": "green" } } }
          ],
          "color": { "mode": "thresholds" },
          "thresholds": { "mode": "absolute", "steps": [ { "color": "red", "value": null }, { "color": "green", "value": 1 } ] }
        }
      },
      "options": { "colorMode": "background", "graphMode": "none", "textMode": "value", "reduceOptions": { "calcs": ["lastNotNull"], "fields": "", "values": false } },
      "gridPos": { "x": 12, "y": 8, "w": 12, "h": 5 }
    },
    {
      "type": "table",
      "title": "Verdict — SLO thresholds",
      "targets": [
        { "expr": "last_over_time(dlh_verdict_threshold_pass{dlh_workflow=\"$workflow\"}[7d])", "datasource": { "type": "prometheus", "uid": "VictoriaMetrics" }, "format": "table", "instant": true, "refId": "A" },
        { "expr": "last_over_time(dlh_verdict_threshold_value{dlh_workflow=\"$workflow\"}[7d])", "datasource": { "type": "prometheus", "uid": "VictoriaMetrics" }, "format": "table", "instant": true, "refId": "B" }
      ],
      "transformations": [
        { "id": "joinByField", "options": { "byField": "name", "mode": "outer" } },
        {
          "id": "organize",
          "options": {
            "excludeByName": {
              "Time": true, "Time 1": true, "Time 2": true,
              "__name__": true, "__name__ 1": true, "__name__ 2": true,
              "dlh_workflow": true, "dlh_workflow 1": true, "dlh_workflow 2": true,
              "dlh_scenario": true, "dlh_scenario 1": true, "dlh_scenario 2": true
            },
            "renameByName": { "name": "threshold", "Value #A": "pass", "Value #B": "value" },
            "indexByName": { "name": 0, "Value #A": 1, "Value #B": 2 }
          }
        }
      ],
      "fieldConfig": {
        "defaults": {},
        "overrides": [
          {
            "matcher": { "id": "byName", "options": "pass" },
            "properties": [
              { "id": "mappings", "value": [ { "type": "value", "options": { "0": { "text": "FAIL", "color": "red" }, "1": { "text": "PASS", "color": "green" } } } ] },
              { "id": "custom.cellOptions", "value": { "type": "color-background" } }
            ]
          }
        ]
      },
      "gridPos": { "x": 0, "y": 13, "w": 24, "h": 8 }
    }
  ]
}
JSON
```

- [ ] **Step 2: Validate + mirror + commit**

```bash
cd /Users/allen/repo/dlh-test-fw-phase2
jq empty dashboards/grafana/dlh-doris.json && echo "valid"
cp dashboards/grafana/dlh-doris.json helm/dlh-test-fw/files/dashboards/dlh-doris.json
diff -q dashboards/grafana/dlh-doris.json helm/dlh-test-fw/files/dashboards/dlh-doris.json && echo "in sync"
git add dashboards/grafana/dlh-doris.json helm/dlh-test-fw/files/dashboards/dlh-doris.json
git commit -m "dashboard: dlh-doris — panel template (Doris NO-GO; populates once target is alive)"
```

---

## Task 7: Apply chart and verify `dlh-doris` provisions cleanly (with empty data)

**Files:** verify-only.

- [ ] **Step 1: helm upgrade + sidecar import + Grafana API probe**

```bash
cd /Users/allen/repo/dlh-test-fw-phase2
helm upgrade --install dlh helm/dlh-test-fw -n dlh-test-fw \
  -f helm/dlh-test-fw/values.yaml -f helm/dlh-test-fw/values-minikube.yaml --timeout 5m 2>&1 | tail -3
kubectl -n dlh-test-fw logs deploy/dlh-grafana -c grafana-sc-dashboard --tail=10 2>&1 | grep -i "dlh-doris" | tail -1

PW=$(kubectl -n dlh-test-fw get secret grafana-admin-credentials -o jsonpath='{.data.admin-password}' | base64 -d)
kubectl -n dlh-test-fw port-forward svc/dlh-grafana 3000:80 >/tmp/gf.log 2>&1 &
sleep 3
curl -s -u "admin:${PW}" "http://127.0.0.1:3000/api/dashboards/uid/dlh-doris" | jq '.dashboard | {title, uid, panels: (.panels | length)}'
# Confirm the doris series are ABSENT (expected — Doris is NO-GO)
curl -s -u "admin:${PW}" -G "http://127.0.0.1:3000/api/datasources/proxy/uid/VictoriaMetrics/api/v1/series" \
  --data-urlencode "match[]=k6_dlh_doris_streamload_rows_total_total" | jq '.data | length'
kill %1
```

Expected:
- Dashboard returns title `DLH — Doris`, 6 panels.
- doris series count = 0 (no data yet; the dashboard's "No data" state is correct).

- [ ] **Step 2: No commit — verification only.**

---

## Task 8: Cross-link buttons on `dlh-run-detail`

**Files:**
- Modify: `dashboards/grafana/dlh-run-detail.json`
- Modify: `helm/dlh-test-fw/files/dashboards/dlh-run-detail.json` (mirror)

Add a top-level `links` array with three dashboard links so a user on Run Detail can jump straight to the matching per-type dashboard with the variable context preserved (`includeVars: true`).

- [ ] **Step 1: Check the existing `links` field if present**

```bash
cd /Users/allen/repo/dlh-test-fw-phase2
jq '.links // []' dashboards/grafana/dlh-run-detail.json
```

Expected: either `null` or `[]` (Phase 1 dashboard didn't have links).

- [ ] **Step 2: Add three dashboard links via jq**

```bash
cd /Users/allen/repo/dlh-test-fw-phase2
jq '.links = [
  { "title": "Open in MySQL", "type": "dashboards", "tags": [], "asDropdown": false, "includeVars": true, "keepTime": true, "url": "/d/dlh-mysql/", "targetBlank": false },
  { "title": "Open in Kafka", "type": "dashboards", "tags": [], "asDropdown": false, "includeVars": true, "keepTime": true, "url": "/d/dlh-kafka/", "targetBlank": false },
  { "title": "Open in Doris", "type": "dashboards", "tags": [], "asDropdown": false, "includeVars": true, "keepTime": true, "url": "/d/dlh-doris/", "targetBlank": false }
]' dashboards/grafana/dlh-run-detail.json > /tmp/dlh-run-detail.json && mv /tmp/dlh-run-detail.json dashboards/grafana/dlh-run-detail.json
jq empty dashboards/grafana/dlh-run-detail.json && echo "valid"
jq '.links | length' dashboards/grafana/dlh-run-detail.json
```

Expected: `valid` then `3`.

- [ ] **Step 3: Mirror to chart-embed copy**

```bash
cd /Users/allen/repo/dlh-test-fw-phase2
cp dashboards/grafana/dlh-run-detail.json helm/dlh-test-fw/files/dashboards/dlh-run-detail.json
diff -q dashboards/grafana/dlh-run-detail.json helm/dlh-test-fw/files/dashboards/dlh-run-detail.json && echo "in sync"
```

Expected: `in sync`.

- [ ] **Step 4: helm upgrade and verify links propagate**

```bash
cd /Users/allen/repo/dlh-test-fw-phase2
helm upgrade --install dlh helm/dlh-test-fw -n dlh-test-fw \
  -f helm/dlh-test-fw/values.yaml -f helm/dlh-test-fw/values-minikube.yaml --timeout 5m 2>&1 | tail -3

PW=$(kubectl -n dlh-test-fw get secret grafana-admin-credentials -o jsonpath='{.data.admin-password}' | base64 -d)
kubectl -n dlh-test-fw port-forward svc/dlh-grafana 3000:80 >/tmp/gf.log 2>&1 &
sleep 3
curl -s -u "admin:${PW}" "http://127.0.0.1:3000/api/dashboards/uid/dlh-run" | jq '.dashboard.links | map(.title)'
kill %1
```

Expected: `["Open in MySQL", "Open in Kafka", "Open in Doris"]`.

- [ ] **Step 5: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw-phase2
git add dashboards/grafana/dlh-run-detail.json helm/dlh-test-fw/files/dashboards/dlh-run-detail.json
git commit -m "dashboard(run-detail): cross-link buttons to per-type dashboards (Plan 8)"
```

---

## Task 9: End-to-end visual smoke (the only step you actually need a browser for)

**Files:** verify-only.

The previous tasks proved the dashboards exist + parse + have real series available. This task is a manual visual confirmation that what a user sees in Grafana matches expectations.

- [ ] **Step 1: Port-forward + open Grafana**

```bash
kubectl -n dlh-test-fw port-forward svc/dlh-grafana 3000:80
# (leave running; open http://localhost:3000 in a browser)
# username: admin / password: $(kubectl -n dlh-test-fw get secret grafana-admin-credentials -o jsonpath='{.data.admin-password}' | base64 -d)
```

- [ ] **Step 2: Visit each dashboard and confirm**

For `http://localhost:3000/d/dlh-mysql/`:
- Title bar shows "DLH — MySQL"
- Top-right dropdowns: `Scenario` (mysql-pod-delete should be selectable), `Workflow` (most recent mysql workflow's timestamp name)
- Three top-row timeseries have data (query rate by op, p95 latency, errors)
- Verdict overall + chaos stat panels show PASS/FAIL colors
- Threshold table has 2 rows (p95-query-latency-chaos, error-rate-recovery)
- Top-right link "Open in Run Detail" navigates with variables preserved

Same checks for `http://localhost:3000/d/dlh-kafka/` (kafka workflow + topic-level panels).

For `http://localhost:3000/d/dlh-doris/`:
- Title bar shows "DLH — Doris"
- Variables dropdowns may be empty (no doris workflow ever ran)
- Top-row panels show "No data" (expected — Doris is NO-GO)
- Verdict panels also empty (no doris verdict ever produced)

For `http://localhost:3000/d/dlh-run/` (existing Run Detail):
- Three new top-of-dashboard link buttons: "Open in MySQL", "Open in Kafka", "Open in Doris"
- Clicking each opens the right dashboard with the current $scenario / $workflow carried over

- [ ] **Step 3: Close the port-forward (Ctrl-C). No commit — verification only.**

---

## Task 10: FINDINGS append + Phase 2 milestone wrap-up

**Files:**
- Modify: `spikes/k6-vm-remote-write/FINDINGS.md` (append a section)

Document the Phase 2 finish state for future sessions. Mirrors the Phase 1 "Post-Phase-1 amendments" pattern.

- [ ] **Step 1: Append to FINDINGS.md**

Open the file and append at the end:

```markdown

## Plan 8 + Phase 2 milestone wrap-up (2026-05-17)

Phase 2 (`feat/phase-2-scripts-dashboards`) lands the custom `dlh-k6:0.1.0`
image (Plan 6), real-protocol scenarios via env-driven runners (Plan 7),
and three per-type dashboards (Plan 8). MySQL + Kafka scenarios run
end-to-end with real protocols and produce real verdicts; Doris is
deferred (target unavailable on this minikube).

### Dashboards now in Grafana

| UID | Title | Driven by |
|---|---|---|
| `dlh-run-detail` | DLH — Run Detail (generic k6 + verdict view; Phase 1) | `k6_*` built-ins + `dlh_verdict_*` |
| `dlh-history` | DLH — History (cross-scenario; Phase 1) | `k6_*` + `dlh_verdict_*` |
| `dlh-mysql` | DLH — MySQL | `k6_dlh_mysql_*`, `k6_dlh_app_errors_*`, `dlh_verdict_*` |
| `dlh-kafka` | DLH — Kafka | `k6_dlh_kafka_*`, `k6_dlh_app_errors_*`, `dlh_verdict_*` |
| `dlh-doris` | DLH — Doris | `k6_dlh_doris_*` (empty until Doris is up), `dlh_verdict_*` |

All five dashboards share `datasource.uid = VictoriaMetrics`. Cross-links
from Run Detail to the three per-type dashboards (and back via the
`Open in Run Detail` link on each).

### Out-of-the-box-broken caveats

- `dlh-doris` shows "No data" — Doris target deploy was NO-GO on this
  workstation (apache/doris all-in-one image entrypoint exits before
  BE registers). Re-enable after a working `targets/doris/deploy.yaml`
  lands; dashboards will populate without any JSON change.
- `dlh-run-detail`'s k6 panels still reference `k6_http_*` series. Those
  series are NO LONGER EMITTED by the new runners (real protocol tests,
  not HTTP). The dashboard's k6 panels go blank for any post-Plan-7
  workflow. Fixing them requires either:
    1. Adding HTTP-mode synthetic workloads back, OR
    2. Rewriting Run Detail's k6 panels to use the new `k6_dlh_<type>_*`
       gauges (per-target view defeats the "generic" purpose).
  Cleanest follow-up: replace Run Detail's k6 panels with a per-active-
  scenario summary (top-N error kinds, total ops/sec) — Phase 3.

### Phase 2 git boundary

When merged to main, Phase 2 will appear as one `--no-ff` merge commit
matching the Phase 1 convention. The 23+ atomic commits beneath are
preserved for archaeology; `git log --first-parent` stays clean.

### Verified end-to-end (Plan 8 task 9)

- MySQL scenario: dashboard panels populated, verdict PASS after the
  Plan 7 fixes (table prep + reconnect logic).
- Kafka scenario: dashboard panels populated, verdict PASS.
- Doris dashboard: provisioned via sidecar, panels render "No data"
  state correctly (no errors in the Grafana JS console).
```

- [ ] **Step 2: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw-phase2
git add spikes/k6-vm-remote-write/FINDINGS.md
git commit -m "findings: Plan 8 + Phase 2 wrap-up — three per-type dashboards live"
```

---

## Task 11: Tag the Phase 2 MVP

**Files:** git tag only.

- [ ] **Step 1: Tag the current HEAD**

```bash
cd /Users/allen/repo/dlh-test-fw-phase2
git tag -a phase-2-mvp -m "Phase 2 MVP: dlh-k6 image + protocol-level scenarios + per-type dashboards ($(date +%Y-%m-%d))"
git tag -l phase-2-mvp
git log --oneline -3
```

Expected: tag appears in the list; `git log -3` shows the recent commits.

- [ ] **Step 2: No commit — tag only.**

---

## Definition of Done

- [ ] `dashboards/grafana/dlh-mysql.json` exists and is valid JSON.
- [ ] `dashboards/grafana/dlh-kafka.json` exists and is valid JSON.
- [ ] `dashboards/grafana/dlh-doris.json` exists and is valid JSON.
- [ ] All three chart-embed mirrors exist and are byte-identical to source.
- [ ] `helm upgrade` registers three new `dlh-dashboard-dlh-{mysql,kafka,doris}` ConfigMaps with `dlh-dashboard=true` label.
- [ ] Grafana sidecar logs show three `Writing /tmp/dashboards/dlh-{mysql,kafka,doris}.json` lines.
- [ ] `curl /api/dashboards/uid/dlh-mysql` (and kafka, doris) each return a valid dashboard with 6 panels.
- [ ] For the latest mysql + kafka workflows, the per-type dashboards' key queries return ≥1 series each.
- [ ] `dlh-doris` dashboard registers and renders panels (no JS console errors); panels show "No data" — expected and correct.
- [ ] `dlh-run-detail` has 3 cross-link buttons to the new per-type dashboards; visible via `/api/dashboards/uid/dlh-run` `links` field.
- [ ] `spikes/k6-vm-remote-write/FINDINGS.md` has a "Plan 8 + Phase 2 milestone wrap-up" section.
- [ ] Git tag `phase-2-mvp` exists.
- [ ] Each task is its own atomic commit on `feat/phase-2-scripts-dashboards`.
- [ ] No files changed outside `dashboards/grafana/`, `helm/dlh-test-fw/files/dashboards/`, `spikes/k6-vm-remote-write/FINDINGS.md`.

---

## Self-review notes

- **Spec coverage:** Implements spec §"Per-type Grafana dashboards" sub-sections `dlh-mysql panels`, `dlh-kafka panels`, `dlh-doris panels`, and `Cross-linking`. Each panel from the spec table appears in the corresponding Task 2/4/6 file with the queries listed in the spec (corrected per Plan 7 FINDINGS for k6_ prefix and double `_total`). Doris caveat from spec §"Migration / Doris caveat" honored: dashboard ships but with empty panel state.

- **Placeholder scan:** Every JSON code block contains the full dashboard content. No `TBD`, `TODO`, "fill in", or "similar to Task N" — each dashboard's full panel set is in its own task. No copy-paste-and-edit-later instructions.

- **Type consistency:**
  - The k6_-prefix + double-`_total` Counter naming rule is applied uniformly: `k6_dlh_mysql_queries_total_total`, `k6_dlh_kafka_messages_produced_total_total`, `k6_dlh_doris_streamload_rows_total_total`, `k6_dlh_app_errors_total_total`.
  - Trend gauges use the `<metric>_p95` form per Plan 7 task 1's empirical confirmation: `k6_dlh_mysql_query_duration_seconds_p95`, `k6_dlh_kafka_produce_duration_seconds_p95`, `k6_dlh_doris_streamload_duration_seconds_p95`.
  - Verdict metrics keep their no-prefix form: `dlh_verdict_overall`, `dlh_verdict_chaos_pass`, `dlh_verdict_threshold_pass`, `dlh_verdict_threshold_value` — all wrapped in `last_over_time(...[7d])`.
  - Datasource UID `VictoriaMetrics` is consistent across every panel target and every variable query.
  - Dashboard UIDs `dlh-mysql`, `dlh-kafka`, `dlh-doris` are referenced consistently in cross-links from `dlh-run-detail` and in the FINDINGS summary table.
  - Variable names `$scenario`, `$workflow` match the run-detail dashboard's existing pattern (cascade by marker metric).
  - Gridpos layout is byte-identical across the three new dashboards (only varies in the panel data, not in panel boxes).
