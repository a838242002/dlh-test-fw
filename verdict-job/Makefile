SHELL := /usr/bin/env bash
.PHONY: test build image load-image clean

test:
	go test ./...

build:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/verdict ./cmd/verdict

image:
	docker build -t dlh-verdict:0.1.0 .

# Load into the running minikube so chart's image ref resolves.
load-image: image
	minikube image load dlh-verdict:0.1.0

clean:
	rm -rf bin/
