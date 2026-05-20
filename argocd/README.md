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
