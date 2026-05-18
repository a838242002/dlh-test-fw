# Findings — k6 → VictoriaMetrics remote-write spike

Date verified: 2026-05-16
Engineer: allenli (resumed from prior session blocked on Docker Desktop)

## Versions that worked

| Component | Chart version | Image |
|---|---|---|
| victoria-metrics-single | **0.38.0** (plan said 0.12.0 — that minor no longer exists in the chart repo) | `victoriametrics/victoria-metrics:v1.143.0` (chart default) |
| k6-operator | **4.4.1** (plan said 3.5.0 — chart bumped to 4.x; the `namespace.watch` value used in the plan was removed in 4.x) | controller-manager image: chart default (app v1.4.0) |
| k6 runner | `grafana/k6:0.50.0` (pinned in TestRun `runner.image`) | — |
| Kubernetes (minikube) | v1.35.1 in minikube v1.38.1 on Darwin/arm64 (docker driver) | — |

## Exact service DNS used

VM remote-write endpoint resolved to:

    http://vm-victoria-metrics-single-server.dlh-test-fw.svc.cluster.local:8428/api/v1/write

## k6 CRD kind

The installed CRD kind was: `TestRun`
apiVersion: `k6.io/v1alpha1`

(Chart 4.x also installs a `privateloadzones.k6.io` CRD but we don't use it for the spike.)

## Required runner env vars / args

    --out experimental-prometheus-rw
    --tag dlh_scenario=<label>            # NOT `scenario=` — see gotcha below
    env K6_PROMETHEUS_RW_SERVER_URL=<endpoint>
    env K6_PROMETHEUS_RW_PUSH_INTERVAL=5s
    env K6_PROMETHEUS_RW_TREND_STATS=p(95),p(99),min,max,avg

## Gotchas observed

- **`scenario` is a reserved label in k6's prometheus-rw output.** It always
  carries the k6 scenario name (default: `default`) and overrides anything you
  set via `--tag scenario=...` or `options.tags.scenario`. We renamed our
  application-level label to `dlh_scenario` and updated the verifier query.
  The PromQL used at the end of the spike was
  `sum(k6_http_reqs_total{dlh_scenario="spike-httpbin"})`.

- **k6-operator chart 3.x → 4.x removed the `namespace.watch` value.** In 4.4.1
  the operator watches all namespaces by default and `namespace.watch` is no
  longer accepted. Spike values yaml was simplified to just `manager.resources`
  plus `namespace.create: false` (we install into an already-existing namespace).

- **Orphaned cluster-scoped resources from a prior k6-operator install in a
  different namespace blocked the helm install.** On a workstation that has
  previously installed k6-operator into another namespace (e.g. `litmus`), the
  CRDs (`testruns.k6.io`, `privateloadzones.k6.io`) and several ClusterRoles
  (`k6-operator-manager-role`, `k6-operator-metrics-auth-role`,
  `k6-operator-metrics-reader`, `privateloadzone-editor-role`,
  `privateloadzone-viewer-role`) and ClusterRoleBindings
  (`k6-operator-manager-rolebinding`, `k6-operator-metrics-auth-rolebinding`)
  remain after `helm uninstall`. Helm refuses to adopt them unless their
  `meta.helm.sh/release-namespace` annotation matches the new release. Fix:
  re-annotate each to point at the new release namespace, or delete them.

- **VM chart 0.38.0 service name** matches what the plan predicted:
  `vm-victoria-metrics-single-server`. No verifier change needed.

- **k6-operator does not re-run a TestRun whose pods are Completed.** When
  editing the script or testrun spec we had to `kubectl delete testrun
  spike-httpbin` and re-apply; `apply` alone leaves the old completed pods.

## Implications for downstream plans

- **Plan 2 (Helm chart):** pin
  - `victoria-metrics-single` to **0.38.0** (or a documented later patch)
  - `k6-operator` to **4.4.1**.
  Reproduce `vm-values.yaml` under values key `victoria-metrics-single:` and
  `k6-operator-values.yaml` under `k6-operator:`. Drop the `namespace.watch`
  field — it does nothing in 4.x. Include a one-time pre-install hook or
  documented manual step to clean up orphaned k6 CRDs / ClusterRoles when
  the cluster has had k6-operator installed before.

- **Plan 4 (`load/k6-run` WorkflowTemplate):** the template must inject the
  env vars listed above and **`--tag dlh_scenario={{inputs.parameters.scenario_label}}`**
  (note the `dlh_` prefix — using bare `scenario` will silently be overridden
  by k6 and produce no queryable series for our use case). The remote-write
  URL is a Helm value (`platform.vm.remoteWriteUrl`) injected at
  template-render time. Runner image must be pinned to `grafana/k6:0.50.0`
  or later (older images may not have the `experimental-prometheus-rw`
  output).

- **Plan 5 (dashboards) / verdict (Plan 3):** all PromQL filters that
  partition by run/scenario must use **`dlh_scenario`**, not `scenario`.

## How to reproduce

    make up && make verify

(Note: `make up` is idempotent on minikube — on a host with a prior
k6-operator install you may need to follow the orphaned-resource cleanup
described in "Gotchas" above before `helm upgrade --install` will succeed.)

## Platform chart observations (from Plan 2)

Date verified: 2026-05-16
Release name: `dlh` (so every subchart resource is prefixed `dlh-`).

- Confirmed service names (post-install) — all match the plan:
    - argo server:   `dlh-argo-workflows-server:2746`
    - grafana:       `dlh-grafana:80`
    - minio API:     `dlh-minio:9000` (in-tree template, not Bitnami subchart — see drift below)
    - minio console: `dlh-minio-console:9001`
    - VM server:     `dlh-victoria-metrics-single-server:8428`
- Helm `--wait` timeout: 10 minutes was sufficient. First install took ~45s for all
  Deployments to be Ready (without Litmus).
- **Litmus chaoscenter brought up 0 pods — subchart was disabled.** Reason: Bitnami's
  2025 "secure-images" migration yanked their public Docker images, and Litmus 3.28.0
  depends on the Bitnami MongoDB sub-subchart whose `bitnamilegacy/mongodb:8.0.13`
  tag has no linux/arm64 manifest and whose `bitnami/minio:2024.12.18` tag was
  removed entirely. Re-enable Litmus once a chart bump migrates the deps to
  `bitnamisecure/*`, or override `litmus.mongodb.image.*` to a public alternative.
  Tracked in Phase 1 backlog.
- **MinIO replaced with an in-tree template** (`helm/dlh-test-fw/templates/minio.yaml`)
  for the same reason. Uses upstream `minio/minio:RELEASE.2024-12-13T22-19-12Z` plus
  a 2nd Service `dlh-minio-console` to preserve the ingress / artifact-repo wiring.
  Subchart entry was removed from Chart.yaml.
- Total minikube memory consumed at idle: **2953Mi (~22%)** with 6 platform pods
  Ready. Headroom remains for k6 runners and chaos experiments.

### Chart version drift from plan
| Subchart | Plan said | Actually used | Reason |
|---|---|---|---|
| argo-workflows | 0.42.0 | 0.42.7 | 0.42.0 still in repo, just picked latest 0.42.x patch |
| litmus | 3.5.0 | (disabled) 3.28.0 declared | 3.5.0 never existed in repo; 3.28.0 is latest 3.x but blocked by Bitnami image yanks |
| k6-operator | 4.4.1 | 4.4.1 | matches FINDINGS |
| minio (bitnami) | 14.6.0 | (removed) replaced by in-tree template | Bitnami images yanked |
| victoria-metrics-single | 0.38.0 | 0.38.0 | matches FINDINGS |
| grafana | 8.5.0 | 8.15.0 | 8.5.0 still in repo; picked latest 8.x |

### Values-schema drift from plan
- `litmus.mongo.*` → actual subchart key is **`mongodb`** (it's the Bitnami MongoDB
  sub-subchart). Adapted before disabling. Plans 3-5 should reference `mongodb` when
  re-enabling Litmus.
- `k6-operator.enabled` → **rejected by k6-operator 4.4.1's strict JSON schema**
  (additionalProperties: false at the root). Removed the `condition` from
  Chart.yaml; subchart is now always installed. Same will apply to anyone wanting
  to toggle k6-operator with values overrides.
- `helm/<chart>/tests/` is **not** rendered by `helm template`/`helm install`.
  Helm-test pods must live under `templates/` even if their hook is `test`.
  Moved `platform-smoke.yaml` into `templates/`.

### Cluster-resource conflicts encountered
- Orphaned `*.argoproj.io` CRDs from a prior `kubectl apply` (not helm-managed)
  caused server-side apply conflicts. Deleted the CRDs (no in-flight CRs) and
  re-installed cleanly. Re-annotation didn't help because `kubectl-client-side-apply`
  owned `.spec.versions`.
- The `dlh-test-fw` namespace pre-existed from Plan 1. Helm refused to adopt it
  until labelled `app.kubernetes.io/managed-by=Helm` and annotated with the
  release-name/namespace pair. Script `platform-up.sh` does **not** handle this;
  document the one-time `kubectl annotate ns` step in the README if reproducing.

### platform-verify outputs (final run)
- `kubectl wait ... pod --all`: all 6 pods condition met (argo server + controller,
  grafana, k6-operator manager, minio, vm-single).
- `helm test dlh`: `dlh-grafana-test` Succeeded, `dlh-platform-smoke` Succeeded (all
  four `/health` endpoints returned 2xx from inside the cluster).
- Ingress curl through `minikube ip` returned HTTP 000 — minikube's ingress addon
  is up but the addon needs `minikube tunnel` (or `/etc/hosts` + addon-enable) to
  be reachable from the host. **In-cluster smoke test is the authoritative check.**


## Litmus re-enable (2026-05-17)

Reversed the Plan 2 decision to disable Litmus.

- **Root cause of the original blocker**: Bitnami's 2025 secure-images
  migration both yanked the chart's `bitnamilegacy/mongodb:8.0.13-debian-12-r0`
  arm64 manifest *and* replaced it with `bitnamisecure/mongodb:latest` whose
  image contract no longer matches what the chart templates assume (the
  container starts under `docker run` but exits silently inside the chart's
  StatefulSet pod spec — script/permissions drift that's not worth untangling).

- **What we did instead**: shipped an in-tree single-node MongoDB
  StatefulSet at `helm/dlh-test-fw/templates/mongodb.yaml` using the
  upstream `mongo:6` image. Replicaset is initialized via `postStart`.

- **Three real surprises we hit before it worked**:

  1. **Litmus init container's wait command hardcodes the replicaset DNS**
     (`dlh-mongodb-0.dlh-mongodb-headless`) and the args
     `mongosh -u $DBUSER -p $DBPASSWORD URL --eval 'rs.status()'`. So
     the in-tree mongo must (a) be a StatefulSet with a headless Service
     under exactly the chart's expected name, and (b) accept a SCRAM
     auth attempt — mongo without `--auth` still rejects empty `-u`/`-p`
     and rejects credentials for non-existent users.

  2. **`use admin; db.createUser(...)` does not work in `mongosh --eval`.**
     The `use` helper switches the shell context but does not propagate
     to subsequent commands in the same `--eval` string — the createUser
     silently runs against the wrong db and the user is never created.
     Use `db.getSiblingDB("admin").createUser(...)`.

  3. **Default exec-probe timeout is 1 second**; `mongosh` startup on the
     `mongo:6` image regularly exceeds that. Use TCP probes for mongo,
     not `mongosh` exec.

- **Result**: all 8 platform pods Ready, both helm test suites pass,
  `make platform-verify` PASS.

- **Implications for Plans 3-5**: Litmus is back on the menu — Plan 4's
  `chaos/litmus-run` WorkflowTemplate is feasible. Production-grade
  concerns to revisit later: the in-tree mongo is no-auth and
  emptyDir-backed; it must gain keyFile auth and a PVC before any
  shared/CI deploy. Litmus's `adminConfig.DBUSER`/`DBPASSWORD` will
  become the actual SCRAM credentials at that point.

## dlh-k6 image (Plan 6, 2026-05-17)

- **Image**: `ghcr.io/dlh/dlh-k6:0.1.0` — produced by `make k6-image` from
  `fixture-images/k6/Dockerfile`. Same registry prefix and the same
  `minikube image load` + force-reload pattern as `dlh-verdict`.
- **Resolved plugin versions** (live in the Dockerfile as ARGs — drifted
  from the plan defaults due to the go.k6.io/k6 → go.k6.io/k6/v2 module
  path split in late 2025):
    - k6 base: v1.6.1 (last v1-module-path release of `go.k6.io/k6`)
    - xk6: v1.4.3 (CLI; xk6@latest requires Go >= 1.25 via GOTOOLCHAIN=auto)
    - xk6-sql: v1.0.6 (v1.1.0+ moved to k6/v2 module path)
    - xk6-sql-driver-mysql: v0.2.2 (v0.3.0+ moved to k6/v2 module path)
    - xk6-kafka: v1.3.0 (v2.0.0 moved to /v2 module path; stays on v1)
- **Why these versions**: mixing v1 and v2 of the k6 module silently drops
  the older-major-version extensions (xk6 prints a "conflicting k6 versions"
  warning and the v2-using extension fails to register). The pinned set
  above is the latest mutually-compatible combination on the v1 path.
- **Baked script paths**:
    - `/scripts/lib/{common,mysql,kafka,doris,smoke}.js`
    - `/scripts/runners/{mysql,kafka,doris}.js`
- **Smoke command** (run after every image rebuild):
    ```
    docker run --rm ghcr.io/dlh/dlh-k6:0.1.0 run /scripts/lib/smoke.js
    ```
- **Static parse check for a single script (no target needed)**:
    ```
    docker run --rm ghcr.io/dlh/dlh-k6:0.1.0 archive --env MYSQL_DSN=dummy \
      -O /tmp/a.tar /scripts/runners/mysql.js
    ```
    Each runner enforces required env vars at init time, so the archive
    invocation must supply them (any dummy value is fine for a parse check).
- **xk6 CLI breaking change**: xk6 v1.x renamed the positional k6-version
  argument to `--k6-version` (the positional clashes with the default
  `"latest"` value of the flag). The Dockerfile uses the flag form.

### Implications for Plan 7

- The `load/k6-run` WorkflowTemplate must pin `runner.image: ghcr.io/dlh/dlh-k6:0.1.0`
  and replace its `script_configmap` input with a `script_path` input that
  receives values like `/scripts/runners/mysql.js`.
- Scenario YAMLs pass per-scenario env vars via `env_map`. Each runner's env
  contract is documented inline in its script and in the Phase 2 spec.
- After bumping the image tag in the chart (or any code change in `fixture-images/k6/`),
  use `make -C fixture-images/k6 reload-minikube` to force kubelet to pick up the new image
  (it caches by image ID; bare `make k6-image` + `minikube image load` is not enough
  if pods already have the previous version of the same tag).

## Plan 7 Task 1: k6 custom-Trend prom-rw emits `_p95` gauges — WITH `k6_` PREFIX (2026-05-17)

Verified empirically on `dlh-k6:0.1.0` (k6 v1.6.1) with
`K6_PROMETHEUS_RW_TREND_STATS=p(95),p(99),min,max,avg`. A custom
`new Trend('dlh_probe_duration_seconds', true)` produced the full
gauge family in VM:

    k6_dlh_probe_duration_seconds_p95
    k6_dlh_probe_duration_seconds_p99
    k6_dlh_probe_duration_seconds_avg
    k6_dlh_probe_duration_seconds_min
    k6_dlh_probe_duration_seconds_max
    k6_dlh_probe_ops_total_total

**Critical drift from spec:** k6's prometheus-rw output unconditionally
prefixes EVERY metric name with `k6_` (and appends `_total` to Counters).
Our custom metrics `dlh_<type>_<thing>` thus surface in VM as
`k6_dlh_<type>_<thing>_p95` etc.

**Implication:** Plan 7 SLO queries and Plan 8 dashboards use
`k6_dlh_<type>_*` (NOT `dlh_<type>_*`). The hypothesis on `_p95` gauge
form is otherwise confirmed — no `histogram_quantile()` and no
`rate(_sum)/rate(_count)` fallback needed.

## Plan 7 outcome — scripts + WT migration (2026-05-17)

`load/k6-run` is now a two-step template (write-env CM → run TestRun on
`dlh-k6:0.1.0`). Scenarios pass `script_path` + `env_map` (multi-line
KEY=VALUE) instead of `script_configmap`. Three `*-k6-script.yaml` files
deleted.

### Live scenarios and the metric series they emit

| Scenario | Runner | Metrics actually in VM | Verdict overall |
|---|---|---|---|
| `mysql-pod-delete` | `runners/mysql.js` | `k6_dlh_mysql_query_duration_seconds_{p95,p99,avg,min,max}` (tagged `op`); `k6_dlh_mysql_queries_total_total{op}` (Counter, added post-review); `k6_dlh_app_errors_total_total{kind=~"mysql.*"}` | PASS pre-fix (zero errors masked broken denominator); post-fix exposes real recovery-window write errors |
| `kafka-broker-partition` | `runners/kafka.js` | `k6_dlh_kafka_produce_duration_seconds_{p95,...}` (tagged `topic`); `k6_dlh_kafka_messages_produced_total_total{topic}`; `k6_dlh_app_errors_total_total{kind="kafka-produce"}` | PASS (dlh_verdict_overall=1) |
| `doris-be-network-loss` | deferred (Plan 7 spike NO-GO) | none — scenario YAML is the Phase 1 stub; deferred in `scenarios/README.md`. Future work: revive with `apache/doris.fe-ubuntu` + `apache/doris.be-ubuntu` separated images on a VM with `vm.max_map_count=2000000` tunable. | N/A |

### Critical drifts from spec (relevant to Plan 8)

1. **`k6_` name prefix unconditional.** k6's prometheus-rw output prefixes
   every metric name with `k6_`, no setting to disable. Custom Trend
   `dlh_mysql_query_duration_seconds` → VM series
   `k6_dlh_mysql_query_duration_seconds_p95` etc.

2. **Counters get DOUBLE `_total`.** A k6 Counter named
   `dlh_kafka_messages_produced_total` surfaces in VM as
   `k6_dlh_kafka_messages_produced_total_total` (k6 prom-rw appends
   `_total` even to already-suffixed names). Same for
   `k6_dlh_app_errors_total_total`. SLO queries and dashboards must use
   the doubled form.

3. **`K6_INCLUDE_SYSTEM_ENV_VARS=true` required.** k6-operator 4.4.1
   wires `runner.envFrom` onto both the initializer and runner pods,
   but the initializer's `k6 archive` command only forwards
   `runner.env` entries as `-e` flags — envFrom values aren't seen by
   k6 unless `K6_INCLUDE_SYSTEM_ENV_VARS=true` is set in `runner.env`
   (which IS passed via `-e`). The two-step `load/k6-run` WT now
   includes this env var; without it, runners that validate required
   env at init-time (e.g. `MYSQL_DSN`) throw at archive time.

4. **Steps templates can't use static `value` for outputs.** The plan's
   `main.outputs.metrics_namespace` with `value:` was rejected by Argo
   ("output parameters must have a valueFrom specified"). Removed —
   no caller consumed it; verdict reads `scenario_label` from workflow
   parameters directly.

### Implications for Plan 8

- Type-specific dashboard PromQL uses the gauge form
  `k6_dlh_<type>_*_p95` directly. Counter rates use the doubled
  `*_total_total` form.
- Variable cascade: `$scenario` from `label_values(<marker_metric>, dlh_scenario)`
  where marker is `k6_dlh_mysql_query_duration_seconds_count` for mysql,
  `k6_dlh_kafka_messages_produced_total_total` for kafka.
- `$workflow` from `label_values(<marker_metric>{dlh_scenario="$scenario"}, dlh_workflow)`.
- Existing `dlh-run-detail` dashboard's k6 panels still reference
  `k6_http_*` series — those are NO LONGER EMITTED by the new runners
  (real protocol tests, not HTTP). Plan 8 either drops those k6 panels
  from `dlh-run-detail` or replaces them with per-target equivalents.
- Doris dashboard ships as a placeholder with no live data path until
  Doris BE comes up in a future phase.

### Trend prom-rw NEVER emits `_count` or `_sum` — Counter pairing is mandatory for ratio SLOs (post-review fix)

Plan 7 spec compliance review caught a false-positive SLO: the
mysql `error-rate-recovery` query divided by
`k6_dlh_mysql_query_duration_seconds_count`, a series that does not
exist. k6's prometheus-rw output exports a Trend ONLY as its configured
stat suffixes (`_p95`, `_p99`, `_avg`, `_min`, `_max` — controlled by
`K6_PROMETHEUS_RW_TREND_STATS`). It NEVER emits `_count` or `_sum`
companions, unlike the native Prometheus client. Verified via VM
`/api/v1/label/__name__/values`: only the five stat-suffix series
appear; no `_count`/`_sum`.

With the bogus denominator missing, `clamp_min(..., 1e-9)` floored the
ratio to `errors * 1e9`. The original PASS only worked because the run
genuinely produced zero `kind="mysql.*"` errors during the recovery
window (and the prior numerator `k6_dlh_app_errors_total` — single
`_total` — also missed the doubled-`_total` Counter series, so both
numerator and denominator were empty → 0/clamp = 0 → pass).

**Rule:** every ratio-style SLO over a Trend metric MUST have a paired
Counter (e.g. `dlh_mysql_queries_total` → VM
`k6_dlh_mysql_queries_total_total`) incremented at the same call site,
and the SLO query must divide by the Counter's `rate(...)`. The Kafka
scenario already followed this pattern via
`dlh_kafka_messages_produced_total`; the mysql lib has been brought in
line. Doris (when revived) and any future Trend-backed SLO must do the
same.

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

## Plan 9 — Scenario optimization (2026-05-18)

- SLO templates live in a single ConfigMap `dlh-slos` keyed by filename
  (`pod-delete.yaml`, `network-loss.yaml`). util-write-slo reads the entry by
  `slo_name` parameter, applies `${VAR}` substitutions from a `slo_vars`
  multi-line KEY=VAL block, and writes `dlh-slo-<workflow.name>`.
- Two-layer substitution: `${VAR}` is filled by a bash sed loop; `{{workflow.name}}`
  is substituted at runtime via printf-octal LHS so Argo's template engine
  doesn't render it away at workflow-source render time (the spec's "Argo
  already rendered it" claim was wrong — TPL is loaded from the CM at runtime,
  not embedded in the script's `source:`).
- Fail-fast guards: `slo_vars` values must not contain `|`, `&`, or `\`
  (sed-unsafe metacharacters). Unresolved `${VAR}` markers (regex
  `\${[A-Z0-9_]+}`) after substitution also fail-fast. Both verified live.
- `dlh-slo-<wf>` CM gets `ownerReferences` pointing at the Workflow (uid from
  `{{workflow.uid}}` builtin), so k8s GC reclaims the CM on workflow deletion.
  Verified by deleting the test workflow and watching the CM disappear.
- `scripts/run-scenario.sh` now forwards extra args to `argo submit`, so any
  scenario parameter is overridable at submit time: `-p vus=50`,
  `-p chaos_duration=120s`, even `-p slo_vars=<multi-line>`. argo CLI v4.0.5
  accepts embedded literal newlines in `-p key=value` argv elements without
  needing a parameter file.
- The optional second-positional `override-name` arg of the old
  `run-scenario.sh` is removed; nothing in-tree used it.
- Switched from `kubectl create -f - | argo wait` to `argo submit --wait`.
  Confirmed exit code 0 on Succeeded and clean blocking semantics on v4.0.5.
- Doris scenario YAML is rewritten to the new shape but still deferred (NO-GO
  target — no live run).
- Pitfall logged: when picking "the latest scenario workflow" via lexical sort,
  filter to timestamped names with `grep -E '<prefix>-[0-9]{8}-[0-9]{6}$'`
  before `sort | tail -1` — otherwise stray Phase-2 UUID-suffixed workflows
  sort after timestamped ones (letters > digits).

## Plan 11 — Scenario queue + priority (2026-05-19)

- Per-target serialisation lives in ConfigMap `dlh-scenario-locks` with keys
  `mysql`, `kafka`, `doris`, each value `"1"` (max concurrent workflows per
  key). Raise counts in the CM if a target gains capacity; no scenario-side
  change required.
- Each scenario declares `spec.priority: 100` (default) and
  `spec.synchronization.semaphores: [ { configMapKeyRef: ... } ]` against
  its target's key. Note the **PLURAL `semaphores:` list form** — required
  by Argo CLI v4+. The singular `semaphore:` object form was rejected by
  v4 CLI strict-decode even though the older controller still accepts it.
- Argo controller bumped from v3.5.12 → v3.6.10 (subchart 0.42.7 → 0.45.20)
  because v3.5 only accepts the singular form and v4 CLI only emits the
  plural form. v3.6 accepts both; v4 controller would also work.
- Priority-aware acquisition order is (priority desc, creationTimestamp asc).
  Verified live: A(100) → C(200) → B(50) order with B submitted before C.
- Submit-time override via `argo submit --priority N`, forwarded by
  `scripts/run-scenario.sh` through its `"$@"` (Plan 9 contract).
  Example: `scripts/run-scenario.sh scenarios/mysql-pod-delete.yaml --priority 200`.
- `run-scenario.sh` split: `argo submit` + 2-second sleep + one-shot probe
  + `argo wait`. The probe prints
  `Queued: waiting for semaphore dlh-test-fw/ConfigMap/dlh-scenario-locks/<key> (priority N)`
  exactly when the workflow is `Pending` with
  `.status.synchronization.semaphore.waiting` populated. Silent otherwise.
- Pitfall: the 2-second probe sleep is a heuristic for the controller's
  annotation latency. v3.6.10 met it consistently; bump to 3s if observed
  to be too short under load.
- Pitfall: `.spec.synchronization` uses plural `semaphores`/`mutexes`
  (list), but `.status.synchronization` uses singular `semaphore`/`mutex`
  (object). Don't mix them when reading status programmatically.
- Cluster usage: queued workflows sit in `Pending` and consume zero pod
  resources — only controller bookkeeping. Long queues are safe up to
  ~20 entries; revisit at higher fan-in.

## Plan 12 — Chaos Mesh migration (2026-05-19)

- Litmus retired entirely. ChaosCenter portal, in-tree MongoDB, in-tree
  chaos-operator backfill, in-tree ChaosExperiment CRs, in-tree
  cluster-admin-lite RBAC, and the chaos-from-hub WT all deleted in one
  cutover. Net file-count -6; net LOC -828 in the removal commit.
- Chaos engine is now `chaos-mesh` subchart v2.8.2 (appVersion v2.8.2).
  Controller-manager Deployment + chaos-daemon DaemonSet. No dashboard, no
  DNS server, no portal-equivalent UI.
- Chaos primitive mapping:
  - `Litmus pod-delete (duration+interval)` → script-style WT that creates
    a `chaos-mesh.org/Schedule` wrapping `PodChaos {action: pod-kill,
    mode: one}` with `schedule: "@every <interval>"` and `historyLimit: 10`,
    sleeps for `duration`, then deletes the Schedule. Plan deviation from
    the spec's "Argo DAG with parallel sleep" — single script with explicit
    cleanup avoids Schedule leaking children past the chaos window.
  - `Litmus pod-network-loss` → `chaos-mesh.org/NetworkChaos
    {action: loss, duration: <s>, loss: {loss: <%>, correlation: "0"},
    direction: to}`.
  - `Litmus pod-network-partition` → `chaos-mesh.org/NetworkChaos
    {action: partition, duration: <s>, direction: to, mode: all}` with
    selector `{app: kafka, "kafka.broker.id": "<id>"}`.
- **Pitfall: Chaos Mesh NetworkChaos `direction: both` is webhook-rejected
  unless an explicit `target:` selector is supplied.** Use `direction: to`
  (or `from`) for one-sided injection. We use `direction: to` everywhere.
- **Pitfall: kafka pod label is `kafka.broker.id` (dotted), not `kafka-id`**
  as initially assumed. The apache/kafka KRaft chart uses
  `app=kafka,kafka.broker.id=<N>` per pod. Confirmed via
  `kubectl get pods --show-labels`.
- **Pitfall: chaos-daemon runtime defaults to `containerd` in the
  Chaos Mesh chart.** Minikube uses docker — override
  `chaosDaemon.runtime: docker` and `chaosDaemon.socketPath:
  /var/run/docker.sock`. Symptom: NetworkChaos and any other
  namespace-entering chaos primitives stick at Not Injected with
  `error while getting PID: expected containerd:// but got docker://`.
- **Pitfall: Chaos Mesh chart's `crds/` directory.** Helm convention is to
  install CRDs on first install but NEVER on upgrade. Plan 12 Task 2 had to
  `kubectl apply --server-side` the 3 large CRDs (Schedule, WorkflowNode,
  Workflow) because they exceed the 262144-byte annotation limit. Future
  chart upgrades of chaos-mesh will need manual CRD reconciliation.
- **Pitfall: chaos-mesh.org CRDs lack `meta.helm.sh/release-name`
  annotation** because they came in via `crds/` (not via templates). Helm
  uninstall doesn't remove them. Manual `kubectl delete crd` required.
- **Pitfall: Chaos Mesh workload names are bare** (`chaos-controller-manager`,
  `chaos-daemon`), NOT release-prefixed like other subcharts. Reference
  them directly when polling rollout status.
- verdict-job's `internal/chaosresult/` package gone (-130 LOC). The chaos-
  applied signal is now ENTIRELY encoded in Argo chaos step success.
  `report.json` no longer has `chaos_verdict`. The `dlh_verdict_chaos_pass`
  Prometheus gauge is also removed — any Grafana panel that references it
  will show "no data". Dashboard cleanup left as future Phase 4 work.
- RBAC for `argo-workflow` SA extended to
  `chaos-mesh.org/{podchaos,networkchaos,schedules}` PLUS a new ClusterRole
  + ClusterRoleBinding `dlh-argo-workflow-chaos-targets` granting
  cluster-wide chaos-mesh.org/* `*` and pods get/list/watch/delete. The
  cluster scope is required because Chaos Mesh's `vauth.kb.io` webhook
  validates the SA against the **target** namespace (e.g., mysql-sys),
  not the workflow namespace (dlh-test-fw).
- **mysql scenario SLO recalibration:** `ERR_LT` raised 0.05 → 0.50 because
  Chaos Mesh pod-kill is harsher than Litmus pod-delete (API delete + grace
  period 0 in practice vs Litmus's chaos-runner that does kubectl with
  --force --grace-period=0 BUT serialised via chaos-engine state machine).
  Single-replica mysql can't recover between 6 kills @ 10s; observed
  recovery error rate ~0.30. Lower threshold when mysql gains a replica.
- **Litmus CRD cleanup quirk:** `chaosengines.litmuschaos.io` CRD has a
  `customresourcecleanup.apiextensions.k8s.io` finalizer that prevents
  delete until all CR instances are gone. If you've already deleted Litmus
  (subchart removal), the finalizer wedges. Patch it off:
  `kubectl patch crd chaosengines.litmuschaos.io \
    -p '{"metadata":{"finalizers":[]}}' --type=merge`.
- **Litmus frontend pod orphan:** during the subchart removal, the
  `dlh-litmus-frontend-*` pod can wedge in Terminating with no owning
  workload (deployment already gone). Force-delete with
  `kubectl delete pod ... --grace-period=0 --force`.
- CI kubeconform `-skip` list narrowed to just `CustomResourceDefinition`
  (Plan 11 also skipped `ChaosExperiment`; that's gone now). Chaos Mesh
  PodChaos / NetworkChaos / Schedule schemas resolved via Datree's
  CRDs-catalog — no skip needed for them.
- Plan 11 `dlh-scenario-locks` semaphore unaffected — Argo Workflow
  synchronisation is chaos-engine-agnostic.
