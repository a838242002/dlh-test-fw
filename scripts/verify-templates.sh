#!/usr/bin/env bash
set -euo pipefail
EXPECTED=(
  fixture-minio-load-mysql
  fixture-minio-load-doris
  fixture-kafka-topic-seed
  chaos-pod-delete
  chaos-network-loss
  chaos-kafka-broker-partition
  chaos-from-hub
  load-k6-run
  verdict-slo-eval
)
missing=0
for t in "${EXPECTED[@]}"; do
  if kubectl -n dlh-test-fw get workflowtemplate "$t" >/dev/null 2>&1; then
    echo "OK   $t"
  else
    echo "MISS $t" >&2
    missing=$((missing+1))
  fi
done
if (( missing > 0 )); then
  echo "FAIL: $missing WorkflowTemplates missing" >&2
  exit 1
fi
echo "PASS: all 9 WorkflowTemplates present"
