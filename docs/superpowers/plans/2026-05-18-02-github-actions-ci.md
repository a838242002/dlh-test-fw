# GitHub Actions CI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a single `.github/workflows/ci.yml` workflow with four parallel jobs (`helm`, `go`, `shellcheck`, `kubeconform`) that block merge on broken Helm templating, broken Go code, buggy shell scripts, and scenario YAMLs that violate K8s/CRD schemas — all without requiring a live cluster, in ≤ 3 minutes cold-cache.

**Architecture:** One workflow file under `.github/workflows/`. Triggers on `pull_request` and `push` to `main`. `concurrency.cancel-in-progress` so PR force-pushes don't pile up. `permissions: { contents: read }` at workflow level. Each job runs independently on `ubuntu-latest`, `timeout-minutes: 10`, with caching where it saves measurable time (Go modules via setup-go; Helm subchart deps via actions/cache).

**Tech Stack:** GitHub Actions YAML; `actions/checkout@v4`, `azure/setup-helm@v4`, `actions/setup-go@v5`, `actions/cache@v4`, `ludeeus/action-shellcheck@2.0.0`, `kubeconform v0.6.7`, Datree CRDs-catalog.

**Reference spec:** `docs/superpowers/specs/2026-05-18-github-actions-design.md`. Re-read for the per-job rationale and risk register before starting.

**Branch & worktree:** Per `CLAUDE.md` conventions, create a feature worktree at `../dlh-test-fw-plan10` on branch `feat/plan10-github-actions-ci` before Task 2. Task 1 happens from the main worktree (baseline verification — no commits).

**Verification model:** Every command the CI will run (`helm lint`, `go test`, `shellcheck`, `kubeconform`) can be run locally first. The plan verifies each job's commands pass locally on the current main BEFORE adding the corresponding job to `ci.yml`. The final task pushes the branch and confirms GitHub Actions runs green.

---

## File Structure

**New files:**
- `.github/workflows/ci.yml` — single workflow, four jobs.

**Modified files:** None.

**Unchanged:** Everything else. No make targets, no scripts, no chart content, no Go code.

---

## Task 1: Baseline — run all four checks locally on current main

This task makes no commits. It verifies that the commands the CI will run actually pass on the current state of `main`. If any fails, we discover and resolve it BEFORE the corresponding job exists in CI (so the first push isn't immediately red).

**Files:** None modified.

Work from: `/Users/allen/repo/dlh-test-fw` (main worktree).

- [ ] **Step 1: Confirm clean working tree**

```bash
git status
git log --first-parent --oneline -3
```

Expected: clean tree on `main`; HEAD is `e56661e` (the GitHub Actions spec commit) or newer.

- [ ] **Step 2: Helm baseline**

```bash
helm version --short
helm dependency update helm/dlh-test-fw
helm lint helm/dlh-test-fw
helm template dlh helm/dlh-test-fw > /tmp/rendered.yaml
test -s /tmp/rendered.yaml
grep -q 'kind: WorkflowTemplate' /tmp/rendered.yaml
grep -q 'name: dlh-slos' /tmp/rendered.yaml
echo "helm baseline OK"
```

Expected: Helm 3.14.x; lint reports `0 chart(s) failed` (INFO/WARN are fine); rendered output is non-empty and contains both `WorkflowTemplate` and `dlh-slos`. If `helm dependency update` fails on subchart fetch, fix `Chart.lock` before proceeding.

- [ ] **Step 3: Go baseline**

```bash
cd verdict-job
go version
go vet ./...
go test ./...
cd -
echo "go baseline OK"
```

Expected: Go 1.26.x available; `go vet` silent; all 7 test files pass. If a test fails, fix it now (it'll fail in CI too) — Plan 9 didn't touch Go code, so this should be the same as the last `verdict-job` Plan 3 baseline.

- [ ] **Step 4: shellcheck baseline**

```bash
shellcheck --version
shellcheck -S error scripts/*.sh
echo "shellcheck baseline OK (exit 0 means clean)"
```

Expected: `shellcheck` installed (`brew install shellcheck` if missing). At severity `error`, all six scripts should pass cleanly — they already use `set -euo pipefail`. If you get error-level findings, EITHER fix the scripts now (one commit on main) OR document the finding and adjust the CI job to allow it. Do NOT loosen `-S error` casually; the whole point of this job is to catch real bugs.

- [ ] **Step 5: kubeconform baseline (the riskiest baseline check)**

```bash
kubeconform -v 2>&1 || echo "need to install: brew install kubeconform"
# Render chart (already done in step 2 but redo to be safe)
helm template dlh helm/dlh-test-fw > /tmp/rendered.yaml
# Validate rendered chart
kubeconform -strict -summary \
  -schema-location default \
  -schema-location 'https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json' \
  /tmp/rendered.yaml
echo "---"
# Validate scenarios
kubeconform -strict -summary \
  -schema-location default \
  -schema-location 'https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json' \
  scenarios/*.yaml
echo "kubeconform baseline OK"
```

Expected: both invocations exit 0. Likely failure modes if not 0:
- Unknown CRD Kind from a subchart (e.g., `monitoring.coreos.com/ServiceMonitor` if any subchart ships one). If so, identify the unknown Kind(s) and add a `-skip <Kind>` flag to the failing invocation, then re-run. Document each skip with a 1-line comment in the eventual CI job.
- Schema validation failure in our own files (chart helper or scenario YAML). If so, fix the YAML now in a single commit on main (out-of-band from this plan).

Record the exact `-skip` list (if any) — you'll bake it into the `kubeconform` job in Task 6. If the list is long (>5 skips), STOP and discuss with the user whether to relax `-strict` instead.

- [ ] **Step 6: Summarise baseline**

Capture the four findings in your head / a scratchpad:
- Helm: pass / fail (and what failed)
- Go: pass / fail
- shellcheck: pass / fail and any `-S error` findings
- kubeconform: pass / fail and the `-skip` list to bake in

If anything failed and you fixed it in a separate commit, note the commit SHA. Proceed to Task 2.

---

## Task 2: Worktree + workflow scaffold (no jobs yet)

**Files:**
- Create: `.github/workflows/ci.yml`

- [ ] **Step 1: Create the feature worktree**

From `/Users/allen/repo/dlh-test-fw`:

```bash
git worktree add ../dlh-test-fw-plan10 -b feat/plan10-github-actions-ci main
cd ../dlh-test-fw-plan10
git status
```

Expected: on `feat/plan10-github-actions-ci`, working tree clean.

All subsequent tasks operate from `/Users/allen/repo/dlh-test-fw-plan10`.

- [ ] **Step 2: Create `.github/workflows/ci.yml` with triggers + concurrency only**

```bash
mkdir -p .github/workflows
```

Write `.github/workflows/ci.yml`:

```yaml
name: CI

on:
  pull_request:
  push:
    branches: [main]

concurrency:
  group: ${{ github.workflow }}-${{ github.ref }}
  cancel-in-progress: true

permissions:
  contents: read

jobs:
  # Placeholder job so the workflow is syntactically valid before real
  # jobs land in Tasks 3-6. Removed in Task 6.
  noop:
    runs-on: ubuntu-latest
    timeout-minutes: 5
    steps:
      - run: echo "scaffold OK"
```

- [ ] **Step 3: Lint the workflow YAML locally**

```bash
# yq is a lightweight YAML linter; if not installed, use python -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yml'))"
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yml'))" && echo "YAML OK"
```

Expected: `YAML OK`. If you have `actionlint` installed (`brew install actionlint`), also run `actionlint .github/workflows/ci.yml` for stronger validation; if not, skip.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: scaffold workflow with triggers + concurrency + noop placeholder"
```

---

## Task 3: Add `helm` job

**Files:**
- Modify: `.github/workflows/ci.yml`

- [ ] **Step 1: Replace the noop job with the helm job**

Edit `.github/workflows/ci.yml`. Replace the `jobs:` block (keep everything above unchanged) with:

```yaml
jobs:
  helm:
    runs-on: ubuntu-latest
    timeout-minutes: 10
    steps:
      - uses: actions/checkout@v4
      - uses: azure/setup-helm@v4
        with:
          version: v3.14.4
      - name: Cache subchart deps
        uses: actions/cache@v4
        with:
          path: helm/dlh-test-fw/charts
          key: helm-deps-${{ hashFiles('helm/dlh-test-fw/Chart.lock') }}
      - name: helm dependency update
        run: helm dependency update helm/dlh-test-fw
      - name: helm lint
        run: helm lint helm/dlh-test-fw
      - name: helm template (smoke)
        run: |
          helm template dlh helm/dlh-test-fw > /tmp/rendered.yaml
          test -s /tmp/rendered.yaml
          grep -q 'kind: WorkflowTemplate' /tmp/rendered.yaml
          grep -q 'name: dlh-slos' /tmp/rendered.yaml
```

- [ ] **Step 2: Lint the workflow YAML**

```bash
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yml'))" && echo "YAML OK"
```

Expected: `YAML OK`.

- [ ] **Step 3: Dry-run the job's commands locally**

You already did this in Task 1 Step 2, but re-confirm from the new worktree:

```bash
helm dependency update helm/dlh-test-fw
helm lint helm/dlh-test-fw
helm template dlh helm/dlh-test-fw > /tmp/rendered.yaml
test -s /tmp/rendered.yaml && grep -q 'kind: WorkflowTemplate' /tmp/rendered.yaml && grep -q 'name: dlh-slos' /tmp/rendered.yaml && echo "helm job will pass"
```

Expected: `helm job will pass`.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: add helm job — helm lint + helm template smoke"
```

---

## Task 4: Add `go` job

**Files:**
- Modify: `.github/workflows/ci.yml`

- [ ] **Step 1: Add the go job to the jobs block**

Edit `.github/workflows/ci.yml`. Add `go` as a sibling of `helm` under `jobs:`. Final `jobs:` block:

```yaml
jobs:
  helm:
    runs-on: ubuntu-latest
    timeout-minutes: 10
    steps:
      - uses: actions/checkout@v4
      - uses: azure/setup-helm@v4
        with:
          version: v3.14.4
      - name: Cache subchart deps
        uses: actions/cache@v4
        with:
          path: helm/dlh-test-fw/charts
          key: helm-deps-${{ hashFiles('helm/dlh-test-fw/Chart.lock') }}
      - name: helm dependency update
        run: helm dependency update helm/dlh-test-fw
      - name: helm lint
        run: helm lint helm/dlh-test-fw
      - name: helm template (smoke)
        run: |
          helm template dlh helm/dlh-test-fw > /tmp/rendered.yaml
          test -s /tmp/rendered.yaml
          grep -q 'kind: WorkflowTemplate' /tmp/rendered.yaml
          grep -q 'name: dlh-slos' /tmp/rendered.yaml

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
      - name: go vet
        run: go vet ./...
      - name: go test
        run: go test ./...
```

- [ ] **Step 2: Lint the workflow YAML**

```bash
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yml'))" && echo "YAML OK"
```

Expected: `YAML OK`.

- [ ] **Step 3: Dry-run locally**

```bash
cd verdict-job && go vet ./... && go test ./... && cd - && echo "go job will pass"
```

Expected: `go job will pass`.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: add go job — go vet + go test on verdict-job"
```

---

## Task 5: Add `shellcheck` job

**Files:**
- Modify: `.github/workflows/ci.yml`

- [ ] **Step 1: Add the shellcheck job to the jobs block**

Edit `.github/workflows/ci.yml`. Add `shellcheck` as a sibling under `jobs:`. The final shape (showing only the new job — keep `helm` and `go` unchanged):

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

- [ ] **Step 2: Lint the workflow YAML**

```bash
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yml'))" && echo "YAML OK"
```

Expected: `YAML OK`.

- [ ] **Step 3: Dry-run locally**

```bash
shellcheck -S error scripts/*.sh && echo "shellcheck job will pass"
```

Expected: `shellcheck job will pass` (exit 0). If you get findings, address them now per Task 1 Step 4's guidance.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: add shellcheck job — -S error on scripts/*.sh"
```

---

## Task 6: Add `kubeconform` job (and remove the noop placeholder if still present)

**Files:**
- Modify: `.github/workflows/ci.yml`

- [ ] **Step 1: Add the kubeconform job to the jobs block**

Edit `.github/workflows/ci.yml`. Add `kubeconform` as a sibling under `jobs:`. Use the `-skip` list captured in Task 1 Step 5 (if empty, omit the `-skip` flag entirely):

```yaml
  kubeconform:
    runs-on: ubuntu-latest
    timeout-minutes: 10
    steps:
      - uses: actions/checkout@v4
      - uses: azure/setup-helm@v4
        with:
          version: v3.14.4
      - name: Install kubeconform
        run: |
          curl -fsSL -o /tmp/kubeconform.tgz \
            https://github.com/yannh/kubeconform/releases/download/v0.6.7/kubeconform-linux-amd64.tar.gz
          sudo tar -C /usr/local/bin -xzf /tmp/kubeconform.tgz kubeconform
          kubeconform -v
      - name: helm dependency update
        run: helm dependency update helm/dlh-test-fw
      - name: helm template
        run: helm template dlh helm/dlh-test-fw > /tmp/rendered.yaml
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

If Task 1 Step 5 captured a non-empty `-skip` list, add it as the FIRST argument to BOTH `kubeconform` invocations, e.g. `kubeconform -skip ServiceMonitor,PrivateLoadZone -strict -summary ...`. Document the list with a one-line comment above each invocation, e.g. `# -skip: subchart CRs not in Datree catalog`.

- [ ] **Step 2: Confirm no placeholder noop job remains**

Read `.github/workflows/ci.yml` end-to-end and confirm:
- Exactly four jobs under `jobs:`: `helm`, `go`, `shellcheck`, `kubeconform`
- No `noop:` job lingering

If a `noop:` block remains from Task 2, delete it now (it should have been replaced in Task 3 — Task 3 Step 1 says "Replace the noop job with the helm job").

- [ ] **Step 3: Lint the workflow YAML**

```bash
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yml'))" && echo "YAML OK"
```

Expected: `YAML OK`.

- [ ] **Step 4: Dry-run locally**

```bash
helm template dlh helm/dlh-test-fw > /tmp/rendered.yaml
# Use whatever -skip list Task 1 Step 5 produced:
kubeconform -strict -summary \
  -schema-location default \
  -schema-location 'https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json' \
  /tmp/rendered.yaml \
  && kubeconform -strict -summary \
       -schema-location default \
       -schema-location 'https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json' \
       scenarios/*.yaml \
  && echo "kubeconform job will pass"
```

Expected: `kubeconform job will pass`.

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: add kubeconform job — schema-validate rendered chart + scenarios"
```

---

## Task 7: Push branch, verify CI runs green, merge to main

**Files:** No file changes in this task.

- [ ] **Step 1: Push the feature branch**

```bash
git push -u origin feat/plan10-github-actions-ci
```

Expected: push succeeds.

If `origin` doesn't have the branch yet, this creates it. The push triggers the CI on the branch's `pull_request` event ONLY when a PR is opened — `push` alone does not because the workflow's `push:` filter only matches `main`. So you'll see no run yet.

- [ ] **Step 2: Open a draft PR to trigger CI**

```bash
gh pr create --draft --title "ci: PR guardrails workflow (Plan 10)" \
  --body "Implements docs/superpowers/specs/2026-05-18-github-actions-design.md.

Four parallel jobs: helm, go, shellcheck, kubeconform. ≤ 3 min cold-cache.
See docs/superpowers/plans/2026-05-18-02-github-actions-ci.md."
```

Expected: PR URL printed. The PR opening event fires the workflow.

- [ ] **Step 3: Watch the CI run**

```bash
sleep 10
gh pr checks --watch
```

Expected (in `gh pr checks` output):
- `helm` — pass, ≤ 1 min
- `go` — pass, ≤ 2 min (cold cache; warm runs ≤ 30s)
- `shellcheck` — pass, ≤ 30s
- `kubeconform` — pass, ≤ 2 min (cold cache)
- All four marked `pass`, no `fail` / `cancelled`

If any fails:
1. Open the run in the browser (`gh pr view --web`).
2. Read the failure step's logs.
3. Reproduce locally if possible — the dry-runs in Tasks 3–6 should have caught real failures, so a CI-only failure points at an environmental difference (Go toolchain auto-download, Helm version pin, missing dep, etc.).
4. Push a fix commit to the branch. CI re-runs automatically.
5. Re-watch.

- [ ] **Step 4: Confirm concurrency cancellation works**

While CI is running (or by pushing a quick no-op fixup), push a follow-up commit and watch:

```bash
git commit --allow-empty -m "ci: trigger re-run to verify cancel-in-progress"
git push
sleep 5
gh run list --branch feat/plan10-github-actions-ci --limit 3
```

Expected: the prior run shows `cancelled`, the new run shows `in_progress` or `completed`. If both ran to completion, the `cancel-in-progress` setting didn't apply — investigate the `concurrency.group` expression.

- [ ] **Step 5: Mark PR ready, wait for green**

```bash
gh pr ready
gh pr checks --watch
```

Expected: all four checks pass.

- [ ] **Step 6: Merge with `--no-ff` from the main worktree**

```bash
cd /Users/allen/repo/dlh-test-fw
git status                          # must be clean
git checkout main
git pull origin main                # in case main moved
git merge --no-ff feat/plan10-github-actions-ci -m "Merge feat/plan10-github-actions-ci: PR guardrails CI

One workflow .github/workflows/ci.yml with four parallel jobs on PR + push to main:
- helm:        helm lint + helm template smoke (catches tpl errors, broken Files.Glob)
- go:          go vet + go test on verdict-job (7 test files)
- shellcheck:  -S error on scripts/*.sh (six bash scripts)
- kubeconform: -strict on rendered chart + scenarios/*.yaml (Datree CRDs catalog)

Target: ≤ 3 min cold-cache. cancel-in-progress on PR force-push.
No image publish, no E2E — deferred per spec scope."
git push origin main
```

Expected: merge succeeds, push succeeds. The push to main triggers the workflow once more on `main` itself — confirm it goes green.

- [ ] **Step 7: Clean up worktree + branch**

```bash
git worktree remove ../dlh-test-fw-plan10
git push origin --delete feat/plan10-github-actions-ci
git branch -d feat/plan10-github-actions-ci
git worktree list
```

Expected: only the main worktree remains; remote branch deleted; local branch deleted (refuses with `-d` if unmerged — should not happen).

- [ ] **Step 8: Tag the milestone**

```bash
git tag plan10-github-actions-ci
git log --first-parent --oneline -5
```

Expected: tag at the merge commit; merge boundary visible in `--first-parent`.

- [ ] **Step 9: Manual follow-up — configure branch protection**

NOT part of this plan, but document for the user. After merging, go to:

```
https://github.com/a838242002/dlh-test-fw/settings/branches
```

1. Add rule for `main`.
2. Tick **Require status checks to pass before merging**.
3. Add the four required checks: `helm`, `go`, `shellcheck`, `kubeconform`.
4. Leave **Require branches to be up to date before merging** OFF (per spec).
5. Save.

Verify by trying to push a deliberately-broken commit to a new PR — merge should be blocked until checks pass.

(This step is informational; do not block plan completion on it.)

---

## Self-Review notes (author check, fresh-eyes pass)

- Spec section "Goals (in scope)" 1–5: covered by Tasks 3 (helm), 4 (go), 5 (shellcheck), 6 (kubeconform), and 7 Step 3 (wall-clock verification).
- Spec section "File layout": this plan creates exactly the one file specified (`.github/workflows/ci.yml`); the noop placeholder is introduced and removed within Tasks 2→3.
- Spec section "Per-job details": each job's YAML in the plan matches the spec verbatim (helm 3.14.4, kubeconform v0.6.7, action versions pinned, Datree schema URL).
- Spec section "Caching": setup-go cache enabled in Task 4; helm subchart cache in Task 3; kubeconform/datree intentionally uncached per spec.
- Spec section "Triggers & concurrency": Task 2 sets these up before any job exists.
- Spec section "Branch protection": Task 7 Step 9 documents it as a manual follow-up, not part of the workflow file — matches spec.
- Spec section "Risks":
  - Datree catalog availability: not pre-empted; if the live URL is down, Task 1 Step 5 (baseline) will fail and the implementer will see it before going further.
  - Helm version drift: pin tracked verbatim (3.14.4); update path noted in spec.
  - `helm dependency update` network flakiness: cache step in Task 3 mitigates after first run.
  - `go 1.26.3` toolchain auto-download: `setup-go@v5` handles it; first run slower; cached after.
  - Kubeconform `-strict` over-zealous: Task 1 Step 5 explicitly captures the `-skip` list and bakes it into Task 6 — no preemptive guessing.
- Spec section "Success criteria" 1–6: directly verified in Task 7 (open PR, watch checks, confirm cancellation, merge).
- Placeholder scan: no TBD/TODO. Two conditional sections ("if any failed" in Task 1, "if `-skip` list non-empty" in Task 6) are explicit branches, not deferrals.
- Type consistency: job names (`helm`, `go`, `shellcheck`, `kubeconform`) identical across plan, spec, branch-protection notes, and merge commit body. Action versions consistent (`@v4`, `@v5`, `2.0.0`).

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-05-18-02-github-actions-ci.md`. Two execution options:

**1. Subagent-Driven (recommended)** — fresh subagent per task, review between tasks. Fast for a small mechanical plan like this.

**2. Inline Execution** — execute tasks in this session using executing-plans, batch execution with checkpoints.

Which approach?
