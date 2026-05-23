SHELL := /usr/bin/env bash
.PHONY: platform-up platform-down platform-verify chart-lint fixture-images sync-dashboards run-mysql run-kafka

# NOTE (Plan 18): the platform-up/down/verify and run-scenario shell scripts
# were removed. These targets are thin convenience wrappers around the
# canonical `helm` + `dlh` commands documented in CLAUDE.md's
# "Operational model: GitOps vs local-dev" section. They are LOCAL-DEV only;
# production clusters are GitOps-managed via argocd/ (see
# docs/operations/bootstrap-via-argocd.md).

sync-dashboards:
	cp dashboards/grafana/*.json helm/dlh-test-fw/files/dashboards/

fixture-images:
	for d in mysql doris kafka; do \
	  docker build -t dlh-fixture-$$d:0.1.0 fixture-images/$$d && \
	  minikube image load dlh-fixture-$$d:0.1.0 ; \
	done

chart-lint:
	helm lint helm/dlh-test-fw

platform-up: chart-lint
	helm dependency update helm/dlh-test-fw
	helm upgrade --install dlh helm/dlh-test-fw \
	  -f helm/dlh-test-fw/values.yaml \
	  -f helm/dlh-test-fw/values-minikube.yaml \
	  --namespace dlh-test-fw --create-namespace --wait --timeout 5m

platform-down:
	helm uninstall dlh -n dlh-test-fw

platform-verify:
	helm test dlh -n dlh-test-fw

# Scenario submission goes through the controlplane API. Build the CLI with
# `cd controlplane && make cli`, then run e.g. `dlh run mysql-pod-delete --wait`.
# These targets assume `dlh` is on PATH and DLH_ENDPOINT points at the
# controlplane (default http://localhost:8080 via port-forward).
run-mysql:
	dlh run mysql-pod-delete --wait

run-kafka:
	dlh run kafka-broker-partition --wait

# --- k6 image (Plan 6) ---
.PHONY: k6-image k6-smoke

k6-image:
	$(MAKE) -C fixture-images/k6 image

k6-smoke:
	$(MAKE) -C fixture-images/k6 smoke
