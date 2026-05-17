SHELL := /usr/bin/env bash
.ONESHELL:
.SHELLFLAGS := -euo pipefail -c

IMG ?= ghcr.io/dlh/dlh-k6:0.1.0

.PHONY: image smoke reload-minikube clean

image:
	docker build -t $(IMG) .

smoke: image
	docker run --rm $(IMG) version
	docker run --rm $(IMG) run /scripts/lib/smoke.js

reload-minikube:
	minikube ssh -- "docker ps -aq --filter ancestor=$(IMG) | xargs -r docker rm -f"
	minikube ssh -- docker rmi -f $(IMG) || true
	$(MAKE) image
	minikube image load $(IMG)

clean:
	docker rmi -f $(IMG) || true
