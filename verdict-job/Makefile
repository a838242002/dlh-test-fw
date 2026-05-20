SHELL := /usr/bin/env bash
.ONESHELL:
.SHELLFLAGS := -euo pipefail -c

# Match the canonical tag that helm/dlh-test-fw/values.yaml references
# (platform.verdict.image + tag). The chart sets imagePullPolicy: Never,
# so the kubelet only finds this image if it's loaded under THIS exact tag.
IMG ?= ghcr.io/dlh/dlh-verdict:0.1.0

.PHONY: test build image load-image reload-minikube clean

test:
	go test ./...

build:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/verdict ./cmd/verdict

image:
	docker build -t $(IMG) .

# Load into the running minikube so chart's image ref resolves.
# Caveat: if a pod already pulled this tag, kubelet keeps the cached
# layer — `minikube image load` does NOT replace it. For a fresh rebuild
# in an existing cluster, use `reload-minikube` instead.
load-image: image
	minikube image load $(IMG)

# Force-replace the cached image in minikube. Mirrors
# fixture-images/k6/Makefile reload-minikube. Use after every verdict
# code change so the next workflow run picks up the rebuild.
reload-minikube:
	minikube ssh -- "docker ps -aq --filter ancestor=$(IMG) | xargs -r docker rm -f"
	minikube ssh -- docker rmi -f $(IMG) || true
	$(MAKE) image
	minikube image load $(IMG)

clean:
	rm -rf bin/
