SHELL := /usr/bin/env bash
.PHONY: platform-up platform-down platform-verify platform-crds chart-lint fixture-images sync-dashboards run-mysql run-kafka k6-reload quickstart

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

# platform-crds: server-side-apply all CRDs from the umbrella chart.
# Required once on a clean cluster before make platform-up, because several
# Chaos Mesh CRDs exceed Helm's 256 KB client-side-apply annotation limit and
# prevent the chart from installing its own crds/ directory.
# The kubectl label/annotate step hands Helm ownership of the CRDs so that
# subsequent `helm upgrade --install` does not refuse to manage them.
platform-crds: chart-lint
	helm dependency update helm/dlh-test-fw
	helm template dlh helm/dlh-test-fw \
	  -f helm/dlh-test-fw/values.yaml \
	  -f helm/dlh-test-fw/values-minikube.yaml \
	  --include-crds \
	  | awk '/^---/{p=0} /kind: CustomResourceDefinition/{p=1} p{print}' \
	  > /tmp/dlh-crds.yaml
	kubectl apply --server-side --force-conflicts -f /tmp/dlh-crds.yaml
	kubectl wait --for=condition=Established crd --all --timeout=120s
	kubectl label -f /tmp/dlh-crds.yaml \
	  app.kubernetes.io/managed-by=Helm --overwrite
	kubectl annotate -f /tmp/dlh-crds.yaml \
	  meta.helm.sh/release-name=dlh \
	  meta.helm.sh/release-namespace=dlh-test-fw --overwrite

# quickstart: one-command local-dev bootstrap (running minikube → green
# VERDICT: PASS). Thin alias for scripts/quickstart.sh; see that script for
# flags (--rebuild, --with-kafka). Local-dev only.
quickstart:
	scripts/quickstart.sh

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
.PHONY: k6-image k6-smoke k6-reload

k6-image:
	$(MAKE) -C fixture-images/k6 image

# k6-reload: build the dlh-k6 image AND load it into minikube. Use this for
# local dev; k6-image alone only builds the Docker image without loading.
k6-reload:
	$(MAKE) -C fixture-images/k6 reload-minikube

k6-smoke:
	$(MAKE) -C fixture-images/k6 smoke
