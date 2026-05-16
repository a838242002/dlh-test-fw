#!/usr/bin/env bash
# Submit a scenario Workflow and wait for it to finish.
# Usage: scripts/run-scenario.sh scenarios/<name>.yaml
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 scenarios/<name>.yaml" >&2
  exit 2
fi

file=$1
name=$(kubectl create -f "$file" -o jsonpath='{.metadata.name}')
echo "Submitted workflow: $name"
argo wait -n dlh-test-fw "$name" || true
status=$(kubectl -n dlh-test-fw get workflow "$name" -o jsonpath='{.status.phase}')
echo "Final phase: $status"
echo "Report ConfigMap: kubectl -n dlh-test-fw get cm dlh-result-$name -o jsonpath='{.data.result\.json}' | jq ."
[[ "$status" == "Succeeded" ]]
