# Scenarios

Concrete Argo Workflows that exercise the WorkflowTemplate library
(`chaos-*`, `fixture-*`, `load-k6-run`, `verdict-slo-eval`).

## Run one

    kubectl apply -f scenarios/mysql-pod-delete-k6-script.yaml    # once
    kubectl create -f scenarios/mysql-pod-delete.yaml
    argo watch -n dlh-test-fw @latest

(Use `kubectl create`, not `kubectl apply`, because the manifest uses
`generateName`.)

When the workflow finishes, the verdict step's exit code propagates to the
Workflow status: `Succeeded` = PASS, `Failed` = FAIL. The HTML report can
be downloaded from the Argo UI artifact viewer; the JSON report from the
same place or via:

    kubectl -n dlh-test-fw get cm dlh-result-<wf-name> -o jsonpath='{.data.result\.json}' | jq .

## SLO is embedded inline

Each scenario's first step (`prep-slo`) materialises a ConfigMap
`dlh-slo-<workflow-name>` containing the SLO YAML. The `verdict-slo-eval`
template mounts it. Edit the inline YAML in the scenario file to tune SLOs.

**PromQL filter label is `dlh_scenario`, not `scenario`.** k6 reserves
`scenario` for its internal scenario name, so the load WT tags
remote-write samples with `dlh_scenario` instead.

## k6 scripts live in static ConfigMaps

Per-scenario k6 scripts are static ConfigMaps (`k6-script-<scenario>`)
applied separately. They don't need to be reapplied per workflow run.

## Phase 1 caveat: HTTP proxy stand-in

k6 doesn't ship MySQL/Kafka/Doris drivers. Phase 1 scenarios point k6 at
the in-cluster `httpbin` Service as a stand-in to validate platform
mechanics. For real load testing, swap the URL in the k6 script for a
service that fronts the target system (or use xk6 extensions like
xk6-sql / xk6-kafka).

## Scenario status

| Scenario                  | Target deployed | Smoke-tested |
|---------------------------|-----------------|--------------|
| mysql-pod-delete          | yes             | yes (Task 12) |
| kafka-broker-partition    | yes             | yes (Task 13) |
| doris-be-network-loss     | **no** (deferred — arm64/memory) | no  |

## Adding a new scenario

1. Pick one chaos + one fixture + one load template from the library.
2. Copy `mysql-pod-delete.yaml`; rename and rewire parameters.
3. Adjust the inline SLO YAML in the `write-slo` template (use `dlh_scenario`).
4. Create a `k6-script-<name>` ConfigMap.
5. `kubectl create` (not apply) and `argo watch`.
