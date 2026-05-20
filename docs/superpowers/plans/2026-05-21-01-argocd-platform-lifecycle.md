# Argo CD Platform Lifecycle — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Introduce an `argocd/` directory with an AppProject + ApplicationSet + two Applications (umbrella chart and a controlplane placeholder) so the framework cluster can be bootstrapped and reconciled from git without `helm` / `kubectl` shell access; annotate existing shell scripts as local-dev only; document the production bootstrap procedure.

**Architecture:** The umbrella chart in `helm/dlh-test-fw/` stays as-is for Phase A. A new `argocd/` directory defines (a) an `AppProject` scoping cluster + namespace access, (b) a `dlh-test-fw-chart` Application that sources the umbrella chart with a production-shaped values overlay, (c) a `dlh-controlplane` Application placeholder reserved for the companion spec, (d) an `ApplicationSet` referencing the two Applications as a single bootstrap unit. CI is extended to render and `kubeconform`-validate the Argo CD manifests so drift breaks the build. Splitting chaos-mesh / WorkflowTemplates / dashboards into independent Applications is **deferred** to a later phase — the spec recommended it but called it out as a plan-time decision; for this phase, the umbrella chart as a single sync unit is the smaller, lower-risk delivery.

**Tech Stack:** Argo CD v2.x manifests (apiVersion `argoproj.io/v1alpha1`), Helm 4 (existing umbrella chart), kubeconform (existing CI), GitHub Actions (existing CI).

**Reference spec:** `docs/superpowers/specs/2026-05-21-argocd-platform-lifecycle-design.md`. Re-read §3 non-goals, §5.2 Application boundaries (note the plan-time decision below), and §10 rollback before starting.

**Branch & worktree:** Per `CLAUDE.md` conventions, create a feature worktree at `../dlh-test-fw-plan14` on branch `feat/plan14-argocd-platform-lifecycle` before Task 2. Task 1 runs from the main worktree.

**Plan-time decisions (deviations / clarifications relative to the spec):**

1. **WorkflowTemplates stay inside the umbrella chart.** Spec §5.2 recommended extracting them; that's a structural chart change that adds risk without delivering Phase A's core value (GitOps lifecycle). Defer to a follow-up plan if scenario-template churn becomes a sync-cycle problem.
2. **Dashboards stay inside the umbrella chart.** Same reasoning. Plan 13's `make sync-dashboards` workflow remains in place.
3. **Chaos Mesh stays as a chart dependency.** Same reasoning; it already has its own values block in `values.yaml` enforcing the docker-runtime override (FINDINGS.md #5).
4. **Secret backend deferred.** Spec §5.6 leaves this open. The framework values file uses placeholder credentials with a `useExternal:` hook the chart already exposes; choosing sealed-secrets vs external-secrets is out of scope.
5. **CI bootstrap-from-clean-kind job is omitted.** Spec §6 flagged it as desirable-but-heavy; adding kind to CI is a 5-minute build cost on every PR. The plan extends the existing `kubeconform` job to lint the Argo CD manifests, which catches the bulk of regressions without the runtime cost.

---

## File Structure

**New files:**
- `argocd/appproject.yaml` — AppProject scoping cluster + namespace + repo access.
- `argocd/apps/dlh-test-fw-chart.yaml` — Application: umbrella chart sync.
- `argocd/apps/dlh-controlplane.yaml` — Application placeholder for companion spec.
- `argocd/appset/dlh-platform.yaml` — ApplicationSet aggregating the two Applications.
- `argocd/values/framework/chart-values.yaml` — production-shaped values overlay.
- `argocd/README.md` — short orientation file pointing at the bootstrap doc.
- `docs/operations/bootstrap-via-argocd.md` — step-by-step production bootstrap procedure.

**Modified files:**
- `.github/workflows/ci.yml` — extend the `kubeconform` job to also validate `argocd/**.yaml`.
- `scripts/platform-up.sh` — header annotation `# local-dev only`.
- `scripts/platform-down.sh` — header annotation.
- `scripts/platform-verify.sh` — header annotation.
- `scripts/verify-templates.sh` — header annotation.
- `CLAUDE.md` — append a new section describing the GitOps operational model.
- `docs/FINDINGS.md` — append the Phase A operational shift findings.
- `README.md` — add a Plan 14 row to the plan table.

**Unchanged:** umbrella chart (`helm/dlh-test-fw/`), WorkflowTemplates, scenarios, dashboards, verdict-job, k6 image, `scripts/run-scenario.sh` (companion spec replaces it), `scripts/minikube-up.sh` (local-dev only by design).

---

## Task 1: Baseline + worktree creation

This task makes no commits. Confirms tree is clean, CI is green on main, then creates the feature worktree.

**Files:** None modified.

Work from: `/Users/allen/repo/dlh-test-fw` (main worktree, branch `main`).

- [ ] **Step 1: Confirm clean tree + recent state**

```bash
cd /Users/allen/repo/dlh-test-fw
git status
git log --first-parent --oneline -5
```

Expected: clean tree on `main`; HEAD includes `263acea` (the two spec files committed) or newer.

- [ ] **Step 2: Confirm CI is green on main**

```bash
gh run list --branch main --limit 3
```

Expected: the most recent push has `success` for the `CI` workflow.

- [ ] **Step 3: Confirm both specs are present**

```bash
ls docs/superpowers/specs/2026-05-21-*.md
```

Expected: both `argocd-platform-lifecycle-design.md` and `dlh-controlplane-design.md` listed.

- [ ] **Step 4: Create feature worktree**

```bash
git worktree add ../dlh-test-fw-plan14 -b feat/plan14-argocd-platform-lifecycle main
cd ../dlh-test-fw-plan14
git status
```

Expected: clean tree on `feat/plan14-argocd-platform-lifecycle`. All remaining tasks run from this worktree.

- [ ] **Step 5: Sanity-check the chart still renders**

```bash
helm dependency update helm/dlh-test-fw
helm template dlh helm/dlh-test-fw > /tmp/rendered-baseline.yaml
test -s /tmp/rendered-baseline.yaml && echo "OK"
```

Expected: `OK`. The render is the baseline we must not regress.

---

## Task 2: Create argocd/ directory + AppProject

The `AppProject` defines the security scope: which git repos this project may sync from, which destination cluster + namespaces it may write to, and which resource kinds are allowed. It is the security boundary for everything else in this plan.

**Files:**
- Create: `argocd/appproject.yaml`

Work from: `/Users/allen/repo/dlh-test-fw-plan14`.

- [ ] **Step 1: Create the argocd/ directory**

```bash
mkdir -p argocd/apps argocd/appset argocd/values/framework
```

- [ ] **Step 2: Write argocd/appproject.yaml**

```yaml
apiVersion: argoproj.io/v1alpha1
kind: AppProject
metadata:
  name: dlh-test-fw
  namespace: argocd
  labels:
    app.kubernetes.io/part-of: dlh-test-fw
spec:
  description: dlh-test-fw chaos + load testing platform.
  sourceRepos:
    # Replace per-environment with the actual repo URL.
    - https://github.com/REPLACE-OWNER/dlh-test-fw.git
    # External Helm repos referenced by the umbrella chart's dependencies.
    - https://argoproj.github.io/argo-helm
    - https://charts.chaos-mesh.org
    - https://grafana.github.io/helm-charts
    - https://victoriametrics.github.io/helm-charts/
  destinations:
    - server: https://kubernetes.default.svc
      namespace: dlh-test-fw
    - server: https://kubernetes.default.svc
      namespace: chaos-mesh
    - server: https://kubernetes.default.svc
      namespace: argocd
  clusterResourceWhitelist:
    - group: ""
      kind: Namespace
    - group: rbac.authorization.k8s.io
      kind: ClusterRole
    - group: rbac.authorization.k8s.io
      kind: ClusterRoleBinding
    - group: apiextensions.k8s.io
      kind: CustomResourceDefinition
    - group: admissionregistration.k8s.io
      kind: ValidatingWebhookConfiguration
    - group: admissionregistration.k8s.io
      kind: MutatingWebhookConfiguration
  namespaceResourceWhitelist:
    - group: "*"
      kind: "*"
  orphanedResources:
    warn: true
```

- [ ] **Step 3: Render-validate the AppProject with kubeconform**

```bash
kubeconform -skip CustomResourceDefinition -strict -summary \
  -schema-location default \
  -schema-location 'https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json' \
  argocd/appproject.yaml
```

Expected: `Summary: 1 resource found … 0 errors`. (AppProject is a CRD, so it's allowed via the Datree catalog.)

If `kubeconform` isn't installed locally, this step's validation runs in CI (Task 7). Skip locally and proceed.

- [ ] **Step 4: Commit**

```bash
git add argocd/appproject.yaml
git commit -m "feat(argocd): add AppProject dlh-test-fw scoping sources + destinations"
```

---

## Task 3: Write the framework values overlay

The umbrella chart already has `values.yaml` (defaults) and `values-minikube.yaml` (laptop overrides). This task adds a third file, `argocd/values/framework/chart-values.yaml`, that the chart Application references. It's a *template* — operators copy it per environment and fill in placeholders.

**Files:**
- Create: `argocd/values/framework/chart-values.yaml`

- [ ] **Step 1: Write argocd/values/framework/chart-values.yaml**

```yaml
# Framework-cluster Helm values overlay for the dlh-test-fw umbrella chart.
#
# This file is the production-shaped sibling of helm/dlh-test-fw/values-minikube.yaml.
# Argo CD's dlh-test-fw-chart Application references it via spec.source.helm.valueFiles.
#
# Replace REPLACE-* placeholders before deploying to a real cluster. The
# umbrella chart's defaults (helm/dlh-test-fw/values.yaml) are inherited;
# only fields below override them.

platform:
  ingress:
    enabled: true
    className: nginx
    hosts:
      # Replace with real DNS for the framework cluster's ingress controller.
      argo: argo.REPLACE-DOMAIN
      grafana: grafana.REPLACE-DOMAIN
      minio: minio.REPLACE-DOMAIN

  secrets:
    # Set to true and populate secretName fields below once a secret backend
    # (sealed-secrets / external-secrets / SOPS) is chosen — see companion
    # spec §5.6.
    useExternal: false
    minio:
      user: REPLACE-MINIO-USER
      password: REPLACE-MINIO-PASSWORD
    grafana:
      user: REPLACE-GRAFANA-USER
      password: REPLACE-GRAFANA-PASSWORD

# ---------- subchart resource allocations (production-shaped) ----------
# values-minikube.yaml has dev-sized resource caps; production should
# allocate enough headroom for the recorded workload. Tune per environment.

victoria-metrics-single:
  server:
    persistentVolume:
      enabled: true
      storageClassName: REPLACE-STORAGE-CLASS
      size: 50Gi
    resources:
      requests: { cpu: 500m, memory: 1Gi }
      limits:   { cpu: 2,    memory: 4Gi }

grafana:
  persistence:
    enabled: true
    storageClassName: REPLACE-STORAGE-CLASS
    size: 10Gi
  resources:
    requests: { cpu: 100m, memory: 256Mi }
    limits:   { cpu: 500m, memory: 512Mi }

argo-workflows:
  controller:
    resources:
      requests: { cpu: 100m, memory: 128Mi }
      limits:   { cpu: 500m, memory: 512Mi }
  server:
    resources:
      requests: { cpu: 100m, memory: 128Mi }
      limits:   { cpu: 500m, memory: 512Mi }
```

- [ ] **Step 2: Confirm the chart still renders with this overlay**

```bash
helm template dlh helm/dlh-test-fw \
  -f helm/dlh-test-fw/values.yaml \
  -f argocd/values/framework/chart-values.yaml \
  > /tmp/rendered-framework.yaml
test -s /tmp/rendered-framework.yaml && echo "OK"
grep -q 'argo.REPLACE-DOMAIN' /tmp/rendered-framework.yaml && echo "ingress override applied"
```

Expected: `OK` and `ingress override applied`. The render succeeds because Helm doesn't validate placeholder strings — they're just text until ingress reconciliation, which won't run in the template step.

- [ ] **Step 3: Commit**

```bash
git add argocd/values/framework/chart-values.yaml
git commit -m "feat(argocd): add framework chart values overlay template"
```

---

## Task 4: Write the dlh-test-fw-chart Application

The Application points Argo CD at the umbrella chart with the framework values overlay applied.

**Files:**
- Create: `argocd/apps/dlh-test-fw-chart.yaml`

- [ ] **Step 1: Write argocd/apps/dlh-test-fw-chart.yaml**

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: dlh-test-fw-chart
  namespace: argocd
  labels:
    app.kubernetes.io/part-of: dlh-test-fw
  finalizers:
    - resources-finalizer.argocd.argoproj.io
spec:
  project: dlh-test-fw
  source:
    # Replace per-environment with the actual repo URL (must also be listed
    # in argocd/appproject.yaml's sourceRepos).
    repoURL: https://github.com/REPLACE-OWNER/dlh-test-fw.git
    targetRevision: main
    path: helm/dlh-test-fw
    helm:
      valueFiles:
        - ../../argocd/values/framework/chart-values.yaml
  destination:
    server: https://kubernetes.default.svc
    namespace: dlh-test-fw
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
      - ServerSideApply=true
      - ApplyOutOfSyncOnly=true
    retry:
      limit: 5
      backoff:
        duration: 5s
        factor: 2
        maxDuration: 3m
  revisionHistoryLimit: 10
```

- [ ] **Step 2: Validate the manifest renders as valid YAML**

```bash
python3 -c "import sys, yaml; list(yaml.safe_load_all(open('argocd/apps/dlh-test-fw-chart.yaml')))" && echo "valid YAML"
```

Expected: `valid YAML`.

- [ ] **Step 3: Commit**

```bash
git add argocd/apps/dlh-test-fw-chart.yaml
git commit -m "feat(argocd): add dlh-test-fw-chart Application syncing umbrella chart"
```

---

## Task 5: Write the dlh-controlplane Application placeholder

The companion spec deploys the controlplane via Argo CD. This placeholder reserves the Application name and validates the AppProject's sourceRepos + destinations cover what the controlplane will need. The `path:` points at a directory we'll create as an empty marker so Argo CD's sync is a no-op until the companion spec populates it.

**Files:**
- Create: `argocd/apps/dlh-controlplane.yaml`
- Create: `controlplane/deploy/.gitkeep` (empty marker)

- [ ] **Step 1: Create the empty marker directory**

```bash
mkdir -p controlplane/deploy
touch controlplane/deploy/.gitkeep
```

- [ ] **Step 2: Write argocd/apps/dlh-controlplane.yaml**

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: dlh-controlplane
  namespace: argocd
  labels:
    app.kubernetes.io/part-of: dlh-test-fw
  # No resources-finalizer: the placeholder owns nothing yet, so finalization
  # is unnecessary. The companion spec will add the finalizer when populating
  # controlplane/deploy/.
spec:
  project: dlh-test-fw
  source:
    repoURL: https://github.com/REPLACE-OWNER/dlh-test-fw.git
    targetRevision: main
    # Placeholder path: directory contains only .gitkeep until the companion
    # spec (2026-05-21-dlh-controlplane-design.md) populates it with the
    # controlplane Deployment + Service + Ingress manifests.
    path: controlplane/deploy
    directory:
      recurse: false
  destination:
    server: https://kubernetes.default.svc
    namespace: dlh-test-fw
  syncPolicy:
    # NOT automated yet — the placeholder has no manifests to sync, so we
    # leave sync manual until the companion spec gives it real content.
    syncOptions:
      - CreateNamespace=false
      - ServerSideApply=true
    retry:
      limit: 5
      backoff:
        duration: 5s
        factor: 2
        maxDuration: 3m
  revisionHistoryLimit: 10
```

- [ ] **Step 3: Validate the manifest renders as valid YAML**

```bash
python3 -c "import sys, yaml; list(yaml.safe_load_all(open('argocd/apps/dlh-controlplane.yaml')))" && echo "valid YAML"
```

Expected: `valid YAML`.

- [ ] **Step 4: Commit**

```bash
git add argocd/apps/dlh-controlplane.yaml controlplane/deploy/.gitkeep
git commit -m "feat(argocd): add dlh-controlplane Application placeholder

Placeholder Application reserved for the companion spec
(2026-05-21-dlh-controlplane-design.md). Source path points at
controlplane/deploy/, which contains only .gitkeep until that
spec's Phase B populates it. Sync left manual until then."
```

---

## Task 6: Write the ApplicationSet aggregator

A single ApplicationSet that fans out into the two Applications above. Today it uses a `list` generator with a single entry per Application (i.e., the ApplicationSet is mostly an aggregation point for documentation + bulk delete). Future phases can swap the generator to `git` or `matrix` for multi-environment fan-out without touching the per-Application templates.

**Files:**
- Create: `argocd/appset/dlh-platform.yaml`

- [ ] **Step 1: Write argocd/appset/dlh-platform.yaml**

```yaml
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: dlh-platform
  namespace: argocd
  labels:
    app.kubernetes.io/part-of: dlh-test-fw
spec:
  goTemplate: true
  goTemplateOptions:
    - missingkey=error
  generators:
    # Static list of the framework cluster's Applications. To extend to
    # multiple clusters/environments later, swap this for a `clusters` or
    # `git` generator without modifying the template body.
    - list:
        elements:
          - appName: dlh-test-fw-chart
            sourcePath: helm/dlh-test-fw
            valueFile: argocd/values/framework/chart-values.yaml
            destNamespace: dlh-test-fw
            autoSync: "true"
          - appName: dlh-controlplane
            sourcePath: controlplane/deploy
            valueFile: ""
            destNamespace: dlh-test-fw
            autoSync: "false"
  template:
    metadata:
      name: '{{ .appName }}'
      namespace: argocd
      labels:
        app.kubernetes.io/part-of: dlh-test-fw
    spec:
      project: dlh-test-fw
      source:
        repoURL: https://github.com/REPLACE-OWNER/dlh-test-fw.git
        targetRevision: main
        path: '{{ .sourcePath }}'
        # The chart Application uses helm values; the placeholder uses a
        # plain directory. ApplicationSet templating evaluates both blocks,
        # so the chart entry leans on .valueFile while the placeholder
        # uses an empty valueFile and the directory block kicks in via
        # Argo CD's auto-detection of the source type.
        helm:
          valueFiles:
            - '../../{{ .valueFile }}'
      destination:
        server: https://kubernetes.default.svc
        namespace: '{{ .destNamespace }}'
      syncPolicy:
        syncOptions:
          - CreateNamespace=true
          - ServerSideApply=true
        retry:
          limit: 5
          backoff:
            duration: 5s
            factor: 2
            maxDuration: 3m
```

> **Note for the engineer:** The ApplicationSet and the individual Application manifests (Tasks 4 + 5) are intentionally redundant. Some operators prefer ApplicationSet for everything; others prefer pinned Application manifests. Both work; both validate. We ship both because (a) the per-Application manifests are easier to read and review individually, and (b) the ApplicationSet gives the single-bulk-bootstrap path the bootstrap doc references. In a real cluster you'd pick *one* set as the source of truth — the ApplicationSet generates Applications dynamically, so applying both would create name collisions. The bootstrap doc (Task 8) clarifies which to use.

- [ ] **Step 2: Validate the manifest renders as valid YAML**

```bash
python3 -c "import sys, yaml; list(yaml.safe_load_all(open('argocd/appset/dlh-platform.yaml')))" && echo "valid YAML"
```

Expected: `valid YAML`.

- [ ] **Step 3: Write argocd/README.md (orientation)**

```markdown
# Argo CD platform manifests

This directory defines the GitOps surface for `dlh-test-fw`. See
[`docs/operations/bootstrap-via-argocd.md`](../docs/operations/bootstrap-via-argocd.md)
for the end-to-end bootstrap procedure.

## Layout

- `appproject.yaml` — `AppProject dlh-test-fw` defining source repos and
  destination namespaces. Apply this first.
- `apps/dlh-test-fw-chart.yaml` — `Application` syncing the umbrella chart.
- `apps/dlh-controlplane.yaml` — `Application` placeholder reserved for the
  companion spec (`docs/superpowers/specs/2026-05-21-dlh-controlplane-design.md`).
- `appset/dlh-platform.yaml` — `ApplicationSet` aggregating both
  Applications. **Mutually exclusive** with the manifests in `apps/` —
  pick one set, not both. The bootstrap doc explains the trade-off.
- `values/framework/chart-values.yaml` — production-shaped Helm values
  overlay referenced by the chart Application. Replace `REPLACE-*`
  placeholders per environment before deploying.

## Replace-before-deploy placeholders

Search for `REPLACE-` across this directory. Every match must be
substituted before applying to a real cluster.
```

- [ ] **Step 4: Commit**

```bash
git add argocd/appset/dlh-platform.yaml argocd/README.md
git commit -m "feat(argocd): add ApplicationSet dlh-platform + argocd/README

The ApplicationSet provides a single-apply bootstrap path; the per-app
manifests in apps/ provide an alternative for operators who prefer
pinned Application definitions. README clarifies they are mutually
exclusive."
```

---

## Task 7: Extend CI to validate argocd/ manifests

The existing `kubeconform` CI job validates the rendered chart + scenarios. Extend it to also validate `argocd/**.yaml`. This catches schema regressions in the AppProject / Application / ApplicationSet manifests on every PR.

**Files:**
- Modify: `.github/workflows/ci.yml`

- [ ] **Step 1: Read the current kubeconform job to find the right insertion point**

```bash
sed -n '/kubeconform:/,/^  [a-z]/p' .github/workflows/ci.yml | head -50
```

Look for the last `- name: Validate ...` step under the `kubeconform:` job — the new step inserts after it.

- [ ] **Step 2: Add an `argocd/` validation step**

Append the following step to the `kubeconform` job, immediately after the existing `Validate scenarios/*.yaml` step. The full job after the change should look like the snippet below — only the *new* `Validate argocd/ manifests` step is added.

```yaml
      # New step — validates Argo CD AppProject / Application / ApplicationSet
      # manifests. The Datree CRD catalog covers argoproj.io/v1alpha1.
      - name: Validate argocd/ manifests
        run: |
          kubeconform -skip CustomResourceDefinition -strict -summary \
            -schema-location default \
            -schema-location 'https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json' \
            $(find argocd -name '*.yaml' | sort | tr '\n' ' ')
```

Use the `Edit` tool: find the unique line `            scenarios/*.yaml` (the last line of the previous step) and replace it with that line plus the new step above.

- [ ] **Step 3: Render-validate the framework values overlay also produces valid YAML**

Add a second new step (after the one in Step 2) that runs `helm template` with the framework values file and runs the rendered output through kubeconform:

```yaml
      - name: Validate chart render with framework values
        run: |
          helm template dlh helm/dlh-test-fw \
            -f helm/dlh-test-fw/values.yaml \
            -f argocd/values/framework/chart-values.yaml \
            > /tmp/rendered-framework.yaml
          kubeconform -skip CustomResourceDefinition -strict -summary \
            -schema-location default \
            -schema-location 'https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json' \
            /tmp/rendered-framework.yaml
```

- [ ] **Step 4: Smoke-test the workflow file locally for YAML validity**

```bash
python3 -c "import sys, yaml; list(yaml.safe_load_all(open('.github/workflows/ci.yml')))" && echo "valid YAML"
```

Expected: `valid YAML`.

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: validate argocd/ manifests + framework values overlay in kubeconform job"
```

---

## Task 8: Write the bootstrap documentation

The "no shell" promise requires that someone other than the original authors can stand up the platform with only this document. Write the procedure for both fresh cluster and reconnect-to-existing scenarios.

**Files:**
- Create: `docs/operations/bootstrap-via-argocd.md`

- [ ] **Step 1: Create the docs/operations directory if absent**

```bash
mkdir -p docs/operations
```

- [ ] **Step 2: Write docs/operations/bootstrap-via-argocd.md**

```markdown
# Bootstrap `dlh-test-fw` via Argo CD

This procedure stands up the framework cluster's dlh-test-fw platform
entirely through GitOps. After step 4, no `helm` / `kubectl apply`
commands are needed for platform operations — Argo CD reconciles
everything from this repo.

For local-minikube development, use `scripts/platform-up.sh` instead;
this document targets a production-shaped cluster where shell access
is restricted.

## Prerequisites

1. A Kubernetes cluster (≥ 1.28) with cluster-admin access for the
   one-time bootstrap.
2. Argo CD already installed in the cluster. If not, follow the
   upstream installation guide:
   https://argo-cd.readthedocs.io/en/stable/getting_started/
3. A fork or mirror of this repository accessible to Argo CD (HTTPS or
   SSH credential configured in Argo CD's repository settings).
4. An ingress controller (the chart defaults assume `nginx`).
5. A default StorageClass (or a class to substitute for `REPLACE-STORAGE-CLASS`
   in the framework values file).

## Step 1 — Fork the repository

Argo CD watches a git URL. Production deployments must point at a fork
under your organization's control, not the upstream project:

```
GH_OWNER=your-org
gh repo fork --org "$GH_OWNER" --remote
```

## Step 2 — Substitute placeholders

The shipped manifests contain `REPLACE-*` markers that must be
substituted before applying. The cheapest path is `sed`:

```
cd path/to/your/dlh-test-fw-fork
DOMAIN=dlh-test-fw.example.com
OWNER=$GH_OWNER
STORAGE_CLASS=gp3
MINIO_USER=ops-bootstrap
MINIO_PASSWORD="$(openssl rand -base64 24)"
GRAFANA_USER=ops-bootstrap
GRAFANA_PASSWORD="$(openssl rand -base64 24)"

# Repo URL in all argocd/ manifests
find argocd -name '*.yaml' -exec sed -i "s|REPLACE-OWNER|$OWNER|g" {} +

# Domain + storage in the framework values overlay
sed -i "s|REPLACE-DOMAIN|$DOMAIN|g" argocd/values/framework/chart-values.yaml
sed -i "s|REPLACE-STORAGE-CLASS|$STORAGE_CLASS|g" argocd/values/framework/chart-values.yaml
sed -i "s|REPLACE-MINIO-USER|$MINIO_USER|g" argocd/values/framework/chart-values.yaml
sed -i "s|REPLACE-MINIO-PASSWORD|$MINIO_PASSWORD|g" argocd/values/framework/chart-values.yaml
sed -i "s|REPLACE-GRAFANA-USER|$GRAFANA_USER|g" argocd/values/framework/chart-values.yaml
sed -i "s|REPLACE-GRAFANA-PASSWORD|$GRAFANA_PASSWORD|g" argocd/values/framework/chart-values.yaml

git checkout -b bootstrap-substitutions
git commit -am "bootstrap: substitute placeholders for $DOMAIN"
git push -u origin bootstrap-substitutions
```

**Note on secrets:** the placeholder substitution above commits credentials
to git. This is acceptable for an initial spike-grade bootstrap; for any
shared environment, choose a secret backend (external-secrets / sealed-secrets
/ SOPS) before step 4 and modify the values file to reference Secrets by
name rather than embedding credentials. See the companion spec
(`docs/superpowers/specs/2026-05-21-argocd-platform-lifecycle-design.md` §5.6)
for the recommendation framework.

## Step 3 — Apply the AppProject

The AppProject must exist before any Application referencing it can
sync:

```
kubectl apply -f argocd/appproject.yaml
kubectl -n argocd get appproject dlh-test-fw
```

Expected: AppProject is present with `sourceRepos` listing your fork.

## Step 4 — Apply the ApplicationSet

This is the single-command bootstrap. Choose one of two paths:

**Path A — ApplicationSet (recommended):**

```
kubectl apply -f argocd/appset/dlh-platform.yaml
kubectl -n argocd get applicationset dlh-platform
kubectl -n argocd get applications -l app.kubernetes.io/part-of=dlh-test-fw
```

Expected: ApplicationSet `dlh-platform` present; two Applications
(`dlh-test-fw-chart`, `dlh-controlplane`) are generated within ~30s.

**Path B — Pinned Application manifests (for operators who prefer
explicit per-Application files):**

```
kubectl apply -f argocd/apps/dlh-test-fw-chart.yaml
kubectl apply -f argocd/apps/dlh-controlplane.yaml
```

**Do not apply both paths.** Both define Applications with the same
names; the second apply will conflict.

## Step 5 — Watch the initial sync

```
kubectl -n argocd get applications -w
```

Expected: `dlh-test-fw-chart` progresses through `Progressing` →
`Healthy` + `Synced` within ~3 minutes. `dlh-controlplane` stays
`OutOfSync` (it's a placeholder, not auto-sync) — this is intentional
until the companion spec populates `controlplane/deploy/`.

## Step 6 — Verify

The Argo CD UI now shows the platform's state. Beyond that:

```
kubectl -n dlh-test-fw get pods
```

Expected: argo-workflows, chaos-mesh, victoria-metrics, grafana, minio
pods all `Ready`.

Browser access (if DNS / `/etc/hosts` is configured for the placeholder
domain):

- `argo.<DOMAIN>` — Argo Workflows UI
- `grafana.<DOMAIN>` — Grafana
- `minio.<DOMAIN>` — MinIO console

## Day-2 operations

- **All platform changes go through git PRs to your fork.** Argo CD
  auto-syncs the umbrella chart Application; manual `kubectl apply`
  against the framework namespace is reverted by self-heal.
- **Pause sync** during planned maintenance:
  `argocd app set dlh-test-fw-chart --sync-policy none`
- **Roll back** by reverting the offending commit in git; Argo CD
  re-syncs to the new HEAD automatically.

## Teardown

```
kubectl delete -f argocd/appset/dlh-platform.yaml
# Optional — only if the AppProject is no longer needed:
kubectl delete -f argocd/appproject.yaml
```

The `resources-finalizer.argocd.argoproj.io` finalizer on the chart
Application ensures all chart-managed resources are deleted; the
placeholder's lack of finalizer means it deletes immediately.
```

- [ ] **Step 3: Commit**

```bash
git add docs/operations/bootstrap-via-argocd.md
git commit -m "docs: production bootstrap procedure via Argo CD"
```

---

## Task 9: Annotate existing scripts as local-dev only

The shell scripts remain valid for laptop minikube use. Add a header so that anyone reading them immediately understands the production path is Argo CD, not these scripts.

**Files:**
- Modify: `scripts/platform-up.sh`
- Modify: `scripts/platform-down.sh`
- Modify: `scripts/platform-verify.sh`
- Modify: `scripts/verify-templates.sh`

For each script, the new header lines are inserted after the shebang and any existing `set -euo pipefail` line.

- [ ] **Step 1: Annotate scripts/platform-up.sh**

Use Edit to insert the comment block. Find the unique line
`cd "$(dirname "$0")/.."` (which appears immediately after the
`set -euo pipefail` line) and replace it with:

```bash
cd "$(dirname "$0")/.."

# ============================================================================
# LOCAL-DEV ONLY. Production cluster bootstrap is GitOps via Argo CD;
# see docs/operations/bootstrap-via-argocd.md.
# ============================================================================
```

- [ ] **Step 2: Annotate scripts/platform-down.sh**

Find the unique first non-shebang line and insert the same comment block
immediately after `set -euo pipefail`. Use the existing first
post-`set` line as the unique anchor for Edit.

```bash
# ============================================================================
# LOCAL-DEV ONLY. Production teardown is via Argo CD:
#   kubectl delete -f argocd/appset/dlh-platform.yaml
# See docs/operations/bootstrap-via-argocd.md.
# ============================================================================
```

- [ ] **Step 3: Annotate scripts/platform-verify.sh**

```bash
# ============================================================================
# LOCAL-DEV ONLY. Production verification is via Argo CD's sync/health
# status; see docs/operations/bootstrap-via-argocd.md.
# ============================================================================
```

- [ ] **Step 4: Annotate scripts/verify-templates.sh**

```bash
# ============================================================================
# LOCAL-DEV ONLY. In a GitOps-managed cluster, WorkflowTemplate presence
# is observable via Argo CD's sync status and (after the companion spec's
# Phase B) via the controlplane's GET /api/scenarios endpoint.
# ============================================================================
```

- [ ] **Step 5: Confirm the scripts still pass shellcheck**

```bash
shellcheck -S error scripts/platform-up.sh scripts/platform-down.sh scripts/platform-verify.sh scripts/verify-templates.sh
```

Expected: no output, exit 0. (Comments don't affect shellcheck.)

If `shellcheck` isn't installed locally, the existing CI job
(`shellcheck:` in `.github/workflows/ci.yml`) catches regressions.

- [ ] **Step 6: Commit**

```bash
git add scripts/platform-up.sh scripts/platform-down.sh scripts/platform-verify.sh scripts/verify-templates.sh
git commit -m "docs(scripts): annotate platform-up/down/verify scripts as local-dev only

Production bootstrap is GitOps via Argo CD per
docs/operations/bootstrap-via-argocd.md. Scripts remain valid for
laptop minikube usage."
```

---

## Task 10: Update CLAUDE.md with the GitOps operational model

`CLAUDE.md` is the orientation guide for future agents. Add a section
describing when to use the Argo CD path versus the local-dev scripts.

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Read CLAUDE.md to find the right insertion point**

```bash
grep -n "^## " CLAUDE.md
```

Expected: a list of section headers. The new section "Operational model: GitOps vs local-dev" inserts after the "Branching & worktree conventions" section and before "Image build + minikube reload".

- [ ] **Step 2: Insert the new section**

Use Edit. Find the unique line `## Image build + minikube reload` and replace it with the new section followed by that line:

```markdown
## Operational model: GitOps vs local-dev

After Plan 14, the framework cluster has two operational modes — pick
the one that matches your environment.

### Local-dev (laptop minikube)

Use the existing shell scripts. They're annotated `# LOCAL-DEV ONLY` at
the top:

- `scripts/minikube-up.sh` — destructive cluster reset
- `scripts/platform-up.sh` — `helm upgrade --install` the umbrella chart
- `scripts/platform-down.sh` — `helm uninstall`
- `scripts/platform-verify.sh` — `helm test` + ingress reachability
- `scripts/verify-templates.sh` — WorkflowTemplate presence check

`scripts/run-scenario.sh` is the local-dev scenario submission path.
(The companion spec replaces it with `dlh` CLI + controlplane API.)

### Production / shared cluster (GitOps via Argo CD)

Use the manifests in `argocd/`:

- `argocd/appproject.yaml` — `AppProject dlh-test-fw` (security scope).
- `argocd/apps/dlh-test-fw-chart.yaml` — umbrella chart Application.
- `argocd/apps/dlh-controlplane.yaml` — placeholder for the companion spec.
- `argocd/appset/dlh-platform.yaml` — single-apply ApplicationSet (mutually
  exclusive with the per-app manifests; pick one).
- `argocd/values/framework/chart-values.yaml` — values overlay template
  with `REPLACE-*` placeholders.

Full procedure: `docs/operations/bootstrap-via-argocd.md`.

### Which to use

- Laptop development → local-dev.
- Anything someone else shares with you (preprod / prod) → GitOps. The
  scripts are not safe in shared environments because manual changes
  get reverted by Argo CD's self-heal.
- In any doubt → check whether Argo CD is installed in the target
  cluster. If yes, GitOps. If no, scripts.

## Image build + minikube reload
```

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md
git commit -m "docs(CLAUDE.md): document GitOps vs local-dev operational model"
```

---

## Task 11: Append Plan 14 findings to FINDINGS.md

`docs/FINDINGS.md` is the authoritative cross-plan reference. Every plan
appends a section. Plan 14 adds findings about the operational shift.

**Files:**
- Modify: `docs/FINDINGS.md`

- [ ] **Step 1: Find the end of the file**

```bash
tail -20 docs/FINDINGS.md
```

Expected: the Plan 13 section's tail. The new section appends after the last line.

- [ ] **Step 2: Append the Plan 14 section**

Use Edit. Find the last unique line of FINDINGS.md (whatever it currently is) and replace it with that line plus a blank line plus the new section below.

```markdown

---

## Plan 14 — Argo CD platform lifecycle (2026-05-21)

### What landed

- `argocd/` directory: AppProject + two Applications (umbrella chart + controlplane placeholder) + ApplicationSet aggregator + framework values overlay template.
- `docs/operations/bootstrap-via-argocd.md`: production bootstrap procedure.
- Existing `scripts/platform-*.sh` annotated `# LOCAL-DEV ONLY`; CLAUDE.md gained a "GitOps vs local-dev" section.
- CI extended: `kubeconform` job now validates `argocd/**.yaml` and the framework-values-overlaid chart render.

### Operational pitfalls discovered (record so the next plan doesn't relearn)

1. **`REPLACE-*` placeholders are easy to forget.** The shipped manifests contain repo URLs, domain names, storage classes, and credentials marked `REPLACE-`. A `grep -r REPLACE- argocd/` before applying is mandatory pre-flight. Consider a `make argocd-check` target in a future plan.

2. **ApplicationSet vs pinned Applications are mutually exclusive.** Both shapes live in the repo so reviewers can choose, but applying both creates Application-name collisions. The bootstrap doc spells this out; the README mirrors it. Don't add a third path.

3. **`dlh-controlplane` placeholder Application stays OutOfSync.** This is intentional — the companion spec populates it. Operators who haven't read both specs may flag it as a sync error; the placeholder's `controlplane/deploy/` directory contains only `.gitkeep`.

4. **Argo CD self-heal masks broken manual edits.** Once Argo CD owns the chart, `kubectl edit` against the framework namespace is reverted within seconds. Operators used to the script-driven workflow ("just patch it live") need to learn the PR-driven workflow. CLAUDE.md's new section makes this explicit.

5. **`secrets.useExternal: false` ships embedded credentials.** The framework values overlay defaults to embedded placeholders for MinIO / Grafana credentials. Any real environment must (a) flip `useExternal: true`, (b) wire a secret backend (sealed-secrets / external-secrets / SOPS) — decision deferred per spec §5.6, and (c) avoid committing real credentials to git via the substitution step.

### Carry-forward for the companion spec

The companion spec's Phase B will populate `controlplane/deploy/` and add a `resources-finalizer.argocd.argoproj.io` finalizer + auto-sync to the `dlh-controlplane` Application. Until then, the placeholder is dormant.
```

- [ ] **Step 3: Commit**

```bash
git add docs/FINDINGS.md
git commit -m "docs(findings): record Plan 14 Argo CD platform lifecycle"
```

---

## Task 12: Add Plan 14 row to README.md

`README.md` has a plan table. Add a row for Plan 14.

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Find the Plan 13 row**

```bash
grep -n "^| Plan 13 " README.md
```

Expected: a single match — the Plan 13 row.

- [ ] **Step 2: Append Plan 14 row**

Use Edit. Find the unique Plan 13 row line and replace it with that line plus a new Plan 14 row below.

The commit hash placeholder `XXXXXXX` will be filled in during Task 13's merge step.

```markdown
| Plan 13 | `438ecb1` | Per-target dashboard enrichment (+12 panels across mysql/kafka/doris) + chaos timeline overlay via `useValueForTime` annotations |
| Plan 14 | `XXXXXXX` | Argo CD platform lifecycle — `argocd/` AppProject + ApplicationSet + chart Application + controlplane Application placeholder; production bootstrap doc; scripts annotated as local-dev only |
```

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs(readme): add Plan 14 row — Argo CD platform lifecycle"
```

(The hash will be backfilled at merge time in Task 13.)

---

## Task 13: Render verification + merge to main

Final pre-merge verification, then merge with `--no-ff` per `CLAUDE.md` conventions.

**Files:** None modified by edits; commits land via merge.

- [ ] **Step 1: Confirm CI on the feature branch passes**

```bash
git push -u origin feat/plan14-argocd-platform-lifecycle
gh run watch
```

Expected: all four CI jobs (`helm`, `go`, `shellcheck`, `kubeconform`) pass. The `kubeconform` job's new steps (`Validate argocd/ manifests`, `Validate chart render with framework values`) must be green.

If `kubeconform` fails on `argocd/`: the Datree CRD catalog covers most of `argoproj.io/v1alpha1` but may miss specific fields on `ApplicationSet`. If a field is flagged as unrecognized, **do not** add `-ignore-missing-schemas` (it would hide real errors). Instead, narrow the validation: add a `-skip ApplicationSet` flag scoped just to that field, and document the skip in the workflow comment.

- [ ] **Step 2: Run the full render baseline check locally**

```bash
helm dependency update helm/dlh-test-fw
helm template dlh helm/dlh-test-fw \
  -f helm/dlh-test-fw/values.yaml \
  -f argocd/values/framework/chart-values.yaml \
  > /tmp/rendered-framework.yaml
helm template dlh helm/dlh-test-fw > /tmp/rendered-default.yaml
diff <(grep -E '^(kind|  name):' /tmp/rendered-default.yaml | sort -u) \
     <(grep -E '^(kind|  name):' /tmp/rendered-framework.yaml | sort -u)
```

Expected: the diff shows only ingress hostnames + persistence + resource quantities changing. No resources added or removed by the framework overlay.

- [ ] **Step 3: Confirm placeholder count**

```bash
grep -rn "REPLACE-" argocd/ docs/operations/bootstrap-via-argocd.md
```

Expected: at least one match per `REPLACE-` token category (`REPLACE-OWNER`, `REPLACE-DOMAIN`, `REPLACE-STORAGE-CLASS`, `REPLACE-MINIO-USER`, `REPLACE-MINIO-PASSWORD`, `REPLACE-GRAFANA-USER`, `REPLACE-GRAFANA-PASSWORD`). Every placeholder must be findable, since the bootstrap doc relies on `sed` substitution.

- [ ] **Step 4: Merge to main**

```bash
cd /Users/allen/repo/dlh-test-fw
git checkout main
git pull --ff-only origin main
git merge --no-ff feat/plan14-argocd-platform-lifecycle -m "Merge feat/plan14-argocd-platform-lifecycle: Argo CD platform lifecycle

Introduces argocd/ with AppProject + ApplicationSet + Applications for
the umbrella chart and a controlplane placeholder reserved for the
companion spec. Adds production bootstrap documentation at
docs/operations/bootstrap-via-argocd.md. Annotates existing
platform-up/down/verify/verify-templates scripts as local-dev only;
script behaviour is unchanged. Extends the kubeconform CI job to
validate argocd/ manifests and the framework-values-overlaid chart
render.

Plan 14 of dlh-test-fw. See:
- docs/superpowers/specs/2026-05-21-argocd-platform-lifecycle-design.md
- docs/superpowers/plans/2026-05-21-01-argocd-platform-lifecycle.md"
```

- [ ] **Step 5: Backfill the README plan hash**

```bash
MERGE_HASH=$(git log --first-parent --format=%h -1)
sed -i "" "s|| Plan 14 | \`XXXXXXX\`|| Plan 14 | \`$MERGE_HASH\`|" README.md
git add README.md
git commit -m "docs(readme): backfill Plan 14 merge hash"
```

- [ ] **Step 6: Push main**

```bash
git push origin main
```

- [ ] **Step 7: Confirm CI is green on main**

```bash
gh run list --branch main --limit 1
```

Expected: status `success` within ~3 minutes.

- [ ] **Step 8: Worktree cleanup**

```bash
git worktree remove ../dlh-test-fw-plan14
git branch -d feat/plan14-argocd-platform-lifecycle
git push origin --delete feat/plan14-argocd-platform-lifecycle
```

Expected: worktree directory removed; branch deleted locally and remotely.

- [ ] **Step 9: Verify final state**

```bash
git log --first-parent --oneline -3
ls argocd/ docs/operations/
grep "^| Plan 14" README.md
```

Expected: the merge commit is the most recent first-parent entry; argocd/ and docs/operations/bootstrap-via-argocd.md exist; README has the backfilled Plan 14 row.

---

## Done

Plan 14 lands a complete GitOps surface for `dlh-test-fw` without changing any runtime behaviour. The companion spec's Phase B is now unblocked: the `dlh-controlplane` Application is reserved, the AppProject permits the framework namespace, and the bootstrap procedure is documented.
