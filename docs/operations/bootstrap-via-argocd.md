# Bootstrap `dlh-test-fw` via Argo CD

This procedure stands up the framework cluster's dlh-test-fw platform
entirely through GitOps. After step 4, no `helm` / `kubectl apply`
commands are needed for platform operations — Argo CD reconciles
everything from this repo.

For local-minikube development, use `helm upgrade --install` directly
(Plan 14's `platform-up.sh` was removed in Plan 18; see CLAUDE.md's
local-dev section for the equivalent commands). This document targets a
production-shaped cluster where shell access is restricted.

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

# Controlplane manifests (Plan 15)
sed -i "s|REPLACE-VIEWER-GROUP|dlh-viewers|g" controlplane/deploy/roles-configmap.yaml
sed -i "s|REPLACE-RUNNER-GROUP|dlh-runners|g" controlplane/deploy/roles-configmap.yaml
sed -i "s|REPLACE-ADMIN-GROUP|dlh-admins|g" controlplane/deploy/roles-configmap.yaml
sed -i "s|REPLACE-OIDC-ISSUER|https://your-idp.example.com|g" controlplane/deploy/deployment.yaml
sed -i "s|REPLACE-OIDC-CLIENT-ID|dlh-controlplane|g" controlplane/deploy/deployment.yaml
sed -i "s|dlh.REPLACE-DOMAIN|dlh.$DOMAIN|g" controlplane/deploy/ingress.yaml

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
