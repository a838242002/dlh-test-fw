#!/usr/bin/env bash
set -euo pipefail

# ============================================================================
# LOCAL-DEV ONLY. Production teardown is via Argo CD:
#   kubectl delete -f argocd/appset/dlh-platform.yaml
# See docs/operations/bootstrap-via-argocd.md.
# ============================================================================

helm uninstall dlh -n dlh-test-fw || true
# Don't delete the namespace; user may want to inspect remaining state.
echo "uninstalled. To wipe entirely: kubectl delete ns dlh-test-fw"
