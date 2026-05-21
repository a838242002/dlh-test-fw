# dlh-controlplane

Read-only viewer + (later) submission API for dlh-test-fw scenarios.
Phase B ships only the read path. See
`docs/superpowers/specs/2026-05-21-dlh-controlplane-design.md`.

## Build

```
make codegen    # regenerate from api/openapi.yaml
make ui-build   # build the React app into web/dist
make build      # build the Go binary (embeds web/dist)
make image      # docker build
make reload-minikube   # force kubelet to pick up the new image
```

## Layout

- `cmd/dlh-controlplane/main.go` — entry point
- `api/openapi.yaml` — single source of truth for the API
- `internal/` — backend packages
- `web/` — React UI (Vite + Tailwind)
- `deploy/` — k8s manifests applied by Argo CD
