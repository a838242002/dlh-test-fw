#!/usr/bin/env bash
set -euo pipefail

NS=dlh-test-fw
DEADLINE=$(( $(date +%s) + 180 ))

# Find VM single's service. Chart names it victoria-metrics-single-server typically.
VM_SVC=$(kubectl -n "$NS" get svc -l app.kubernetes.io/name=victoria-metrics-single \
  -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)

if [[ -z "$VM_SVC" ]]; then
  echo "FAIL: victoria-metrics-single Service not found in namespace $NS" >&2
  exit 1
fi

# Port-forward in the background; kill on exit.
kubectl -n "$NS" port-forward "svc/$VM_SVC" 8428:8428 >/tmp/vm-pf.log 2>&1 &
PF_PID=$!
trap 'kill $PF_PID 2>/dev/null || true' EXIT
sleep 3

QUERY='sum(k6_http_reqs_total{dlh_scenario="spike-httpbin"})'

while (( $(date +%s) < DEADLINE )); do
  RESP=$(curl -s --get "http://127.0.0.1:8428/api/v1/query" \
    --data-urlencode "query=${QUERY}")
  VAL=$(echo "$RESP" | jq -r '.data.result[0].value[1] // "0"')
  if [[ "$VAL" != "0" && "$VAL" != "null" ]]; then
    echo "PASS: k6_http_reqs_total{dlh_scenario=spike-httpbin} = $VAL"
    exit 0
  fi
  echo "waiting… current value=$VAL"
  sleep 5
done

echo "FAIL: metric did not appear within 180s. Last response: $RESP" >&2
exit 1
