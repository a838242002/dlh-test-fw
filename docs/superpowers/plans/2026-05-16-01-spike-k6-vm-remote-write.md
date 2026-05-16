# Plan 1 — Spike: k6 → VictoriaMetrics Remote-Write Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prove on minikube that a k6 load job running via k6-operator can remote-write Prometheus metrics into a single-node VictoriaMetrics instance, and that those metrics are queryable by PromQL filtered on a `scenario=<label>` tag.

**Architecture:** Run minikube locally (6 CPU, 12 GiB). Install `victoria-metrics-single` and `k6-operator` via their official Helm charts with overrides that (a) enable `-remoteWrite` ingestion on VM and (b) inject `--out experimental-prometheus-rw=...` into k6 runner pods. Target an in-cluster `httpbin` deployment so the spike depends on nothing outside the cluster. Verify by `curl`-ing VM's `/api/v1/query` and asserting an HTTP request counter with the right `scenario` label is non-zero.

**Tech Stack:** minikube, Helm v3, k6-operator (Grafana), victoria-metrics-single (VictoriaMetrics), httpbin (kennethreitz), kubectl, jq, bash.

**Why this plan is a spike:** Spec section "已知風險與 spike 順序" item 1 says k6→Prom remote-write is Day-1 risk; verdict has no data source unless this works. This plan produces a small, self-contained directory (`spikes/k6-vm-remote-write/`) plus a `FINDINGS.md` documenting exact versions, flags, and gotchas — those findings feed into Plan 2 (Helm chart) and Plan 4 (WorkflowTemplate `load/k6-run`).

**Out of scope for this plan:** Argo Workflows, Litmus, MinIO, fixture loading, verdict job, Grafana dashboards, ingress, multi-scenario, production hardening. We only validate the metric pipeline.

---

## File Structure

Files this plan creates (all under repo root `/Users/allen/repo/dlh-test-fw/`):

```
spikes/k6-vm-remote-write/
├── README.md                          # How to run, how to tear down
├── FINDINGS.md                        # Versions + flags + gotchas (fed into Plan 2/4)
├── Makefile                           # Convenience targets: up / down / verify
├── scripts/
│   ├── minikube-up.sh                 # Idempotent minikube boot with required addons
│   └── verify.sh                      # Polls VM API, asserts metric+label present
├── helm/
│   ├── vm-values.yaml                 # victoria-metrics-single overrides
│   └── k6-operator-values.yaml        # k6-operator overrides
└── manifests/
    ├── httpbin.yaml                   # Deployment + Service (target under load)
    ├── k6-script-configmap.yaml       # Trivial k6 script that hits httpbin
    └── k6-testrun.yaml                # K6 CRD invocation with prom-rw output
```

Responsibilities:
- `scripts/minikube-up.sh` — single-shot bootstrap; safe to re-run.
- `scripts/verify.sh` — exit 0 iff the spike's success criterion is met (used as the "test" in TDD-style steps).
- `helm/*.yaml` — minimal overrides; everything else stays default so we can later port the same overrides into the umbrella Helm chart.
- `manifests/*.yaml` — the workload under test.
- `Makefile` — wraps `kubectl`, `helm`, and scripts so the README is short.
- `FINDINGS.md` — written **last**; downstream plans depend on it.

---

## Pre-flight Assumptions

- Engineer has `minikube`, `kubectl`, `helm`, `jq`, `curl`, `bash` on `$PATH`.
- Docker / podman driver is functional for minikube (`minikube start` succeeds without flags).
- Workstation has ≥ 6 free CPU and ≥ 12 GiB free RAM (matches spec resource budget for "without Doris").
- No prior `minikube` cluster named `minikube` is running with conflicting config; if there is, `scripts/minikube-up.sh` will `minikube delete` and recreate (destructive — engineer is warned in README).

---

## Pinned Versions

To keep the spike reproducible (and feed Plan 2's `Chart.yaml` pinning), use these versions throughout:

| Component | Version | Source |
|---|---|---|
| minikube | latest stable | (whatever `minikube start` resolves) |
| Kubernetes inside minikube | v1.30.x | minikube default |
| Helm chart `victoria-metrics/victoria-metrics-single` | `0.12.x` | https://victoriametrics.github.io/helm-charts/ |
| Helm chart `grafana/k6-operator` | `3.x` | https://grafana.github.io/helm-charts |
| k6 runner image | `grafana/k6:0.50.0` | docker.io |
| httpbin image | `kennethreitz/httpbin:latest` | docker.io |
| VictoriaMetrics image | (chart default; pinned via chart version) | — |

**If the engineer cannot find one of the chart versions** (e.g. chart repo has moved on), they should pick the closest available patch in the same minor and record the exact resolved version into `FINDINGS.md`. Do not silently let Helm pull `latest`.

---

## Task 1: Repo scaffolding + Makefile

**Files:**
- Create: `spikes/k6-vm-remote-write/README.md`
- Create: `spikes/k6-vm-remote-write/Makefile`

- [ ] **Step 1: Write `spikes/k6-vm-remote-write/README.md`**

```markdown
# Spike: k6 → VictoriaMetrics remote-write

Validates the Day-1 risk in the platform design: that k6 (run by k6-operator)
can push metrics into a single-node VictoriaMetrics via Prometheus remote-write,
and that those metrics are queryable filtered by a `scenario` label.

## Run

    make up       # boot minikube, install charts, apply manifests, run k6
    make verify   # poll VM API; exits 0 iff success criterion is met
    make down     # tear down minikube cluster

## Success criterion

The PromQL query

    sum(k6_http_reqs_total{scenario="spike-httpbin"})

returns a value > 0 within 120 seconds of `make up` completing.

## Warning

`make up` will run `minikube delete` if a cluster already exists. Do not run
this on a workstation hosting other minikube clusters you care about.
```

- [ ] **Step 2: Write `spikes/k6-vm-remote-write/Makefile`**

```makefile
SHELL := /usr/bin/env bash
.ONESHELL:
.SHELLFLAGS := -euo pipefail -c

.PHONY: up down verify

up:
	./scripts/minikube-up.sh
	helm repo add victoria-metrics https://victoriametrics.github.io/helm-charts/ || true
	helm repo add grafana https://grafana.github.io/helm-charts || true
	helm repo update
	kubectl create namespace dlh-test-fw --dry-run=client -o yaml | kubectl apply -f -
	helm upgrade --install vm victoria-metrics/victoria-metrics-single \
	  --version 0.12.0 \
	  -n dlh-test-fw \
	  -f helm/vm-values.yaml --wait
	helm upgrade --install k6-operator grafana/k6-operator \
	  --version 3.5.0 \
	  -n dlh-test-fw \
	  -f helm/k6-operator-values.yaml --wait
	kubectl -n dlh-test-fw apply -f manifests/httpbin.yaml
	kubectl -n dlh-test-fw rollout status deploy/httpbin --timeout=120s
	kubectl -n dlh-test-fw apply -f manifests/k6-script-configmap.yaml
	kubectl -n dlh-test-fw apply -f manifests/k6-testrun.yaml

verify:
	./scripts/verify.sh

down:
	minikube delete
```

- [ ] **Step 3: Commit scaffolding**

```bash
cd /Users/allen/repo/dlh-test-fw
git add spikes/k6-vm-remote-write/README.md spikes/k6-vm-remote-write/Makefile
git commit -m "spike(k6-vm): scaffold spike directory with README and Makefile"
```

---

## Task 2: minikube bootstrap script

**Files:**
- Create: `spikes/k6-vm-remote-write/scripts/minikube-up.sh`

- [ ] **Step 1: Write the failing verifier first (will live in Task 3, but we sketch it here as the bootstrap success criterion)**

We can't write a unit test for a bash bootstrap, but we encode the contract as a manual smoke check in the script's last line. The script must exit 0 only if `kubectl get nodes` reports a Ready node.

- [ ] **Step 2: Write `scripts/minikube-up.sh`**

```bash
#!/usr/bin/env bash
set -euo pipefail

# Destructive: ensures clean slate. Spike-only behavior.
if minikube status >/dev/null 2>&1; then
  echo "Existing minikube cluster found — deleting (spike requires a clean cluster)."
  minikube delete
fi

minikube start \
  --cpus=6 \
  --memory=12g \
  --disk-size=40g \
  --addons=ingress,metrics-server

# Wait until the API is actually Ready (start returns before kubelet is fully up sometimes).
for i in {1..30}; do
  if kubectl get nodes 2>/dev/null | grep -q ' Ready '; then
    echo "minikube Ready."
    exit 0
  fi
  sleep 2
done

echo "minikube failed to reach Ready within 60s" >&2
kubectl get nodes || true
exit 1
```

- [ ] **Step 3: Make executable**

```bash
chmod +x spikes/k6-vm-remote-write/scripts/minikube-up.sh
```

- [ ] **Step 4: Run it to verify**

```bash
cd /Users/allen/repo/dlh-test-fw/spikes/k6-vm-remote-write
./scripts/minikube-up.sh
```

Expected: ends with `minikube Ready.` and exit code 0. `kubectl get nodes` shows one Ready node.

- [ ] **Step 5: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add spikes/k6-vm-remote-write/scripts/minikube-up.sh
git commit -m "spike(k6-vm): add minikube bootstrap script"
```

---

## Task 3: Verifier script (the "test")

**Files:**
- Create: `spikes/k6-vm-remote-write/scripts/verify.sh`

This script is the executable success criterion. It runs **before** the rest of the pipeline exists (it will fail), then we make it pass by adding VM + k6 + httpbin.

- [ ] **Step 1: Write `scripts/verify.sh`**

```bash
#!/usr/bin/env bash
set -euo pipefail

NS=dlh-test-fw
DEADLINE=$(( $(date +%s) + 180 ))

# Find VM single's service. Chart names it victoria-metrics-single-server typically.
VM_SVC=$(kubectl -n "$NS" get svc -l app.kubernetes.io/name=victoria-metrics-single \
  -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)

if [[ -z "$VM_SVC" ]]; then
  echo "FAIL: victoria-metrics-single Service not found in namespace $NS" >&2
  exit 1
fi

# Port-forward in the background; kill on exit.
kubectl -n "$NS" port-forward "svc/$VM_SVC" 8428:8428 >/tmp/vm-pf.log 2>&1 &
PF_PID=$!
trap 'kill $PF_PID 2>/dev/null || true' EXIT
sleep 3

QUERY='sum(k6_http_reqs_total{scenario="spike-httpbin"})'

while (( $(date +%s) < DEADLINE )); do
  RESP=$(curl -s --get "http://127.0.0.1:8428/api/v1/query" \
    --data-urlencode "query=${QUERY}")
  VAL=$(echo "$RESP" | jq -r '.data.result[0].value[1] // "0"')
  if [[ "$VAL" != "0" && "$VAL" != "null" ]]; then
    echo "PASS: k6_http_reqs_total{scenario=spike-httpbin} = $VAL"
    exit 0
  fi
  echo "waiting… current value=$VAL"
  sleep 5
done

echo "FAIL: metric did not appear within 180s. Last response: $RESP" >&2
exit 1
```

- [ ] **Step 2: Make executable**

```bash
chmod +x spikes/k6-vm-remote-write/scripts/verify.sh
```

- [ ] **Step 3: Run it to confirm it fails (no VM installed yet)**

```bash
cd /Users/allen/repo/dlh-test-fw/spikes/k6-vm-remote-write
./scripts/verify.sh; echo "exit=$?"
```

Expected: prints `FAIL: victoria-metrics-single Service not found in namespace dlh-test-fw`, exit code 1. **This failure is the goal of this step** — it proves the verifier is wired up before we build the thing it verifies.

- [ ] **Step 4: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add spikes/k6-vm-remote-write/scripts/verify.sh
git commit -m "spike(k6-vm): add verifier script (currently failing — expected)"
```

---

## Task 4: VictoriaMetrics single install

**Files:**
- Create: `spikes/k6-vm-remote-write/helm/vm-values.yaml`

VM-single accepts Prometheus remote-write at `/api/v1/write` when the server is started with the `-remoteWrite.maxLineSize` / general ingestion enabled. As of chart 0.12.x, remote-write ingestion on the `-server` is on by default (port 8428 serves both query and write). We just need to make sure the chart doesn't strip the ingestion port and that resources fit the budget.

- [ ] **Step 1: Write `helm/vm-values.yaml`**

```yaml
# victoria-metrics-single chart 0.12.x overrides
server:
  retentionPeriod: 30d        # matches spec verdict.retention guideline
  resources:
    requests:
      cpu: 100m
      memory: 256Mi
    limits:
      cpu: 500m
      memory: 512Mi
  persistentVolume:
    enabled: false             # spike-only; ephemeral PV is fine
  extraArgs:
    # Permissive label cardinality for the spike; we'll tighten in Plan 2.
    search.maxUniqueTimeseries: "100000"
service:
  type: ClusterIP
  servicePort: 8428
```

- [ ] **Step 2: Install via Helm**

```bash
helm repo add victoria-metrics https://victoriametrics.github.io/helm-charts/ || true
helm repo update
kubectl create namespace dlh-test-fw --dry-run=client -o yaml | kubectl apply -f -
helm upgrade --install vm victoria-metrics/victoria-metrics-single \
  --version 0.12.0 \
  -n dlh-test-fw \
  -f spikes/k6-vm-remote-write/helm/vm-values.yaml --wait
```

Expected: helm reports `STATUS: deployed`; `kubectl -n dlh-test-fw get pods -l app.kubernetes.io/name=victoria-metrics-single` shows 1/1 Running.

- [ ] **Step 3: Smoke-check remote-write endpoint accepts a sample**

```bash
kubectl -n dlh-test-fw port-forward svc/vm-victoria-metrics-single-server 8428:8428 &
PF=$!
sleep 3
# Use VM's text-import endpoint to insert a sample; if write path is healthy we see 204.
curl -s -o /dev/null -w "%{http_code}\n" \
  -X POST 'http://127.0.0.1:8428/api/v1/import/prometheus' \
  --data-binary 'spike_smoke 1'
kill $PF
```

Expected: `204`.

- [ ] **Step 4: Resolve actual service name and pin in FINDINGS draft**

```bash
kubectl -n dlh-test-fw get svc -l app.kubernetes.io/name=victoria-metrics-single
```

Record the exact service name printed. The chart may name it `vm-victoria-metrics-single-server` (release name `vm` prefix) — note it for FINDINGS.md and confirm the verifier's label selector still matches.

- [ ] **Step 5: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add spikes/k6-vm-remote-write/helm/vm-values.yaml
git commit -m "spike(k6-vm): victoria-metrics-single helm values"
```

---

## Task 5: httpbin target

**Files:**
- Create: `spikes/k6-vm-remote-write/manifests/httpbin.yaml`

- [ ] **Step 1: Write `manifests/httpbin.yaml`**

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: httpbin
  labels: { app: httpbin }
spec:
  replicas: 1
  selector: { matchLabels: { app: httpbin } }
  template:
    metadata:
      labels: { app: httpbin }
    spec:
      containers:
      - name: httpbin
        image: kennethreitz/httpbin:latest
        ports:
        - containerPort: 80
        resources:
          requests: { cpu: 50m, memory: 64Mi }
          limits:   { cpu: 200m, memory: 128Mi }
---
apiVersion: v1
kind: Service
metadata:
  name: httpbin
spec:
  selector: { app: httpbin }
  ports:
  - port: 80
    targetPort: 80
```

- [ ] **Step 2: Apply and wait**

```bash
kubectl -n dlh-test-fw apply -f spikes/k6-vm-remote-write/manifests/httpbin.yaml
kubectl -n dlh-test-fw rollout status deploy/httpbin --timeout=120s
```

Expected: `deployment "httpbin" successfully rolled out`.

- [ ] **Step 3: Sanity-check reachability from inside the cluster**

```bash
kubectl -n dlh-test-fw run curl-sanity --rm -it --restart=Never \
  --image=curlimages/curl:8.8.0 -- \
  -sS http://httpbin.dlh-test-fw.svc.cluster.local/status/200 -o /dev/null -w "%{http_code}\n"
```

Expected: `200`.

- [ ] **Step 4: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add spikes/k6-vm-remote-write/manifests/httpbin.yaml
git commit -m "spike(k6-vm): httpbin target deployment"
```

---

## Task 6: k6-operator install

**Files:**
- Create: `spikes/k6-vm-remote-write/helm/k6-operator-values.yaml`

- [ ] **Step 1: Write `helm/k6-operator-values.yaml`**

```yaml
# grafana/k6-operator chart 3.x
manager:
  resources:
    requests: { cpu: 50m, memory: 64Mi }
    limits:   { cpu: 200m, memory: 128Mi }
# Watch only our namespace to keep spike scope tight.
namespace:
  watch: dlh-test-fw
```

- [ ] **Step 2: Install**

```bash
helm repo add grafana https://grafana.github.io/helm-charts || true
helm repo update
helm upgrade --install k6-operator grafana/k6-operator \
  --version 3.5.0 \
  -n dlh-test-fw \
  -f spikes/k6-vm-remote-write/helm/k6-operator-values.yaml --wait
```

Expected: helm `STATUS: deployed`; controller pod 1/1 Running:

```bash
kubectl -n dlh-test-fw get pods -l app.kubernetes.io/name=k6-operator
```

- [ ] **Step 3: Verify the `TestRun` (or `K6`) CRD is registered**

```bash
kubectl get crd | grep -E 'testruns?\.k6\.io|k6s\.k6\.io'
```

Expected: at least one CRD listed. **Record the exact CRD name** — chart 3.x switched from `K6` to `TestRun` at some point. If `TestRun` is what's present, adjust `manifests/k6-testrun.yaml` in Task 7 to use `kind: TestRun` and `apiVersion: k6.io/v1alpha1`. If only `K6` is present, use that.

- [ ] **Step 4: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add spikes/k6-vm-remote-write/helm/k6-operator-values.yaml
git commit -m "spike(k6-vm): k6-operator helm values"
```

---

## Task 7: k6 script ConfigMap + TestRun

**Files:**
- Create: `spikes/k6-vm-remote-write/manifests/k6-script-configmap.yaml`
- Create: `spikes/k6-vm-remote-write/manifests/k6-testrun.yaml`

- [ ] **Step 1: Write the k6 script as a ConfigMap**

```yaml
# manifests/k6-script-configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: k6-spike-script
data:
  script.js: |
    import http from 'k6/http';
    import { sleep } from 'k6';

    export const options = {
      vus: 5,
      duration: '60s',
      // Tag every metric with the scenario label so VM-side filtering works.
      tags: { scenario: 'spike-httpbin' },
    };

    export default function () {
      http.get('http://httpbin.dlh-test-fw.svc.cluster.local/status/200');
      sleep(1);
    }
```

- [ ] **Step 2: Write the TestRun CRD**

Use `kind: TestRun` (chart 3.x default). If Task 6 step 3 found only `K6`, replace with `kind: K6` — fields below are identical.

```yaml
# manifests/k6-testrun.yaml
apiVersion: k6.io/v1alpha1
kind: TestRun
metadata:
  name: spike-httpbin
spec:
  parallelism: 1
  script:
    configMap:
      name: k6-spike-script
      file: script.js
  arguments: >-
    --tag scenario=spike-httpbin
    --out experimental-prometheus-rw
  runner:
    env:
    - name: K6_PROMETHEUS_RW_SERVER_URL
      value: http://vm-victoria-metrics-single-server.dlh-test-fw.svc.cluster.local:8428/api/v1/write
    - name: K6_PROMETHEUS_RW_TREND_STATS
      value: "p(95),p(99),min,max,avg"
    - name: K6_PROMETHEUS_RW_PUSH_INTERVAL
      value: "5s"
```

**Note for the engineer:** The exact VM service DNS name was determined in Task 4 Step 4. If it differs from `vm-victoria-metrics-single-server`, update the `K6_PROMETHEUS_RW_SERVER_URL` value before applying.

- [ ] **Step 3: Apply**

```bash
kubectl -n dlh-test-fw apply -f spikes/k6-vm-remote-write/manifests/k6-script-configmap.yaml
kubectl -n dlh-test-fw apply -f spikes/k6-vm-remote-write/manifests/k6-testrun.yaml
```

Expected: both objects created without error.

- [ ] **Step 4: Watch k6 runner pod come up**

```bash
kubectl -n dlh-test-fw get pods -l k6_cr=spike-httpbin -w
```

Wait until a pod transitions to `Running` (≤ 60s) then `Completed` (≤ 90s after that). Ctrl-C when done.

If the pod stays `Pending` or `CrashLoopBackOff`, run:

```bash
kubectl -n dlh-test-fw describe pod -l k6_cr=spike-httpbin
kubectl -n dlh-test-fw logs -l k6_cr=spike-httpbin --tail=100
```

Common failures and fixes:
- **Pod ImagePullBackOff** → the chart's default runner image is unreachable; set `runner.image: grafana/k6:0.50.0` in the TestRun spec.
- **k6 logs show "unknown output experimental-prometheus-rw"** → the runner image is too old; pin `runner.image: grafana/k6:0.50.0`.
- **k6 logs show "connection refused" to VM** → service DNS name in `K6_PROMETHEUS_RW_SERVER_URL` doesn't match Task 4's actual VM service name.

- [ ] **Step 5: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add spikes/k6-vm-remote-write/manifests/k6-script-configmap.yaml \
        spikes/k6-vm-remote-write/manifests/k6-testrun.yaml
git commit -m "spike(k6-vm): k6 script and TestRun with prom-rw output"
```

---

## Task 8: Run the verifier (the "test passes")

- [ ] **Step 1: Run `verify.sh`**

```bash
cd /Users/allen/repo/dlh-test-fw/spikes/k6-vm-remote-write
./scripts/verify.sh
```

Expected: within 180s, prints `PASS: k6_http_reqs_total{scenario=spike-httpbin} = <N>` with N > 0, and exits 0.

If it fails:
1. Re-check VM service DNS in `manifests/k6-testrun.yaml`.
2. `kubectl -n dlh-test-fw logs -l k6_cr=spike-httpbin --tail=200` — look for `output: prometheus-rw flush error` or similar.
3. Curl VM directly: `curl -s 'http://127.0.0.1:8428/api/v1/label/__name__/values' | jq '.data[] | select(startswith("k6_"))'` — if no `k6_*` series exist at all, remote-write isn't reaching VM; if they exist but `scenario` label is absent, the `--tag scenario=` argument isn't being applied to the prom-rw output specifically (try moving the tag into `K6_PROMETHEUS_RW_LABEL_*` env or hardcoding the tag in `options.tags` in `script.js`).

- [ ] **Step 2: No commit needed — this is a green run, not a code change.**

---

## Task 9: Document findings

**Files:**
- Create: `spikes/k6-vm-remote-write/FINDINGS.md`

This is the **deliverable** that feeds Plan 2 and Plan 4. Write it last so it reflects what actually worked, not what we expected.

- [ ] **Step 1: Write `FINDINGS.md` using this template**

```markdown
# Findings — k6 → VictoriaMetrics remote-write spike

Date verified: <YYYY-MM-DD>
Engineer: <name>

## Versions that worked

| Component | Chart version | Image |
|---|---|---|
| victoria-metrics-single | <fill> | <fill> |
| k6-operator | <fill> | <fill> |
| k6 runner | grafana/k6:<fill> | — |
| Kubernetes (minikube) | <fill> | — |

## Exact service DNS used

VM remote-write endpoint resolved to:
    <fill, e.g. http://vm-victoria-metrics-single-server.dlh-test-fw.svc.cluster.local:8428/api/v1/write>

## k6 CRD kind

The installed CRD kind was: <fill — `TestRun` or `K6`>
apiVersion: <fill>

## Required runner env vars / args

    --out experimental-prometheus-rw
    --tag scenario=<label>
    env K6_PROMETHEUS_RW_SERVER_URL=<endpoint>
    env K6_PROMETHEUS_RW_PUSH_INTERVAL=5s
    env K6_PROMETHEUS_RW_TREND_STATS=p(95),p(99),min,max,avg

## Gotchas observed

- <fill: e.g. "k6 runner image had to be pinned to 0.50.0; chart default was 0.49 which lacks the experimental-prometheus-rw output">
- <fill: e.g. "VM service name differs from chart name by release-prefix">
- <fill: anything else>

## Implications for downstream plans

- **Plan 2 (Helm chart):** pin the above chart versions in `Chart.yaml` dependencies. Reproduce `vm-values.yaml` and `k6-operator-values.yaml` under `helm/dlh-test-fw/values.yaml` keys `victoria-metrics-single:` and `k6-operator:`.
- **Plan 4 (`load/k6-run` WorkflowTemplate):** the template must inject the env vars listed above and `--tag scenario={{inputs.parameters.scenario_label}}`. The remote-write URL is a Helm value (`platform.vm.remoteWriteUrl`) injected at template-render time.

## How to reproduce

    make up && make verify
```

- [ ] **Step 2: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add spikes/k6-vm-remote-write/FINDINGS.md
git commit -m "spike(k6-vm): document findings (chart versions, env vars, gotchas)"
```

---

## Task 10: Tag the spike completion

- [ ] **Step 1: Tag the commit so downstream plans can reference it**

```bash
cd /Users/allen/repo/dlh-test-fw
git tag -a spike-k6-vm-remote-write -m "k6→VM remote-write spike verified $(date +%Y-%m-%d)"
```

- [ ] **Step 2: Leave the cluster up (optional)**

Downstream work (Plan 2) can reuse the running minikube. If switching workstations or pausing, run `make down` first.

---

## Definition of Done

- [ ] `make up && make verify` runs from a clean machine and exits 0 within 5 minutes total.
- [ ] `FINDINGS.md` has every `<fill>` slot filled.
- [ ] Git tag `spike-k6-vm-remote-write` exists.
- [ ] Plan 2 author can read `FINDINGS.md` and copy exact chart versions, env vars, and gotchas into the umbrella Helm chart without re-discovering them.

---

## Self-Review Notes

- **Spec coverage:** This plan implements only the spec's Day-1 risk item (k6→Prom remote write). All other spec requirements are explicitly out of scope — handled by Plans 2-5.
- **Placeholders:** The only `<fill>` slots are inside `FINDINGS.md`, which is by design — those are observations the engineer must record, not future-engineer's problems.
- **Type consistency:** Service name `vm-victoria-metrics-single-server` appears in both Task 7 manifest and Task 3 verifier; if Task 4 Step 4 reveals a different actual name, **both** files must be updated before re-running verify. (Engineer warned in Task 7 Step 2.)
