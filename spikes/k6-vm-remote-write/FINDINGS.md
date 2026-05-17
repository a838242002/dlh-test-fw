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
