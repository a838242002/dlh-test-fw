SHELL := /usr/bin/env bash
.PHONY: platform-up platform-down platform-verify chart-lint fixture-images verify-templates

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
