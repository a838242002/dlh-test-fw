# dlh-test-fw

[![CI](https://github.com/a838242002/dlh-test-fw/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/a838242002/dlh-test-fw/actions/workflows/ci.yml)

Chaos + load test platform that runs on Kubernetes. Submit a `Workflow`
that **prepares an SLO → loads a fixture → injects chaos in parallel with
real-protocol load → evaluates the SLO**, get back a machine-readable
verdict (MinIO artifact + VictoriaMetrics gauges) and a Grafana dashboard
with the run's metrics.

```
                  ┌─────────────────────────────────────────────────────────────┐
                  │  Argo Workflow (scenarios/<name>.yaml)                      │
                  │                                                             │
                  │   util/write-slo  →  fixture/*  →  util/ensure-mysql-table  │
                  │   (render SLO CM)    (mc, mysql,    (parameterised CREATE   │
                  │                       kcat ...)      TABLE; mysql only)     │
                  │                                                             │
                  │       chaos/*       ║       load/k6-run                     │
                  │   (Litmus chaos-    ║   (dlh-k6 image: xk6-sql +            │
                  │    operator;        ║    xk6-kafka; prom-rw → VM;           │
                  │    pod-delete,      ║    runners baked in at                │
                  │    network-loss,    ║    /scripts/runners/{mysql,kafka,     │
                  │    broker-partition)║                       doris}.js)      │
                  │                                                             │
                  │              verdict/slo-eval (Go binary)                   │
                  │  (mounts dlh-slo-<wf> CM; reads VM + ChaosResult;           │
                  │   pushes dlh_verdict_* gauges; writes JSON+HTML to MinIO)   │
                  └─────────────────────────────────────────────────────────────┘
                                          │
                       ┌──────────────────┼──────────────────┐
                       ▼                  ▼                  ▼
                VictoriaMetrics    MinIO artifact      ChaosResult CR
                  (PromQL)         (verdict report)    (Litmus)
                       │
                       ▼
                    Grafana
              dlh-history · dlh-run-detail · dlh-mysql · dlh-kafka · dlh-doris
```

The platform itself is one umbrella Helm chart; everything below runs in a
single Kubernetes namespace (`dlh-test-fw`) on minikube.

---

## What's running

| Component | Role | Source |
|---|---|---|
| Argo Workflows | Orchestrates prep / fixture / chaos / load / verdict steps | Helm `argo-workflows` 0.42.7 |
| Litmus ChaosCenter + chaos-operator + ChaosExperiments | Pod/network chaos | Helm `litmus` 3.28.0 + in-tree backfills |
| VictoriaMetrics single | Receives k6 prometheus-remote-write; PromQL backend for verdict + dashboards | Helm `victoria-metrics-single` 0.38.0 |
| MinIO | S3 store for fixtures + verdict artifacts | In-tree (Bitnami images yanked — see FINDINGS) |
| MongoDB | Litmus state store | In-tree (Bitnami images yanked — see FINDINGS) |
| Grafana | Five dashboards (history + run-detail + per-type) | Helm `grafana` 8.15.0 |
| Verdict | Single Go binary; reads SLO CM + VM + ChaosResult, emits report + VM gauges | `verdict-job/` (in this repo) |
| dlh-k6 image | Custom k6 build with `xk6-sql` (mysql + doris) + `xk6-kafka`; baked runners | `fixture-images/k6/` (in this repo) |

---

## Quickstart

Requires `minikube`, `kubectl`, `helm`, `docker`, `make`, `jq`, `curl`,
`bash`, `argo` CLI v4+. On Apple Silicon minikube uses the docker driver.

```bash
# 1. Bring up minikube (6 CPU / 12 GiB; idempotent)
scripts/minikube-up.sh

# 2. Build + load the local images into minikube
make fixture-images        # mysql / kafka / doris fixture-shells
make k6-image              # custom dlh-k6 (xk6-sql + xk6-kafka + baked runners)
( cd verdict-job && make load-image )

# 3. Install / upgrade the platform
make platform-up           # helm dependency update + helm upgrade --install dlh ...
make platform-verify       # in-cluster smoke; expects PASS

# 4. Submit the sample scenarios
make run-mysql             # mysql-pod-delete
make run-kafka             # kafka-broker-partition
```

`make run-{mysql,kafka}` wrap `scripts/run-scenario.sh`, which submits via
`argo submit` then blocks on `argo wait`, and forwards extra args. Override
any scenario parameter at submit time:

```bash
scripts/run-scenario.sh scenarios/mysql-pod-delete.yaml \
    -p vus=50 -p chaos_duration=120s -p mysql_op_mix=read:100
```

Submissions to the same target serialise via Argo `synchronization.semaphores`
(per-target keys in `dlh-scenario-locks`). Submissions to different targets run
in parallel. Within a queue, higher `spec.priority` wins; override at submit time
via `argo submit --priority N`:

```bash
scripts/run-scenario.sh scenarios/mysql-pod-delete.yaml --priority 200
# Prints "Queued: waiting for semaphore .../mysql (priority 200)" if blocked.
```

To tear down: `make platform-down` (helm uninstall) and `minikube delete`.

---

## What you can do with it

### Run an existing scenario

```bash
make run-mysql
# or, with overrides:
scripts/run-scenario.sh scenarios/mysql-pod-delete.yaml -p vus=20 -p load_duration=300s
```

The script:
- timestamps `metadata.generateName` into `metadata.name: <prefix>-YYYYMMDD-HHMMSS`
- `argo submit`s, then probes once for queue state, then `argo wait`s
- prints `Queued: waiting for semaphore <name> (priority N)` if the workflow is held back by a per-target semaphore
- exits 0 iff phase is `Succeeded`

### Read the verdict

The verdict is a JSON+HTML report stored as an Argo workflow artifact in
MinIO, **and** a set of gauges in VictoriaMetrics (`dlh_verdict_overall`,
`dlh_verdict_threshold_value`, `dlh_verdict_threshold_passed`).

```bash
# JSON report from MinIO
wf=mysql-pod-delete-YYYYMMDD-HHMMSS
kubectl -n dlh-test-fw exec deploy/dlh-minio -- \
    mc cat "local/artifacts/${wf}/${wf}-main-*/verdict/report.json" | jq .
```

```bash
# Or query the verdict gauges directly:
curl -s 'http://localhost:8428/api/v1/query?query=dlh_verdict_overall{dlh_workflow="'"$wf"'"}' | jq .
```

### See the dashboards

```bash
kubectl -n dlh-test-fw port-forward svc/dlh-grafana 3001:80
# http://localhost:3001  admin / <secret value of dlh-grafana-credentials>
```

Five dashboards ship by default (sidecar picks them up via
`dlh-dashboard=true` label):
- **dlh-history** — verdict history across all scenarios
- **dlh-run-detail** — per-run k6 metrics + verdict summary (cross-links from history)
- **dlh-mysql** — per-run mysql-specific panels (xk6-sql gauges, error breakdown)
- **dlh-kafka** — per-run kafka-specific panels (xk6-kafka produce/consume)
- **dlh-doris** — per-run doris panels (Stream Load + query; deferred target)

### Add a new scenario

Look at `scenarios/mysql-pod-delete.yaml` as the reference. A scenario is
an Argo `Workflow` that composes:

1. `util-write-slo` — picks an SLO library entry from `dlh-slos`
   ConfigMap (`pod-delete` or `network-loss`) and renders per-workflow
   `dlh-slo-<wf>` CM using the scenario's `slo_vars` block.
2. (Optional) a fixture step — `fixture-minio-load-{mysql,doris}` or
   `fixture-kafka-topic-seed`.
3. (mysql only) `util-ensure-mysql-table` — parameterised CREATE TABLE.
4. `chaos-*` in parallel with `load-k6-run`.
5. `verdict-slo-eval` — mounts `dlh-slo-<wf>`, queries VM, reads
   ChaosResult, writes the report artifact + VM gauges.

Every tunable lives in the scenario's top-level `arguments.parameters`
block and is overridable via `scripts/run-scenario.sh -p key=value`.

You do NOT need to ship a k6 script ConfigMap — the dlh-k6 image bakes
runners at `/scripts/runners/{mysql,kafka,doris}.js`. `load/k6-run`
takes `script_path` + an `env_map` block (`KEY=VAL` per line).

All PromQL filters MUST use `dlh_scenario="<label>"`, NOT `scenario=` —
k6 reserves `scenario` for its own internal scenario name. Each workflow
also tags series with `dlh_workflow="<workflow.name>"` for per-run
partitioning. Documented (with reasons) in
`docs/FINDINGS.md`.

---

## Repository layout

```
dlh-test-fw/
├── helm/dlh-test-fw/              # Umbrella Helm chart (the platform)
│   ├── Chart.yaml                 # Pinned subchart deps
│   ├── values.yaml                # Defaults
│   ├── values-minikube.yaml       # Local-dev overlay
│   ├── files/
│   │   ├── workflowtemplates/     # 11 reusable Argo WorkflowTemplates
│   │   │   ├── chaos/             #   pod-delete, network-loss, kafka-broker-partition, from-hub
│   │   │   ├── fixture/           #   kafka-topic-seed, minio-load-mysql, minio-load-doris
│   │   │   ├── load/k6-run.yaml   #   k6 TestRun (dlh-k6 image) → prom-rw to VM
│   │   │   ├── util/              #   write-slo, ensure-mysql-table  (Plan 9)
│   │   │   └── verdict/slo-eval.yaml
│   │   ├── slos/                  # SLO template library → ConfigMap dlh-slos (Plan 9)
│   │   │   ├── pod-delete.yaml
│   │   │   └── network-loss.yaml
│   │   └── dashboards/            # Grafana dashboards (chart-embedded copies)
│   └── templates/                 # Our own ns, RBAC, mongo, minio, slos CM, ...
├── verdict-job/                   # Plan 3: the verdict Go binary
│   ├── cmd/verdict/main.go
│   ├── internal/{slo,window,prom,chaosresult,eval,report,publish}/
│   ├── Dockerfile                 #   distroless; GOTOOLCHAIN=auto
│   └── Makefile                   #   `make image / load-image`
├── fixture-images/                # Per-target build helpers (one Dockerfile each)
│   ├── kafka/   (alpine + mc + kcat)
│   ├── mysql/   (alpine + mc + mysql client)
│   ├── doris/   (alpine + mc + curl for Stream Load)
│   └── k6/      (custom k6 + xk6-sql + xk6-kafka + baked runners — Plan 6)
├── scenarios/                     # Composed Workflows
│   ├── mysql-pod-delete.yaml      #   Reference scenario; runs end-to-end
│   ├── kafka-broker-partition.yaml
│   └── doris-be-network-loss.yaml #   Deferred (arm64 + memory)
├── targets/                       # Minimal target deploys for the scenarios
│   ├── mysql/      (mysql:8, native-password)
│   ├── kafka/      (apache/kafka:3.7.0 KRaft single-broker)
│   └── doris/      (deferred — README only)
├── dashboards/grafana/            # Dashboard source-of-truth JSONs (history, run-detail, mysql, kafka, doris)
├── scripts/                       # platform-up/down/verify, run-scenario, verify-templates
├── docs/
│   ├── FINDINGS.md                # Authoritative cross-plan gotchas; keep reading
│   └── superpowers/
│       ├── specs/                 # Phase design docs (incl. 2026-05-18 scenario optimization)
│       └── plans/                 # Implementation plans (one per executable unit)
├── scripts/                       # platform-up/down/verify, run-scenario, minikube-up, verify-templates
├── CLAUDE.md                      # Project conventions (worktree, branching, image reload)
└── Makefile                       # Top-level targets (platform-*, run-mysql, run-kafka, k6-image, ...)
```

---

## Read this before you touch the chart: FINDINGS.md

`docs/FINDINGS.md` is the authoritative log of what
worked / didn't and why. **The high-impact items:**

- **`dlh_scenario`, not `scenario`.** k6 reserves the `scenario` label for
  its internal scenario name (always `default` in our case). Every
  PromQL filter, dashboard query, and SLO threshold partitions on
  `dlh_scenario`. Per-run filters use `dlh_workflow`.
- **k6 prom-rw emits gauges, not histogram buckets.**
  `histogram_quantile(..._seconds_bucket)` returns nothing for k6
  metrics. Use the pre-computed quantile gauges (`*_p95`, `*_p99`,
  controlled by `K6_PROMETHEUS_RW_TREND_STATS`).
- **Verdict output is an Argo artifact, NOT a ConfigMap.** The original
  Phase-1 design used `dlh-result-<wf>` CM; switched in commit `e136e9a`
  to MinIO artifact + VM gauges (`dlh_verdict_*`). Dashboards query via
  PromQL only — no Infinity datasource.
- **Bitnami's 2025 "secure-images" migration broke arm64.** MongoDB and
  MinIO Bitnami subcharts are unusable; we ship in-tree replacements at
  `templates/{mongodb,minio}.yaml`. Both are dev-grade (no auth,
  emptyDir-backed).
- **Litmus chart 3.x ships only the portal.** chaos-operator and
  per-namespace ChaosExperiment CRs are backfilled by
  `templates/litmus-chaos-{operator,experiments}.yaml`.
- **MinIO pinned to `RELEASE.2024-12-13T22-19-12Z`** — newer releases
  dropped the admin console from the community edition.
- **VM lookback-delta is 5 minutes.** A single end-of-run gauge push
  (`dlh_verdict_*`) goes stale to instant queries after 5 min. Dashboards
  wrap them in `last_over_time(...[7d])`.
- **`imagePullPolicy: Never` is intentional** for `dlh-k6` and
  `dlh-verdict` — the `ghcr.io/dlh/` registry prefix isn't published;
  kubelet must use the locally-loaded image unconditionally.
- **`minikube image load` doesn't replace a running pod's cached image.**
  Force the reload via the `reload-minikube` Makefile target in
  `fixture-images/k6/` and `verdict-job/` (rm running containers + `docker rmi -f`).

---

## Continuous integration

`.github/workflows/ci.yml` runs four parallel jobs on every PR and on push to `main`. Cold cache wall-clock under 3 minutes; warm cache under 1 minute.

| Job | What it checks | Local equivalent |
|---|---|---|
| `helm` | `helm lint` + `helm template` smoke (Files.Glob, helpers, `tpl` escapes, `dlh-slos` CM renders) | `helm lint helm/dlh-test-fw && helm template dlh helm/dlh-test-fw` |
| `go` | `go vet ./...` + `go test ./...` in `verdict-job/` | `cd verdict-job && go vet ./... && go test ./...` |
| `shellcheck` | `-S error` on `scripts/*.sh` | `shellcheck -S error scripts/*.sh` |
| `kubeconform` | `-strict` against rendered chart + `scenarios/*.yaml`, Datree CRDs-catalog as schema source, `-skip CustomResourceDefinition,ChaosExperiment` | `helm template ... \| kubeconform -skip CustomResourceDefinition,ChaosExperiment -strict ...` |

Pinned: Helm `v4.2.0`, kubeconform `v0.6.7`, `ludeeus/action-shellcheck@2.0.0`. `cancel-in-progress` is on, keyed per ref.

Out of scope (deferred): image publish to GHCR, KinD-based E2E scenario runs.

---

## Phase status

| Tag | Scope |
|---|---|
| `phase-1-mvp` | First end-to-end run: chaos + k6 load + verdict, mysql + kafka scenarios |
| `phase-2-mvp` | Custom dlh-k6 image (xk6-sql + xk6-kafka), real-protocol scenarios with per-target metric series, three per-type Grafana dashboards (mysql / kafka / doris) |
| `plan9-scenario-optimization` | Inline `write-slo` + `ensure-load-table` heredocs lifted into `util-write-slo` + `util-ensure-mysql-table` WorkflowTemplates; chart-managed SLO template library (`dlh-slos` CM); submit-time `-p` overrides via `run-scenario.sh` |
| `plan10-github-actions-ci` | PR guardrails CI in `.github/workflows/ci.yml`: parallel `helm` (lint + template smoke), `go` (vet + test on `verdict-job`), `shellcheck`, and `kubeconform` (rendered chart + scenarios). ~1 min wall-clock on a warm cache. No image publish, no E2E. |
| `plan11-scenario-queue` | Per-target serialisation + priority via Argo native `spec.synchronization.semaphores` (ConfigMap `dlh-scenario-locks`, keys mysql/kafka/doris, count=1 each). Same-target scenarios queue; different-target run in parallel. `--priority N` override at submit time. `scripts/run-scenario.sh` prints a `Queued: ...` line when blocked. Argo controller bumped to v3.6.10 (subchart `argo-workflows` 0.45.20). |

**Working end-to-end (as of `plan11-scenario-queue`):**
- `mysql-pod-delete` and `kafka-broker-partition` scenarios run real
  protocol load (xk6-sql, xk6-kafka) against the in-cluster targets,
  inject Litmus chaos in parallel, and produce a verdict in both MinIO
  and VM.
- 11 WorkflowTemplates registered: `fixture-*` (3), `chaos-*` (4),
  `load-k6-run`, `verdict-slo-eval`, `util-write-slo`,
  `util-ensure-mysql-table`.
- All scenario tunables (load, chaos, workload, SLO thresholds) are
  top-level workflow parameters; any can be overridden via
  `scripts/run-scenario.sh -p key=value`.
- Five Grafana dashboards live (history, run-detail, mysql, kafka, doris).

**Known deferred:**
- **Doris target** (arm64 + memory). Scenario YAML matches the new
  shape, but the BE/FE deploy isn't viable on a minikube-sized
  workstation. Documented in `targets/doris/README.md`.
- Production-grade auth / persistence for the in-tree MongoDB and MinIO
  (currently no-auth / emptyDir; rotate before any shared deploy).
- Litmus PrivateLoadZone CRD installed by the chart isn't used.
- SLO library only has two entries (`pod-delete`, `network-loss`),
  byte-identical for now — kept separate for future divergence.

---

## Plans + history

Phase 1 was 5 plans; Phase 2 was 2 plans (custom k6 image + per-type
dashboards, merged as one); Plan 9 closed out as a standalone optimization
pass. Each lands on `main` as a `--no-ff` merge commit so the boundary is
grep-able in `git log --first-parent`.

| Plan(s) | Merge commit | Subject |
|---|---|---|
| Phase 1 design + Plan 1 spike + Plan 2 chart | `54df151` | k6→VM spike + umbrella chart (incl. mid-cycle Litmus rescue) |
| Plan 3 | `3a75e01` | verdict Go binary |
| Plan 4 | `d7fe2c3` | WorkflowTemplate library + fixture images |
| Plan 5 | `421c0ea` | Phase 1 scenarios + dashboards (+ Plan 4 backfills) |
| Plans 6 + 7 + 8 (Phase 2) | `a8dbc7b` | Custom dlh-k6 image, real-protocol scenarios, per-type dashboards |
| Plan 9 | `4d68ea3` | util-write-slo + util-ensure-mysql-table; `dlh-slos` CM; `run-scenario.sh -p` overrides |
| Plan 10 | `e6c11e2` | GitHub Actions CI (`helm` + `go` + `shellcheck` + `kubeconform`) |
| Plan 11 | `9c292a7` | Per-target scenario semaphores + priority (Argo v3.6.10); `dlh-scenario-locks` CM; queued-message UX in `run-scenario.sh` |

Each plan's source-of-truth document lives under
`docs/superpowers/plans/` and the deviations from those plans are noted
in the merge commit body and in `FINDINGS.md`'s appended sections.
