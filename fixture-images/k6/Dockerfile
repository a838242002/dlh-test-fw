# syntax=docker/dockerfile:1
# dlh-k6 — custom k6 binary with bundled xk6 plugins + script library.
# See README.md for the version bump procedure and the minikube reload trick.

ARG GO_VERSION=1.23
ARG K6_VERSION=v0.57.0
ARG XK6_VERSION=v1.4.3
ARG XK6_SQL_VERSION=v1.0.4
ARG XK6_SQL_DRIVER_MYSQL_VERSION=v0.3.1
ARG XK6_KAFKA_VERSION=v1.3.0

FROM golang:${GO_VERSION}-alpine AS build
# `xk6 build` invokes `go get`/`go build` on remote modules; git + ca-certs required.
RUN apk add --no-cache git ca-certificates
# Re-declare ARGs after FROM (they don't carry across stages).
ARG K6_VERSION
ARG XK6_VERSION
ARG XK6_SQL_VERSION
ARG XK6_SQL_DRIVER_MYSQL_VERSION
ARG XK6_KAFKA_VERSION
# Toolchain autodownload is needed if any module bumps go.mod past the base image's Go.
# xk6 v1.4.3 requires Go >= 1.25, so the toolchain will auto-download.
ENV GOTOOLCHAIN=auto
ENV PATH="/go/bin:${PATH}"
RUN go install go.k6.io/xk6/cmd/xk6@${XK6_VERSION}
RUN mkdir -p /out
# xk6 v1.x uses --k6-version (positional clashes with the default "latest" flag).
RUN xk6 build --k6-version ${K6_VERSION} \
      --with github.com/grafana/xk6-sql@${XK6_SQL_VERSION} \
      --with github.com/grafana/xk6-sql-driver-mysql@${XK6_SQL_DRIVER_MYSQL_VERSION} \
      --with github.com/mostafa/xk6-kafka@${XK6_KAFKA_VERSION} \
      --output /out/k6

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /out/k6 /usr/bin/k6
COPY lib /scripts/lib
COPY runners /scripts/runners
# k6-operator's TestRun spec appends `run --quiet ...` as args, so the entrypoint
# must be EXACTLY the binary path, no shell wrapper, no extra args.
ENTRYPOINT ["/usr/bin/k6"]
