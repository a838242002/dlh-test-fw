SHELL := /usr/bin/env bash
.ONESHELL:
.SHELLFLAGS := -euo pipefail -c

IMG ?= ghcr.io/dlh/dlh-controlplane:0.1.0

.PHONY: test lint build ui-build ui-install image load-image reload-minikube codegen clean

test:
	go test ./...

lint:
	go vet ./...

build: ui-build
	CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/dlh-controlplane ./cmd/dlh-controlplane

ui-install:
	cd web && pnpm install --frozen-lockfile

ui-build: ui-install
	cd web && pnpm build
	rm -rf internal/api/dist
	cp -R web/dist internal/api/dist

codegen:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
	    -config api/oapi-codegen-server.yaml \
	    api/openapi.yaml
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
	    -config api/oapi-codegen-types.yaml \
	    api/openapi.yaml
	cd web && pnpm openapi-typescript ../api/openapi.yaml -o src/api/gen.ts

image:
	docker build -t $(IMG) .

load-image: image
	minikube image load $(IMG)

reload-minikube: image
	minikube ssh -- "docker ps -aq --filter ancestor=$(IMG) | xargs -r docker rm -f"
	minikube ssh -- docker rmi -f $(IMG) || true
	minikube image load $(IMG)

clean:
	rm -rf bin web/dist
