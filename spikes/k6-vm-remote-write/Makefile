SHELL := /usr/bin/env bash
.ONESHELL:
.SHELLFLAGS := -euo pipefail -c

.PHONY: up down verify

up:
	./scripts/minikube-up.sh
	helm repo add victoria-metrics https://victoriametrics.github.io/helm-charts/ || true
	helm repo add grafana https://grafana.github.io/helm-charts || true
	helm repo update
	kubectl create namespace dlh-test-fw --dry-run=client -o yaml | kubectl apply -f -
	helm upgrade --install vm victoria-metrics/victoria-metrics-single \
	  --version 0.12.0 \
	  -n dlh-test-fw \
	  -f helm/vm-values.yaml --wait
	helm upgrade --install k6-operator grafana/k6-operator \
	  --version 3.5.0 \
	  -n dlh-test-fw \
	  -f helm/k6-operator-values.yaml --wait
	kubectl -n dlh-test-fw apply -f manifests/httpbin.yaml
	kubectl -n dlh-test-fw rollout status deploy/httpbin --timeout=120s
	kubectl -n dlh-test-fw apply -f manifests/k6-script-configmap.yaml
	kubectl -n dlh-test-fw apply -f manifests/k6-testrun.yaml

verify:
	./scripts/verify.sh

down:
	minikube delete
