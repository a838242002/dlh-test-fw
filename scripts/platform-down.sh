#!/usr/bin/env bash
set -euo pipefail
helm uninstall dlh -n dlh-test-fw || true
# Don't delete the namespace; user may want to inspect remaining state.
echo "uninstalled. To wipe entirely: kubectl delete ns dlh-test-fw"
