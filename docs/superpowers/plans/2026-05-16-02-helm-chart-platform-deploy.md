# Plan 2 — Helm Chart + Minikube Platform Deploy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Produce an installable umbrella Helm chart `helm/dlh-test-fw/` that, with one `helm install`, brings up Argo Workflows + Litmus + k6-operator + MinIO + VictoriaMetrics + Grafana on the local minikube created in Plan 1, exposes the three UIs via ingress, and provisions the RBAC, MinIO buckets, and secrets needed by later plans.

**Architecture:** Umbrella chart pattern — `helm/dlh-test-fw/Chart.yaml` declares six pinned subchart dependencies; `helm/dlh-test-fw/values.yaml` sets overrides under each subchart's key namespace. Our own resources (ingress, `cluster-admin-lite` ClusterRole + binding, MinIO bucket-init Job, `dlh-result-*` ConfigMap RBAC for the verdict job, secrets, an empty `WorkflowTemplate` shell that Plan 4 fills) live under `helm/dlh-test-fw/templates/`. Reuses pinned versions and env vars discovered in `spikes/k6-vm-remote-write/FINDINGS.md`.

**Tech Stack:** Helm v3, Kubernetes v1.30, Bitnami / official charts for each component, kubectl, bash, jq, optional `kube-score` / `helm lint` for static checks.

**Prerequisites:**
- Plan 1 complete; `spikes/k6-vm-remote-write/FINDINGS.md` filled in.
- Minikube is up (Plan 1's `make up` left it running) or can be brought up by reusing `spikes/k6-vm-remote-write/scripts/minikube-up.sh`.
- **Plan 1 already installed `k6-operator` and `victoria-metrics-single` releases into the `dlh-test-fw` namespace.** Before running this plan's `helm install dlh ...`, either (a) `helm uninstall -n dlh-test-fw vm k6-operator` so the umbrella release can own those resources, or (b) reuse the same release names by setting `argo-workflows.enabled` etc. and disabling the two already-installed subcharts. Recommended: uninstall the standalone releases first — Plan 1's purpose was to prove the wiring, not to be the production install.
- **Orphaned k6 cluster-scoped resources gotcha (from FINDINGS):** if the cluster has ever had k6-operator installed in a different namespace, helm install will fail to adopt the pre-existing `testruns.k6.io` / `privateloadzones.k6.io` CRDs and the `k6-operator-*` ClusterRoles/Bindings. Fix by re-annotating each with `meta.helm.sh/release-namespace=dlh-test-fw` and `meta.helm.sh/release-name=dlh` (this plan's release name), or `kubectl delete` them with no in-flight TestRun resources.

**Out of scope:** WorkflowTemplate contents (Plan 4), verdict binary image (Plan 3 produces it; we only reference its image tag here), Grafana dashboards (Plan 5), example scenarios (Plan 5).

---

## File Structure

```
helm/dlh-test-fw/
├── Chart.yaml                          # umbrella; lists 6 pinned dependencies
├── values.yaml                         # default overrides for each subchart + our knobs
├── values-minikube.yaml                # local-dev overrides (ingress hosts, low resources)
├── README.md                           # install / upgrade / uninstall
├── templates/
│   ├── _helpers.tpl                    # name/labels helpers
│   ├── namespace.yaml                  # the dlh-test-fw ns itself
│   ├── rbac-litmus-cluster-admin-lite.yaml  # ClusterRole + Binding for litmus SA
│   ├── rbac-verdict.yaml               # Role allowing verdict-job to patch dlh-result-* ConfigMaps + read ChaosResults
│   ├── ingress.yaml                    # argo / grafana / minio (toggle via values)
│   ├── secrets.yaml                    # minio creds, grafana admin (sealed in values)
│   ├── minio-buckets-job.yaml          # post-install Job: create fixtures + artifacts buckets
│   ├── argo-artifact-config.yaml       # ConfigMap "artifact-repositories" + ServiceAccount default-artifact-repository binding
│   └── dlh-workflowtemplates.yaml      # EMPTY placeholder; Plan 4 populates
├── crds/                               # (empty in this plan; subcharts ship their own)
└── tests/                              # helm test hooks
    └── platform-smoke.yaml             # `helm test` Pod: curls each UI's /health
scripts/
├── platform-up.sh                      # helm install + wait for all pods
├── platform-down.sh                    # helm uninstall + cleanup PVCs
└── platform-verify.sh                  # smoke: all pods Ready + ingress hosts answer
Makefile                                # repo-root: platform-up / platform-down / platform-verify
```

Responsibilities:
- `Chart.yaml` — single source of truth for subchart versions (matches Plan 1 FINDINGS.md).
- `values.yaml` — production-ish defaults (used when someone deploys to a non-minikube cluster later).
- `values-minikube.yaml` — local-dev: lower resource requests, ingress hosts at `*.dlh.local`, persistence disabled where safe.
- `templates/rbac-litmus-cluster-admin-lite.yaml` — addresses spec's RBAC concern (observation 294).
- `templates/minio-buckets-job.yaml` — Helm post-install hook Job that runs `mc mb` to create `fixtures` and `artifacts` buckets.
- `templates/argo-artifact-config.yaml` — wires Argo's default artifact repository to MinIO's `artifacts` bucket so verdict reports get archived automatically.
- `templates/dlh-workflowtemplates.yaml` — exists as an empty `{{- /* populated by Plan 4 */ -}}` so Plan 4 has a known file to edit and our chart smoke-render passes today.
- `scripts/platform-verify.sh` — executable success criterion for this plan.

---

## Pinned Subchart Versions

These must match `spikes/k6-vm-remote-write/FINDINGS.md` for `victoria-metrics-single` and `k6-operator`. The other four are pinned here for the first time; the engineer should `helm search repo --versions` to confirm availability before locking.

| Subchart | Helm repo | Version (target) | Notes |
|---|---|---|---|
| `argo-workflows` | `https://argoproj.github.io/argo-helm` | `0.42.x` | charts repo: `argo/argo-workflows` |
| `litmus` (ChaosCenter) | `https://litmuschaos.github.io/litmus-helm/` | `3.x` | full chaoscenter with mongo + frontend |
| `k6-operator` | `https://grafana.github.io/helm-charts` | **4.4.1** (per FINDINGS — chart 3.x is no longer available) | reuse Plan 1; `namespace.watch` value was removed in 4.x |
| `minio` | `https://charts.bitnami.com/bitnami` | `14.x` | use Bitnami chart (single-mode for minikube) |
| `victoria-metrics-single` | `https://victoriametrics.github.io/helm-charts/` | **0.38.0** (per FINDINGS — chart 0.12.x is no longer in the repo) | reuse Plan 1 |
| `grafana` | `https://grafana.github.io/helm-charts` | `8.x` | sidecar disabled for now; dashboards in Plan 5 |

**If any pinned version is unavailable**, pick the closest patch in the same minor and update both this plan's table and `Chart.yaml` in the same commit.

---

## Task 1: Chart skeleton + helpers

**Files:**
- Create: `helm/dlh-test-fw/Chart.yaml`
- Create: `helm/dlh-test-fw/templates/_helpers.tpl`
- Create: `helm/dlh-test-fw/templates/namespace.yaml`
- Create: `helm/dlh-test-fw/templates/dlh-workflowtemplates.yaml` (empty placeholder)
- Create: `helm/dlh-test-fw/README.md`

- [ ] **Step 1: Write `helm/dlh-test-fw/Chart.yaml`**

```yaml
apiVersion: v2
name: dlh-test-fw
description: Chaos + Load Test Platform — umbrella chart (Argo + Litmus + k6 + MinIO + VM + Grafana)
type: application
version: 0.1.0
appVersion: "0.1.0"

dependencies:
- name: argo-workflows
  version: 0.42.0
  repository: https://argoproj.github.io/argo-helm
  condition: argo-workflows.enabled
- name: litmus
  version: 3.5.0
  repository: https://litmuschaos.github.io/litmus-helm/
  condition: litmus.enabled
- name: k6-operator
  version: 4.4.1
  repository: https://grafana.github.io/helm-charts
  condition: k6-operator.enabled
- name: minio
  version: 14.6.0
  repository: https://charts.bitnami.com/bitnami
  condition: minio.enabled
- name: victoria-metrics-single
  version: 0.38.0
  repository: https://victoriametrics.github.io/helm-charts/
  condition: victoria-metrics-single.enabled
- name: grafana
  version: 8.5.0
  repository: https://grafana.github.io/helm-charts
  condition: grafana.enabled
```

- [ ] **Step 2: Write `_helpers.tpl`**

```yaml
{{/* Common labels for our own resources. */}}
{{- define "dlh.labels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
{{- end }}

{{- define "dlh.namespace" -}}
{{ .Values.namespace | default "dlh-test-fw" }}
{{- end }}
```

- [ ] **Step 3: Write `templates/namespace.yaml`**

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: {{ include "dlh.namespace" . }}
  labels:
    {{- include "dlh.labels" . | nindent 4 }}
```

- [ ] **Step 4: Write empty `templates/dlh-workflowtemplates.yaml`**

```yaml
{{- /*
  Placeholder. Populated by Plan 4 (WorkflowTemplate library).
  Kept as a known file so Plan 4 has a stable path to edit.
*/ -}}
```

- [ ] **Step 5: Write `helm/dlh-test-fw/README.md`**

```markdown
# dlh-test-fw

Umbrella Helm chart for the Chaos + Load Test Platform.

## Quick start (minikube)

    helm dependency update helm/dlh-test-fw
    helm upgrade --install dlh helm/dlh-test-fw \
      -n dlh-test-fw --create-namespace \
      -f helm/dlh-test-fw/values-minikube.yaml --wait

## Smoke test

    make platform-verify

## Uninstall

    make platform-down
```

- [ ] **Step 6: helm-lint sanity check**

```bash
cd /Users/allen/repo/dlh-test-fw
helm lint helm/dlh-test-fw
```

Expected: `0 chart(s) failed`. Errors must be fixed inline before commit.

- [ ] **Step 7: Commit**

```bash
git add helm/dlh-test-fw/Chart.yaml \
        helm/dlh-test-fw/templates/_helpers.tpl \
        helm/dlh-test-fw/templates/namespace.yaml \
        helm/dlh-test-fw/templates/dlh-workflowtemplates.yaml \
        helm/dlh-test-fw/README.md
git commit -m "chart: scaffold umbrella chart with pinned subchart deps"
```

---

## Task 2: Production-style values.yaml

**Files:**
- Create: `helm/dlh-test-fw/values.yaml`

These are the values that would survive on a real cluster. `values-minikube.yaml` (Task 3) layers minikube-specific tweaks on top.

- [ ] **Step 1: Write `values.yaml`**

```yaml
namespace: dlh-test-fw

# Knobs that our own templates read (NOT subchart values).
platform:
  vm:
    # Used by Plan 4's load/k6-run template. Matches FINDINGS.md service DNS.
    remoteWriteUrl: http://dlh-victoria-metrics-single-server.dlh-test-fw.svc.cluster.local:8428/api/v1/write
    queryUrl: http://dlh-victoria-metrics-single-server.dlh-test-fw.svc.cluster.local:8428
  chaosHub:
    url: https://github.com/litmuschaos/chaos-charts.git
    ref: master
  verdict:
    image: ghcr.io/dlh/dlh-verdict
    tag: 0.1.0
  ingress:
    enabled: true
    className: nginx
    hosts:
      argo: argo.dlh.local
      grafana: grafana.dlh.local
      minio: minio.dlh.local

# ---------- subchart overrides ----------

argo-workflows:
  enabled: true
  server:
    extraArgs: ["--auth-mode=server"]   # no SSO — confirmed open question 4
    serviceType: ClusterIP
  controller:
    workflowDefaults:
      spec:
        # Default artifact repo wired to MinIO (configured by templates/argo-artifact-config.yaml).
        artifactRepositoryRef:
          configMap: artifact-repositories
          key: default-v1
  workflow:
    serviceAccount:
      create: true
      name: argo-workflow

litmus:
  enabled: true
  portal:
    frontend:
      service:
        type: ClusterIP
  mongo:
    persistence:
      enabled: false                    # spike default; override in values-prod.yaml later

k6-operator:
  enabled: true
  manager:
    resources:
      requests: { cpu: 50m, memory: 64Mi }
      limits:   { cpu: 200m, memory: 128Mi }
  namespace:
    # Chart 3.x had `namespace.watch`; chart 4.x removed it (operator watches
    # all namespaces). Don't re-create the umbrella namespace — Helm-managed.
    create: false

minio:
  enabled: true
  mode: standalone
  auth:
    rootUser: admin
    existingSecret: minio-root-credentials   # created by templates/secrets.yaml
  defaultBuckets: ""                    # we provision via post-install Job for explicitness
  persistence:
    enabled: true
    size: 10Gi
  service:
    type: ClusterIP

victoria-metrics-single:
  enabled: true
  server:
    retentionPeriod: 30d
    persistentVolume:
      enabled: true
      size: 10Gi
    extraArgs:
      search.maxUniqueTimeseries: "100000"
  service:
    type: ClusterIP

grafana:
  enabled: true
  admin:
    existingSecret: grafana-admin-credentials
    userKey: admin-user
    passwordKey: admin-password
  service:
    type: ClusterIP
  datasources:
    datasources.yaml:
      apiVersion: 1
      datasources:
      - name: VictoriaMetrics
        type: prometheus
        access: proxy
        url: http://dlh-victoria-metrics-single-server.dlh-test-fw.svc.cluster.local:8428
        isDefault: true
      - name: Infinity                  # for reading dlh-result-* ConfigMaps in Plan 5
        type: yesoreyeram-infinity-datasource
        access: proxy
  plugins:
  - yesoreyeram-infinity-datasource
  sidecar:
    dashboards:
      enabled: true                     # Plan 5 will drop dashboards as ConfigMaps with label
      label: dlh-dashboard
```

- [ ] **Step 2: Re-run helm lint**

```bash
helm lint helm/dlh-test-fw
```

Expected: `0 chart(s) failed`. Warnings about missing dependency tarballs are fine — we update deps in Task 5.

- [ ] **Step 3: Commit**

```bash
git add helm/dlh-test-fw/values.yaml
git commit -m "chart: production-style values.yaml"
```

---

## Task 3: Minikube overlay values

**Files:**
- Create: `helm/dlh-test-fw/values-minikube.yaml`

- [ ] **Step 1: Write `values-minikube.yaml`**

```yaml
# Overrides for local minikube. Apply with `-f values.yaml -f values-minikube.yaml`,
# or rely on Makefile which passes both.
platform:
  ingress:
    enabled: true                       # minikube has the ingress addon
    className: nginx                    # minikube's ingress addon uses ingress-nginx

minio:
  persistence:
    enabled: false                      # ephemeral — minikube restarts blow away PV anyway
  resources:
    requests: { cpu: 100m, memory: 256Mi }
    limits:   { cpu: 500m, memory: 512Mi }

victoria-metrics-single:
  server:
    persistentVolume:
      enabled: false
    resources:
      requests: { cpu: 100m, memory: 256Mi }
      limits:   { cpu: 500m, memory: 512Mi }

litmus:
  mongo:
    persistence:
      enabled: false
    resources:
      requests: { cpu: 100m, memory: 256Mi }
      limits:   { cpu: 500m, memory: 768Mi }

grafana:
  persistence:
    enabled: false
  resources:
    requests: { cpu: 50m, memory: 128Mi }
    limits:   { cpu: 200m, memory: 256Mi }

argo-workflows:
  controller:
    resources:
      requests: { cpu: 50m, memory: 64Mi }
      limits:   { cpu: 200m, memory: 128Mi }
  server:
    resources:
      requests: { cpu: 50m, memory: 64Mi }
      limits:   { cpu: 200m, memory: 128Mi }
```

- [ ] **Step 2: Commit**

```bash
git add helm/dlh-test-fw/values-minikube.yaml
git commit -m "chart: minikube overlay values (low resources, ephemeral)"
```

---

## Task 4: Litmus cluster-admin-lite RBAC

**Files:**
- Create: `helm/dlh-test-fw/templates/rbac-litmus-cluster-admin-lite.yaml`

Spec section "RBAC" calls out that Litmus' SA needs cross-namespace inject permissions and that an over-tight read-only ClusterRole previously caused failures (observation 294). We provide a deliberately-named ClusterRole and bind Litmus' subject account to it.

- [ ] **Step 1: Write the RBAC template**

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: {{ .Release.Name }}-litmus-cluster-admin-lite
  labels:
    {{- include "dlh.labels" . | nindent 4 }}
rules:
# Pod-level chaos: kill, exec into, monitor.
- apiGroups: [""]
  resources: ["pods", "pods/exec", "pods/log", "pods/eviction"]
  verbs: ["get", "list", "watch", "create", "delete", "deletecollection"]
# Network chaos uses NetworkPolicies + traffic-control via privileged sidecars.
- apiGroups: ["networking.k8s.io"]
  resources: ["networkpolicies"]
  verbs: ["get", "list", "watch", "create", "update", "delete"]
# Litmus CRs.
- apiGroups: ["litmuschaos.io"]
  resources: ["chaosengines", "chaosexperiments", "chaosresults"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
# Read deployments/statefulsets to discover targets.
- apiGroups: ["apps"]
  resources: ["deployments", "statefulsets", "daemonsets", "replicasets"]
  verbs: ["get", "list", "watch"]
# Events for visibility.
- apiGroups: [""]
  resources: ["events"]
  verbs: ["create", "patch"]
- apiGroups: [""]
  resources: ["configmaps", "secrets"]
  verbs: ["get", "list"]
- apiGroups: [""]
  resources: ["services", "namespaces"]
  verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: {{ .Release.Name }}-litmus-cluster-admin-lite
  labels:
    {{- include "dlh.labels" . | nindent 4 }}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: {{ .Release.Name }}-litmus-cluster-admin-lite
subjects:
- kind: ServiceAccount
  name: litmus
  namespace: {{ include "dlh.namespace" . }}
```

- [ ] **Step 2: Render and inspect**

```bash
helm template dlh helm/dlh-test-fw -n dlh-test-fw \
  -f helm/dlh-test-fw/values.yaml -f helm/dlh-test-fw/values-minikube.yaml \
  --show-only templates/rbac-litmus-cluster-admin-lite.yaml
```

Expected: prints a valid ClusterRole and ClusterRoleBinding. No `<no value>` strings.

- [ ] **Step 3: Commit**

```bash
git add helm/dlh-test-fw/templates/rbac-litmus-cluster-admin-lite.yaml
git commit -m "chart: cluster-admin-lite ClusterRole for litmus SA"
```

---

## Task 5: Verdict-job RBAC

**Files:**
- Create: `helm/dlh-test-fw/templates/rbac-verdict.yaml`

Plan 3's verdict binary needs:
- read access to `ChaosResult` CRs (to fetch chaos verdict)
- patch access to `ConfigMap` whose name matches `dlh-result-*` (to publish the result summary)
- create/get on its own logs (default for any pod, no extra grant needed)

Plan 4's `verdict/slo-eval` WorkflowTemplate runs as the `argo-workflow` ServiceAccount (created by argo-workflows subchart). We add the verdict-specific permissions to that SA via a namespace-scoped Role.

- [ ] **Step 1: Write the RBAC template**

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: {{ .Release.Name }}-verdict
  namespace: {{ include "dlh.namespace" . }}
  labels:
    {{- include "dlh.labels" . | nindent 4 }}
rules:
- apiGroups: ["litmuschaos.io"]
  resources: ["chaosresults"]
  verbs: ["get", "list", "watch"]
- apiGroups: [""]
  resources: ["configmaps"]
  verbs: ["get", "create", "patch", "update"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: {{ .Release.Name }}-verdict
  namespace: {{ include "dlh.namespace" . }}
  labels:
    {{- include "dlh.labels" . | nindent 4 }}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: {{ .Release.Name }}-verdict
subjects:
- kind: ServiceAccount
  name: argo-workflow
  namespace: {{ include "dlh.namespace" . }}
```

- [ ] **Step 2: Render + lint**

```bash
helm template dlh helm/dlh-test-fw -n dlh-test-fw \
  -f helm/dlh-test-fw/values.yaml --show-only templates/rbac-verdict.yaml
helm lint helm/dlh-test-fw
```

Expected: clean render; lint passes.

- [ ] **Step 3: Commit**

```bash
git add helm/dlh-test-fw/templates/rbac-verdict.yaml
git commit -m "chart: verdict-job RBAC (read ChaosResults, patch dlh-result ConfigMaps)"
```

---

## Task 6: Secrets

**Files:**
- Create: `helm/dlh-test-fw/templates/secrets.yaml`

For Phase 1 / minikube local we generate dev credentials inline. **In a real deploy, replace these with `existingSecret` references + externally-managed secrets.** A comment in the template documents this.

- [ ] **Step 1: Write `templates/secrets.yaml`**

```yaml
{{- /*
  Phase 1 / minikube: generate static dev secrets if user hasn't provided their own.
  In a real cluster, set platform.secrets.useExternal=true and create the secrets
  out of band (sealed-secrets, vault, etc.) — this template will then no-op.
*/ -}}
{{- if not .Values.platform.secrets.useExternal -}}
apiVersion: v1
kind: Secret
metadata:
  name: minio-root-credentials
  namespace: {{ include "dlh.namespace" . }}
  labels: {{ include "dlh.labels" . | nindent 4 }}
type: Opaque
stringData:
  root-user: {{ .Values.platform.secrets.minio.user | default "admin" }}
  root-password: {{ .Values.platform.secrets.minio.password | default "dlh-dev-secret-please-rotate" }}
---
apiVersion: v1
kind: Secret
metadata:
  name: minio-artifact-creds
  namespace: {{ include "dlh.namespace" . }}
  labels: {{ include "dlh.labels" . | nindent 4 }}
type: Opaque
stringData:
  accesskey: {{ .Values.platform.secrets.minio.user | default "admin" }}
  secretkey: {{ .Values.platform.secrets.minio.password | default "dlh-dev-secret-please-rotate" }}
---
apiVersion: v1
kind: Secret
metadata:
  name: grafana-admin-credentials
  namespace: {{ include "dlh.namespace" . }}
  labels: {{ include "dlh.labels" . | nindent 4 }}
type: Opaque
stringData:
  admin-user: {{ .Values.platform.secrets.grafana.user | default "admin" }}
  admin-password: {{ .Values.platform.secrets.grafana.password | default "dlh-dev-secret-please-rotate" }}
{{- end }}
```

- [ ] **Step 2: Add the matching `platform.secrets` block to `values.yaml`**

Edit `helm/dlh-test-fw/values.yaml` and insert under `platform:`:

```yaml
  secrets:
    useExternal: false
    minio:
      user: admin
      password: dlh-dev-secret-please-rotate
    grafana:
      user: admin
      password: dlh-dev-secret-please-rotate
```

- [ ] **Step 3: Render + verify all three secrets present**

```bash
helm template dlh helm/dlh-test-fw -n dlh-test-fw \
  -f helm/dlh-test-fw/values.yaml --show-only templates/secrets.yaml | grep -c '^kind: Secret'
```

Expected: `3`.

- [ ] **Step 4: Commit**

```bash
git add helm/dlh-test-fw/templates/secrets.yaml helm/dlh-test-fw/values.yaml
git commit -m "chart: dev secrets for minio + grafana (useExternal toggle for prod)"
```

---

## Task 7: MinIO bucket bootstrap Job

**Files:**
- Create: `helm/dlh-test-fw/templates/minio-buckets-job.yaml`

We run `mc mb` against the freshly-deployed MinIO to create `fixtures` and `artifacts` buckets. Helm `post-install,post-upgrade` hook ensures it runs after MinIO is up; `helm.sh/hook-delete-policy: hook-succeeded` keeps the namespace clean.

- [ ] **Step 1: Write the Job template**

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: {{ .Release.Name }}-minio-bootstrap
  namespace: {{ include "dlh.namespace" . }}
  labels: {{ include "dlh.labels" . | nindent 4 }}
  annotations:
    "helm.sh/hook": post-install,post-upgrade
    "helm.sh/hook-weight": "5"
    "helm.sh/hook-delete-policy": hook-succeeded
spec:
  backoffLimit: 5
  template:
    spec:
      restartPolicy: OnFailure
      containers:
      - name: mc
        image: minio/mc:RELEASE.2024-11-21T17-21-54Z
        env:
        - name: MINIO_USER
          valueFrom: { secretKeyRef: { name: minio-root-credentials, key: root-user } }
        - name: MINIO_PASSWORD
          valueFrom: { secretKeyRef: { name: minio-root-credentials, key: root-password } }
        command: ["/bin/sh", "-c"]
        args:
        - |
          set -euo pipefail
          # Wait for MinIO Service to answer.
          for i in $(seq 1 30); do
            if mc alias set dlh http://dlh-minio.dlh-test-fw.svc.cluster.local:9000 \
                 "$MINIO_USER" "$MINIO_PASSWORD" >/dev/null 2>&1; then
              break
            fi
            echo "waiting for minio… ($i)"
            sleep 5
          done
          mc mb --ignore-existing dlh/fixtures
          mc mb --ignore-existing dlh/artifacts
          mc anonymous set download dlh/artifacts || true
          mc ls dlh
```

- [ ] **Step 2: Render + inspect**

```bash
helm template dlh helm/dlh-test-fw -n dlh-test-fw \
  -f helm/dlh-test-fw/values.yaml --show-only templates/minio-buckets-job.yaml
```

Expected: Job with hook annotations and the two `mc mb` calls visible.

- [ ] **Step 3: Commit**

```bash
git add helm/dlh-test-fw/templates/minio-buckets-job.yaml
git commit -m "chart: post-install Job to create fixtures + artifacts buckets"
```

---

## Task 8: Argo artifact repository wiring

**Files:**
- Create: `helm/dlh-test-fw/templates/argo-artifact-config.yaml`

Argo Workflows reads a ConfigMap named `artifact-repositories` (per Argo docs convention) to know where to ship artifacts. We point its `default-v1` key at the MinIO `artifacts` bucket.

- [ ] **Step 1: Write the ConfigMap**

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: artifact-repositories
  namespace: {{ include "dlh.namespace" . }}
  annotations:
    workflows.argoproj.io/default-artifact-repository: default-v1
  labels: {{ include "dlh.labels" . | nindent 4 }}
data:
  default-v1: |
    s3:
      endpoint: dlh-minio.{{ include "dlh.namespace" . }}.svc.cluster.local:9000
      bucket: artifacts
      insecure: true
      accessKeySecret:
        name: minio-artifact-creds
        key: accesskey
      secretKeySecret:
        name: minio-artifact-creds
        key: secretkey
```

- [ ] **Step 2: Render + verify**

```bash
helm template dlh helm/dlh-test-fw -n dlh-test-fw \
  -f helm/dlh-test-fw/values.yaml --show-only templates/argo-artifact-config.yaml
```

Expected: ConfigMap with `default-v1:` key, MinIO endpoint, and both secret refs resolved.

- [ ] **Step 3: Commit**

```bash
git add helm/dlh-test-fw/templates/argo-artifact-config.yaml
git commit -m "chart: wire Argo default artifact repo to MinIO artifacts bucket"
```

---

## Task 9: Ingress

**Files:**
- Create: `helm/dlh-test-fw/templates/ingress.yaml`

Three Ingresses, gated on `platform.ingress.enabled`.

- [ ] **Step 1: Write `templates/ingress.yaml`**

```yaml
{{- if .Values.platform.ingress.enabled -}}
{{- $svcMap := dict "argo" (dict "host" .Values.platform.ingress.hosts.argo "svc" "dlh-argo-workflows-server" "port" 2746)
                   "grafana" (dict "host" .Values.platform.ingress.hosts.grafana "svc" "dlh-grafana" "port" 80)
                   "minio" (dict "host" .Values.platform.ingress.hosts.minio "svc" "dlh-minio-console" "port" 9001) -}}
{{- range $key, $cfg := $svcMap }}
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: dlh-{{ $key }}
  namespace: {{ include "dlh.namespace" $ }}
  labels: {{- include "dlh.labels" $ | nindent 4 }}
spec:
  ingressClassName: {{ $.Values.platform.ingress.className }}
  rules:
  - host: {{ $cfg.host }}
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: {{ $cfg.svc }}
            port:
              number: {{ $cfg.port }}
{{- end -}}
{{- end -}}
```

**Engineer note:** Service names (`dlh-argo-workflows-server`, `dlh-grafana`, `dlh-minio-console`) depend on each subchart's naming convention. After `helm install` in Task 12, run `kubectl -n dlh-test-fw get svc` and confirm. If a name differs, update this template and re-deploy.

- [ ] **Step 2: Render + verify three ingresses**

```bash
helm template dlh helm/dlh-test-fw -n dlh-test-fw \
  -f helm/dlh-test-fw/values.yaml -f helm/dlh-test-fw/values-minikube.yaml \
  --show-only templates/ingress.yaml | grep -c '^kind: Ingress'
```

Expected: `3`.

- [ ] **Step 3: Commit**

```bash
git add helm/dlh-test-fw/templates/ingress.yaml
git commit -m "chart: ingress for argo / grafana / minio consoles"
```

---

## Task 10: helm test smoke pod

**Files:**
- Create: `helm/dlh-test-fw/tests/platform-smoke.yaml`

`helm test` runs Pods marked with the `helm.sh/hook: test` annotation. Ours `curl`s each UI's healthz from inside the cluster.

- [ ] **Step 1: Write the smoke Pod**

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: {{ .Release.Name }}-platform-smoke
  namespace: {{ include "dlh.namespace" . }}
  annotations:
    "helm.sh/hook": test
    "helm.sh/hook-delete-policy": hook-succeeded
spec:
  restartPolicy: Never
  containers:
  - name: smoke
    image: curlimages/curl:8.8.0
    command: ["/bin/sh", "-c"]
    args:
    - |
      set -euo pipefail
      fail=0
      check() {
        local url=$1
        if curl -sf --max-time 5 "$url" >/dev/null; then
          echo "OK   $url"
        else
          echo "FAIL $url" >&2
          fail=1
        fi
      }
      check http://dlh-argo-workflows-server.dlh-test-fw.svc.cluster.local:2746/
      check http://dlh-grafana.dlh-test-fw.svc.cluster.local:80/api/health
      check http://dlh-minio.dlh-test-fw.svc.cluster.local:9000/minio/health/live
      check http://dlh-victoria-metrics-single-server.dlh-test-fw.svc.cluster.local:8428/health
      exit $fail
```

- [ ] **Step 2: Commit**

```bash
git add helm/dlh-test-fw/tests/platform-smoke.yaml
git commit -m "chart: helm test pod that curls each UI's health endpoint"
```

---

## Task 11: Repo-root scripts + Makefile

**Files:**
- Create: `scripts/platform-up.sh`
- Create: `scripts/platform-down.sh`
- Create: `scripts/platform-verify.sh`
- Create or modify: `Makefile` (repo root)

- [ ] **Step 1: Write `scripts/platform-up.sh`**

```bash
#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

# Pre-flight: minikube must be Ready.
if ! kubectl get nodes 2>/dev/null | grep -q ' Ready '; then
  echo "minikube not Ready. Run: ./spikes/k6-vm-remote-write/scripts/minikube-up.sh" >&2
  exit 1
fi

helm repo add argo https://argoproj.github.io/argo-helm || true
helm repo add litmuschaos https://litmuschaos.github.io/litmus-helm/ || true
helm repo add grafana https://grafana.github.io/helm-charts || true
helm repo add bitnami https://charts.bitnami.com/bitnami || true
helm repo add victoria-metrics https://victoriametrics.github.io/helm-charts/ || true
helm repo update

helm dependency update helm/dlh-test-fw

helm upgrade --install dlh helm/dlh-test-fw \
  -n dlh-test-fw --create-namespace \
  -f helm/dlh-test-fw/values.yaml \
  -f helm/dlh-test-fw/values-minikube.yaml \
  --wait --timeout 10m
```

- [ ] **Step 2: Write `scripts/platform-down.sh`**

```bash
#!/usr/bin/env bash
set -euo pipefail
helm uninstall dlh -n dlh-test-fw || true
# Don't delete the namespace; user may want to inspect remaining state.
echo "uninstalled. To wipe entirely: kubectl delete ns dlh-test-fw"
```

- [ ] **Step 3: Write `scripts/platform-verify.sh`**

```bash
#!/usr/bin/env bash
set -euo pipefail

NS=dlh-test-fw

# Step 1: every Helm-managed pod Ready.
echo "==> waiting for all pods Ready"
kubectl -n "$NS" wait --for=condition=Ready pod --all --timeout=300s

# Step 2: run helm test.
echo "==> helm test"
helm test dlh -n "$NS" --timeout 5m

# Step 3: ingress reachability via minikube IP + /etc/hosts hint.
MIP=$(minikube ip)
echo "==> ingress hosts should resolve via: $MIP"
echo "    Add to /etc/hosts (if not already):"
echo "    $MIP argo.dlh.local grafana.dlh.local minio.dlh.local"

# Step 4: curl through ingress (with Host header override so /etc/hosts isn't required).
for host in argo.dlh.local grafana.dlh.local minio.dlh.local; do
  code=$(curl -sk -o /dev/null -w "%{http_code}" --resolve "$host:80:$MIP" "http://$host/" || true)
  echo "    $host -> HTTP $code"
done

echo "PASS"
```

- [ ] **Step 4: Make scripts executable**

```bash
chmod +x scripts/platform-up.sh scripts/platform-down.sh scripts/platform-verify.sh
```

- [ ] **Step 5: Add Makefile targets**

```makefile
# Append to repo-root Makefile (create if absent)
SHELL := /usr/bin/env bash
.PHONY: platform-up platform-down platform-verify chart-lint

chart-lint:
	helm lint helm/dlh-test-fw

platform-up: chart-lint
	./scripts/platform-up.sh

platform-down:
	./scripts/platform-down.sh

platform-verify:
	./scripts/platform-verify.sh
```

- [ ] **Step 6: Commit**

```bash
git add scripts/platform-up.sh scripts/platform-down.sh scripts/platform-verify.sh Makefile
git commit -m "chart: install / uninstall / verify scripts and Makefile targets"
```

---

## Task 12: End-to-end install (the "test passes")

- [ ] **Step 1: Lint**

```bash
make chart-lint
```

Expected: `0 chart(s) failed`.

- [ ] **Step 2: Install**

```bash
make platform-up
```

Expected: `helm upgrade --install` succeeds within 10 minutes; all pods Ready.

If pods don't go Ready:
- `kubectl -n dlh-test-fw get pods` to see which one is stuck.
- `kubectl -n dlh-test-fw describe pod <stuck>` and `logs`.
- Common: minikube ran out of RAM/CPU → reduce limits in `values-minikube.yaml` or stop one subchart (`litmus.enabled=false` is the heaviest).

- [ ] **Step 3: Verify**

```bash
make platform-verify
```

Expected: ends with `PASS`. All four healthz checks should return `200` (Argo may return `302` to login — acceptable; tweak verify.sh if so).

- [ ] **Step 4: Manual UI check (optional but recommended)**

Add the `/etc/hosts` line printed by `platform-verify.sh`, then visit:
- `http://argo.dlh.local` — Argo workflow list (empty)
- `http://grafana.dlh.local` — Grafana login (admin / dev secret from values)
- `http://minio.dlh.local` — MinIO console (admin / dev secret); confirm `fixtures` and `artifacts` buckets exist.

- [ ] **Step 5: Service-name reality check**

```bash
kubectl -n dlh-test-fw get svc -o wide
```

Compare against names referenced in `templates/ingress.yaml`, `templates/platform-smoke.yaml`, `values.yaml` (`platform.vm.remoteWriteUrl`, `platform.vm.queryUrl`), and `templates/argo-artifact-config.yaml`. **If any name differs, fix it in the template and re-run `make platform-up` before proceeding.** Stale DNS names here will silently break Plans 3-5.

- [ ] **Step 6: No code change → no commit. (If you adjusted service names in Step 5, commit those.)**

---

## Task 13: Update FINDINGS with chart-wide observations

**Files:**
- Modify: `spikes/k6-vm-remote-write/FINDINGS.md` (append a section)

- [ ] **Step 1: Append to FINDINGS.md**

```markdown
## Platform chart observations (from Plan 2)

- Confirmed service names (post-install):
    - argo server:  <fill>
    - grafana:      <fill>
    - minio API:    <fill>
    - minio console:<fill>
    - VM server:    <fill>
- Helm `--wait` timeout needed: <fill, default 10m sufficient?>
- Litmus chaoscenter brought up <fill> pods; mongo PVC size used: <fill>
- Total minikube memory consumed at idle: <fill, kubectl top nodes>
```

- [ ] **Step 2: Commit**

```bash
git add spikes/k6-vm-remote-write/FINDINGS.md
git commit -m "findings: append platform chart deploy observations"
```

---

## Definition of Done

- [ ] `helm lint helm/dlh-test-fw` passes.
- [ ] `make platform-up && make platform-verify` succeeds from a fresh `make down && spikes/k6-vm-remote-write/scripts/minikube-up.sh`.
- [ ] `kubectl -n dlh-test-fw get pods` shows every pod Ready.
- [ ] MinIO contains `fixtures` and `artifacts` buckets (visible in console).
- [ ] Argo's `artifact-repositories` ConfigMap references those buckets.
- [ ] Service names referenced in chart templates match actual deployed services (Task 12 Step 5).
- [ ] FINDINGS.md "Platform chart observations" section is filled in.

---

## Self-Review Notes

- **Spec coverage:** Implements spec sections "Repository 結構" (`helm/dlh-test-fw/` tree), "Deploy 拓樸" (minikube + ingress + values), "RBAC" (cluster-admin-lite ClusterRole), Helm values block (`chaos_hub`, `minio.buckets`, `victoriametrics`, `verdict`). Leaves WorkflowTemplate body to Plan 4 (placeholder file exists), dashboards to Plan 5.
- **Placeholders:** Only inside FINDINGS append section (intentional — engineer fills after deploy).
- **Type consistency:** ServiceAccount `argo-workflow` referenced in Task 5 RBAC matches the name we tell `argo-workflows` subchart to create in `values.yaml`. ConfigMap names `artifact-repositories` and `dlh-result-*` are used identically across Tasks 5 and 8. VM service DNS `dlh-victoria-metrics-single-server.dlh-test-fw.svc.cluster.local:8428` appears in `values.yaml`, ingress template, smoke pod, datasources — all consistent. **Risk:** the literal `dlh-` prefix assumes Helm release name == `dlh`; Task 12 Step 5 explicitly validates this and instructs correction. If a future deploy uses a different release name, every reference must be parameterised — out of scope for this plan.
