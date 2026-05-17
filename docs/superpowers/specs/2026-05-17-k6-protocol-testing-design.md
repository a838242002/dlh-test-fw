# k6 Protocol-Level Testing — Phase 2 Design

**Date**: 2026-05-17
**Status**: Draft, awaiting review
**Project**: dlh-test-fw
**Builds on**: Phase 1 MVP (tag `phase-1-mvp`) — Argo + Litmus + k6-operator + MinIO + VM + Grafana platform, with HTTP stand-in scenarios

## Why

Phase 1 proved the platform end-to-end with k6 hitting `httpbin` as a load stand-in. The chaos+verdict pipeline works, but the load step doesn't exercise the actual target system (MySQL, Kafka, Doris). Phase 2 makes k6 talk the real protocols so SLOs measure system behaviour under chaos, not HTTP echo behaviour.

## Goals (in scope)

1. Ship a custom k6 binary (`dlh-k6`) with community xk6 plugins bundled in:
   `xk6-sql` + `xk6-sql-driver-mysql` (covers MySQL queries AND Doris queries — Doris is MySQL-protocol compatible) and `xk6-kafka` (Kafka produce/consume). Doris ingestion uses k6's built-in `k6/http` against the Stream Load REST API.
2. Bake a reusable JS script tree into that image at `/scripts/`:
   - `/scripts/lib/{common,mysql,kafka,doris}.js` — primitives
   - `/scripts/runners/{mysql,kafka,doris}.js` — generic per-target runners driven by env vars (one runner per target, N scenarios share it)
3. Migrate the three existing scenarios (`mysql-pod-delete`, `kafka-broker-partition`, `doris-be-network-loss`) to hit real targets via the new runners.
4. Ship three Grafana dashboards (`dlh-mysql`, `dlh-kafka`, `dlh-doris`) focused on each protocol's load-side metrics.

## Goals (out of scope, deferred to later phases)

- Target-side observability via `mysql_exporter` / `kafka_exporter` / Doris `/metrics` scraping. Dashboards stay load-side (k6 + xk6) only.
- Authoring new xk6 plugins (e.g. custom `xk6-doris`). Pure community plugins.
- Workload patterns beyond `steady` (constant VUs). `spike` / `ramp` / `warmup-then-load` get a reserved env hook (`WORKLOAD`) but no implementation in Phase 2.
- Production-grade image hygiene (CVE scans, SBOM, signed manifests). Local-dev grade.
- Multi-instance / sharded target deploys. All targets stay single-replica in `targets/<type>/`.
- SLO content changes. Plan 5's SLOs in the scenario YAMLs stay unchanged in shape — the underlying PromQL series names move (k6 built-in → `dlh_<type>_*` custom Trends) and the SLO `query:` lines update accordingly.

## Architecture

```
                       load/k6-run WorkflowTemplate
                                  │
                                  ▼
            ┌─────────────────────────────────────────┐
            │  k6 TestRun, runner.image = dlh-k6:0.1.0│
            │  --script /scripts/runners/<type>.js    │
            │  env_map: MYSQL_DSN=...                 │
            │           KAFKA_BOOTSTRAP=...           │
            │           DORIS_FE_HOST=...             │
            └─────────────────────────────────────────┘
                                  │
            ┌─────────────────────┼──────────────────────┐
            ▼                     ▼                      ▼
       MySQL :3306         Kafka :9092          Doris FE :8030
       (xk6-sql)           (xk6-kafka)         (xk6-sql query
                                                + k6/http Stream Load)
                                  │
                                  ▼
                        VictoriaMetrics
                                  │
            ┌─────────────────────┼──────────────────────┐
            ▼                     ▼                      ▼
       dlh-mysql              dlh-kafka              dlh-doris
       dashboard              dashboard              dashboard
            │                     │                      │
            └─────────────────────┴──────────────────────┘
                                  │
                                  ▼
                       cross-link → dlh-run-detail (generic),
                                    dlh-history (cross-scenario)
```

## Repository changes

```
dlh-test-fw/
├── fixture-images/
│   └── k6/                                        ← NEW
│       ├── Dockerfile                              # multi-stage: xk6 build + alpine runtime
│       ├── Makefile
│       ├── lib/
│       │   ├── common.js
│       │   ├── mysql.js
│       │   ├── kafka.js
│       │   ├── doris.js
│       │   └── smoke.js                            # forces import of every xk6 module
│       └── runners/
│           ├── mysql.js                            # ONE script for all MySQL scenarios
│           ├── kafka.js                            # ONE script for all Kafka scenarios
│           └── doris.js                            # ONE script for all Doris scenarios
├── helm/dlh-test-fw/files/workflowtemplates/load/
│   └── k6-run.yaml                                 ← CHANGED: dlh-k6 image, script_path param, env_map wired
├── scenarios/
│   ├── mysql-pod-delete.yaml                       ← CHANGED: real MySQL target, env_map for runner
│   ├── kafka-broker-partition.yaml                 ← CHANGED: real Kafka target, env_map for runner
│   └── doris-be-network-loss.yaml                  ← CHANGED: real Doris target, env_map for runner
│   *-k6-script.yaml                                ← DELETED (scripts now in image)
├── dashboards/grafana/
│   ├── dlh-mysql.json                              ← NEW
│   ├── dlh-kafka.json                              ← NEW
│   ├── dlh-doris.json                              ← NEW
│   ├── dlh-run-detail.json                         ← CHANGED: add cross-links to type dashboards
│   ├── dlh-history.json                            ← unchanged
└── helm/dlh-test-fw/files/dashboards/              ← embed copies (synced by `make sync-dashboards`)
```

## dlh-k6 image

Multi-stage Dockerfile, all plugin versions pinned as `ARG`s for one-line bumping:

```dockerfile
ARG GO_VERSION=1.23
ARG K6_VERSION=v0.55.0
ARG XK6_SQL_VERSION=v1.0.4
ARG XK6_SQL_DRIVER_MYSQL_VERSION=v1.0.1
ARG XK6_KAFKA_VERSION=v0.27.0

FROM golang:${GO_VERSION}-alpine AS build
RUN apk add --no-cache git
RUN go install go.k6.io/xk6/cmd/xk6@latest
RUN xk6 build ${K6_VERSION} \
      --with github.com/grafana/xk6-sql@${XK6_SQL_VERSION} \
      --with github.com/grafana/xk6-sql-driver-mysql@${XK6_SQL_DRIVER_MYSQL_VERSION} \
      --with github.com/mostafa/xk6-kafka@${XK6_KAFKA_VERSION} \
      --output /out/k6

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /out/k6 /usr/bin/k6
COPY lib /scripts/lib
COPY runners /scripts/runners
ENTRYPOINT ["/usr/bin/k6"]
```

**Image identity:** `ghcr.io/dlh/dlh-k6:0.1.0` (same registry prefix as `dlh-verdict`). Built locally with `docker build`; loaded into minikube with `minikube image load`. Top-level `make k6-image` target.

**Build / reload gotcha** (lessons from `dlh-verdict`): minikube caches by image ID; bumping `0.1.0` without `docker rmi` + `minikube ssh -- docker rmi -f` leaves the old binary in pods that pull `imagePullPolicy: Never`. The Makefile's reload target chains these steps.

**Runtime base — alpine, not distroless:** k6 occasionally spawns sub-processes and benefits from a real `/bin/sh` for scenario debugging. Alpine adds ~7MB on top of k6's ~80MB binary.

**Entrypoint:** `ENTRYPOINT ["/usr/bin/k6"]` — k6-operator's TestRun appends `run --quiet ...` as args, so the entrypoint must be exactly the binary.

**Smoke target:** `make k6-smoke` runs:
1. `docker run --rm dlh-k6:0.1.0 version` — confirms binary works
2. `docker run --rm dlh-k6:0.1.0 run /scripts/lib/smoke.js` — imports `k6/x/sql`, `k6/x/sql/driver/mysql`, `k6/x/kafka` to fail-fast on missing extension links

## Script library

### `lib/common.js`

Shared helpers every runner imports:

```js
import { Counter } from 'k6/metrics';

export function buildOptions({ scenarioLabel, vus, duration }) {
  return {
    vus: parseInt(__ENV.VUS || String(vus)),
    duration: __ENV.DURATION || duration,
    tags: { dlh_scenario: scenarioLabel },   // dlh_workflow comes from --tag in load/k6-run WT
  };
}

export const errCounter = new Counter('dlh_app_errors_total');

// "read:70,write:30" -> { pick: () => 'read'|'write' }
export function parseOpMix(spec) { /* ... */ }
```

### `lib/mysql.js`

Thin wrapper over xk6-sql. Records query latency as a custom `Trend` series tagged `op`:

```js
import sql from 'k6/x/sql';
import driver from 'k6/x/sql/driver/mysql';
import { Trend } from 'k6/metrics';

const queryDuration = new Trend('dlh_mysql_query_duration_seconds', true);

export function openConn(dsn) { return sql.open(driver, dsn); }
export function exec(db, query, ...args) { /* time + add Trend */ }
export function query(db, q, ...args) { /* time + add Trend */ }
```

`lib/kafka.js` and `lib/doris.js` follow the same shape. Custom metric names use `dlh_` prefix to stay distinguishable from k6's built-in `k6_*` series.

### Custom metric inventory

Emitted by `lib/*.js`. Combined with k6's built-in `k6_*` in dashboards.

| Library | Metric (type) | Per-sample labels |
|---|---|---|
| `mysql.js` | `dlh_mysql_query_duration_seconds` (Trend) | `op` (exec|query) |
| `kafka.js` | `dlh_kafka_produce_duration_seconds` (Trend) | `topic` |
| `kafka.js` | `dlh_kafka_messages_produced_total` (Counter) | `topic` |
| `doris.js` | `dlh_doris_streamload_duration_seconds` (Trend) | `db`, `table` |
| `doris.js` | `dlh_doris_streamload_rows_total` (Counter) | `db`, `table` |
| `doris.js` | `dlh_doris_query_duration_seconds` (Trend) | (none) |
| `common.js` | `dlh_app_errors_total` (Counter) | `kind` (e.g. `mysql-conn`, `kafka-write`) |

All series additionally carry `dlh_scenario` (set in `buildOptions().tags`) and `dlh_workflow` (set by `load/k6-run` WT `--tag dlh_workflow={{workflow.name}}`).

### Generic runners

#### `runners/mysql.js`

| Env | Default | Purpose |
|---|---|---|
| `MYSQL_DSN` | (required) | `user:pass@tcp(host:3306)/db` |
| `MYSQL_OP_MIX` | `read:100` | comma-separated weights, e.g. `read:70,write:30` |
| `MYSQL_READ_SQL` | `SELECT NOW()` | SQL for read ops |
| `MYSQL_WRITE_SQL` | `INSERT INTO dlh_load(ts) VALUES(NOW())` | SQL for write ops |
| `MYSQL_SLEEP_MS` | `0` | per-iteration sleep |
| `WORKLOAD` | `steady` | `steady` only in Phase 2; `spike` / `ramp` reserved |
| `SCENARIO_LABEL` | `mysql` | overridden by WT to the scenario name |
| `VUS` | `10` | overrides scenario YAML `vus` param |
| `DURATION` | `180s` | overrides scenario YAML `duration` param |

Scenario YAML supplies these via `env_map` parameter on `load/k6-run` (see below).

#### `runners/kafka.js`

| Env | Default | Purpose |
|---|---|---|
| `KAFKA_BOOTSTRAP` | (required) | `broker1:9092,broker2:9092` |
| `KAFKA_TOPIC` | (required) | target topic |
| `KAFKA_OP` | `produce` | `produce` / `consume` / `both` |
| `KAFKA_MESSAGE_SIZE` | `256` | bytes per message (random-filled) |
| `KAFKA_CONSUME_GROUP` | `dlh-test-fw` | when consuming |

#### `runners/doris.js`

| Env | Default | Purpose |
|---|---|---|
| `DORIS_FE_HOST` / `DORIS_FE_PORT` | — / `8030` | Stream Load endpoint |
| `DORIS_DB` / `DORIS_TABLE` | (required) | |
| `DORIS_USER` / `DORIS_PASS` | `root` / `` | Stream Load basic auth |
| `DORIS_OP` | `stream_load` | `stream_load` / `query` / `both` |
| `DORIS_BATCH_ROWS` | `1000` | rows per Stream Load batch |
| `DORIS_QUERY_SQL` | `SELECT COUNT(*) FROM <table>` | when OP includes `query` |

### `load/k6-run` WorkflowTemplate change

Parameter contract changes from:
```yaml
- name: script_configmap   # name of ConfigMap with script.js
```
to:
```yaml
- name: script_path        # e.g. /scripts/runners/mysql.js
```

The TestRun spec switches accordingly:
```yaml
script:
  localFile: {{`{{inputs.parameters.script_path}}`}}
```

The `env_map` parameter (reserved-but-unused in Phase 1) gets wired. k6-operator 4.4.1's TestRun spec accepts `runner.env: [...]`, so `env_map` is parsed line-by-line (KEY=VALUE) by a Helm/Argo expression that produces a list of `EnvVar` objects, or — if that's too gnarly — by routing through a per-run ConfigMap mounted as `envFrom`. The plan's first task picks the simpler of the two after testing what k6-operator actually accepts.

`runner.image` pin moves from `grafana/k6:0.50.0` to `ghcr.io/dlh/dlh-k6:0.1.0`.

### Example scenario YAML (post-migration)

```yaml
- name: load
  templateRef: { name: load-k6-run, template: main }
  arguments:
    parameters:
    - { name: script_path,    value: "/scripts/runners/mysql.js" }
    - { name: scenario_label, value: "mysql-pod-delete" }
    - { name: vus,            value: "10" }
    - { name: duration,       value: "180s" }
    - { name: env_map, value: |
        MYSQL_DSN=root:mysql@tcp(mysql.mysql-sys.svc.cluster.local:3306)/test
        MYSQL_OP_MIX=read:70,write:30
        MYSQL_SLEEP_MS=50
      }
```

Adding a new scenario = new YAML. No new JS file. No image rebuild.

## Per-type Grafana dashboards

All three follow the same skeleton:

- **Variables** (PromQL `label_values` query, multi=false, refresh on time range change):
  - `$scenario` — `label_values(<type-marker-metric>, dlh_scenario)`
  - `$workflow` — `label_values(<type-marker-metric>{dlh_scenario="$scenario"}, dlh_workflow)`
  - Marker metric per type: MySQL `dlh_mysql_query_duration_seconds_count`, Kafka `dlh_kafka_messages_produced_total`, Doris `dlh_doris_streamload_rows_total`
- **Time range** default `now-7d to now`. Verdict instant gauges wrapped in `last_over_time(...[7d])` (same staleness fix as `dlh-run-detail`).
- **Datasource UID** `VictoriaMetrics` (pinned in chart, same as existing dashboards).

### `dlh-mysql` panels

| Row | Type | Title | Query |
|---|---|---|---|
| 1 | timeseries | Query rate by op | `sum by (op) (rate(dlh_mysql_query_duration_seconds_count{dlh_scenario="$scenario",dlh_workflow="$workflow"}[30s]))` |
| 1 | timeseries | Query p95 latency | `dlh_mysql_query_duration_seconds_p95{dlh_scenario="$scenario",dlh_workflow="$workflow"}` |
| 1 | timeseries | Errors by kind | `sum by (kind) (rate(dlh_app_errors_total{kind=~"mysql.*",dlh_workflow="$workflow"}[30s]))` |
| 2 | stat | Active VUs | `last_over_time(k6_vus{dlh_workflow="$workflow"}[7d])` |
| 2 | stat | Total ops | `sum(increase(dlh_mysql_query_duration_seconds_count{dlh_workflow="$workflow"}[$__range]))` |
| 2 | stat | Verdict — overall | `last_over_time(dlh_verdict_overall{dlh_workflow="$workflow"}[7d])` (PASS/FAIL color map) |
| 3 | table | Verdict — SLO thresholds | joinByField `name` of `last_over_time(dlh_verdict_threshold_pass[7d])` + `_value` |

### `dlh-kafka` panels

| Row | Type | Title | Query |
|---|---|---|---|
| 1 | timeseries | Produce rate (msgs/sec) | `sum by (topic) (rate(dlh_kafka_messages_produced_total{dlh_scenario="$scenario",dlh_workflow="$workflow"}[30s]))` |
| 1 | timeseries | Produce p95 latency | `dlh_kafka_produce_duration_seconds_p95{dlh_scenario="$scenario",dlh_workflow="$workflow"}` |
| 1 | timeseries | Errors by kind | `sum by (kind) (rate(dlh_app_errors_total{kind=~"kafka.*",dlh_workflow="$workflow"}[30s]))` |
| 2 | stat × 3 | Active VUs / Total msgs / Verdict — overall | (analogous to mysql) |
| 3 | table | Verdict — SLO thresholds | (analogous) |

### `dlh-doris` panels

| Row | Type | Title | Query |
|---|---|---|---|
| 1 | timeseries | Stream Load rate (rows/sec) | `sum by (table) (rate(dlh_doris_streamload_rows_total{dlh_scenario="$scenario",dlh_workflow="$workflow"}[30s]))` |
| 1 | timeseries | Stream Load p95 latency | `dlh_doris_streamload_duration_seconds_p95{dlh_workflow="$workflow"}` |
| 1 | timeseries | Stream Load error rate | `sum(rate(dlh_app_errors_total{kind="doris-streamload",dlh_workflow="$workflow"}[30s])) / clamp_min(sum(rate(dlh_doris_streamload_rows_total{dlh_workflow="$workflow"}[30s])), 1e-9)` |
| 1 | timeseries (conditional) | Query p95 latency | `dlh_doris_query_duration_seconds_p95{dlh_workflow="$workflow"}` (only meaningful when `DORIS_OP` includes `query`) |
| 2 | stat × 3 | Active VUs / Total rows / Verdict — overall | (analogous) |
| 3 | table | Verdict — SLO thresholds | (analogous) |

### Cross-linking

Every per-type dashboard adds a top-of-panel link to `dlh-run-detail` (generic k6 view) at
`/d/dlh-run/?var-scenario=${scenario}&var-workflow=${workflow}`. `dlh-run-detail` gets three links back (`/d/dlh-mysql`, `/d/dlh-kafka`, `/d/dlh-doris`) so the navigation is symmetric.

### Open behavioural question — verified in Plan 7 Task 1

k6 documents that `K6_PROMETHEUS_RW_TREND_STATS` (set in `load/k6-run` WT to `p(95),p(99),min,max,avg`) applies to ALL `Trend` metrics, not just k6 built-ins. The dashboard PromQL above assumes that — `dlh_mysql_query_duration_seconds_p95` etc. should exist. Plan 7's first task runs a single smoke scenario and `curl`s VM's `/api/v1/label/__name__/values` to confirm. If absent, dashboards fall back to `<metric>_count` + `<metric>_sum` rate-divided averages (precision drops, but functional).

## Migration

1. **Plan 6 lands** dlh-k6 image only. `load/k6-run` WT is NOT yet switched. Existing scenarios continue to run on `grafana/k6:0.50.0` against `httpbin`. Smoke gate before Plan 7 starts: `make k6-smoke` PASS.
2. **Plan 7 lands** in one step (no halfway state):
   - Targets (`targets/mysql/`, `targets/kafka/`, `targets/doris/`) confirmed healthy or freshly deployed
   - `load/k6-run` WT pinned to `dlh-k6:0.1.0` + `script_path` + `env_map` wired
   - Three scenario YAMLs rewritten to use real targets and runners
   - `scenarios/*-k6-script.yaml` (3 files) deleted
   - `make run-mysql` + `make run-kafka` + `make run-doris` all Succeed end-to-end
3. **Plan 8 lands** the three dashboards. Smoke gate: open each dashboard in a browser after a fresh run; every panel has data.

There is **no backwards-compatibility shim** for the old `script_configmap` parameter. Phase 1 MVP's HTTP stand-in scenarios are replaced, not preserved.

### Doris caveat

Phase 1 left `targets/doris/` deferred (arm64 + memory). Plan 7's task 0 is a time-boxed (1 day) spike to bring a viable single-pod Doris up on minikube — likely `selectdb/doris.fe-ubuntu` + `selectdb/doris.be-ubuntu` quickstart images, possibly merged into one pod for memory savings. **If the spike fails, Plan 7 scope shrinks to MySQL + Kafka only**; the Doris runner, scenario, and dashboard ship in a follow-up phase. The milestone doesn't block on Doris.

## Testing

| Plan | Approach |
|---|---|
| 6 (dlh-k6 image) | `make k6-smoke` — `docker run dlh-k6 version` exits 0; `docker run dlh-k6 run /scripts/lib/smoke.js` exits 0 (validates xk6 module linkage). No JS unit tests — exercised by Plan 7's scenarios. |
| 7 (scripts + WT) | Per-runner smoke scenario at `scenarios/<type>-smoke.yaml`, 30s duration, posts custom `dlh_<type>_*` series to VM. `scripts/run-scenario.sh` exits 0 for each. Then the three real chaos scenarios (`mysql-pod-delete` etc.) run to completion. |
| 8 (dashboards) | `make verify-dashboards` — for each dashboard, hit Grafana `/api/dashboards/uid/<uid>` (renders the JSON), then for each panel's PromQL hit `/api/datasources/proxy/uid/VictoriaMetrics/api/v1/query` and assert non-error response (syntax check). Human visual check at end: open `dlh-mysql` etc. in browser, confirm panels populated. |

## Success criteria

1. `make k6-image && make k6-smoke` succeeds on a clean checkout.
2. `make run-mysql` runs end-to-end against MySQL :3306 in `mysql-sys` namespace; `dlh_mysql_query_duration_seconds_count` and `dlh_mysql_query_duration_seconds_p95` present in VM; `dlh-mysql` dashboard shows query rate / p95 / errors / verdict panels with data.
3. Same for `make run-kafka` against `kafka-sys`.
4. Doris go/no-go decision documented in FINDINGS append; if go, same as 2/3 against `doris-sys`.
5. `dlh-run-detail` and `dlh-history` dashboards still work (no regression from Phase 1 MVP).
6. `helm lint helm/dlh-test-fw` clean; `helm upgrade --install` reaches `STATUS: deployed`; all chart pods Ready (no new pods added, just `load/k6-run` references a different image).

## Risks

- **k6 custom Trend metric prom-rw export shape** — Plan 7 Task 1 verifies. Mitigation: fall back to `_count`/`_sum` rate-divided averages in dashboards.
- **k6-operator 4.4.1 env_map ergonomics** — may need a wrapper ConfigMap. Mitigation: Plan 7 task 0 picks the simpler path after trying both.
- **xk6 plugin version compatibility** — `xk6-sql v1.0.4` is a recent major rewrite; if its API has drifted from any examples, expect 1-2 commits of friction. Mitigation: Plan 6 task 1 pins versions explicitly and the smoke import test catches breakage before any scenario depends on it.
- **Doris on arm64 / 12GB minikube** — see above. Mitigation: time-boxed spike with explicit go/no-go.
- **xk6 build time** — local build takes ~2-3min on M-series. Mitigation: layer cache covers iterations; only goes slow on plugin-version bumps.
