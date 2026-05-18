#!/usr/bin/env bash
# Submit a scenario Workflow and wait for it to finish.
#
# Replaces metadata.generateName: <prefix>- with metadata.name: <prefix>-YYYYMMDD-HHMMSS
# so the run is sortable + easy to find in `kubectl get workflow` and Grafana.
#
# Usage:
#   scripts/run-scenario.sh scenarios/<name>.yaml [argo-submit-args...]
#
# Examples:
#   scripts/run-scenario.sh scenarios/mysql-pod-delete.yaml
#   scripts/run-scenario.sh scenarios/mysql-pod-delete.yaml -p vus=50 -p mysql_op_mix=read:100
#   scripts/run-scenario.sh scenarios/mysql-pod-delete.yaml --priority 200
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: $0 scenarios/<name>.yaml [argo-submit-args...]" >&2
  exit 2
fi

file=$1; shift

prefix=$(awk '/^[[:space:]]*generateName:/ { sub(/.*generateName: */, ""); sub(/-$/, ""); print; exit }' "$file")
if [[ -z "$prefix" ]]; then
  echo "error: $file has no metadata.generateName line to derive a prefix from" >&2
  exit 1
fi
ts=$(date -u +%Y%m%d-%H%M%S)
name="${prefix}-${ts}"

rendered=$(mktemp)
trap 'rm -f "$rendered"' EXIT
sed "s|^\([[:space:]]*\)generateName:.*|\1name: ${name}|" "$file" > "$rendered"

echo "Submitting workflow: $name"
argo submit -n dlh-test-fw "$rendered" "$@" >/dev/null

# One-shot probe: if the workflow is queued behind a semaphore, surface it.
# (Sleep gives the controller a moment to annotate .status.synchronization.)
sleep 2
phase=$(kubectl -n dlh-test-fw get workflow "$name" -o jsonpath='{.status.phase}' 2>/dev/null || echo "")
if [[ "$phase" == "Pending" ]]; then
  blocked=$(kubectl -n dlh-test-fw get workflow "$name" \
              -o jsonpath='{.status.synchronization.semaphore.waiting[0].semaphore}' 2>/dev/null || echo "")
  if [[ -n "$blocked" ]]; then
    prio=$(kubectl -n dlh-test-fw get workflow "$name" \
             -o jsonpath='{.spec.priority}' 2>/dev/null || echo "default")
    echo "Queued: waiting for semaphore ${blocked} (priority ${prio})"
  fi
fi

argo wait -n dlh-test-fw "$name" || true
status=$(kubectl -n dlh-test-fw get workflow "$name" -o jsonpath='{.status.phase}')
echo "Final phase: $status"
echo "Report artifact: argo get -n dlh-test-fw $name  # see artifact section, or:"
echo "                 kubectl -n dlh-test-fw exec deploy/dlh-minio -- mc cat \"local/artifacts/${name}/${name}-main-*/verdict/report.json\" | jq ."
[[ "$status" == "Succeeded" ]]
