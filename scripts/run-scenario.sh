#!/usr/bin/env bash
# Submit a scenario Workflow and wait for it to finish.
#
# Replaces the scenario's `metadata.generateName: <prefix>-` with an explicit
# `metadata.name: <prefix>-YYYYMMDD-HHMMSS` so the run name is sortable + easy
# to recognise in `kubectl get workflow` and the Grafana workflow dropdown.
# The timestamp is UTC, all lowercase + digits + dashes (DNS-1123-safe).
#
# Usage: scripts/run-scenario.sh scenarios/<name>.yaml [override-name]
set -euo pipefail

if [[ $# -lt 1 || $# -gt 2 ]]; then
  echo "usage: $0 scenarios/<name>.yaml [override-name]" >&2
  exit 2
fi

file=$1

# Derive a workflow name. Default is "<prefix>-YYYYMMDD-HHMMSS"; an explicit
# override can be passed as $2 (useful for tagged runs in CI / scripts).
if [[ $# -eq 2 ]]; then
  name=$2
else
  prefix=$(awk '/^[[:space:]]*generateName:/ { sub(/.*generateName: */, ""); sub(/-$/, ""); print; exit }' "$file")
  if [[ -z "$prefix" ]]; then
    echo "error: $file has no metadata.generateName line to derive a prefix from" >&2
    exit 1
  fi
  ts=$(date -u +%Y%m%d-%H%M%S)
  name="${prefix}-${ts}"
fi

# Inline-edit: drop generateName, inject explicit name. sed is fine here since
# each scenario yaml has exactly one generateName line.
created=$(sed "s|^\([[:space:]]*\)generateName:.*|\1name: ${name}|" "$file" \
            | kubectl create -f - -o jsonpath='{.metadata.name}')
echo "Submitted workflow: $created"
argo wait -n dlh-test-fw "$created" || true
status=$(kubectl -n dlh-test-fw get workflow "$created" -o jsonpath='{.status.phase}')
echo "Final phase: $status"
echo "Report artifact: argo get -n dlh-test-fw $created  # shows the MinIO key in the artifact section, or:"
echo "                 kubectl -n dlh-test-fw exec deploy/dlh-minio -- mc cat \"local/artifacts/${created}-main-*/verdict/report.json\" | jq ."
[[ "$status" == "Succeeded" ]]
