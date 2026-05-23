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
                  │   (Chaos Mesh:      ║   (dlh-k6 image: xk6-sql +            │
                  │    PodChaos +       ║    xk6-kafka; prom-rw → VM;           │
                  │    NetworkChaos;    ║    runners baked in at                │
                  │    Schedule wraps   ║    /scripts/runners/{mysql,kafka,     │
                  │    pod-kill loop)   ║                       doris}.js)      │
                  │                                                             │
                  │              verdict/slo-eval (Go binary)                   │
                  │  (mounts dlh-slo-<wf> CM; reads VM only — chaos signal      │
                  │   is Argo step success; writes JSON+HTML to MinIO)          │
                  └─────────────────────────────────────────────────────────────┘
                                          │
                            ┌─────────────┴─────────────┐
                            ▼                           ▼
                   VictoriaMetrics              MinIO artifact
                     (PromQL)                  (verdict report)
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
| Argo Workflows | Orchestrates prep / fixture / chaos / load / verdict steps | Helm `argo-workflows` 0.45.20 (controller v3.6.10) |
| Chaos Mesh | Pod/network chaos via PodChaos / NetworkChaos / Schedule CRs | Helm `chaos-mesh` 2.8.2 (chaos-controller-manager + chaos-daemon DaemonSet) |
| VictoriaMetrics single | Receives k6 prometheus-remote-write; PromQL backend for verdict + dashboards | Helm `victoria-metrics-single` 0.38.0 |
| MinIO | S3 store for fixtures + verdict artifacts | In-tree (Bitnami images yanked — see FINDINGS) |
| Grafana | Five dashboards (history + run-detail + per-type) | Helm `grafana` 8.15.0 |
| Verdict | Single Go binary; reads SLO CM + VM, emits report + VM gauges | `verdict-job/` (in this repo) |
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

# 4. Start the controlplane
cd controlplane && make ui-build && make build
DLH_AUTH_DISABLED=true ./dlh-controlplane

# 5. Submit the sample scenarios
dlh run mysql-pod-delete --wait
dlh run kafka-broker-partition --wait
```

Scenario parameters are overridable at submit time with `-p key=value`:

```bash
dlh run mysql-pod-delete --wait -p vus=50 -p chaos_duration=120s -p mysql_op_mix=read:100
```

Submissions to the same target serialise via Argo `synchronization.semaphores`
(per-target keys in `dlh-scenario-locks`). Submissions to different targets run
in parallel. Within a queue, higher `spec.priority` wins; override at submit time
via `--priority N`:

```bash
dlh run mysql-pod-delete --priority 200
# The controlplane prints "Queued" state in `dlh runs ls` output when blocked.
```

To tear down: `make platform-down` (helm uninstall) and `minikube delete`.

---

## What you can do with it

### Run an existing scenario

```bash
dlh run mysql-pod-delete --wait
# or, with overrides:
dlh run mysql-pod-delete --wait -p vus=20 -p load_duration=300s
```

The controlplane:
- timestamps `metadata.name` to `<scenarioID>-YYYYMMDD-HHMMSS`
- submits the WorkflowTemplate and streams status when `--wait` is passed
- prints queue position when the workflow is held back by a per-target semaphore
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
4. `chaos-*` in parallel with `load-k6-run`. Each chaos WT is a script
   step that submits a Chaos Mesh CR (`Schedule` wrapping `PodChaos` for
   pod-kill; `NetworkChaos` for loss/partition), polls/sleeps for the
   chaos window, then deletes the CR.
5. `verdict-slo-eval` — mounts `dlh-slo-<wf>`, queries VM, writes the
   report artifact + VM gauges. Chaos completion signal is the Argo step's
   success — no separate chaos verdict CR read.

Every tunable lives in the scenario's top-level `arguments.parameters`
block and is overridable via `dlh run <scenario> -p key=value`.

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
│   │   ├── workflowtemplates/     # 10 reusable Argo WorkflowTemplates
│   │   │   ├── chaos/             #   pod-delete, network-loss, kafka-broker-partition (Chaos Mesh)
│   │   │   ├── fixture/           #   kafka-topic-seed, minio-load-mysql, minio-load-doris
│   │   │   ├── load/k6-run.yaml   #   k6 TestRun (dlh-k6 image) → prom-rw to VM
│   │   │   ├── util/              #   write-slo, ensure-mysql-table  (Plan 9)
│   │   │   └── verdict/slo-eval.yaml
│   │   ├── slos/                  # SLO template library → ConfigMap dlh-slos (Plan 9)
│   │   │   ├── pod-delete.yaml
│   │   │   └── network-loss.yaml
│   │   └── dashboards/            # Grafana dashboards (chart-embedded copies)
│   └── templates/                 # Our own ns, RBAC, minio, slos CM, scenario-locks CM, ...
├── verdict-job/                   # Plan 3: the verdict Go binary
│   ├── cmd/verdict/main.go
│   ├── internal/{slo,window,prom,eval,report,metrics}/
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
- **Bitnami's 2025 "secure-images" migration broke arm64.** MinIO's
  Bitnami subchart is unusable; we ship an in-tree replacement at
  `templates/minio.yaml` (dev-grade — no auth, emptyDir-backed). Plan 12
  retired Litmus + its MongoDB dep, so the in-tree MongoDB workaround is
  gone too.
- **Chaos Mesh chaos-daemon runtime defaults to containerd.** Minikube
  uses docker. Override `chaosDaemon.runtime: docker` +
  `chaosDaemon.socketPath: /var/run/docker.sock` in the chaos-mesh values
  block. Otherwise NetworkChaos sticks at NotInjected with
  `error while getting PID: expected containerd:// but got docker://`.
- **Chaos Mesh NetworkChaos `direction: both` is webhook-rejected** unless
  paired with an explicit `target:` selector. Use `direction: to`.
- **Chaos Mesh chart's `crds/` directory** ships CRDs that Helm installs
  only on first install, never on upgrade, and never with release-name
  annotation. Chart upgrade or removal requires manual
  `kubectl apply --server-side` or `kubectl delete crd ...`.
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
| `plan9-scenario-optimization` | Inline `write-slo` + `ensure-load-table` heredocs lifted into `util-write-slo` + `util-ensure-mysql-table` WorkflowTemplates; chart-managed SLO template library (`dlh-slos` CM); submit-time `-p` overrides via `argo submit` |
| `plan10-github-actions-ci` | PR guardrails CI in `.github/workflows/ci.yml`: parallel `helm` (lint + template smoke), `go` (vet + test on `verdict-job`), `shellcheck`, and `kubeconform` (rendered chart + scenarios). ~1 min wall-clock on a warm cache. No image publish, no E2E. |
| `plan11-scenario-queue` | Per-target serialisation + priority via Argo native `spec.synchronization.semaphores` (ConfigMap `dlh-scenario-locks`, keys mysql/kafka/doris, count=1 each). Same-target scenarios queue; different-target run in parallel. `--priority N` override at submit time. Argo controller bumped to v3.6.10 (subchart `argo-workflows` 0.45.20). |
| `plan12-chaos-mesh-migration` | Litmus retired; Chaos Mesh adopted (subchart `chaos-mesh` 2.8.2). 3 chaos WTs rewritten to script-style submit + poll/sleep + cleanup (Schedule wrapping PodChaos for pod-kill; NetworkChaos for loss/partition). `verdict-job/internal/chaosresult/` deleted (-130 LOC); chaos verdict signal is now Argo step success alone. MongoDB, ChaosCenter portal, in-tree Litmus backfills all gone. |
| `plan13-dashboard-enrichment` | Per-target dashboards gain richer panels (mysql 5→8, kafka 5→11, doris 5→8) using k6 metrics already in VM — VUs, iteration p95, data throughput, latency percentile overlays, xk6-kafka writer internals. Chaos timeline overlay: verdict-job emits `dlh_chaos_window_{start,end}_unixtime` gauges; all 4 per-run dashboards get Grafana annotations with `useValueForTime:true` rendering orange/green vertical marks at chaos start/recovery. |

**Working end-to-end (as of `plan13-dashboard-enrichment`):**
- `mysql-pod-delete` and `kafka-broker-partition` scenarios run real
  protocol load (xk6-sql, xk6-kafka) against the in-cluster targets,
  inject Chaos Mesh chaos in parallel, and produce a verdict in both
  MinIO and VM.
- 10 WorkflowTemplates registered: `fixture-*` (3), `chaos-*` (3),
  `load-k6-run`, `verdict-slo-eval`, `util-write-slo`,
  `util-ensure-mysql-table`.
- All scenario tunables (load, chaos, workload, SLO thresholds) are
  top-level workflow parameters; any can be overridden via
  `dlh run <scenario> -p key=value`.
- Five Grafana dashboards live (history, run-detail, mysql, kafka, doris).

**Known deferred:**
- **Doris target** (arm64 + memory). Scenario YAML matches the new
  shape, but the BE/FE deploy isn't viable on a minikube-sized
  workstation. Documented in `targets/doris/README.md`.
- Production-grade auth / persistence for the in-tree MinIO (currently
  no-auth / emptyDir; rotate before any shared deploy).
- k6-operator's `PrivateLoadZone` CRD is installed by the subchart but
  unused.
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
| Plan 9 | `4d68ea3` | util-write-slo + util-ensure-mysql-table; `dlh-slos` CM; submit-time `-p` overrides |
| Plan 10 | `e6c11e2` | GitHub Actions CI (`helm` + `go` + `shellcheck` + `kubeconform`) |
| Plan 11 | `9c292a7` | Per-target scenario semaphores + priority (Argo v3.6.10); `dlh-scenario-locks` CM; queued-message UX at submit time |
| Plan 12 | `a1a9af1` | Litmus → Chaos Mesh migration; verdict-job's `chaosresult` package removed; MongoDB + ChaosCenter retired |
| Plan 13 | `438ecb1` | Per-target dashboard enrichment (+12 panels across mysql/kafka/doris) + chaos timeline overlay via `useValueForTime` annotations |
| Plan 14 | `130a0c1` | Argo CD platform lifecycle — `argocd/` AppProject + ApplicationSet + chart Application + controlplane Application placeholder; production bootstrap doc; scripts annotated as local-dev only |
| Plan 15 | `01d3f5e` | dlh-controlplane Phase B (read-only) — Go service + embedded React UI + OIDC auth + scoped RBAC + Workflow informer + MinIO report.json reader + SSE event stream; `controlplane/deploy/` manifests; `dlh-controlplane` Argo CD Application activated |
| Plan 16 | `abf407d` | dlh-controlplane Phase C — `/internal/chaos` endpoint + watchdog reconciler; chaos WTs rewired from kubectl-in-script to HTTP API; `dlh` CLI (`dlh run` + `dlh runs ls/show/logs/cancel`); `run-scenario.sh` deprecated as shim (removed in Plan 18); end-to-end smoke test against minikube |
| Plan 17 | `e9d73b6` | dlh-controlplane Phase D (remote targets) — Target registry from ConfigMap+Secrets; RemoteChaosClient + Router; /api/targets + UI TargetsPage + TargetPicker; dlh run --target; chaos WTs forward target_id |
| Plan 18 | `XXXXXXX` | dlh-controlplane Phase E (CI integration + cleanup) — POST /api/oidc/exchange + GET /api/auth/info; dlh login device-code; GH Actions composite + example release-gate workflow; kafka+doris promoted to chart-managed WTs; 5 shell scripts deleted |

Each plan's source-of-truth document lives under
`docs/superpowers/plans/` and the deviations from those plans are noted
in the merge commit body and in `FINDINGS.md`'s appended sections.
