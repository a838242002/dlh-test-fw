# syntax=docker/dockerfile:1
# NOTE: plan specified golang:1.22-alpine, but local toolchain wrote go 1.26.3 to go.mod.
# Using golang:1.23-alpine (alpine images for 1.26 not yet on Docker Hub at build time).
# Go's toolchain directive will auto-download the required toolchain on `go build`.
FROM golang:1.23-alpine AS build
# Enable the toolchain directive in go.mod (`go 1.26.3`) to auto-download a
# matching Go via the GOTOOLCHAIN mechanism. Alpine's base image defaults
# this to `local` which blocks downloads.
ENV GOTOOLCHAIN=auto
# alpine doesn't ship git; the toolchain installer + module fetcher need it.
RUN apk add --no-cache git
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/verdict ./cmd/verdict

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/verdict /verdict
USER nonroot:nonroot
ENTRYPOINT ["/verdict"]
