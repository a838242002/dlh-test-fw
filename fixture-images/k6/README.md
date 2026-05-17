# dlh-k6 — custom k6 image with xk6 plugins

Custom k6 binary with three community xk6 plugins bundled in:

| Plugin | Version | What it gives k6 |
|---|---|---|
| `xk6-sql` | v1.0.6 | `k6/x/sql` module — generic SQL driver entrypoint |
| `xk6-sql-driver-mysql` | v0.2.2 | MySQL driver consumed by `xk6-sql` (covers Doris too — Doris is MySQL-protocol compatible for queries) |
| `xk6-kafka` | v1.3.0 | `k6/x/kafka` module — Writer/Reader for Kafka |

Base k6 is v1.6.1 (last release on the `go.k6.io/k6` v1 module path; xk6-sql v1.1.0+ and xk6-sql-driver-mysql v0.3.0+ moved to `go.k6.io/k6/v2`, which conflicts with xk6-kafka v1.3.0).

Plus a baked-in script tree at `/scripts/lib/` (primitives) and `/scripts/runners/` (generic per-target runners).

## Build

    make image            # builds ghcr.io/dlh/dlh-k6:0.1.0 locally
    make smoke            # runs xk6 module link check inside a throwaway container

From the repo root, the same targets are exposed as `make k6-image` and `make k6-smoke`.

## Load into minikube

    minikube image load ghcr.io/dlh/dlh-k6:0.1.0

## Reload after a code change

The k6-operator runner uses `imagePullPolicy: Never` (after Plan 7) and minikube caches by image ID, so a `docker build` with the same tag will NOT replace what's already running. Force a clean reload:

    minikube ssh -- "docker ps -aq --filter ancestor=ghcr.io/dlh/dlh-k6:0.1.0 | xargs -r docker rm -f"
    minikube ssh -- docker rmi -f ghcr.io/dlh/dlh-k6:0.1.0
    make image
    minikube image load ghcr.io/dlh/dlh-k6:0.1.0

The same pattern bit Plan 3 (verdict-job image). Same fix, same script.

## Bumping a plugin version

Edit the `ARG` line near the top of `Dockerfile`, then `make image` to rebuild.
