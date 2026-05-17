#!/usr/bin/env bash
set -euo pipefail

# Destructive: ensures clean slate. Spike-only behavior.
if minikube status >/dev/null 2>&1; then
  echo "Existing minikube cluster found — deleting (spike requires a clean cluster)."
  minikube delete
fi

minikube start \
  --cpus=6 \
  --memory=12g \
  --disk-size=40g \
  --addons=ingress,metrics-server

# Wait until the API is actually Ready (start returns before kubelet is fully up sometimes).
for i in {1..30}; do
  if kubectl get nodes 2>/dev/null | grep -q ' Ready '; then
    echo "minikube Ready."
    exit 0
  fi
  sleep 2
done

echo "minikube failed to reach Ready within 60s" >&2
kubectl get nodes || true
exit 1
