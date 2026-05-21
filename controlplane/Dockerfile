# syntax=docker/dockerfile:1.7

# --- UI build stage ---------------------------------------------------------
FROM node:20-alpine AS ui
WORKDIR /src
RUN npm install -g pnpm@10.33.0
# Copy lockfiles first for better Docker layer caching.
COPY web/package.json web/pnpm-lock.yaml ./web/
RUN cd web && pnpm install --frozen-lockfile
# Copy the rest of the UI source plus the API spec.
COPY api ./api
COPY web ./web
RUN cd web && \
    pnpm openapi-typescript ../api/openapi.yaml -o src/api/gen.ts && \
    pnpm build

# --- Go build stage --------------------------------------------------------
FROM golang:1.26-alpine AS gobuild
WORKDIR /src
# Cache module downloads.
COPY go.mod go.sum ./
RUN go mod download
# Bring in all Go sources.
COPY . .
# Replace the locally-built dist (if any leaked in via COPY .) with the
# image's freshly built bundle from the ui stage.
RUN rm -rf internal/api/dist
COPY --from=ui /src/web/dist ./internal/api/dist
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/dlh-controlplane ./cmd/dlh-controlplane

# --- Runtime stage ---------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot
USER 65532
COPY --from=gobuild /out/dlh-controlplane /dlh-controlplane
EXPOSE 8080
ENTRYPOINT ["/dlh-controlplane"]
