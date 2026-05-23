# Quickstart Simplification — `scripts/quickstart.sh`

*Design doc — 2026-05-24. Output of `superpowers:brainstorming`.*

## Problem

The local-dev first-run experience is a **10-step manual sequence** in the
README mixing `minikube`, server-side CRD apply, four separate image
build+load cycles, `helm install`, controlplane deploy + env patch, MinIO
fixture seeding via `mc`, a target deploy, and finally the scenario submit.
It interleaves several `port-forward … &` backgrounded in the user's
interactive shell, `sleep`s, and ordering constraints. A newcomer has many
opportunities to mis-order, drop a port-forward, or hit the image cache
trap, and there is no single "land me at a green verdict" entry point.

## Goal

One command takes a developer from a **running minikube** to a **green
`VERDICT: PASS`**, with clear staged progress and a copy-pasteable
"Next steps" block for ongoing UI/Grafana access. (Goals, per
brainstorming: *fewer commands to type* + *better first-run experience*.)

Non-goal: replacing the GitOps/production path (`argocd/`), or managing the
minikube lifecycle (that stays `scripts/minikube-up.sh`).

## Decisions (from brainstorming)

| Question | Decision |
|---|---|
| Form factor | A single bash script `scripts/quickstart.sh`, sibling to `minikube-up.sh`. A `make quickstart` one-line alias delegates to it. |
| Plan-18 "no scripts" | Deliberate, **documented exception** — same justification that kept `minikube-up.sh` (bootstrap needs real control flow Make can't express: live port-forwards, wait loops, idempotency, progress). |
| Scope | Everything **except** the minikube reset. Assumes a running cluster; checks and bails with guidance if not. |
| Re-run behavior | Idempotent / skip-if-done, plus a `--rebuild` flag that forces the (slow) image + CLI rebuilds. No `--from-step N` (skip-if-done already gives resumability; numbering contract is brittle). |
| Port-forwards | Ephemeral, trap-cleaned; prefer `kubectl cp`/`exec` to avoid them entirely where possible. Print a Next-steps block with the port-forward commands + URLs + live-fetched Grafana creds at the end. No orphan processes. |
| Final verdict run | Submit **lightened** chaos params so the newcomer sees `VERDICT: PASS`. Print a one-line note that defaults are heavier and FAIL by design. |

## Form factor & UX

A single `scripts/quickstart.sh`:

- `set -euo pipefail`.
- Structured progress: `▶ [4/9] Installing platform…`, `✓ [4/9] skipped (helm release present)`, `✗` on failure with the failing command.
- An `EXIT` trap that kills any port-forward PIDs the script spawned (and removes temp files), so a `Ctrl-C` or failure never leaves orphans.
- Flags: `--rebuild` (force image + CLI rebuilds), `--with-kafka` (append the kafka target + `kafka-broker-partition` run), `--help`.
- `make quickstart:` target is a one-liner delegating to the script, so `make` users discover it next to the other platform targets.

## Preflight & safety gates

Run **before any mutating action**; fail fast.

1. **Tool check** — verify `kubectl helm docker make go pnpm mc jq minikube` on PATH; collect and print *all* missing tools, then exit non-zero.
2. **Cluster-up check** — `minikube status`; if not running, print
   `Run scripts/minikube-up.sh first.` and exit. (We never reset for the user.)
3. **Context safety gate** — `kubectl config current-context` MUST be
   `minikube`. This is the load-bearing guard from CLAUDE.md's "scripts are
   unsafe on shared clusters" — refuse to proceed against any other context.

## Step sequence

Idempotent; each step's skip-check makes a re-run converge cheaply. `--rebuild`
bypasses the image/CLI skip-checks.

| # | Step | Action | Skip-check |
|---|------|--------|-----------|
| 1 | CRDs | `helm template … --include-crds` → server-side apply → stamp Helm ownership (the existing `make platform-crds` logic) | chaos-mesh CRDs already `Established` |
| 2 | Images | Build + `minikube image load` each: `dlh-fixture-{mysql,kafka,doris}:0.1.0`, `ghcr.io/dlh/dlh-k6:0.1.0`, `ghcr.io/dlh/dlh-verdict:0.1.0`, `ghcr.io/dlh/dlh-controlplane:0.1.0` (controlplane needs `make ui-build` first → pnpm) | each tag present in `minikube image ls` |
| 3 | Platform | `helm dependency update` + `helm upgrade --install dlh … -f values.yaml -f values-minikube.yaml --wait --timeout 5m` | always runs (converging; cheap no-op) |
| 4 | Controlplane | `kubectl apply -f controlplane/deploy/` + `set env … DLH_AUTH_DISABLED=true` + `rollout status --timeout=120s` | rollout status gates readiness |
| 5 | CLI | `cd controlplane && make cli`; add `bin/` to the script's own PATH | binary exists |
| 6 | Seed MinIO | **port-forward-free**: `kubectl cp fixtures/mysql-users.sql <minio-pod>:/tmp/` then `kubectl exec … -- mc alias set … && mc cp /tmp/… local/fixtures/mysql-users.sql` | object already in bucket (`mc stat` via exec) |
| 7 | mysql target | `kubectl apply -f targets/mysql/deploy.yaml` + `rollout status -n mysql-sys deploy/mysql` | deployment exists & Available |
| 8 | Submit run | ephemeral `port-forward svc/dlh-controlplane 8080:80` (trap-cleaned) + `DLH_ENDPOINT`/`DLH_TOKEN` + `dlh run mysql-pod-delete --wait -p load_duration=180s -p chaos_duration=15s -p chaos_start_after=60s` | — |
| 9 | Report | Print verdict (dlh streams it) + Next-steps block | — |

`--with-kafka` appends: `kubectl apply -f targets/kafka/deploy.yaml` +
`rollout status statefulset/kafka` + `dlh run kafka-broker-partition --wait`.

## Next-steps output

After the run, print a copy-pasteable block:

```
✓ Quickstart complete — VERDICT: PASS

Ongoing access (run in a spare terminal):
  Controlplane UI : kubectl -n dlh-test-fw port-forward svc/dlh-controlplane 8080:80
                    → http://localhost:8080
  Grafana         : kubectl -n dlh-test-fw port-forward svc/dlh-grafana 3001:80
                    → http://localhost:3001   (admin / <fetched live>)

Use the dlh CLI:
  export PATH="$PWD/controlplane/bin:$PATH"
  export DLH_ENDPOINT=http://localhost:8080
  export DLH_TOKEN='fake:dev:dev@example.com:dlh-admins'
  dlh run mysql-pod-delete --wait              # defaults FAIL the SLO by design
  dlh run mysql-pod-delete --wait -p chaos_duration=15s   # lightened → PASS

Note: this quickstart ran lightened chaos so the verdict is PASS. The
default mysql-pod-delete is heavier and FAILs the SLO on purpose.
```

- Grafana credentials fetched **live** from the `grafana-admin-credentials`
  Secret (keys `admin-user` / `admin-password`) — *not* a hardcoded value,
  and not the stale `dlh-grafana-credentials` name still in the README.

## Documentation changes

- **README Quickstart** — rewrite to lead with
  `scripts/minikube-up.sh && scripts/quickstart.sh`. Move the current
  10-step sequence into a collapsed "Manual steps / what quickstart does
  under the hood" appendix (kept for transparency, not deleted). Fix the
  stale `dlh-grafana-credentials` → `grafana-admin-credentials` reference.
- **CLAUDE.md** — under "Operational model: local-dev", add
  `scripts/quickstart.sh` next to `scripts/minikube-up.sh` and note it as
  the second sanctioned script (with the exception rationale).

## Testing / verification

- `bash -n scripts/quickstart.sh` (syntax) + `shellcheck` clean.
- Manual end-to-end on a fresh minikube: `scripts/quickstart.sh` → ends
  `VERDICT: PASS`; re-run immediately → all steps report `✓ skipped`, fast.
- Safety gates exercised: wrong kube-context → refuses; minikube down →
  instructs `minikube-up.sh`; a missing tool → lists it.
- `--rebuild` forces image rebuilds; `--with-kafka` adds the kafka run.

## Out of scope

- GitOps/production bootstrap (`argocd/`, `docs/operations/bootstrap-via-argocd.md`).
- minikube lifecycle (stays `scripts/minikube-up.sh`).
- Any change to the controlplane, chart, or scenario behavior — this is
  orchestration + docs only.
