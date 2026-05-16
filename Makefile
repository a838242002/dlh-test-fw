SHELL := /usr/bin/env bash
.PHONY: platform-up platform-down platform-verify chart-lint

chart-lint:
	helm lint helm/dlh-test-fw

platform-up: chart-lint
	./scripts/platform-up.sh

platform-down:
	./scripts/platform-down.sh

platform-verify:
	./scripts/platform-verify.sh
