# dlh-controlplane — Design

**Date:** 2026-05-21
**Status:** Draft (brainstormed; pending review)
**Companion spec:** `2026-05-21-argocd-platform-lifecycle-design.md` (platform lifecycle; prerequisite for this spec's deployment story)

---

## 1. Context

After the Argo CD platform-lifecycle work lands, the framework cluster is GitOps-managed but scenario submission still requires `argo submit` / `kubectl get workflow` / `kubectl -n dlh-test-fw exec deploy/dlh-minio -- mc cat …` — i.e. a shell with cluster credentials. In a production-shaped environment this isn't viable:

- Developers and QA cannot get kubectl access to the framework cluster.
- Scenarios may need to inject chaos into target clusters that the framework cluster cannot touch directly via kubectl.
- CI pipelines need a stable, authenticated API surface, not a shell.

This spec describes **dlh-controlplane** — a Go service running inside the framework cluster that exposes a REST API and an embedded web UI for submitting scenarios, viewing runs, and reading verdicts, plus a `dlh` CLI that wraps the same API for power users and CI. The controlplane is the single runtime entry point; everything that used to require a shell happens through it.

The platform-lifecycle layer (Argo CD) is assumed to already be in place — see the companion spec.

## 2. Goals

1. A developer or QA engineer can submit a chaos+load scenario, watch it run, and read its verdict without any shell access to any cluster.
2. CI pipelines can trigger the same scenarios via an authenticated REST API using OIDC token exchange — no long-lived static tokens.
3. Chaos can be injected into target clusters that are network-reachable but operated by a different team (no kubectl access, only a scoped ServiceAccount kubeconfig).
4. The system has a clean upgrade path from today's single-cluster shell-driven model, with each phase shippable and reversible on its own.
5. The new operational surface preserves today's invariants: `dlh_scenario` label discipline, k6 trend-stats quantile gauges, MinIO-as-artifact-store, last_over_time wrapping for verdict gauges.

## 3. Non-goals

- **Scenario editing UI.** Scenarios remain as `WorkflowTemplate` YAML in git. The controlplane reads them, never writes them.
- **Dashboard editor.** Grafana stays the source of truth; the UI deep-links to Grafana panels.
- **Multi-tenancy.** A single controlplane instance serves one team. Tenant boundaries can be added later without re-architecting.
- **Run comparison and trend analytics in the UI.** Grafana already aggregates `dlh_verdict_*` gauges over time.
- **Notifications (Slack, email).** Out of v1. An event-bus interface is left in place for later subscribers.
- **Web terminal / kubectl-in-browser.** Explicitly rejected — that would reintroduce the shell access we're eliminating.
- **Self-service target registration via UI.** Targets are registered through PR (Argo-CD-synced ConfigMap + Secret), not at runtime.
- **Run replay / clone.** Not in v1; trivial to add later.

## 4. Architecture overview

```
┌───────────────────────────────────────────────────────────────────────┐
│ git                                                                   │
│  ├── controlplane/                  ── source + Helm chart fragment   │
│  ├── helm/dlh-test-fw/templates/    ── controlplane Deployment        │
│  │     (referenced by Argo CD App from companion spec)                │
│  └── targets/targets.yaml           ── ConfigMap (registered targets) │
└──────────────────────────┬────────────────────────────────────────────┘
                           │ Argo CD reconciles (companion spec)
                           ▼
┌───────────────────────────────────────────────────────────────────────┐
│ Framework cluster                                                     │
│  ├── argo-workflows, chaos-mesh, vm, grafana, minio                   │
│  └── dlh-controlplane (Deployment + Service + Ingress)                │
│        ├── REST API (OpenAPI-defined)                                 │
│        ├── embedded React UI (go:embed dist/*)                        │
│        ├── OIDC auth + role ConfigMap                                 │
│        ├── ScopedSA → in-cluster k8s API (Workflow CRs)               │
│        ├── /internal/chaos endpoint (called by Workflow steps)        │
│        ├── ChaosClient interface                                      │
│        │     ├── LocalChaosClient (Phase C: framework cluster)        │
│        │     └── RemoteChaosClient (Phase D: per-target kubeconfig)   │
│        ├── Watchdog reconciler (orphan-chaos cleanup)                 │
│        └── MinIO client (manifests, indexes, report.json reads)       │
└──────────────────────────┬────────────────────────────────────────────┘
                           │ per-target kubeconfig Secret
                           ▼
┌───────────────────────────────────────────────────────────────────────┐
│ Target cluster (Phase D)                                              │
│  ├── chaos-mesh (per-cluster install, Argo CD synced)                 │
│  └── target workloads (mysql / kafka / doris / real services)         │
└───────────────────────────────────────────────────────────────────────┘
```

### Key boundary

- **Argo CD** owns *what exists* in the cluster (chart, WorkflowTemplates, dashboards, the controlplane Deployment itself).
- **Controlplane** owns *what runs at submission time* (Workflow CRs, chaos lifecycle, manifest writes).

Argo CD never creates a `Workflow`. The controlplane never deploys a chart. Each layer is the only writer of its own resource types.

## 5. Domain model

| Object | What it is | Backed by |
|---|---|---|
| **Scenario** | A named, parameterisable test (e.g. `mysql-pod-delete`). Static catalog entry. | A `WorkflowTemplate` in the framework cluster, discovered by the controlplane at startup and on informer events. Optional sidecar `scenarios/<name>.meta.yaml` adds description, expected duration, target type, default params, allowed-target filter. |
| **Run** | One execution of a Scenario against a Target. Has params, status, verdict, links. | A `Workflow` CR in the framework cluster (live state) + a `manifest.json` in MinIO (historical state). The controlplane joins both at read time. |
| **Target** | A registered downstream cluster + namespace ("staging-mysql", "preprod-kafka"). | A row in `targets/targets.yaml` ConfigMap (Argo-CD-synced) + a `Secret` holding the remote kubeconfig (also Argo-CD-synced via the chosen secret backend). |
| **Verdict** | The pass/fail/score from a Run. | Pulled from MinIO `report.json` at Run completion. Denormalised summary copied into `manifest.json` for fast list queries. |

## 6. Storage model (MinIO, no relational database)

Two data sources, no separate database:

1. **In-flight / recent runs → Kubernetes API.** Controlplane runs an informer/watch on `Workflow` CRs in the framework namespace. Argo's TTL controller keeps Workflows around for ~7 days. Live status comes from etcd; nothing is persisted by the controlplane for active runs.

2. **Historical runs → MinIO**, structured for prefix LIST queries:

```
artifacts/
  runs/
    by-id/{run-id}/report.json    # verdict-job writes (existing path; normalized)
    by-id/{run-id}/manifest.json  # controlplane writes at submit + on terminal phase
                                  #   {scenario, target, params, started, finished,
                                  #    status, score, workflow_name}
    index/
      by-target/{target}/{YYYY-MM-DD}/{run-id}.json     # manifest copy
      by-scenario/{scenario}/{YYYY-MM-DD}/{run-id}.json # manifest copy
```

- `?target=staging-mysql&since=24h` → `LIST artifacts/runs/index/by-target/staging-mysql/2026-05-21/`. No scan.
- `manifest.json` is the controlplane's record; `report.json` is verdict-job's. The two are separate so a list query always has a manifest even if the run crashed and never produced a report.
- Pagination is alphabetical-prefix based; per-day partitioning bounds individual prefix size.

**Trade-off:** multi-dimensional analytical queries are not supported by this layout. Acceptable for v1 — UI use cases are "list by target", "list by scenario", "show one run". If LIST latency becomes painful at scale (>10k objects per prefix), add pre-computed rollup objects.

## 7. API surface (illustrative; final shape in OpenAPI spec at plan time)

```
GET    /api/scenarios                       # catalog (with optional metadata)
GET    /api/scenarios/{id}                  # detail + param schema + allowed targets
POST   /api/runs                            # submit: {scenario_id, target_id, params}
GET    /api/runs?target=...&scenario=...    # history with filters
       &status=...&since=...
GET    /api/runs/{id}                       # status + verdict + workflow refs + grafana links
DELETE /api/runs/{id}                       # cancel (translates to Argo terminate + chaos cleanup)
GET    /api/runs/{id}/events                # SSE — live status
GET    /api/targets                         # registered downstream clusters (read-only)
POST   /api/oidc/exchange                   # CI: exchange external OIDC for controlplane session
GET    /healthz, /readyz, /metrics          # standard
POST   /internal/chaos                      # SA-only: create chaos in named target (called by Workflow step)
DELETE /internal/chaos/{chaos-ref}          # SA-only: cleanup
```

The CLI shares the generated client with the UI:

```
dlh login                                          # OIDC device-code flow
dlh run mysql-pod-delete --target staging-mysql \
        --duration 5m --vus 20 --wait
dlh runs ls --target staging-mysql --since 24h
dlh runs show <id>                                 # verdict + Grafana links
dlh runs logs <id>                                 # streams via SSE
```

If a feature exists in the UI but not the CLI (or vice versa), it's a bug.

## 8. Cross-cluster chaos model

Chaos Mesh controllers are cluster-scoped — chaos CRs must be created in the same cluster as the targets. Three patterns considered:

1. **Per-cluster chaos-mesh + controlplane as remote client.** Each target cluster has its own chaos-mesh (Argo-CD-installed); controlplane holds a kubeconfig Secret per Target and creates remote chaos CRs through that. **Chosen.**
2. **Chaos Mesh's native multi-cluster federation.** Has been experimental for years; flaky cross-cluster status sync. Rejected for v1.
3. **Sidecar agent per target cluster.** Cleanest network model if the framework cluster cannot reach target k8s API endpoints. Held in reserve behind the `ChaosClient` interface; can swap in later without API changes.

The `ChaosClient` interface abstracts pattern selection. Phase C ships `LocalChaosClient` (target == framework cluster). Phase D adds `RemoteChaosClient`. A hypothetical `AgentClient` (pattern 3) is a future extension.

### Chaos lifecycle (middle-ground approach)

The Workflow remains the chaos-lifecycle orchestrator: scenario WorkflowTemplates contain `inject-chaos` and `cleanup-chaos` steps that `http`-call the controlplane's `/internal/chaos` endpoint. The controlplane forwards to the appropriate `ChaosClient`.

A **watchdog reconciler** in the controlplane provides the safety net: every 30s it scans manifests, finds any run that has reached a terminal status while its associated chaos CRs still exist remotely, and force-deletes them. This guarantees "target cluster never retains orphaned chaos" even if the Workflow's cleanup step fails (TTL cleanup, terminate, controller crash).

This is a deliberate middle ground between "Workflow owns everything" (simpler but can leak chaos) and "controlplane owns everything" (cleanest but requires reworking every WorkflowTemplate into a load-only shape).

## 9. Auth & RBAC

### 9.1 User → controlplane

OIDC. The controlplane delegates auth to the org's IdP via standard `Authorization: Bearer <id_token>`. Browser flow uses cookie sessions backed by id_tokens. The specific IdP (Dex / Google / Okta) is an environment-time decision, not an architectural one.

Authorization is a small role set, stored in a ConfigMap synced by Argo CD:

| Role | Capabilities |
|---|---|
| `viewer` | GET scenarios, runs, verdicts. No submission. |
| `runner` | viewer + POST /api/runs + DELETE /api/runs/{id} (cancel own runs). |
| `admin` | runner + see all users' runs + view targets. |

Bindings: OIDC group claim → role. Per-target restrictions are an enrichment of `runner` (`runner-of:{target-id}`), enforced at submission time.

### 9.2 Controlplane → framework cluster k8s API

Dedicated ServiceAccount + Role scoped to:

- `argoproj.io/Workflow` — create/get/list/watch/delete in framework namespace only.
- `argoproj.io/WorkflowTemplate` — get/list only.
- `core/Secret` — get on specific `resourceNames:` (target kubeconfigs).
- No cluster-scoped access. No access outside framework namespace.

### 9.3 Controlplane → target cluster k8s API

Per-target kubeconfig in a Secret. Remote SA on each target cluster has minimal scope:

- `chaos-mesh.org/*` — create/delete in allowed chaos namespace.
- `core/pods` — get/list only (label-selector resolution + injection verification).
- Nothing else. No exec, log read, secret access, or node access.

Secrets sourced from external-secrets / sealed-secrets / SOPS — decision deferred to platform-lifecycle spec §5.6. Controlplane watches Secrets and reloads remote clients on change.

### 9.4 CLI / CI tokens

- **CLI:** OIDC device-code flow; id_token cached at `~/.config/dlh/token`.
- **CI:** `/api/oidc/exchange` accepts the CI provider's OIDC token (GH Actions, GitLab JWT, etc.), validates issuer / audience / repository claim, returns a short-lived controlplane session token.

No PATs. No long-lived shared secrets. No kubectl tokens distributed.

## 10. UI shape

Embedded React + TypeScript SPA served by the controlplane binary (`go:embed`). Vite build. Tailwind + shadcn-style components. API client generated from OpenAPI spec.

Four screens:

1. **Scenarios** — catalog grid. Cards show name + target type + description. "Run" opens a schema-driven param form derived from the WorkflowTemplate parameters; submit creates a Run and redirects.
2. **Runs** — table, filterable by target / scenario / status / time window. Row: scenario, target, started, duration, status badge, verdict score, link.
3. **Run detail** — header (scenario, target, params, status, cancel); timeline of workflow steps with live SSE updates; verdict panel rendering `report.json` as a table; embedded Grafana iframe with time range = run start/end and the run's chaos-window annotation visible; artifact links via signed MinIO URLs.
4. **Targets** (admin) — read-only list of registered downstream clusters with last-ping status + "test connection" diagnostic button.

Grafana is the trend-dashboard surface; the UI never reimplements charts.

## 11. End-to-end flow

```
1. UI / CLI / CI → POST /api/runs
     body: {scenario_id, target_id, params}
     header: Authorization: Bearer <token>

2. controlplane:
     - validates OIDC token, checks runner-of:{target_id}
     - generates run_id = "<scenario>-YYYYMMDD-HHMMSS"
     - writes MinIO manifest.json (status: Submitted) + index objects
     - creates Workflow CR in framework cluster via scoped SA
         spec.workflowTemplateRef.name = scenario_id
         spec.arguments.parameters = merged params + target_id
         annotations: dlh.run-id, dlh.target
     - returns 202 {run_id, status_url, events_url}

3. Workflow step "inject-chaos":
     - http template → POST controlplane:/internal/chaos
     - controlplane.ChaosClient.Inject(target_id, chaos_spec)
       (LocalChaosClient in Phase C; RemoteChaosClient in Phase D)
     - waits for phase=AllInjected; returns chaos_ref

4. Workflow step "run-load":
     - k6 in framework cluster, prom-rw to VM with dlh_scenario={run_id}

5. Workflow step "cleanup-chaos":
     - http template → DELETE controlplane:/internal/chaos/{chaos_ref}

6. Workflow step "verdict":
     - verdict-job queries VM, writes report.json to MinIO,
       pushes dlh_verdict_* gauges (unchanged from today).

7. Workflow terminal phase:
     - controlplane Workflow informer updates manifest.json + indexes
       with finished_at, status, score (read from report.json).

8. UI SSE stream surfaces step transitions live; on terminal phase the UI
   switches to verdict view with embedded Grafana panel.

Watchdog (parallel, always-on):
     - every 30s, scan manifests with status ∈ {Done, Failed, Cancelled}
     - for each, list expected chaos CRs in target cluster
     - if any still exist, force-delete via ChaosClient
```

Invariants preserved: `dlh_scenario` label discipline (run_id == dlh_scenario), MinIO artifact path conventions, chaos blast radius (target cluster sees only chaos CRs and k6 data-plane traffic — no k6 pod, no verdict-job, no scenario logic).

## 12. Phasing

Each phase is independently shippable and reversible.

### Phase B — controlplane skeleton, read-only

- Go binary, OIDC, role ConfigMap, scoped SA + Role.
- `GET /api/scenarios`, `GET /api/runs`, `GET /api/runs/{id}` (joins Workflow + MinIO report.json if present), SSE for live updates.
- UI screens 1, 2, 3 read-only. Scenario submission still via `run-scenario.sh`.
- Helm chart fragment for controlplane Deployment; referenced by the `dlh-controlplane` Argo CD Application from the companion spec.
- OpenAPI spec; `oapi-codegen` produces Go server stub + TS client.

End state: a viewer for everything already happening. No new submission path. Fully reversible (delete the Deployment).

### Phase C — controlplane submission (single-cluster)

- `POST /api/runs`, manifest + index writes, Workflow informer, watchdog reconciler.
- `/internal/chaos` endpoint with `LocalChaosClient` (target == framework cluster).
- Modify all existing WorkflowTemplates to call `/internal/chaos` instead of inlining chaos CRs; add `onExit` cleanup.
- `dlh` CLI; `run-scenario.sh` becomes a deprecated shim calling `dlh run`.

End state: full no-shell submission flow, single-cluster only. WorkflowTemplate changes are a one-way door (revertible by git revert but not by configuration).

### Phase D — remote targets

- `Target` type, `targets.yaml` ConfigMap, per-target kubeconfig Secret.
- `ChaosClient` interface extraction; `RemoteChaosClient` impl.
- `/internal/chaos` routes by `target_ref`.
- Argo CD Application for chaos-mesh-in-target-cluster.
- UI Targets screen (read-only); scenario submission form gains target dropdown filtered by scenario's allowed target types.
- E2E test with two minikube profiles or kind clusters.

End state: framework cluster serves chaos to N remote target clusters. Highest-risk phase — a 1-day spike before plan-phase is recommended to validate network reachability + RBAC + secret distribution.

### Phase E — CI integration + cleanup

- `/api/oidc/exchange` endpoint.
- Reusable GitHub Action (`.github/actions/dlh-run/action.yml`).
- Example release-gating workflow.
- Delete `run-scenario.sh`, `platform-up.sh`, `platform-down.sh`, `platform-verify.sh`, `verify-templates.sh`.
- Keep `minikube-up.sh` for local-dev.
- Documentation rewrite; FINDINGS.md update.

End state: no kubectl/helm/argo commands in the repo's docs except local-dev.

### Phase F — Schedule surface (optional)

- Wrap Argo's `CronWorkflow` CRD as a `Schedule` resource in the controlplane API.
- UI screen for schedules (list, pause, next-run); `dlh schedule …` CLI commands.

End state: continuous-chaos-as-monitoring use case is covered.

## 13. Testing strategy

| Layer | Where |
|---|---|
| Unit (Go) — handlers, OIDC, MinIO index, watchdog, ChaosClient impls against fakes | `controlplane/internal/**/_test.go` |
| Unit (TS) — UI components, generated client sanity | `controlplane/web/src/**/*.test.tsx` |
| Contract — OpenAPI is source of truth; CI fails on drift | CI job |
| Integration (controlplane ↔ k8s) — envtest / kind, Argo + chaos-mesh CRDs | `controlplane/test/integration/` |
| Integration (controlplane ↔ MinIO) — testcontainers MinIO | same |
| E2E single-cluster — extended `helm test` | existing infra |
| E2E multi-cluster — two minikube profiles | Phase D |
| UI smoke — Playwright against minikube | Phase B+ |

Auth tested against a fake OIDC issuer; real IdP verified manually per environment in Phase E.

## 14. Open questions (resolve at plan time)

1. IdP selection (Dex / Google / Okta).
2. Secret backend for target kubeconfigs (cross-spec dependency — see companion spec §5.6).
3. UI delivery: embedded in controlplane binary (recommended for v1) vs separate static-site image.
4. WorkflowTemplate modification scope in Phase C: keep current "scenario = chaos + load + verdict" template shape with `/internal/chaos` calls, or split into "load-only template + chaos descriptor" pairs. The simpler path keeps current shape.
5. CronWorkflow inheritance in Phase F: do scheduled runs share the same Run object model, or is `Schedule` independent of `Run` until firing time?

## 15. Risks

- **Phase D unknowns.** Cross-cluster reachability, RBAC negotiation with target-cluster owners, secret distribution. Mitigated by a 1-day spike before plan-phase commits.
- **WorkflowTemplate breaking change in Phase C.** Mechanical but easy to miss a cleanup edge case. Mitigated by an integration test per template asserting onExit deletes the chaos CR.
- **MinIO LIST latency at scale.** Not a v1 problem but worth measuring once production traffic exists. Mitigation path (rollup objects) is straightforward.
- **OIDC provider drift.** Configuration is environment-specific; spec deliberately doesn't pick. Each environment validates its own IdP path.
- **Controlplane becoming stateful via watchdog.** The watchdog adds a goroutine with implicit state (which chaos CRs it has seen). Two-replica deployments require either leader election (Kubernetes lease) or watchdog idempotency. Default to single-replica + lease-based HA later.

## 16. Dependencies

- **Argo CD platform-lifecycle spec** (`2026-05-21-argocd-platform-lifecycle-design.md`) must be in place before this spec's controlplane is deployable through GitOps. Local-dev development of the controlplane against minikube does not require it.
- **Existing platform invariants** documented in `docs/FINDINGS.md` and `CLAUDE.md`: `dlh_scenario` label discipline, prom-rw trend-stats quantile gauges, MinIO artifact path conventions, `last_over_time` wrapping for verdict gauges, `imagePullPolicy: Never` for locally-built images. None of these change.
