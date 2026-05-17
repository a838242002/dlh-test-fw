# Chaos + Load Test Platform — Phase 1 Design

**Date**: 2026-05-16
**Status**: Draft, awaiting review
**Project**: dlh-test-fw

## 目標

在 Kubernetes 上建立一個整合 chaos + load test 的測試平台，針對應用層服務（DB、Kafka、HTTP API）執行測試場景，並依 SLO 自動產出 Pass/Fail 判定。

Phase 1 為 **YAML-only**：使用者撰寫 Argo `Workflow` YAML、`kubectl apply` 觸發，結果透過 Argo UI、Grafana、HTML report 呈現。UI 編輯延後到 Phase 2。

## 範圍

### In scope（Phase 1）
- 透過 Litmus 進行 K8s 層 chaos（pod kill、network partition…）
- 透過 k6 進行應用層 load test（MySQL、Doris、Kafka、HTTP API）
- 從 MinIO 載入 fixture 資料到目標系統
- 多時間窗 SLO 評估
- 可重用的 `WorkflowTemplate` 函式庫（fixture / chaos / load / verdict）
- 一鍵部署的 Helm chart
- 結果檢視：Argo UI、Grafana dashboard、HTML report

### Out of scope（延後）
- 編輯 scenario 的 Web UI（Phase 2）
- 跨 cluster 編排
- 自訂 RBAC（先用 K8s 預設 + service account）
- 自訂 `TestScenario` CRD 與 controller
- 即時 WebSocket dashboard

## 架構

### 元件配置
```
namespace: dlh-test-fw
├── argo-workflows         # workflow engine + Web UI
├── litmus-chaos           # chaos injection（ChaosEngine / ChaosResult CRD）
├── k6-operator            # load testing（K6 CRD）
├── minio                  # fixture 儲存 + Argo artifact repo
├── victoriametrics-single # metrics 後端
└── grafana                # dashboard

# 目標系統（mysql / doris-system / kafka-system）各自獨立 namespace
```

### 資料流
```
scenario.yaml (Workflow) ── kubectl apply ──▶ Argo Workflow Engine
                                                       │
            ┌──────────────────────┬───────────────────┴───────────────┐
            ▼                      ▼                                   ▼
       fixture-load          chaos-inject                      k6-load
       (Job: MinIO →        (ChaosEngine →                    (K6 CRD →
        target DB)            pod kill etc.)                   target service)
            │                      │                                   │
            └──────────────────────┼───────────────────────────────────┘
                                   ▼
                            VictoriaMetrics ◀── k6 remote-write metrics
                                   │
                                   ▼
                             verdict-job
                                   │
              ┌────────────────────┼────────────────────┐
              ▼                    ▼                    ▼
       Argo artifact     VM gauges (dlh_verdict_*)  Workflow exit code
    (report.json + .html → MinIO)  ────► Grafana    (0=Pass, 1=Fail)
                                        dashboard
```

### 時序模型

每個 scenario 都遵循三段窗格：

```
t=0                  t=chaos_start_after        t=chaos_start_after+chaos_duration       t=load_duration
│════════════════════╪══════════════════════════╪═══════════════════════════════════════│
│  baseline window   │       chaos window       │           recovery window             │
│ (建立基準)         │  (chaos 注入中，嚴格 SLO) │  (chaos 結束，觀察復原)               │
```

Constraint（由 verdict template 在啟動前驗證）：`chaos_start_after + chaos_duration ≤ load_duration`。

### Workflow 編排（順序保證）

使用 Argo Workflow `steps` 的兩段 dash 規則保證執行序：

```yaml
steps:
- - name: load-fixture          # 第 1 組（獨立）→ 必須先完成
- - name: chaos                  # 第 2 組（並行）
    continueOn: {failed: true}   #   chaos 失敗時 load 仍跑完
  - name: load                   #   k6 與 chaos 同時啟動
- - name: verdict                # 第 3 組（獨立）→ 等前一組全部完成
```

- 不同 `- -` 群組之間 = 嚴格依序
- 同群組內多個 `- name:` = 並行
- Fixture 失敗無 `continueOn` → 整個 workflow fail-fast（避免無資料的 k6 跑完還算 SLO）

### Load 開始時間點

`load_start_ts`（verdict 切時間窗用）使用 Argo 內建 step status：

```yaml
# verdict step arguments
parameters:
- name: load_start_ts
  value: "{{steps.load.startedAt}}"
```

誤差 < 1s（pod schedule 到 container start 之間），對 chaos engineering 的 metric window 評估足夠。

## Repository 結構

```
dlh-test-fw/
├── helm/dlh-test-fw/                # umbrella Helm chart
│   ├── Chart.yaml
│   ├── values.yaml
│   ├── charts/                       # subcharts（pinned versions）
│   │   ├── argo-workflows-*/
│   │   ├── litmus-*/
│   │   ├── k6-operator-*/
│   │   ├── minio-*/
│   │   ├── victoria-metrics-single-*/
│   │   └── grafana-*/
│   └── templates/
│       └── dlh-workflowtemplates.yaml  # 我們自己的 WorkflowTemplate
├── templates/                       # WorkflowTemplate 源碼
│   ├── fixture/
│   │   ├── minio-load-mysql.yaml
│   │   ├── minio-load-doris.yaml
│   │   └── kafka-topic-seed.yaml
│   ├── chaos/
│   │   ├── pod-delete.yaml
│   │   ├── network-loss.yaml
│   │   ├── kafka-broker-partition.yaml
│   │   └── from-hub.yaml             # 從 ChaosHub 動態載入
│   ├── load/
│   │   └── k6-run.yaml
│   └── verdict/
│       └── slo-eval.yaml
├── verdict-job/                     # Go 源碼
│   ├── cmd/verdict/main.go
│   ├── internal/prom/
│   ├── internal/slo/
│   ├── internal/report/             # JSON + HTML 渲染
│   └── Dockerfile
├── scenarios/                       # 範例 scenarios
│   ├── mysql-pod-delete.yaml
│   ├── doris-be-network-loss.yaml
│   └── kafka-broker-partition.yaml
├── dashboards/grafana/
│   ├── dlh-run-detail.json
│   └── dlh-history.json
└── docs/superpowers/specs/
    └── 2026-05-16-chaos-loadtest-platform-design.md
```

## WorkflowTemplate 函式庫

所有 template 都註冊為 `WorkflowTemplate` 於 `dlh-test-fw` namespace，scenario 透過 `templateRef` 呼叫。

### Fixture templates

| 名稱 | 輸入 | 輸出 | 行為 |
|---|---|---|---|
| `fixture/minio-load-mysql` | `uri` (s3://...)、`db_host`、`credentials_secret` | `loaded_rows`、`duration_sec` | mc cp + mysql CLI |
| `fixture/minio-load-doris` | `uri`、`fe_host`、`stream_load_credentials_secret` | 同上 | mc cp + curl Doris Stream Load API |
| `fixture/kafka-topic-seed` | `bootstrap`、`topic`、`seed_uri` | `produced_count` | mc cp + kcat -P |

Container images（包入 chart）：`dlh-fixture-mysql`、`dlh-fixture-doris`、`dlh-fixture-kafka`（alpine + 對應工具 + mc）。

### Chaos templates

底層 wrap Litmus `ChaosEngine`。ChaosHub 內的 experiment 透過 `chaos/from-hub` 動態載入。

| 名稱 | 輸入 | 輸出 |
|---|---|---|
| `chaos/pod-delete` | `target_pod_selector`、`duration`、`interval`、`force` | `chaos_result_name` |
| `chaos/network-loss` | `target_pod_selector`、`loss_percent`、`duration` | `chaos_result_name` |
| `chaos/kafka-broker-partition` | `kafka_namespace`、`broker_id`、`duration` | `chaos_result_name` |
| `chaos/from-hub` | `hub_url`、`experiment_name`、`args_map` | `chaos_result_name` |

### Load template

| 名稱 | 輸入 | 輸出 |
|---|---|---|
| `load/k6-run` | `script_configmap`、`vus`、`duration`、`env_map`、`scenario_label` | `metrics_namespace`（供 verdict 過濾 metric） |

k6 CRD 建立時開啟 `--out experimental-prometheus-rw=http://victoriametrics-single:8429/api/v1/write`，所有 metric 標上 `scenario=<scenario_label>`。

### Verdict template

| 名稱 | 輸入 | 輸出 |
|---|---|---|
| `verdict/slo-eval` | `slo_yaml`、`chaos_result_name`、`load_start_ts`、`chaos_start_after`、`chaos_duration`、`load_duration`、`metrics_namespace`、`workflow_name` | artifact: `report.json` + `report.html` (archived to MinIO `artifacts/<workflow>/verdict/report/` by Argo); VM gauges: `dlh_verdict_overall`/`chaos_pass`/`threshold_pass`/`threshold_value` |

## SLO Verdict 設計

### YAML schema（在 scenario.yaml 內）

```yaml
verdict:
  thresholds:
    - metric: <human-readable name>
      query: <PromQL，可用 $SCENARIO 等變數>
      lt: <number>          # 或 gt: <number>
      window: baseline | chaos | recovery | full
  raw_promql: <PromQL>      # optional escape hatch — 必須回 1=pass / 0=fail
  raw_window: chaos         # 若使用 raw_promql 則必填
```

### Verdict job 邏輯

```text
1. 讀 SLO YAML
2. 依 load_start_ts、chaos_start_after、chaos_duration、load_duration 算出三個窗
3. for each threshold:
     window_range = compute_window(threshold.window, params)
     value = prom.query_at(threshold.query, t=window_range.end)
     passed = (value < threshold.lt) if threshold.lt else (value > threshold.gt)
4. 若有 raw_promql：執行，期望 1=pass / 0=fail
5. 透過 K8s API 讀 ChaosResult.status.experimentStatus.verdict（"Pass" / "Fail" / "Awaited"）
   - "Awaited" 時 bounded retry（最多 30s）
6. Overall = all(threshold_pass) AND raw_promql_pass AND chaos_verdict == "Pass"
7. 輸出 report.json 與 report.html 為 Argo artifact（Argo 自動歸檔到 MinIO `artifacts/<workflow>/verdict/report/`）
8. POST verdict 摘要 gauges 到 VictoriaMetrics（`dlh_verdict_overall`/`chaos_pass`/`threshold_pass`/`threshold_value`,labels: `dlh_workflow`+`dlh_scenario`+`name`）
9. sys.exit(0 if overall else 1)
```

### Report 內容

**`report.json`**：完整 machine-readable，包含每個 metric 的 PromQL 原文、數值、時間窗、Pass 狀態、Grafana / Argo link。

**`report.html`**：self-contained 單檔 HTML，由 verdict binary 內建 `html/template` 渲染：
- 上方 verdict banner（Pass/Fail 大字 + scenario / duration / chaos verdict）
- 中間 metric 表格（per-window value、threshold、Pass）
- 下方按鈕：Open in Grafana / Download JSON / View Argo workflow

`report.html` 可直接在 Argo UI artifact viewer 內預覽，或從 MinIO bucket 取得永久 URL。

**VictoriaMetrics gauges**（給 Grafana dashboard 用,取代原設計的 ConfigMap+Infinity datasource）：

| Metric | Type | Labels | 值 |
|---|---|---|---|
| `dlh_verdict_overall` | gauge | `dlh_workflow`, `dlh_scenario` | 0=Fail, 1=Pass |
| `dlh_verdict_chaos_pass` | gauge | `dlh_workflow`, `dlh_scenario` | 0/1 |
| `dlh_verdict_threshold_pass` | gauge | `name`, `dlh_workflow`, `dlh_scenario` | 0/1（每條 SLO threshold 一條 series） |
| `dlh_verdict_threshold_value` | gauge | `name`, `dlh_workflow`, `dlh_scenario` | 實測 PromQL 值 |

完整 query/window/lt|gt 等結構化資料在 `report.json` artifact 裡;dashboard 用 PromQL `last_over_time(metric[7d])` 撈最近一筆。

## 結果檢視路徑

| 路徑 | URL | 使用者 | 看什麼 |
|---|---|---|---|
| Argo UI workflow detail | `argo.<domain>/workflows/dlh-test-fw/<name>` | dev / QA | DAG、logs、artifacts（json + html） |
| Grafana DLH Run Detail | `grafana.<domain>/d/dlh-run/?var-workflow=<name>` | dev / SRE | k6 metric 時間序列 + chaos 窗 overlay + verdict summary panel |
| Grafana DLH History | `grafana.<domain>/d/dlh-history/` | 主管 / 週會 | 最近 N 次 run 的 verdict badge + 平均 metric |
| MinIO Console | `minio-console.<domain>` | archival | 翻歷史 artifact |
| `argo wait` + exit code | n/a | CI/CD | 純 Pass/Fail |

## Deploy 拓樸

Phase 1 開發/測試環境：**minikube**（single-node local K8s）。

單一 namespace（`dlh-test-fw`）佈署所有平台元件；目標系統（mysql / kafka / doris）依資源狀況選擇性部署於各自 namespace。

### minikube 啟動配置

```bash
minikube start \
  --cpus=6 \
  --memory=12g \
  --disk-size=40g \
  --addons=ingress,metrics-server
```

資源預估（同時跑平台 + 目標系統）：

| 元件 | RAM | CPU |
|---|---|---|
| argo-workflows (controller + server) | 256 MB | 0.2 |
| litmus-chaos (server + frontend + mongo) | 1.5 GB | 0.5 |
| k6-operator | 128 MB | 0.1 |
| minio (single) | 512 MB | 0.2 |
| victoriametrics-single | 512 MB | 0.2 |
| grafana | 256 MB | 0.1 |
| MySQL（測試目標） | 512 MB | 0.5 |
| Kafka（測試目標，KRaft mode 單節點） | 1 GB | 0.5 |
| Doris FE + BE（測試目標） | 4 GB | 1.5 |
| **總計（含 Doris）** | **~9 GB** | **~4** |
| **不含 Doris** | **~5 GB** | **~2.5** |

**建議 Phase 1 順序**：先 MySQL + Kafka scenario 跑通，Doris 視 minikube 記憶體狀況再加（或改用 podman 主機跑 Doris、k6 從 minikube 連出去）。

### Ingress 與 UI 存取

minikube ingress addon 開啟後，Helm chart 預設建立三個 Ingress：

| Service | Host (etc/hosts 配對 minikube IP) |
|---|---|
| Argo UI | `argo.dlh.local` |
| Grafana | `grafana.dlh.local` |
| MinIO Console | `minio.dlh.local` |

退路：若不想設 ingress，全部用 `kubectl port-forward` 也可。

### RBAC

Litmus service account 需要跨 namespace 注入 chaos 的權限：Helm chart 提供 `cluster-admin-lite` ClusterRole（精簡的 verbs，避免前次 read-only ClusterRole 不足的雷 — observation 294）。

主要 Helm values：
```yaml
chaos_hub:
  url: https://github.com/litmuschaos/chaos-charts.git
  ref: master
minio:
  buckets: [fixtures, artifacts]
  artifact_credentials_secret: minio-artifact-creds
victoriametrics:
  retention: 30d
  remote_write_enabled: true
verdict:
  image: ghcr.io/<org>/dlh-verdict:<version>
```

## 已知風險與 spike 順序

1. **k6 → Prometheus remote write**（承接 Plan 3 observation S105 / 332-333）：第一個要驗的事。k6 Operator argument 設 `--out experimental-prometheus-rw=...`、VM single 開 `-remoteWrite.url` flag。Day 1 spike，否則 verdict 沒資料源。
2. **Argo parallel chaos+load 失敗處理**：chaos step 失敗時 load 是否繼續？預設 `continueOn: failed`（讓 verdict 仍能拿到 partial picture），整體 verdict 由 verdict job 算。
3. **ChaosResult CRD 非同步建立**：verdict job 必須 bounded retry 等 `Awaited` → 終態。
4. **ChaosHub manifest 版本相容性**：Helm values 釘住特定 ref，避免 upstream 改 schema 突然破。

## 工程估算

| 階段 | 工作 | 估算 |
|---|---|---|
| Phase 1 MVP | WorkflowTemplate 函式庫、verdict binary、HTML report、Helm chart、Grafana dashboard、k6→Prom 修復、範例 scenarios | **~5 週** |
| Phase 2 | Authoring UI（form generator → Workflow YAML）、live dashboard | +4-6 週 |
| Phase 3 | 自訂 TestScenario CRD + controller、auth/RBAC、跨 cluster | +4-6 週 |

## 演化路徑

Phase 1 跑 1-2 個月真實 scenarios 後再評估：
- UI 是否真有需求？工程師可能滿足於 YAML；QA / PM 可能才需要 form-based UI
- 找出最常重複的 3-5 個 scenario → 是否值得包成命名 blueprint
- 是否值得引入 TestScenario CRD（controller 維護成本 vs YAML 可讀性）
- VictoriaMetrics single 是否需要升級成 cluster

## 實作前已確認 / 待確認的問題

1. ~~平台部署到哪個 cluster？~~ **已確認：minikube（本地開發/測試）**
2. ~~ChaosHub 來源？~~ **已確認：直接拉公開 Litmus hub** (`github.com/litmuschaos/chaos-charts`)，後續若有需要再轉內部 mirror
3. Helm chart 與 verdict binary 由誰維護 — 留待後續決定（不影響實作）
4. ~~Argo UI / Grafana SSO？~~ **已確認：不接 SSO**，使用 Argo / Grafana / MinIO 內建 admin 帳號（minikube 本地測試 sufficient）

---

**Next step**：使用者 review 此 spec、確認上述四個 open question 後，進入 `writing-plans` 階段，產出可執行的 implementation plan。

---

## Post-Phase-1 amendments

This spec was authored before Phase 1 implementation. The following design points were revised based on real-world findings during execution. Plans 1-5 reflect the original intent; the live code (tag `phase-1-mvp` and beyond) reflects these amendments.

### A1. `dlh_scenario` label replaces `scenario`
k6's `experimental-prometheus-rw` output reserves the `scenario` label for its own internal scenario name (always `default`). The platform's user-facing partitioning label is therefore **`dlh_scenario`**. Applies everywhere PromQL filters by run identity (SLO YAML, dashboards, verdict metric labels).

### A2. k6 only emits pre-computed quantile gauges, not histogram buckets
`k6_http_req_duration_seconds_bucket` doesn't exist. SLO queries and dashboards must use `k6_http_req_duration_p95` (and friends, per `K6_PROMETHEUS_RW_TREND_STATS`). Unit is seconds.

### A3. Verdict output: artifact, not ConfigMap
Original design wrote a `dlh-result-<workflow>` ConfigMap containing a slimmed report for a Grafana Infinity datasource to read. The Infinity datasource never got the k8s API auth plumbing needed to read ConfigMaps over HTTPS, and bridging it would have duplicated state across two stores.

Replacement: verdict-job emits `report.json` + `report.html` as an Argo workflow artifact (auto-archived to MinIO `artifacts/<workflow>/verdict/report/`), and also POSTs a 4-series summary to VictoriaMetrics. Dashboards read from VM only — same datasource as k6 metrics. ConfigMap and the `configmaps` verb in `rbac-verdict` are removed.

### A4. Workflow names are timestamps, not random suffixes
`scripts/run-scenario.sh` rewrites the scenario's `metadata.generateName: <prefix>-` to `metadata.name: <prefix>-YYYYMMDD-HHMMSS` (UTC) before submission. Results: sortable by name, recognisable in `kubectl get workflow` and the Grafana dropdown.

### A5. Bitnami secure-images migration broke Litmus's MongoDB dependency
Litmus 3.28.0 ships a Bitnami MongoDB sub-subchart whose `bitnamilegacy/*` arm64 image was yanked mid-2025; the `bitnamisecure/mongodb:latest` replacement starts under `docker run` but exits silently inside the chart's StatefulSet pod spec (script/permissions contract drift not worth untangling).

Replacement: in-tree `mongo:6` StatefulSet at `helm/dlh-test-fw/templates/mongodb.yaml` — single-node replicaset, postStart hook idempotently initiates `rs0` and creates the auth user the Litmus chart's wait-init expects. No-auth on the wire; rotate to `--auth --keyFile` before any shared deploy. Same applies to MinIO (Bitnami subchart removed, in-tree Deployment at `templates/minio.yaml`).

### A6. Litmus chart 3.x ships only the portal
ChaosOperator + per-namespace `ChaosExperiment` CRs are NOT installed by the Helm chart. Backfilled in `templates/litmus-chaos-{operator,experiments}.yaml`. Chaos `successCondition` was also rewritten from Litmus 1.x's `status.experimentStatus.verdict in (Pass,...)` to 3.x's `status.engineStatus == completed`.

### A7. Smaller deltas worth noting
- `k6-operator` chart bumped 3.x → 4.4.1; `namespace.watch` value removed (4.x watches all namespaces).
- `victoria-metrics-single` chart bumped 0.12.x → 0.38.0 (0.12 yanked from repo).
- Grafana datasources need explicit `uid:` pinning so shipped dashboards' `datasource.uid` references resolve.
- VM's default lookback-delta is 5min; dashboards wrap point-in-time verdict gauges in `last_over_time(...[7d])` so panels survive past staleness.
- MinIO pinned to `RELEASE.2024-12-13T22-19-12Z` — the last community release with the admin console; newer releases dropped it.

