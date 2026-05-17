SHELL := /usr/bin/env bash
.PHONY: platform-up platform-down platform-verify chart-lint fixture-images verify-templates sync-dashboards run-mysql

sync-dashboards:
	cp dashboards/grafana/*.json helm/dlh-test-fw/files/dashboards/

run-mysql:
	./scripts/run-scenario.sh scenarios/mysql-pod-delete.yaml

fixture-images:
	for d in mysql doris kafka; do \
	  docker build -t dlh-fixture-$$d:0.1.0 fixture-images/$$d && \
	  minikube image load dlh-fixture-$$d:0.1.0 ; \
	done

verify-templates:
	./scripts/verify-templates.sh

chart-lint:
	helm lint helm/dlh-test-fw

platform-up: chart-lint
	./scripts/platform-up.sh

platform-down:
	./scripts/platform-down.sh

platform-verify:
	./scripts/platform-verify.sh

# --- k6 image (Plan 6) ---
.PHONY: k6-image k6-smoke

k6-image:
	$(MAKE) -C fixture-images/k6 image

k6-smoke:
	$(MAKE) -C fixture-images/k6 smoke
