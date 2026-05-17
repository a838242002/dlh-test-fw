# GitHub Actions CI — Design Spec

**Date**: 2026-05-18
**Status**: Draft, awaiting user review
**Project**: dlh-test-fw
**Scope**: PR guardrails only (lint + unit tests + offline schema validation). No image publish, no E2E cluster runs.

## Why

The repo has no CI today. All quality is enforced by hand: developers remember to run `helm lint`, `go test`, etc. before pushing. As Plans 6–9 added more moving parts (custom `dlh-k6` image, util WTs, SLO library, scenario rewrites), the surface area for a regression that only shows up at `make platform-up` time has grown. PRs need a cheap, fast set of checks that catch the regressions a human reviewer would otherwise have to remember to look for.

## Goals (in scope)

1. Block merge on broken Helm templating (missing helpers, broken `tpl` escapes, `Files.Glob` failures, malformed YAML in the chart).
2. Block merge on broken `verdict-job/` Go code (failing tests, `go vet` errors).
3. Block merge on shell scripts with obvious bugs (unquoted vars, undefined names) — there's a lot of bash in `scripts/`.
4. Block merge on scenario YAMLs that violate K8s / Argo / Litmus / k6 CRD schemas — without spinning up a cluster.
5. Run end-to-end in ≤ 3 minutes on a cold cache.

## Goals (out of scope, deferred)

- Image publish (verdict, dlh-k6, fixture images) — user explicitly chose "PR guardrails only".
- KinD + chart-install + scenario run as an E2E smoke. Heavy, flaky, and the Bitnami arm64 workarounds may not behave identically on `ubuntu-latest` linux/amd64 runners. Revisit when there's appetite for ~20–30 min CI.
- `verify-templates.sh` and `platform-verify.sh` — live-cluster only.
- golangci-lint — `go vet` covers the cheap class for now; add later if `vet` proves too narrow.
- yamllint / actionlint on the workflow file itself — defer until/unless it's annoying to maintain.
- Scheduled / nightly runs — without image publish or E2E there's nothing to schedule.
- Branch protection rule configuration — manual one-time setup after the workflow lands (documented below, not part of the workflow file).

## Architecture

```
.github/workflows/ci.yml          ← single workflow file
│
├── triggers
│     pull_request                ← any base branch
│     push:                       ← only branch:
│       branches: [main]
│
├── concurrency
│     group: ${{ github.workflow }}-${{ github.ref }}
│     cancel-in-progress: true    ← PR force-push cancels prior run
│
├── permissions
│     contents: read              ← least-privilege; no write tokens
│
└── jobs (parallel)
    ├── helm           helm lint + helm template smoke
    ├── go             go vet + go test in verdict-job/
    ├── shellcheck     scripts/*.sh static analysis
    └── kubeconform    rendered chart + scenarios/*.yaml schema check
```

All four jobs run on `ubuntu-latest` with `timeout-minutes: 10` (real expected wall-clock: 2–3 min).

No path filters. Every job runs on every event. Trade-off: doc-only PRs pay ~3 min of CI; the YAML stays trivial and branch-protection setup doesn't need `if: always()` wrappers.

## File layout

```
.github/
└── workflows/
    └── ci.yml                    ← NEW (one file, four jobs)
```

No other files touched. No new make targets. No new scripts.

## Per-job details

### `helm` job

```yaml
helm:
  runs-on: ubuntu-latest
  timeout-minutes: 10
  steps:
    - uses: actions/checkout@v4
    - uses: azure/setup-helm@v4
      with: { version: v3.14.4 }
    - name: Cache subchart deps
      uses: actions/cache@v4
      with:
        path: helm/dlh-test-fw/charts
        key: helm-deps-${{ hashFiles('helm/dlh-test-fw/Chart.lock') }}
    - run: helm dependency update helm/dlh-test-fw
    - run: helm lint helm/dlh-test-fw
    - run: helm template dlh helm/dlh-test-fw > /tmp/rendered.yaml
    - name: Smoke-check rendered output
      run: |
        test -s /tmp/rendered.yaml
        grep -q 'kind: WorkflowTemplate' /tmp/rendered.yaml
        grep -q 'name: dlh-slos' /tmp/rendered.yaml
```

Catches: invalid YAML in the chart, missing helper templates in `_helpers.tpl`, broken `{{ }}` expressions, `Files.Glob` misuse, missing SLO library files (the `grep` smoke checks both WT registration and the Plan 9 `dlh-slos` ConfigMap).

Helm version pin (3.14.4) tracks current local dev. Bump when local moves.

### `go` job

```yaml
go:
  runs-on: ubuntu-latest
  timeout-minutes: 10
  defaults:
    run:
      working-directory: verdict-job
  steps:
    - uses: actions/checkout@v4
    - uses: actions/setup-go@v5
      with:
        go-version-file: verdict-job/go.mod
        cache: true
        cache-dependency-path: verdict-job/go.sum
    - run: go vet ./...
    - run: go test ./...
```

`go-version-file` auto-tracks `verdict-job/go.mod` (currently `go 1.26.3`). `setup-go@v5` handles both module cache and build cache when `cache: true`. 7 test files exist (`prom`, `metrics`, `chaosresult`, `window`, `report`, `slo`, `eval`). No `-race` flag — add later if a flake hunt needs it.

### `shellcheck` job

```yaml
shellcheck:
  runs-on: ubuntu-latest
  timeout-minutes: 10
  steps:
    - uses: actions/checkout@v4
    - uses: ludeeus/action-shellcheck@2.0.0
      env:
        SHELLCHECK_OPTS: -S error
      with:
        scandir: scripts
```

Severity `error` (not `warning`/`info`) to keep signal high. All six scripts already use `set -euo pipefail`, so error-level findings should be real bugs. The action handles the install + globbing for us.

### `kubeconform` job

```yaml
kubeconform:
  runs-on: ubuntu-latest
  timeout-minutes: 10
  steps:
    - uses: actions/checkout@v4
    - uses: azure/setup-helm@v4
      with: { version: v3.14.4 }
    - name: Install kubeconform
      run: |
        curl -fsSL -o /tmp/kubeconform.tgz \
          https://github.com/yannh/kubeconform/releases/download/v0.6.7/kubeconform-linux-amd64.tar.gz
        sudo tar -C /usr/local/bin -xzf /tmp/kubeconform.tgz kubeconform
    - run: helm dependency update helm/dlh-test-fw
    - run: helm template dlh helm/dlh-test-fw > /tmp/rendered.yaml
    - name: Validate rendered chart
      run: |
        kubeconform -strict -summary \
          -schema-location default \
          -schema-location 'https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json' \
          /tmp/rendered.yaml
    - name: Validate scenarios/*.yaml
      run: |
        kubeconform -strict -summary \
          -schema-location default \
          -schema-location 'https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json' \
          scenarios/*.yaml
```

Schema sources:
- `default` → built-in Kubernetes types (Pod, ConfigMap, Deployment, etc.).
- Datree's `CRDs-catalog` → covers `argoproj.io` (Workflow, WorkflowTemplate), `litmuschaos.io` (ChaosEngine, ChaosExperiment, ChaosResult), and `k6.io` (TestRun, PrivateLoadZone). Confirmed at design time.

`-strict` plus the default `-ignore-missing-schemas=false` posture means unknown CRDs fail loudly — we want that, so a typo'd CRD reference doesn't silently pass.

Duplication: this job re-runs `helm dependency update` + `helm template` that the `helm` job already did (~10s waste). Trade-off: keeps both jobs parallel and independently restartable. If wall-clock budget gets tight, merge the two jobs and serialise the steps.

## Caching

| What | Where | Key |
|---|---|---|
| Go modules + build cache | `actions/setup-go@v5` built-in | derived from `verdict-job/go.sum` |
| Helm subchart deps | `actions/cache@v4` on `helm/dlh-test-fw/charts` | `hashFiles('helm/dlh-test-fw/Chart.lock')` |
| Kubeconform binary | n/a — small download (~5 MB), not worth caching |
| Datree CRD schemas | n/a — fetched on demand, small per-resource files |

## Triggers & concurrency

```yaml
on:
  pull_request:
  push:
    branches: [main]

concurrency:
  group: ${{ github.workflow }}-${{ github.ref }}
  cancel-in-progress: true
```

`pull_request` fires on open / sync / reopen by default — covers force-pushes. `push` to `main` keeps main always-green even if someone merges a stale PR.

`cancel-in-progress: true` is keyed per ref, so PR force-pushes cancel the prior run but pushes to main don't cancel an unrelated PR's run.

## Branch protection (manual, after first run)

Not part of the workflow file. After the workflow runs successfully at least once:

1. GitHub repo → Settings → Branches → Branch protection rules → Add rule for `main`.
2. **Require status checks to pass before merging** → tick → add `helm`, `go`, `shellcheck`, `kubeconform` (the job names from `ci.yml`).
3. **Require branches to be up to date before merging** → **off** (rebase-toll is high in this repo; small team, low conflict risk).
4. **Require linear history** → optional; current convention is `--no-ff` merge commits per plan, so leave this **off**.

## Testing

| Element | How |
|---|---|
| Workflow file syntax | Push to a feature branch; GitHub renders the run in the Actions tab. If the YAML is malformed, the run never starts. |
| `helm` job catches templating bugs | On a test branch, delete a helper from `_helpers.tpl` or break a `tpl` escape; push; expect `helm lint` or `helm template` to fail. |
| `go` job catches test regressions | Add `t.Fail()` to one test; push; expect job red. |
| `shellcheck` catches script bugs | Add `echo $unquoted` to `scripts/platform-up.sh`; push; expect job red. |
| `kubeconform` catches schema regressions | Add `bogus_field: x` to a scenario YAML; push; expect job red. |
| Concurrency cancels prior run | Push two commits in quick succession to the same PR; expect the first run to show "cancelled" in the Actions tab. |
| Total wall-clock ≤ 3 min cold cache | Inspect GitHub Actions run duration on first push (cold cache); subsequent pushes should be faster. |

## Success criteria

1. `.github/workflows/ci.yml` exists with four jobs (`helm`, `go`, `shellcheck`, `kubeconform`).
2. The workflow triggers on PR and on push-to-main; jobs run in parallel.
3. All four jobs pass on the existing main branch as of merge.
4. Wall-clock on a cold cache ≤ 3 minutes for the slowest job; ≤ 1 minute for warm-cache subsequent runs.
5. Each job catches at least one realistic regression (see the Testing table).
6. `cancel-in-progress` works on PR force-push.

## Risks

- **Datree CRDs-catalog availability.** The schema URLs are fetched at runtime from GitHub raw. If Datree moves the repo or restructures paths, `kubeconform` fails opaquely. Mitigation: if it breaks, switch to a pinned tag (`/main/` → `/v<tag>/`) or vendor the schemas into `.github/schemas/`. Spec leaves the URL un-pinned for now — accept that risk for simplicity.
- **Helm 3.14.x version pin drift.** Local dev may move to 3.15+; CI will lag and may pass things that fail locally (or vice versa). Mitigation: bump the pin when local moves; document in CLAUDE.md when local Helm version changes.
- **`helm dependency update` over the network.** Pulls subcharts from public Helm repos. Flaky network = flaky CI. Mitigation: the `actions/cache@v4` step caches `charts/` between runs, so once warm this only fails on a stale `Chart.lock`.
- **`go 1.26.3` may not be available on `ubuntu-latest` initially.** `actions/setup-go@v5` will download the toolchain if not present (GOTOOLCHAIN behavior). Slow first run; cached after. Acceptable.
- **Kubeconform `-strict` is unforgiving.** Any `additionalProperties` in our YAML that isn't in the schema fails. If a CRD schema is incomplete (Datree catalog isn't always 100%), legitimate config fails CI. Mitigation: if this bites, add `-ignore-filename-pattern=<regex>` for the specific file, or drop a per-resource `kubeconform: ignore` annotation. Don't preemptively loosen `-strict`.
- **Trade-off accepted: no E2E means platform-level regressions slip through.** A scenario YAML can be schema-valid and still fail to run end-to-end (e.g. wrong DNS name, wrong WT parameter name, ChaosResult selector mismatch). This is a known gap; revisit when there's appetite for KinD.

## File summary

| Path | Change |
|---|---|
| `.github/workflows/ci.yml` | NEW — single workflow, four jobs |

No other files change.
