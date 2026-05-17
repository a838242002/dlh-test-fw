# Scenarios

Concrete Argo Workflows that exercise the WorkflowTemplate library
(`chaos-*`, `fixture-*`, `load-k6-run`, `verdict-slo-eval`).

## Run one

    make run-mysql        # mysql-pod-delete
    make run-kafka        # kafka-broker-partition
    # make run-doris      # DEFERRED — see targets/doris/README.md

Each target submits the scenario via `scripts/run-scenario.sh`, which
uses `kubectl create` (the manifests use `generateName`) and waits on
the workflow.

When the workflow finishes, the verdict step's exit code propagates to
the Workflow status: `Succeeded` = PASS, `Failed` = FAIL. The HTML and
JSON reports are archived by Argo into MinIO under
`local/artifacts/<workflow>/<verdict-pod>/verdict/report.{html,json}`
and are also linked from the Argo UI artifact viewer.

## SLO is embedded inline

Each scenario's first step (`prep-slo`) materialises a ConfigMap
`dlh-slo-<workflow-name>` containing the SLO YAML. The `verdict-slo-eval`
template mounts it. Edit the inline YAML in the scenario file to tune
SLOs.

**PromQL filter label is `dlh_scenario`, not `scenario`.** k6 reserves
`scenario` for its internal scenario name, so the load WT tags
remote-write samples with `dlh_scenario` instead. The workflow name is
exposed as `dlh_workflow`.

**Counter metric names get a doubled `_total`.** k6's prom-rw output
appends `_total` to every Counter — including names that already end
in `_total`. So `dlh_mysql_queries_total` surfaces in VM as
`k6_dlh_mysql_queries_total_total`. Ratio-style SLOs MUST divide by
this doubled form, not by a Trend `_count` suffix (Trend prom-rw never
emits `_count` or `_sum`; only the configured stat suffixes
`_p95`/`_p99`/`_avg`/`_min`/`_max`). See
`spikes/k6-vm-remote-write/FINDINGS.md` for the full write-up.

## k6 scripts are baked into the dlh-k6 image

Runner scripts live in `ghcr.io/dlh/dlh-k6:0.1.0` at
`/scripts/runners/{mysql,kafka,doris}.js` (built from
`fixture-images/k6/`, Plan 6). There are no per-scenario ConfigMaps —
the scenario YAML selects a script via the `load/k6-run` WT's
`script_path` parameter and passes target connection details through
`env_map` (a multi-line `KEY=VALUE` block consumed by `write-env-cm`,
which materialises an env ConfigMap for the TestRun).

The runners drive real protocols via xk6 extensions:

| Runner               | Extension(s)                    | Target            |
|----------------------|---------------------------------|-------------------|
| `runners/mysql.js`   | xk6-sql + xk6-sql-driver-mysql  | MySQL (also Doris FE) |
| `runners/kafka.js`   | xk6-kafka                       | Kafka brokers     |
| `runners/doris.js`   | xk6-sql + xk6-sql-driver-mysql  | Doris FE (deferred this workstation) |

No HTTP stand-in remains — scenarios exercise the real wire protocol of
the target system.

## Scenario status

| Scenario                  | Target deployed | Smoke-tested  |
|---------------------------|-----------------|---------------|
| mysql-pod-delete          | yes             | yes (Plan 7)  |
| kafka-broker-partition    | yes             | yes (Plan 7)  |
| doris-be-network-loss     | **DEFERRED** (Plan 7 spike NO-GO; see `targets/doris/README.md`) | no |

## Adding a new scenario

1. Pick one chaos + one fixture + one load template from the library
   (the existing `mysql-pod-delete.yaml` is the simplest reference).
2. Copy that YAML; rename it and rewire parameters to point at your
   target namespace / pod selector / fixture URI.
3. In the `load` step, set `script_path` to one of the baked runner
   paths (`/scripts/runners/<type>.js`) and supply `env_map` with the
   protocol-specific connection details (DSN, brokers, topic, etc.).
4. Edit the inline SLO YAML in the `write-slo` template (use the
   `dlh_workflow` and `dlh_scenario` labels; remember the doubled
   `_total_total` for any Counter denominator).
5. Add a `make run-<name>` target if you want one-shot convenience.
6. `make run-<name>` (or `scripts/run-scenario.sh scenarios/<file>.yaml`).

No new JS file and no new ConfigMap are required for scenarios that
reuse an existing runner. New runners go in `fixture-images/k6/scripts/`
and trigger a `dlh-k6` image rebuild.
