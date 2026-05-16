#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

# Pre-flight: minikube must be Ready.
if ! kubectl get nodes 2>/dev/null | grep -q ' Ready '; then
  echo "minikube not Ready. Run: ./spikes/k6-vm-remote-write/scripts/minikube-up.sh" >&2
  exit 1
fi

helm repo add argo https://argoproj.github.io/argo-helm || true
helm repo add litmuschaos https://litmuschaos.github.io/litmus-helm/ || true
helm repo add grafana https://grafana.github.io/helm-charts || true
helm repo add bitnami https://charts.bitnami.com/bitnami || true
helm repo add victoria-metrics https://victoriametrics.github.io/helm-charts/ || true
helm repo update

helm dependency update helm/dlh-test-fw

helm upgrade --install dlh helm/dlh-test-fw \
  -n dlh-test-fw --create-namespace \
  -f helm/dlh-test-fw/values.yaml \
  -f helm/dlh-test-fw/values-minikube.yaml \
  --wait --timeout 10m
