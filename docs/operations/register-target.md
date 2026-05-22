# Registering a remote target cluster

Phase D allows the controlplane to inject chaos into clusters other than
the framework cluster. This document is the operator runbook.

## Prerequisites

1. The target cluster has chaos-mesh installed (see
   `argocd/apps/dlh-target-chaos-mesh.yaml.example` for a starter
   Argo CD Application that the operator commits + adapts).
2. A scoped ServiceAccount exists in the target cluster with:
   - chaos-mesh.org/* CRUD on schedules / podchaos / networkchaos
   - core/pods get + list (for selector resolution / probe)
   See `controlplane/deploy/targets-rbac.yaml.example`.
3. You have the kubeconfig of that ServiceAccount stored as a Secret
   (we recommend external-secrets / sealed-secrets / SOPS — Phase A's
   open question).

## Register

1. Create a Secret named `dlh-target-<id>` in the framework cluster's
   `dlh-test-fw` namespace, with key `kubeconfig` containing the bytes
   of the kubeconfig.
2. Add an entry to the `dlh-targets` ConfigMap in the same namespace:

   ```yaml
   data:
     targets.yaml: |
       targets:
         - id: staging-mysql
           displayName: "Staging MySQL"
           kubeconfigSecret: dlh-target-staging-mysql
           allowedTargetTypes: [mysql]
           namespace: dlh-test-fw  # chaos namespace on the target
   ```

3. Commit + push the change. Argo CD reconciles within ~30s; the
   controlplane refreshes its registry within another 30s.

## Verify

Browser UI → Targets page → click "test". Expected: green ✓ with a
millisecond latency reading.

CLI:
```
dlh runs ls --target staging-mysql
```

The first scenario run with `--target staging-mysql` should produce a
chaos CR in the **target** cluster's `dlh-test-fw` namespace (not the
framework cluster).
