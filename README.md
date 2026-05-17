# dlh-test-fw

Chaos + load test platform that runs on Kubernetes. Submit a `Workflow`
that **loads a fixture → applies load → injects chaos → evaluates an SLO**,
get back a machine-readable verdict and a Grafana dashboard with the run's
metrics.

```
                  ┌──────────────────────────────────────────────────────────┐
                  │  Argo Workflow (scenarios/<name>.yaml)                   │
                  │                                                          │
                  │   fixture/*  →  load/k6-run  ║  chaos/*  →  verdict/slo-eval
                  │   (mc, kcat,    (k6-operator) ║  (Litmus    (Go binary;
                  │    mysql,        prom-rw out  ║   chaos-     reads VM
                  │    curl ...)     to VM)       ║   operator)  + ChaosResult)
                  └──────────────────────────────────────────────────────────┘
                                       │                            │
                                       ▼                            ▼
                              VictoriaMetrics                 dlh-result-<wf>
                                  (PromQL)                     ConfigMap (JSON
                                       │                        + HTML report)
                                       ▼
                                    Grafana
                          (dlh-history, dlh-run-detail)
```

The platform itself is one umbrella Helm chart; everything below runs in a
single Kubernetes namespace (`dlh-test-fw`) on minikube.

---

## What's running

| Component | Role | Source |
|---|---|---|
| Argo Workflows | Orchestrates fixture / load / chaos / verdict steps | Helm `argo-workflows` 0.42.7 |
| Litmus ChaosCenter + chaos-operator + ChaosExperiments | Pod/network chaos | Helm `litmus` 3.28.0 + in-tree backfills |
| k6-operator | Submits k6 `TestRun` CRDs that load-test from inside the cluster | Helm `k6-operator` 4.4.1 |
| VictoriaMetrics single | Receives k6 prometheus-remote-write; PromQL backend for verdict + dashboards | Helm `victoria-metrics-single` 0.38.0 |
| MinIO | S3 store for fixtures + Argo workflow artifacts | In-tree (Bitnami images yanked — see FINDINGS) |
| MongoDB | Litmus state store | In-tree (Bitnami images yanked — see FINDINGS) |
| Grafana | Dashboards | Helm `grafana` 8.15.0 |
| Verdict | Single Go binary that computes pass/fail per scenario, publishes JSON+HTML to a ConfigMap | `verdict-job/` (in this repo) |

---

## Quickstart

Requires `minikube`, `kubectl`, `helm`, `docker`, `make`, `jq`, `curl`,
`bash`. On Apple Silicon minikube uses the docker driver.

```bash
# 1. Bring up minikube (6 CPU / 12 GiB; idempotent)
spikes/k6-vm-remote-write/scripts/minikube-up.sh

# 2. Build + load fixture images and the verdict image into minikube
make fixture-images
( cd verdict-job && make load-image )

# 3. Install / upgrade the platform
make platform-up        # helm dependency update + helm upgrade --install dlh ...
make platform-verify    # in-cluster smoke; expects PASS

# 4. Submit a sample scenario
make run-mysql          # mysql-pod-delete reference scenario
```

`make run-mysql` watches the workflow to completion and prints the verdict
JSON. Repeat with other scenarios:

```bash
kubectl -n dlh-test-fw create -f scenarios/kafka-broker-partition.yaml
kubectl -n dlh-test-fw create -f scenarios/kafka-broker-partition-k6-script.yaml  # ConfigMap with the k6 script
```

To tear down: `make platform-down` (helm uninstall) and `minikube delete`.

---

## What you can do with it

### Run an existing scenario

```bash
kubectl -n dlh-test-fw apply -f scenarios/mysql-pod-delete-k6-script.yaml
kubectl -n dlh-test-fw create  -f scenarios/mysql-pod-delete.yaml

# wait for Succeeded
kubectl -n dlh-test-fw get workflow -w
```

### Read the verdict

```bash
# JSON
kubectl -n dlh-test-fw get cm -l dlh-test-fw/result -o jsonpath='{.items[-1:].data.report\.json}' | jq .

# HTML
kubectl -n dlh-test-fw get cm -l dlh-test-fw/result -o jsonpath='{.items[-1:].data.report\.html}' > /tmp/verdict.html && open /tmp/verdict.html
```

### See the dashboards

```bash
kubectl -n dlh-test-fw port-forward svc/dlh-grafana 3000:80
# http://localhost:3000  admin / <secret value of dlh-grafana-credentials>
```

Two dashboards ship by default (sidecar picks them up via
`dlh-dashboard=true` label):
- **dlh-history** — recent verdicts + p95 latency trend across scenarios
- **dlh-run-detail** — per-run k6 metrics + verdict summary

### Add a new scenario

A scenario is an Argo `Workflow` that composes the WorkflowTemplate
library. Look at `scenarios/mysql-pod-delete.yaml` as the reference. You
provide:

1. A target deploy under `targets/<name>/` (or reuse an existing one).
2. A k6 script as a ConfigMap (`scenarios/<scenario>-k6-script.yaml`).
3. A scenario Workflow that calls `load/k6-run` + `chaos/<kind>` +
   `verdict/slo-eval` with the right parameters.

All PromQL filters MUST use `dlh_scenario="<label>"`, NOT `scenario=` —
k6 reserves `scenario` for its own internal scenario name. This is
documented (with reason) in `spikes/k6-vm-remote-write/FINDINGS.md`.

---

## Repository layout

```
dlh-test-fw/
├── helm/dlh-test-fw/              # Umbrella Helm chart (the platform)
│   ├── Chart.yaml                 # Pinned subchart deps
│   ├── values.yaml                # Defaults
│   ├── values-minikube.yaml       # Local-dev overlay
│   ├── files/
│   │   ├── workflowtemplates/     # The 9 reusable Argo WorkflowTemplates
│   │   │   ├── chaos/             #   pod-delete, network-loss, kafka-broker-partition, from-hub
│   │   │   ├── fixture/           #   kafka-topic-seed, minio-load-mysql, minio-load-doris
│   │   │   ├── load/k6-run.yaml   #   k6 TestRun → prom-rw to VM (dlh_scenario tag)
│   │   │   └── verdict/slo-eval.yaml
│   │   └── dashboards/            # Grafana dashboards (chart-embedded copies)
│   └── templates/                 # Our own ns, RBAC, mongo, minio, ingress, secrets, ...
├── verdict-job/                   # Plan 3: the verdict Go binary
│   ├── cmd/verdict/main.go        #   CLI flag block at the top
│   ├── internal/{slo,window,prom,chaosresult,eval,report,publish}/
│   ├── Dockerfile                 #   distroless; GOTOOLCHAIN=auto
│   └── Makefile                   #   `make image / load-image`
├── fixture-images/                # Plan 4: arm64 fixture-image Dockerfiles
│   ├── kafka/   (alpine + mc + kcat)
│   ├── mysql/   (alpine + mc + mysql client)
│   └── doris/   (alpine + mc + curl for Stream Load)
├── scenarios/                     # Plan 5: composed Workflows
│   ├── mysql-pod-delete.yaml      #   Reference scenario; runs end-to-end
│   ├── kafka-broker-partition.yaml
│   ├── doris-be-network-loss.yaml #   Deferred (arm64 + memory)
│   └── *-k6-script.yaml           #   Per-scenario k6 script ConfigMaps
├── targets/                       # Minimal target deploys for the scenarios
│   ├── mysql/      (mysql:8, native-password)
│   ├── kafka/      (apache/kafka:3.7.0 KRaft single-broker)
│   └── doris/      (deferred — README only)
├── dashboards/grafana/            # Dashboard source-of-truth JSONs
├── scripts/                       # platform-up/down/verify, run-scenario, verify-templates
├── docs/superpowers/
│   ├── specs/                     # Phase 1 design
│   └── plans/                     # The 5 plans driving the build
├── spikes/k6-vm-remote-write/
│   ├── FINDINGS.md                # Authoritative gotchas; keep reading
│   └── scripts/minikube-up.sh
└── Makefile                       # Top-level targets (platform-*, run-mysql, ...)
```

---

## Read this before you touch the chart: FINDINGS.md

`spikes/k6-vm-remote-write/FINDINGS.md` is the authoritative log of what
worked / didn't and why. **The high-impact items:**

- **`dlh_scenario`, not `scenario`.** k6 reserves the `scenario` label for
  its internal scenario name (always `default` in our case). Every
  PromQL filter, dashboard query, and SLO threshold partitions on
  `dlh_scenario`.
- **k6-operator chart 4.x removed `namespace.watch`.** Single-namespace
  toggle is gone; operator watches all namespaces.
- **Bitnami "secure-images" migration (mid-2025) broke a lot.**
  `bitnamilegacy/mongodb` arm64 was yanked; `bitnamisecure/mongodb:latest`
  exits silently inside Litmus's pod spec; `bitnami/minio` was removed.
  We ship in-tree MongoDB + MinIO as a result (see
  `templates/mongodb.yaml` / `templates/minio.yaml`).
- **Litmus chart 3.x ships only the portal.** chaos-operator and
  per-namespace ChaosExperiment CRs are NOT installed by the chart; we
  backfill them in `templates/litmus-chaos-{operator,experiments}.yaml`.
- **MinIO Console removed in any release after May 2025 (community
  edition).** Stay on `RELEASE.2024-12-13T22-19-12Z` (current pin) to
  keep the browser admin UI; newer releases keep the S3 API but drop
  the console.

---

## Phase 1 MVP status

Tag `phase-1-mvp` marks the working state on `main`. As of that tag:

**Working end-to-end:**
- `mysql-pod-delete` scenario: Argo Workflow → load (k6) + chaos (pod-delete
  on mysql) → verdict (Go binary). Real verdict report in a ConfigMap.
- `kafka-broker-partition` scenario: same shape, with Litmus
  pod-network-partition against the apache/kafka KRaft target.
- Grafana sidecar picks up `dlh-history` + `dlh-run-detail` dashboards.
- All 9 WorkflowTemplates registered and reusable.

**Known deferred:**
- **Doris target** (arm64 + memory). Scenario + k6 script committed, but
  the BE/FE deploy isn't viable on a minikube-sized workstation.
  Documented in `targets/doris/README.md`.
- k6 `_seconds_bucket` histogram series in some scenarios — accepted per
  Plan 5 caveat; validates platform mechanics, not real load.
- Production-grade auth / persistence for the in-tree MongoDB and MinIO
  (currently no-auth / emptyDir; rotate before any shared deploy).
- Litmus PrivateLoadZone CRD installed by the chart isn't used.

---

## Plans + history

Phase 1 was executed as 5 plans, each landing on `main` as a `--no-ff`
merge commit so the boundary is grep-able in `git log --first-parent`:

| Plan | Merge commit | Subject |
|---|---|---|
| Phase 1 design + Plan 1 spike + Plan 2 chart | `54df151` | k6→VM spike + umbrella chart (incl. mid-cycle Litmus rescue) |
| Plan 3 | `3a75e01` | verdict Go binary |
| Plan 4 | `d7fe2c3` | WorkflowTemplate library + fixture images |
| Plan 5 | `421c0ea` | scenarios + dashboards (+ Plan 4 backfills) |

Each plan's source-of-truth document lives at
`docs/superpowers/plans/2026-05-16-0<N>-*.md` and the deviations from
those plans are noted in the merge commit bodies.
