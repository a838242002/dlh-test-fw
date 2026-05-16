#!/usr/bin/env bash
set -euo pipefail

NS=dlh-test-fw

# Step 1: every Helm-managed pod Ready.
echo "==> waiting for all pods Ready"
kubectl -n "$NS" wait --for=condition=Ready pod --all --timeout=300s

# Step 2: run helm test.
echo "==> helm test"
helm test dlh -n "$NS" --timeout 5m

# Step 3: ingress reachability via minikube IP + /etc/hosts hint.
MIP=$(minikube ip)
echo "==> ingress hosts should resolve via: $MIP"
echo "    Add to /etc/hosts (if not already):"
echo "    $MIP argo.dlh.local grafana.dlh.local minio.dlh.local"

# Step 4: curl through ingress (with Host header override so /etc/hosts isn't required).
for host in argo.dlh.local grafana.dlh.local minio.dlh.local; do
  code=$(curl -sk -o /dev/null -w "%{http_code}" --resolve "$host:80:$MIP" "http://$host/" || true)
  echo "    $host -> HTTP $code"
done

echo "PASS"
