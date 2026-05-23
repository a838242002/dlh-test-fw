# Project Conventions — dlh-test-fw

This file is the orientation guide for AI assistants (and humans) joining the project mid-stream. It covers the working conventions used across Phases 1-2 (platform) and the controlplane migration (Plans 14-19); deviate only with a deliberate reason and a commit-message note.

Last revised: 2026-05-24 (Plan 19 — controlplane Phase F).

---

## Repo layout cheatsheet

```
helm/dlh-test-fw/                Umbrella Helm chart (the platform itself)
helm/.../files/workflowtemplates/scenario/   Chart-managed scenario WorkflowTemplates
                                 (mysql-pod-delete, kafka-broker-partition, doris-be-network-loss — Plan 18)
controlplane/                    Go service + embedded React UI + dlh CLI (Plans 15-19)
                                 cmd/{dlh-controlplane,dlh}, internal/{api,auth,config,k8s,minio,model,runs,chaos,targets,schedules}, web/, deploy/
argocd/                          GitOps manifests: AppProject + ApplicationSet + Applications (Plan 14)
verdict-job/                     Plan 3's Go binary (SLO verdict)
fixture-images/                  Per-target build helpers (mysql, kafka, doris, k6 — one Dockerfile each)
scenarios/                       Historical standalone Workflow YAMLs — REMOVED in Plan 18; only README.md remains
targets/                         Minimal target deploys (mysql, kafka, doris) for scenarios
dashboards/grafana/              Dashboard source-of-truth JSONs
scripts/                         minikube-up.sh only (the rest removed in Plan 18 — see operational model below)
docs/FINDINGS.md                 Authoritative cross-plan gotchas
docs/operations/                 Operator runbooks (bootstrap-via-argocd, register-target, ci-integration)
docs/superpowers/specs/          YYYY-MM-DD-<topic>-design.md  (output of brainstorming)
docs/superpowers/plans/          YYYY-MM-DD-NN-<feature>.md    (output of writing-plans)
```

**Authoritative cross-plan reference: `docs/FINDINGS.md`.** Every plan reads it; every plan that hits an unrelated drift appends to it. When a future session asks "is this still the right approach?", the answer is usually in FINDINGS first.

---

## Branching & worktree conventions

### When to use a worktree

Open a feature-branch + worktree for any work that:
- Touches the live cluster's chart or workflow state (so you can `helm upgrade` from a checkout that's *not* `main` and roll back by checking out main)
- Spans multiple commits / multiple sessions
- Is part of a written plan in `docs/superpowers/plans/`

For one-shot read/repair work (e.g., bumping a dashboard JSON, fixing a doc typo) you can work directly on `main`.

### Branch naming

- `feat/<topic>` — feature work (e.g., `feat/phase-2-scripts-dashboards`, `feat/plan3-verdict`)
- `fix/<topic>` — non-feature bugfix that doesn't justify a plan
- `spike/<topic>` — exploratory; may not survive to main

The `phase-N-<topic>` pattern is reserved for cross-plan milestones. The `plan-N-<topic>` pattern is reserved for single-plan branches.

### Worktree layout

Sibling-directory pattern, matching Phase 1's:

```
/Users/allen/repo/
├── dlh-test-fw/                ← main worktree, on `main`
├── dlh-test-fw-phase2/         ← e.g. feat/phase-2-scripts-dashboards
└── dlh-test-fw-plan7/          ← single-plan branches use this naming
```

**Create** (manual git fallback):
```
git worktree add ../dlh-test-fw-<short-name> -b feat/<branch-name> main
```

**If a native worktree tool (`EnterWorktree` or similar) is available in the harness, prefer it over `git worktree add` — it avoids phantom state the harness can't see.**

**Remove after merge**:
```
git worktree remove ../dlh-test-fw-<short-name>     # add --force if a build artifact lingers
git branch -d feat/<branch-name>                    # safe-delete (refuses if unmerged)
```

### Merge style

`--no-ff` for every plan/milestone landing on `main`:

```
git checkout main
git merge --no-ff feat/<branch> -m "Merge feat/<branch>: <one-line summary>

<multi-line body explaining what landed and any drift from the plan>"
```

This produces a grep-able boundary in `git log --first-parent`. The atomic per-task commits are preserved for archaeology, but day-to-day `--first-parent` shows one merge per plan.

Phase 1's main log demonstrates the pattern — every plan is one merge commit; the lines underneath are the per-task atomic commits.

### Rebase before resuming a stale worktree

If a worktree branched off `main` was paused while `main` advanced, rebase before starting:

```
cd ../dlh-test-fw-<name>
git rebase main
```

Plan 5's worktree did this and it was clean.

---

## Operational model: GitOps vs local-dev

After Plan 14, the framework cluster has two operational modes — pick
the one that matches your environment.

### Local-dev (laptop minikube)

After Plan 18, only `scripts/minikube-up.sh` remains. Everything else is
covered by the `dlh` CLI + standard `helm` commands:

- `scripts/minikube-up.sh` — destructive cluster reset (only remaining script)
- `cd controlplane && make ui-build && make build` — build the binary
- `helm upgrade --install dlh helm/dlh-test-fw -f helm/dlh-test-fw/values.yaml -f helm/dlh-test-fw/values-minikube.yaml -n dlh-test-fw --create-namespace` — install the chart (replaces `platform-up.sh`)
- `helm uninstall dlh -n dlh-test-fw` — teardown (replaces `platform-down.sh`)
- `helm test dlh -n dlh-test-fw` — verify (replaces `platform-verify.sh`)
- `kubectl -n dlh-test-fw get workflowtemplates` — list scenarios (replaces `verify-templates.sh`)
- `dlh run mysql-pod-delete --wait` — submit a scenario (replaces `run-scenario.sh`)

### Production / shared cluster (GitOps via Argo CD)

Use the manifests in `argocd/`:

- `argocd/appproject.yaml` — `AppProject dlh-test-fw` (security scope).
- `argocd/apps/dlh-test-fw-chart.yaml` — umbrella chart Application.
- `argocd/apps/dlh-controlplane.yaml` — placeholder for the companion spec.
- `argocd/appset/dlh-platform.yaml` — single-apply ApplicationSet (mutually
  exclusive with the per-app manifests; pick one).
- `argocd/values/framework/chart-values.yaml` — values overlay template
  with `REPLACE-*` placeholders.

Full procedure: `docs/operations/bootstrap-via-argocd.md`.

### Which to use

- Laptop development → local-dev.
- Anything someone else shares with you (preprod / prod) → GitOps. The
  scripts are not safe in shared environments because manual changes
  get reverted by Argo CD's self-heal.
- In any doubt → check whether Argo CD is installed in the target
  cluster. If yes, GitOps. If no, scripts.

## dlh-controlplane (Phase B onwards)

After Plan 15, `controlplane/` is a Go service that exposes the framework
cluster's runtime state via REST + an embedded React UI. Phase B is
read-only — Phase C added `POST /api/runs` and the `dlh run` CLI.
Phase E (Plan 18) removed `run-scenario.sh` entirely.

### Layout

- `cmd/dlh-controlplane/main.go` — entry
- `api/openapi.yaml` — single source of truth (do not hand-edit handlers' request/response types; regenerate)
- `internal/{api,auth,config,k8s,minio,model}/` — backend packages
- `web/` — Vite + React + Tailwind SPA, generated client from openapi-typescript
- `deploy/` — k8s manifests (plain YAML — Argo CD `directory:` source)
- `Makefile` — `codegen`, `ui-build`, `build`, `image`, `reload-minikube`

### Local dev

```
make codegen        # regenerate from OpenAPI
make ui-build       # build the React app and copy into internal/api/dist
DLH_AUTH_DISABLED=true go run ./cmd/dlh-controlplane
```

Fake tokens for `DLH_AUTH_DISABLED=true` mode:
`Authorization: Bearer fake:<sub>:<email>:<group1,group2>`

### Auth model

- OIDC bearer tokens verified against `DLH_OIDC_ISSUER_URL`.
- Groups claim (default `groups`) drives role binding via the
  `dlh-roles` ConfigMap (`bindings.yaml` data key).
- Roles: viewer < runner < admin.
- Phase B only requires viewer for all read endpoints (Phase C will add
  RequireRole(runner) on submit/cancel).

### Phase D additions (Plan 17)

- Registered remote targets live in the `dlh-targets` ConfigMap +
  per-target `dlh-target-<id>` Secrets (each holding `kubeconfig`).
- `POST /api/runs` accepts optional `targetId`; the Workflow gets
  `dlh.target` label + `target_id` workflow argument. Chaos WTs
  forward the latter via `?targetID=...` query in /internal/chaos calls.
- `chaos.Router` picks Local vs Remote per call. Empty targetID is
  the default — preserves Phase C behaviour.
- New endpoints: `GET /api/targets`, `GET /api/targets/{id}`,
  `POST /api/targets/{id}/test`.
- New UI page: Targets (read-only + test-connection button). New
  control in Scenarios cards: a TargetPicker dropdown.
- New CLI flag: `dlh run --target <id>` / `dlh runs ls --target <id>`.
- Operator runbook: `docs/operations/register-target.md`.
- Each scenario's chaos step must declare `target_id` in its
  arguments block (Plan 17 fixed mysql-pod-delete; the kafka + doris
  scenarios may need the same pattern — see Plan 17 FINDING #10).

### Phase F additions (Plan 19)

- Schedules (Argo `CronWorkflow`) are first-class resources via the
  controlplane:
  - `POST /api/schedules` + `GET /api/schedules{,/<id>}` + `DELETE` +
    `POST /api/schedules/<id>/{pause,resume}`.
  - `dlh schedule create / ls / show / pause / resume / delete`.
  - UI Schedules page with inline create form.
- A scheduled run carries `dlh.scenario` + `dlh.target` labels via
  `spec.workflowMetadata.labels` on the CronWorkflow — Plan 17 Syncer
  picks it up automatically; no submitter changes needed.
- Run detail surfaces `triggeredBy.{kind, id}` when the firing
  Workflow has a CronWorkflow owner reference; UI links to /schedules.
- Role extended to grant cronworkflows alongside workflows verbs.

### controlplane UI refresh

- `controlplane/web` uses **shadcn/ui** primitives (vendored in `src/components/ui/`)
  + a dark-default indigo theme with a light toggle (`src/lib/theme.tsx`,
  persisted under `localStorage["dlh-theme"]`).
- Runs is a dashboard landing: stat cards computed client-side
  (`src/lib/stats.ts`) + a polling (5s) runs table.
- Verdict rendering is `src/components/VerdictView.tsx` driven by
  `src/lib/verdict.ts` (parses the verdict-job `report.json` `overall` +
  `thresholds`). Pure logic in `src/lib/` is unit-tested with **Vitest**
  (`pnpm test`); everything else is gated by `pnpm build`.
- shadcn deps are pnpm-managed — adding components updates `pnpm-lock.yaml`,
  which MUST be committed (CI's `make ui-build` uses `--frozen-lockfile`).
- UI optimization pass (Plan `2026-05-23-01`): refined top-nav (icons + active
  pill, `max-w-7xl`), Runs filter bar (search/status/category/time/failed-only)
  + Duration/Verdict columns (Verdict derived from `Run.score` 1/0/null), grouped
  Scenarios by derived category, redesigned Run detail (meta strip, verdict-first,
  Argo group-node steps hidden). Pure logic in `src/lib/{time,category,run,runsFilter,steps,format}.ts`
  is Vitest-tested. `deriveCategory`/`deriveTargetType` are heuristic on scenario id.

## Image build + minikube reload

We have three local images (`dlh-verdict`, `dlh-k6`, plus the three fixture images). They live at `ghcr.io/dlh/*:<tag>` but are never pushed — they're built locally and `minikube image load`-ed.

### The cache trap (bit us twice — Plans 3 and 6)

`minikube image load` writes the image into minikube's docker registry, but if a pod was already pulled with the same tag and `imagePullPolicy: Never`, the kubelet keeps the cached layer. **Re-pushing the same tag does NOT replace what's running.**

Force the reload:

```
minikube ssh -- "docker ps -aq --filter ancestor=<image>:<tag> | xargs -r docker rm -f"
minikube ssh -- docker rmi -f <image>:<tag> || true
docker build -t <image>:<tag> .
minikube image load <image>:<tag>
```

`fixture-images/k6/Makefile` and `verdict-job/Makefile` each have a `reload-minikube` target that runs this sequence.

### `imagePullPolicy: Never` is intentional

Used in `verdict-job` (slo-eval WT) and `dlh-k6` (load-k6-run WT, Plan 7) because the registry prefix `ghcr.io/dlh/` is not actually published; kubelet would try a registry pull and fail. `Never` means "use the local image unconditionally".

---

## Custom k6 image (`dlh-k6`) — short version

- Built by `make k6-image` from `fixture-images/k6/Dockerfile`
- Bundles `xk6-sql` (covers MySQL + Doris query) and `xk6-kafka`
- `xk6-sql-driver-mysql` is bundled inside `xk6-sql` itself (was a separate module pre-2025; consolidated)
- Baked scripts at `/scripts/lib/{common,mysql,kafka,doris,smoke}.js` and `/scripts/runners/{mysql,kafka,doris}.js`
- Smoke target: `make k6-smoke` runs `k6 version` + `k6 run /scripts/lib/smoke.js`

Plan 7 switches `load/k6-run` WorkflowTemplate's `runner.image` from `grafana/k6:0.50.0` to `ghcr.io/dlh/dlh-k6:0.1.0` and replaces the `script_configmap` parameter with `script_path`.

---

## Sticky gotchas (don't relearn these)

1. **`dlh_scenario`, not `scenario`** — k6 reserves the `scenario` label for its internal scenario name (always `default` in our case). Every user-facing partitioning label in PromQL filters / dashboards / verdict metrics is **`dlh_scenario`**. This is non-negotiable.

2. **k6 prom-rw emits gauges, not histogram buckets** — `histogram_quantile(... _seconds_bucket)` returns nothing for k6 metrics. Use the pre-computed quantile gauges (`*_p95`, `*_p99`, controlled by `K6_PROMETHEUS_RW_TREND_STATS`).

3. **Verdict output is an Argo artifact, NOT a ConfigMap** — the original design used `dlh-result-<workflow>` ConfigMap + Grafana Infinity datasource. Switched in commit `e136e9a` to MinIO artifact + VM gauges (`dlh_verdict_*`). Dashboards query via PromQL only — no Infinity.

4. **Bitnami's 2025 secure-images migration broke arm64** — the MinIO Bitnami subchart is unusable. We ship an in-tree replacement at `helm/dlh-test-fw/templates/minio.yaml` (dev-grade — no auth on the wire, emptyDir-backed; promote to keyFile/PVC before any shared deploy). The MongoDB in-tree workaround is gone — Plan 12 retired Litmus and with it the only consumer of MongoDB.

5. **Chaos Mesh chaos-daemon runtime defaults to containerd** — minikube uses docker, so the chart values block must override `chaosDaemon.runtime: docker` + `chaosDaemon.socketPath: /var/run/docker.sock`. Symptom of the default: NetworkChaos sticks at NotInjected with `error while getting PID: expected containerd:// but got docker://`.

6. **MinIO pinned to `RELEASE.2024-12-13T22-19-12Z`** — newer releases removed the admin console from the community edition. Keep the pin or accept losing the browser UI.

7. **VM lookback-delta is 5 minutes** — a single end-of-run gauge push (`dlh_verdict_*`) goes stale to instant queries after 5 min. Dashboards wrap them in `last_over_time(...[7d])`.

8. **Datasource UIDs must be pinned in chart values** — without explicit `uid:` in `grafana.datasources.datasources.yaml`, Grafana auto-assigns random UIDs and dashboards' `datasource.uid` references break.

9. **Workflow names are timestamps** — the controlplane `Submitter` sets `metadata.name: <prefix>-YYYYMMDD-HHMMSS` (previously done by `run-scenario.sh`, removed in Plan 18). Sortable; no random Argo suffixes.

---

## Doc workflow

- **Spec** (`docs/superpowers/specs/YYYY-MM-DD-<topic>-design.md`) — one per milestone, written from `superpowers:brainstorming`
- **Plan** (`docs/superpowers/plans/YYYY-MM-DD-NN-<feature>.md`) — one per executable unit, written from `superpowers:writing-plans` against a spec. Multiple plans per spec is fine.
- Plans are historical — they reflect intent at planning time. Real execution often differs (chart version drift, missing API features, etc.). Capture deviations in the merge commit body and in `FINDINGS.md`'s appended section.
- The spec gets a "Post-Phase-N amendments" section as living architecture truth diverges from the original brainstorm. See `docs/superpowers/specs/2026-05-16-chaos-loadtest-platform-design.md` end for the pattern.

---

## What NOT to do

- Don't commit on `main` if a feature branch + worktree already exists for that line of work. Use the worktree.
- Don't `helm upgrade` from a worktree branch unless you intend to (the live cluster persists state across worktrees, so one worktree's upgrade affects the other's `kubectl get`). If you do upgrade for testing, mention in the commit message.
- Don't delete `docs/FINDINGS.md` — it's load-bearing for every plan.
- Don't introduce a new `bitnami/*` subchart without verifying the image is actually pullable on arm64 (it usually isn't, post-2025).
- Don't use `direction: both` on Chaos Mesh `NetworkChaos` without an explicit `target:` selector — webhook rejects it. Use `direction: to` (or `from`) for one-sided injection.
