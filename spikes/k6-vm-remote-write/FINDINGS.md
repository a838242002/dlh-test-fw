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
