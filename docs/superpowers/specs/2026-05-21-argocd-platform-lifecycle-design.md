# Argo CD Platform Lifecycle — Design

**Date:** 2026-05-21
**Status:** Draft (brainstormed; pending review)
**Companion spec:** `2026-05-21-dlh-controlplane-design.md` (runtime submissions; depends on this spec)

---

## 1. Context

`dlh-test-fw` today is bootstrapped and managed through shell scripts that call `helm`, `kubectl`, and `minikube` directly:

- `scripts/platform-up.sh` — `helm repo add` / `helm dependency update` / `helm upgrade --install` / `kubectl wait`
- `scripts/platform-down.sh` — `helm uninstall` + ns wipe
- `scripts/platform-verify.sh` — `helm test` + ingress reachability
- `scripts/verify-templates.sh` — `kubectl get workflowtemplate …`
- `scripts/minikube-up.sh` — `minikube start` / `minikube delete`

This works for local minikube development but is unusable in a production-shaped environment where:

- Engineers do not have `kubectl` / `helm` against the framework cluster.
- The cluster is restored / re-bootstrapped through GitOps, not by anyone running scripts.
- Changes to chart values, WorkflowTemplate manifests, and dashboards must follow a reviewable, audited path (git PR + automated reconciliation).

This spec describes the Argo CD layer that owns **platform lifecycle** — everything that exists in the cluster *before* a Run is submitted. Runtime submission (creating `Workflow` CRs, injecting chaos, reading verdicts) is intentionally out of scope here and is the subject of the companion spec.

## 2. Goals

1. The framework cluster can be rebuilt from a clean kube-apiserver using only `git` as input: install Argo CD, point it at this repo, everything else reconciles itself.
2. Every change to chart values, WorkflowTemplates, dashboards, chaos-mesh configuration, MinIO configuration, etc. happens via git PR → Argo CD sync, without any shell access to the cluster.
3. Local minikube development continues to work via the existing shell scripts; the GitOps path is **additional**, not exclusive.
4. The cluster's deployed state is observable through Argo CD's UI (sync status, drift, history) without needing kubectl.

## 3. Non-goals

- **Runtime scenario submission.** Argo CD does not create `Workflow` CRs. That responsibility belongs to the controlplane service described in the companion spec. Argo CD only manages reusable templates and platform infrastructure.
- **Per-target Chaos Mesh installs.** Phase A focuses on the framework cluster's own components. Installing Chaos Mesh into remote target clusters via Argo CD is covered in the companion spec (Phase D of the overall migration).
- **Argo CD installation itself.** Bootstrapping Argo CD is a one-time cluster setup activity; this spec assumes Argo CD is already running in the framework cluster.
- **Replacing minikube-based local dev.** `scripts/platform-up.sh` and friends remain valid for laptops.

## 4. Architecture

```
                     git (this repo)
                          │
                          │ (Argo CD watches a subset of paths)
                          ▼
              ┌─────────────────────────┐
              │ Argo CD ApplicationSet  │
              │   ├── dlh-test-fw-chart │── helm/dlh-test-fw/
              │   ├── dlh-workflow-tmpl │── (rendered from chart)
              │   ├── dlh-dashboards    │── dashboards/grafana/
              │   ├── dlh-chaos-mesh    │── upstream helm chart pin
              │   └── dlh-controlplane  │── controlplane Deployment
              │                         │   (added in companion spec)
              └─────────────────────────┘
                          │
                          ▼
                  framework cluster
```

Each `Application` syncs from a specific path in this repo (or from an external helm repo, in the case of upstream charts). Argo CD's reconcile loop is the only thing that should ever apply manifests to the framework namespace once the platform is in GitOps mode.

## 5. Detailed design

### 5.1 Repository layout

A new top-level `argocd/` directory contains the GitOps definitions:

```
argocd/
  appset/
    dlh-platform.yaml      # ApplicationSet generating all framework-cluster Applications
  apps/
    dlh-test-fw-chart.yaml # umbrella chart sync
    dlh-workflow-tmpl.yaml # WorkflowTemplates (if kept separate from the chart)
    dlh-dashboards.yaml    # dashboards ConfigMap kustomization
    dlh-chaos-mesh.yaml    # upstream chaos-mesh chart with our values
    dlh-controlplane.yaml  # placeholder; populated by companion spec
  values/
    framework/
      chart-values.yaml    # values overrides for the umbrella chart in framework cluster
      chaos-mesh.yaml      # chaos-mesh values, including docker runtime overrides
```

The split between `appset/` and `apps/` is deliberate: the ApplicationSet is the entry point that ties Applications together (one place to add/remove the whole platform), while individual Application manifests remain readable and reviewable in isolation.

### 5.2 Application boundaries

| Application | Source | Why separate |
|---|---|---|
| `dlh-test-fw-chart` | `helm/dlh-test-fw/` | The umbrella chart owns most of the platform (Argo Workflows, Victoria Metrics, Grafana, MinIO). One sync target = one revertable unit. |
| `dlh-workflow-templates` | (option A: kept inside the chart) (option B: extracted to `scenarios/templates/`) | Scenarios churn faster than the chart. Extracting them into a separate Application means a scenario update doesn't trigger a full umbrella-chart sync. **Recommend option B**, to be confirmed at plan time. |
| `dlh-dashboards` | `dashboards/grafana/` + kustomize ConfigMap generator | Dashboards churn even faster (every Plan-13-style enrichment). Separate Application = independent sync cycle, no chart redeploy. |
| `dlh-chaos-mesh` | Upstream helm chart, our values | Chaos Mesh has its own release cadence and CRDs; isolating it makes upgrades safer. Also enforces the **docker runtime override** from FINDINGS.md as a values-file invariant. |
| `dlh-controlplane` | This repo (placeholder in Phase A; real in Phase B) | Phase A creates an empty Application that becomes the controlplane's deployment surface later. |

### 5.3 Sync policy

- **Automated sync** for all five Applications.
- **`syncOptions: [Prune=true]`** so removing a manifest in git removes it from the cluster.
- **`syncOptions: [CreateNamespace=true]`** only for chaos-mesh (its namespace is separate). The framework namespace `dlh-test-fw` is created by the chart itself.
- **Self-heal enabled.** Manual `kubectl edit` against the cluster is reverted by Argo CD on the next reconcile — which is desirable, since manual edits are exactly what we're eliminating.
- **Retry with backoff** on sync failures (Argo CD's default `RetryStrategy`).

### 5.4 Replacing the existing scripts

| Script | Disposition after Phase A |
|---|---|
| `scripts/minikube-up.sh` | Keep. Local-dev only. Annotate as such. |
| `scripts/platform-up.sh` | Keep, but annotate `# local-dev only`. Production bootstrap is `kubectl apply -f argocd/appset/`. |
| `scripts/platform-down.sh` | Keep for local-dev. Production teardown is `kubectl delete -f argocd/appset/`. |
| `scripts/platform-verify.sh` | Keep for local-dev (it runs `helm test`). Production health is observed via Argo CD UI + the controlplane's `/healthz` once it exists. |
| `scripts/verify-templates.sh` | Deprecate. Argo CD's sync status reports template presence; the upcoming controlplane's `GET /api/scenarios` is the canonical "what templates exist" surface. |
| `scripts/run-scenario.sh` | Out of scope for Phase A. Replaced in the companion spec. |

No script is deleted in Phase A. They're annotated and the bootstrap path becomes additive.

### 5.5 Local-dev parity

The chart values in `argocd/values/framework/` are framework-cluster-shaped (real ingress hostnames, OIDC config, secret references). Local minikube continues to use the chart's default values (`helm/dlh-test-fw/values.yaml`). This means:

- The chart itself remains **environment-agnostic** — all environment-specific knobs go in values-files referenced by Argo CD Applications.
- A new contributor running `platform-up.sh` on minikube uses the same chart that production uses.

### 5.6 Secret distribution

This spec does **not** prescribe the secret backend (sealed-secrets vs external-secrets vs SOPS-encrypted manifests). Phase A's deliverable is just that the Application manifests reference Secrets by name; the secret distribution mechanism is a follow-on operational decision. The companion spec depends on this decision (it needs target-cluster kubeconfig Secrets), so the choice should be finalized before Phase D of the overall migration.

Recommendation to evaluate at plan time: **external-secrets-operator** if the org already runs a centralized secret store (Vault / AWS SM / GCP SM); **sealed-secrets** if the team wants the secrets to live in this git repo encrypted.

## 6. Testing

- **Render check:** CI runs `argocd app render` (or `helm template` / `kustomize build`) against each Application manifest and asserts the output is valid Kubernetes YAML.
- **Schema check:** existing `kubeconform` integration covers rendered manifests.
- **Smoke environment:** an ephemeral kind cluster + Argo CD install + this repo gets us a "bootstrap from clean" CI job. This is heavier than today's CI; consider running it on PR merge to main rather than every PR.
- **Drift detection:** Argo CD's own `OutOfSync` status surfaces drift after deployment; no separate test is needed.

## 7. Migration steps

1. Add `argocd/` directory with ApplicationSet + Applications + values files.
2. Adjust the umbrella chart so all environment-specific knobs are values-file overridable (audit needed; today some live in templates).
3. Stand up Argo CD in a dedicated test cluster, point it at this repo, verify clean bootstrap.
4. Document the bootstrap procedure at `docs/operations/bootstrap-via-argocd.md`.
5. Annotate existing scripts with `# local-dev only` headers.
6. Update `docs/FINDINGS.md` with the operational shift (Argo CD self-heal can mask broken manual edits — workflow becomes "PR, then watch Argo sync status").

## 8. Open questions (resolve at plan time)

- **WorkflowTemplate split** — kept inside the umbrella chart (single Application) or extracted to a separate `scenarios/templates/` Application? Recommendation: extract. Confirm at plan time based on how often templates change vs chart values.
- **Secret backend** — see §5.6.
- **Argo CD multi-tenancy** — does this Argo CD instance also serve other projects? Affects AppProject scoping and RBAC. Out of scope for this spec; assume single-tenant.
- **Bootstrap from clean kind in CI** — desirable but adds 5+ minutes to a CI job. Decide whether to run on every PR, every merge, or nightly.

## 9. Dependencies on the companion spec

Phase A does not depend on the companion spec at all — it can ship and deliver value independently (the platform becomes GitOps-managed even without the controlplane).

The companion spec **does** depend on Phase A: the controlplane is deployed as an Argo-CD-synced Application (`dlh-controlplane`), and it reads scenario definitions from WorkflowTemplates that Argo CD has synced into the cluster. Without Phase A, the controlplane would still work, but the operational story it lives inside would still rely on shell scripts.

## 10. Rollback

Argo CD ownership is purely additive. Rolling back means:

1. Disable auto-sync on all Applications (`kubectl patch app … --type merge -p '{"spec":{"syncPolicy":null}}'`).
2. Optionally delete the Applications (manifests they synced remain in the cluster; only the reconcile loop stops).
3. Existing scripts continue to work against the cluster.

There is no destructive irreversible step.
