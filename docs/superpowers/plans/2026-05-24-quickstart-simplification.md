# Quickstart Simplification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `scripts/quickstart.sh` — one idempotent command that takes a developer from a running minikube to a green `VERDICT: PASS`, plus the README/CLAUDE.md updates that point at it.

**Architecture:** A single bash script (sibling to `minikube-up.sh`, a deliberate documented Plan-18 exception) built from small composable helper functions: preflight/safety gates → 9 idempotent steps (skip-if-done, `--rebuild` forces image/CLI rebuilds) → a Next-steps block. Ephemeral port-forwards are trap-cleaned on EXIT. A `make quickstart` target delegates to it.

**Tech Stack:** bash (`set -euo pipefail`), `shellcheck` + `bash -n` as the test harness, `kubectl`/`helm`/`minikube`/`mc`/`dlh`. No application code changes — orchestration + docs only.

**Spec:** `docs/superpowers/specs/2026-05-24-quickstart-simplification-design.md`

**Testing note (read first):** This is a shell script, so classic "write a failing unit test" TDD does not map. The per-task verification harness is: (a) `bash -n scripts/quickstart.sh` must parse, (b) `shellcheck scripts/quickstart.sh` must be clean, and (c) where a task adds logic that can be exercised without a full cluster (flag parsing, preflight gates, helpers), invoke the script in a way that reaches just that logic and assert on its output/exit code. Full end-to-end against a live cluster is the final task.

**Constants used throughout (define once in Task 1, referenced later):**
- `NS=dlh-test-fw` — platform namespace
- `IMAGES=( "dlh-fixture-mysql:0.1.0" "dlh-fixture-kafka:0.1.0" "dlh-fixture-doris:0.1.0" "ghcr.io/dlh/dlh-k6:0.1.0" "ghcr.io/dlh/dlh-verdict:0.1.0" "ghcr.io/dlh/dlh-controlplane:0.1.0" )`
- `DLH_TOKEN='fake:dev:dev@example.com:dlh-admins'` (local DLH_AUTH_DISABLED fake token)
- Lightened run params: `-p load_duration=180s -p chaos_duration=15s -p chaos_start_after=60s`

---

## Task 1: Scaffold — header, flags, logging helpers, EXIT trap

**Files:**
- Create: `scripts/quickstart.sh`

- [ ] **Step 1: Create the script skeleton with flag parsing, logging, and cleanup trap**

```bash
#!/usr/bin/env bash
set -euo pipefail

# scripts/quickstart.sh — one-command local-dev bootstrap.
#
# Takes a RUNNING minikube cluster to a green `VERDICT: PASS`:
#   CRDs → images → helm → controlplane → CLI → fixture → mysql target → run.
#
# Deliberate Plan-18 exception (the chart/CLI normally replace scripts): like
# minikube-up.sh, bootstrap needs real control flow — live port-forwards, wait
# loops, idempotency, progress — that Make cannot express cleanly.
#
# Idempotent: re-running skips completed steps. --rebuild forces image/CLI
# rebuilds. --with-kafka also runs the kafka scenario. Assumes minikube is
# already up (run scripts/minikube-up.sh first); never resets the cluster.

NS=dlh-test-fw
DLH_TOKEN='fake:dev:dev@example.com:dlh-admins'
REBUILD=false
WITH_KAFKA=false

# Resolve repo root so the script works regardless of cwd.
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# --- logging ---------------------------------------------------------------
if [[ -t 1 ]]; then
  C_BLUE=$'\033[34m'; C_GREEN=$'\033[32m'; C_RED=$'\033[31m'
  C_DIM=$'\033[2m'; C_RESET=$'\033[0m'
else
  C_BLUE=""; C_GREEN=""; C_RED=""; C_DIM=""; C_RESET=""
fi
TOTAL_STEPS=9
log_step() { printf '%s▶ [%s/%s] %s%s\n' "$C_BLUE" "$1" "$TOTAL_STEPS" "$2" "$C_RESET"; }
log_skip() { printf '%s✓ [%s/%s] %s — skipped (%s)%s\n' "$C_GREEN" "$1" "$TOTAL_STEPS" "$2" "$3" "$C_RESET"; }
log_ok()   { printf '%s✓ %s%s\n' "$C_GREEN" "$1" "$C_RESET"; }
log_info() { printf '%s  %s%s\n' "$C_DIM" "$1" "$C_RESET"; }
die()      { printf '%s✗ %s%s\n' "$C_RED" "$1" "$C_RESET" >&2; exit 1; }

# --- ephemeral port-forwards (trap-cleaned) --------------------------------
PF_PIDS=()
cleanup() {
  for pid in "${PF_PIDS[@]:-}"; do
    [[ -n "$pid" ]] && kill "$pid" 2>/dev/null || true
  done
}
trap cleanup EXIT

# port_forward <svc> <localport> <remoteport> — backgrounds a port-forward,
# records its PID for cleanup, and waits up to ~10s for the local port to open.
port_forward() {
  local svc="$1" lport="$2" rport="$3"
  kubectl -n "$NS" port-forward "svc/$svc" "$lport:$rport" >/dev/null 2>&1 &
  PF_PIDS+=("$!")
  for _ in {1..20}; do
    if nc -z localhost "$lport" 2>/dev/null; then return 0; fi
    sleep 0.5
  done
  die "port-forward to $svc:$rport did not become ready on localhost:$lport"
}

usage() {
  cat <<'EOF'
Usage: scripts/quickstart.sh [--rebuild] [--with-kafka] [--help]

  --rebuild      Force rebuild + reload of all images and the dlh CLI.
  --with-kafka   Also deploy the kafka target and run kafka-broker-partition.
  --help         Show this help.

Assumes minikube is already running (run scripts/minikube-up.sh first).
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --rebuild)    REBUILD=true ;;
    --with-kafka) WITH_KAFKA=true ;;
    --help|-h)    usage; exit 0 ;;
    *)            usage; die "unknown argument: $1" ;;
  esac
  shift
done

cd "$REPO_ROOT"

main() {
  printf '%sQuickstart: running minikube → green VERDICT: PASS%s\n' "$C_BLUE" "$C_RESET"
}

main "$@"
```

- [ ] **Step 2: Make it executable and verify it parses**

Run:
```bash
chmod +x scripts/quickstart.sh
bash -n scripts/quickstart.sh && echo "PARSE OK"
shellcheck scripts/quickstart.sh && echo "SHELLCHECK OK"
```
Expected: `PARSE OK` and `SHELLCHECK OK`.

- [ ] **Step 3: Verify flag handling**

Run:
```bash
scripts/quickstart.sh --help
scripts/quickstart.sh --bogus; echo "exit=$?"
```
Expected: `--help` prints usage and exits 0; `--bogus` prints usage + `✗ unknown argument: --bogus` and `exit=1`.

- [ ] **Step 4: Commit**

```bash
git add scripts/quickstart.sh
git commit -m "feat(quickstart): scaffold script — flags, logging, PF trap"
```

---

## Task 2: Preflight & safety gates

**Files:**
- Modify: `scripts/quickstart.sh`

- [ ] **Step 1: Add the preflight function above `main()`**

Insert before the `main()` definition:

```bash
# --- preflight -------------------------------------------------------------
preflight() {
  log_step 0 "Preflight checks"

  # 1. Required tools.
  local missing=()
  for tool in kubectl helm docker make go pnpm mc jq minikube nc; do
    command -v "$tool" >/dev/null 2>&1 || missing+=("$tool")
  done
  if [[ ${#missing[@]} -gt 0 ]]; then
    die "missing required tools: ${missing[*]}"
  fi

  # 2. minikube must already be running (we never reset it for you).
  if ! minikube status >/dev/null 2>&1; then
    die "minikube is not running. Run scripts/minikube-up.sh first."
  fi

  # 3. Context safety gate — refuse to touch a non-minikube cluster.
  local ctx
  ctx="$(kubectl config current-context 2>/dev/null || true)"
  if [[ "$ctx" != "minikube" ]]; then
    die "kube-context is '$ctx', expected 'minikube'. Refusing to run against a shared cluster."
  fi

  log_ok "preflight passed (context: $ctx)"
}
```

- [ ] **Step 2: Call it first in `main()`**

Replace the `main()` body so it reads:

```bash
main() {
  printf '%sQuickstart: running minikube → green VERDICT: PASS%s\n' "$C_BLUE" "$C_RESET"
  preflight
}
```

- [ ] **Step 3: Verify parse + lint**

Run:
```bash
bash -n scripts/quickstart.sh && shellcheck scripts/quickstart.sh && echo OK
```
Expected: `OK`.

- [ ] **Step 4: Verify the context gate fires (no live cluster needed)**

Run (forces a bogus context via a throwaway kubeconfig):
```bash
KUBECONFIG=/dev/null scripts/quickstart.sh; echo "exit=$?"
```
Expected: fails fast at one of the gates (missing tools are present, so it reaches the minikube/context gate) with a `✗ …` message and `exit=1`. (If minikube is up locally and context is minikube, instead run a quick targeted check: temporarily rename context expectation is out of scope — the `KUBECONFIG=/dev/null` form is the portable check.)

- [ ] **Step 5: Commit**

```bash
git add scripts/quickstart.sh
git commit -m "feat(quickstart): preflight — tools, minikube-up, context safety gate"
```

---

## Task 3: Step 1 — pre-install CRDs (idempotent)

**Files:**
- Modify: `scripts/quickstart.sh`

**Context:** This mirrors `make platform-crds`: render CRDs from the chart, server-side apply (Chaos Mesh CRDs exceed Helm's 256 KB annotation limit), then stamp Helm ownership so later `helm upgrade --install` manages them. Skip-check: a representative Chaos Mesh CRD already exists.

- [ ] **Step 1: Add the `step_crds` function above `main()`**

```bash
step_crds() {
  if kubectl get crd podchaos.chaos-mesh.org >/dev/null 2>&1; then
    log_skip 1 "Pre-install CRDs" "chaos-mesh CRDs already present"
    return 0
  fi
  log_step 1 "Pre-installing CRDs (server-side apply)"
  helm dependency update helm/dlh-test-fw >/dev/null
  helm template dlh helm/dlh-test-fw \
    -f helm/dlh-test-fw/values.yaml \
    -f helm/dlh-test-fw/values-minikube.yaml \
    --include-crds \
    | awk '/^---/{p=0} /kind: CustomResourceDefinition/{p=1} p{print}' \
    > /tmp/dlh-crds.yaml
  kubectl apply --server-side --force-conflicts -f /tmp/dlh-crds.yaml
  kubectl wait --for=condition=Established crd --all --timeout=120s
  kubectl label -f /tmp/dlh-crds.yaml app.kubernetes.io/managed-by=Helm --overwrite
  kubectl annotate -f /tmp/dlh-crds.yaml \
    meta.helm.sh/release-name=dlh \
    meta.helm.sh/release-namespace=dlh-test-fw --overwrite
  log_ok "CRDs installed and stamped for Helm ownership"
}
```

- [ ] **Step 2: Call it from `main()` after `preflight`**

```bash
main() {
  printf '%sQuickstart: running minikube → green VERDICT: PASS%s\n' "$C_BLUE" "$C_RESET"
  preflight
  step_crds
}
```

- [ ] **Step 3: Verify parse + lint**

Run:
```bash
bash -n scripts/quickstart.sh && shellcheck scripts/quickstart.sh && echo OK
```
Expected: `OK`.

- [ ] **Step 4: Commit**

```bash
git add scripts/quickstart.sh
git commit -m "feat(quickstart): step 1 — idempotent CRD pre-install"
```

---

## Task 4: Step 2 — build + load images (idempotent, `--rebuild`)

**Files:**
- Modify: `scripts/quickstart.sh`

**Context:** Six image tags (see Constants). Skip a build if its tag is already in `minikube image ls`, unless `--rebuild`. The controlplane and verdict and k6 images have their own Makefile targets that build+load; the three fixture images use the root `fixture-images` target. To keep skip-granularity per-image, the function checks each tag and dispatches the right build command.

- [ ] **Step 1: Add the `step_images` function above `main()`**

```bash
# build_and_load <tag> <build-command...> — skip if tag present unless --rebuild.
build_and_load() {
  local tag="$1"; shift
  if [[ "$REBUILD" == false ]] && minikube image ls 2>/dev/null | grep -q "$tag"; then
    log_info "image present: $tag (skip)"
    return 0
  fi
  log_info "building + loading: $tag"
  "$@"
}

step_images() {
  log_step 2 "Building + loading images"
  # Fixture shells: the root target builds+loads all three in one shot. Only
  # invoke it if any of the three is missing (or --rebuild).
  if [[ "$REBUILD" == true ]] \
     || ! minikube image ls 2>/dev/null | grep -q "dlh-fixture-mysql:0.1.0" \
     || ! minikube image ls 2>/dev/null | grep -q "dlh-fixture-kafka:0.1.0" \
     || ! minikube image ls 2>/dev/null | grep -q "dlh-fixture-doris:0.1.0"; then
    log_info "building + loading: dlh-fixture-{mysql,kafka,doris}:0.1.0"
    make fixture-images
  else
    log_info "fixture images present (skip)"
  fi

  build_and_load "ghcr.io/dlh/dlh-k6:0.1.0"           make k6-reload
  build_and_load "ghcr.io/dlh/dlh-verdict:0.1.0"      make -C verdict-job load-image
  build_and_load "ghcr.io/dlh/dlh-controlplane:0.1.0" make -C controlplane reload-minikube

  log_ok "images ready"
}
```

- [ ] **Step 2: Call it from `main()` after `step_crds`**

```bash
  step_crds
  step_images
```

- [ ] **Step 3: Verify parse + lint**

Run:
```bash
bash -n scripts/quickstart.sh && shellcheck scripts/quickstart.sh && echo OK
```
Expected: `OK`.

- [ ] **Step 4: Commit**

```bash
git add scripts/quickstart.sh
git commit -m "feat(quickstart): step 2 — idempotent image build/load (--rebuild)"
```

---

## Task 5: Step 3 — install the platform (helm)

**Files:**
- Modify: `scripts/quickstart.sh`

**Context:** `helm upgrade --install` is already converging; always run it (cheap no-op when nothing changed). `--wait` blocks until resources are Ready.

- [ ] **Step 1: Add the `step_platform` function above `main()`**

```bash
step_platform() {
  log_step 3 "Installing the platform (helm upgrade --install)"
  helm dependency update helm/dlh-test-fw >/dev/null
  helm upgrade --install dlh helm/dlh-test-fw \
    -f helm/dlh-test-fw/values.yaml \
    -f helm/dlh-test-fw/values-minikube.yaml \
    --namespace "$NS" --create-namespace --wait --timeout 5m
  log_ok "platform installed"
}
```

- [ ] **Step 2: Call it from `main()` after `step_images`**

```bash
  step_images
  step_platform
```

- [ ] **Step 3: Verify parse + lint**

Run:
```bash
bash -n scripts/quickstart.sh && shellcheck scripts/quickstart.sh && echo OK
```
Expected: `OK`.

- [ ] **Step 4: Commit**

```bash
git add scripts/quickstart.sh
git commit -m "feat(quickstart): step 3 — helm install platform"
```

---

## Task 6: Step 4 — deploy the controlplane

**Files:**
- Modify: `scripts/quickstart.sh`

**Context:** Apply the plain-YAML manifests, set the local-dev auth-disabled env, and block on rollout. `set env` is idempotent. `DLH_AUTH_DISABLED=true` is local-dev ONLY.

- [ ] **Step 1: Add the `step_controlplane` function above `main()`**

```bash
step_controlplane() {
  log_step 4 "Deploying the controlplane"
  kubectl -n "$NS" apply -f controlplane/deploy/
  kubectl -n "$NS" set env deployment/dlh-controlplane DLH_AUTH_DISABLED=true
  kubectl -n "$NS" rollout status deployment/dlh-controlplane --timeout=120s
  log_ok "controlplane ready"
}
```

- [ ] **Step 2: Call it from `main()` after `step_platform`**

```bash
  step_platform
  step_controlplane
```

- [ ] **Step 3: Verify parse + lint**

Run:
```bash
bash -n scripts/quickstart.sh && shellcheck scripts/quickstart.sh && echo OK
```
Expected: `OK`.

- [ ] **Step 4: Commit**

```bash
git add scripts/quickstart.sh
git commit -m "feat(quickstart): step 4 — deploy controlplane (auth-disabled, local-dev)"
```

---

## Task 7: Step 5 — build the dlh CLI

**Files:**
- Modify: `scripts/quickstart.sh`

**Context:** `make cli` builds `controlplane/bin/dlh`. Skip if the binary exists (unless `--rebuild`). Add `controlplane/bin` to the script's own PATH so later steps can call `dlh`.

- [ ] **Step 1: Add the `step_cli` function above `main()`**

```bash
step_cli() {
  if [[ "$REBUILD" == false && -x controlplane/bin/dlh ]]; then
    log_skip 5 "Building the dlh CLI" "binary present"
  else
    log_step 5 "Building the dlh CLI"
    make -C controlplane cli
    log_ok "dlh CLI built"
  fi
  export PATH="$REPO_ROOT/controlplane/bin:$PATH"
}
```

- [ ] **Step 2: Call it from `main()` after `step_controlplane`**

```bash
  step_controlplane
  step_cli
```

- [ ] **Step 3: Verify parse + lint**

Run:
```bash
bash -n scripts/quickstart.sh && shellcheck scripts/quickstart.sh && echo OK
```
Expected: `OK`.

- [ ] **Step 4: Commit**

```bash
git add scripts/quickstart.sh
git commit -m "feat(quickstart): step 5 — build dlh CLI, add to PATH"
```

---

## Task 8: Step 6 — seed the MinIO fixture (ephemeral port-forward)

**Files:**
- Modify: `scripts/quickstart.sh`

**Context / deviation from spec:** The spec hoped to seed port-forward-free via `kubectl exec … mc`. The pinned MinIO **server** image (`RELEASE.2024-12-13…`) does NOT bundle the `mc` client, so exec-in-pod is not viable. The correct implementation uses an **ephemeral, trap-cleaned MinIO port-forward + host `mc`** (mc is a required tool) — consistent with the core "ephemeral port-forward" decision. Root creds come live from the `minio-root-credentials` Secret (keys `root-user`/`root-password`). Skip-check: object already in the `fixtures` bucket.

- [ ] **Step 1: Add the `step_seed_minio` function above `main()`**

```bash
step_seed_minio() {
  log_step 6 "Seeding the MinIO mysql fixture"
  local user pass
  user="$(kubectl -n "$NS" get secret minio-root-credentials -o jsonpath='{.data.root-user}' | base64 -d)"
  pass="$(kubectl -n "$NS" get secret minio-root-credentials -o jsonpath='{.data.root-password}' | base64 -d)"

  port_forward dlh-minio 9000 9000
  mc alias set dlh-local "http://localhost:9000" "$user" "$pass" >/dev/null

  if mc stat dlh-local/fixtures/mysql-users.sql >/dev/null 2>&1; then
    log_skip 6 "Seeding the MinIO mysql fixture" "object already present"
    return 0
  fi
  mc mb --ignore-existing dlh-local/fixtures >/dev/null
  mc cp fixtures/mysql-users.sql dlh-local/fixtures/mysql-users.sql
  log_ok "fixture seeded to dlh-local/fixtures/mysql-users.sql"
}
```

- [ ] **Step 2: Call it from `main()` after `step_cli`**

```bash
  step_cli
  step_seed_minio
```

- [ ] **Step 3: Verify parse + lint**

Run:
```bash
bash -n scripts/quickstart.sh && shellcheck scripts/quickstart.sh && echo OK
```
Expected: `OK`.

- [ ] **Step 4: Commit**

```bash
git add scripts/quickstart.sh
git commit -m "feat(quickstart): step 6 — seed MinIO fixture via ephemeral PF"
```

---

## Task 9: Step 7 — deploy the mysql target

**Files:**
- Modify: `scripts/quickstart.sh`

**Context:** `kubectl apply` of the target manifest (creates `mysql-sys` ns + deployment + creds mirrored into `dlh-test-fw`). Idempotent. Block on rollout in `mysql-sys`.

- [ ] **Step 1: Add the `step_mysql_target` function above `main()`**

```bash
step_mysql_target() {
  log_step 7 "Deploying the mysql target"
  kubectl apply -f targets/mysql/deploy.yaml
  kubectl -n mysql-sys rollout status deploy/mysql --timeout=120s
  log_ok "mysql target ready"
}
```

- [ ] **Step 2: Call it from `main()` after `step_seed_minio`**

```bash
  step_seed_minio
  step_mysql_target
```

- [ ] **Step 3: Verify parse + lint**

Run:
```bash
bash -n scripts/quickstart.sh && shellcheck scripts/quickstart.sh && echo OK
```
Expected: `OK`.

- [ ] **Step 4: Commit**

```bash
git add scripts/quickstart.sh
git commit -m "feat(quickstart): step 7 — deploy mysql target"
```

---

## Task 10: Step 8 — submit the lightened mysql run (green PASS)

**Files:**
- Modify: `scripts/quickstart.sh`

**Context:** Ephemeral controlplane port-forward (8080), then `dlh run` with the documented lightened params so the verdict is `PASS`. `DLH_ENDPOINT`/`DLH_TOKEN` are exported for `dlh`.

- [ ] **Step 1: Add the `step_run` function above `main()`**

```bash
step_run() {
  log_step 8 "Submitting mysql-pod-delete (lightened → PASS)"
  port_forward dlh-controlplane 8080 80
  export DLH_ENDPOINT="http://localhost:8080"
  export DLH_TOKEN
  dlh run mysql-pod-delete --wait \
    -p load_duration=180s -p chaos_duration=15s -p chaos_start_after=60s
  log_ok "run complete"
}
```

- [ ] **Step 2: Call it from `main()` after `step_mysql_target`**

```bash
  step_mysql_target
  step_run
```

- [ ] **Step 3: Verify parse + lint**

Run:
```bash
bash -n scripts/quickstart.sh && shellcheck scripts/quickstart.sh && echo OK
```
Expected: `OK`.

- [ ] **Step 4: Commit**

```bash
git add scripts/quickstart.sh
git commit -m "feat(quickstart): step 8 — submit lightened mysql run via ephemeral PF"
```

---

## Task 11: Step 9 — Next-steps block (live Grafana creds)

**Files:**
- Modify: `scripts/quickstart.sh`

**Context:** Final output: copy-pasteable port-forward commands + URLs + live-fetched Grafana password (secret `grafana-admin-credentials`, keys `admin-user`/`admin-password`) + dlh env exports + the FAIL-by-design note.

- [ ] **Step 1: Add the `step_next_steps` function above `main()`**

```bash
step_next_steps() {
  log_step 9 "Done"
  local guser gpass
  guser="$(kubectl -n "$NS" get secret grafana-admin-credentials -o jsonpath='{.data.admin-user}' | base64 -d)"
  gpass="$(kubectl -n "$NS" get secret grafana-admin-credentials -o jsonpath='{.data.admin-password}' | base64 -d)"
  cat <<EOF

${C_GREEN}✓ Quickstart complete.${C_RESET}

Ongoing access (run in a spare terminal):
  Controlplane UI : kubectl -n ${NS} port-forward svc/dlh-controlplane 8080:80
                    → http://localhost:8080
  Grafana         : kubectl -n ${NS} port-forward svc/dlh-grafana 3001:80
                    → http://localhost:3001   (${guser} / ${gpass})

Use the dlh CLI:
  export PATH="\$PWD/controlplane/bin:\$PATH"
  export DLH_ENDPOINT=http://localhost:8080
  export DLH_TOKEN='${DLH_TOKEN}'
  dlh run mysql-pod-delete --wait                          # defaults FAIL the SLO by design
  dlh run mysql-pod-delete --wait -p chaos_duration=15s    # lightened → PASS

Note: this quickstart ran lightened chaos so the verdict is PASS. The default
mysql-pod-delete is heavier and FAILs the SLO on purpose (all steps still run).
EOF
}
```

- [ ] **Step 2: Call it from `main()` after `step_run`**

```bash
  step_run
  step_next_steps
```

- [ ] **Step 3: Verify parse + lint**

Run:
```bash
bash -n scripts/quickstart.sh && shellcheck scripts/quickstart.sh && echo OK
```
Expected: `OK`.

- [ ] **Step 4: Commit**

```bash
git add scripts/quickstart.sh
git commit -m "feat(quickstart): step 9 — Next-steps block with live Grafana creds"
```

---

## Task 12: `--with-kafka` optional path

**Files:**
- Modify: `scripts/quickstart.sh`

**Context:** When `--with-kafka`, after the mysql run, deploy the kafka target and run `kafka-broker-partition`. Reuses the controlplane env from `step_run` (same process, exports persist).

- [ ] **Step 1: Add the `step_kafka` function above `main()`**

```bash
step_kafka() {
  [[ "$WITH_KAFKA" == true ]] || return 0
  log_step 9 "Optional: kafka scenario"
  kubectl apply -f targets/kafka/deploy.yaml
  kubectl -n kafka-sys rollout status statefulset/kafka --timeout=240s
  dlh run kafka-broker-partition --wait
  log_ok "kafka run complete"
}
```

- [ ] **Step 2: Call it from `main()` between `step_run` and `step_next_steps`**

```bash
  step_run
  step_kafka
  step_next_steps
```

- [ ] **Step 3: Verify parse + lint**

Run:
```bash
bash -n scripts/quickstart.sh && shellcheck scripts/quickstart.sh && echo OK
```
Expected: `OK`.

- [ ] **Step 4: Commit**

```bash
git add scripts/quickstart.sh
git commit -m "feat(quickstart): --with-kafka optional kafka scenario"
```

---

## Task 13: `make quickstart` alias

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: Add the target and update the `.PHONY` line**

In `Makefile`, add `quickstart` to the first `.PHONY:` list, and append this target near `platform-up`:

```makefile
# quickstart: one-command local-dev bootstrap (running minikube → green
# VERDICT: PASS). Thin alias for scripts/quickstart.sh; see that script for
# flags (--rebuild, --with-kafka). Local-dev only.
quickstart:
	scripts/quickstart.sh
```

- [ ] **Step 2: Verify the target is discoverable and delegates**

Run:
```bash
grep -A1 '^quickstart:' Makefile
make -n quickstart
```
Expected: `make -n quickstart` prints `scripts/quickstart.sh`.

- [ ] **Step 3: Commit**

```bash
git add Makefile
git commit -m "feat(quickstart): add make quickstart alias"
```

---

## Task 14: Docs — README + CLAUDE.md

**Files:**
- Modify: `README.md` (Quickstart section)
- Modify: `CLAUDE.md` (Operational model → local-dev)

- [ ] **Step 1: Rewrite the README Quickstart lead-in**

Replace the prose + `bash` block under `## Quickstart` (the 10-step sequence shown in the spec) with a script-first version. The new top of the section:

````markdown
## Quickstart

Requires `minikube`, `kubectl`, `helm`, `docker`, `make`, `go` 1.26+,
`pnpm` (Node 20+), `mc` (MinIO client), `jq`, `nc`, `bash`. On Apple Silicon
minikube uses the docker driver.

```bash
# 1. Start minikube (destructive reset; skip if already running)
scripts/minikube-up.sh

# 2. One command: CRDs → images → helm → controlplane → CLI → fixture →
#    mysql target → submit a lightened mysql-pod-delete run → VERDICT: PASS.
#    Idempotent (re-run skips done steps). Flags: --rebuild, --with-kafka.
scripts/quickstart.sh
```

`scripts/quickstart.sh` is idempotent and safe to re-run; it refuses to run
against any kube-context other than `minikube`. When it finishes it prints a
**Next steps** block with the port-forward commands, URLs, and Grafana
credentials for ongoing access.

<details>
<summary>Manual steps — what quickstart does under the hood</summary>

<the existing 10-step bash block, preserved verbatim for transparency,
but with the stale `dlh-grafana-credentials` reference corrected to
`grafana-admin-credentials`>

</details>
````

Preserve the existing 10-step block inside the `<details>` element (do not delete it). While moving it, fix the one stale reference: change `<secret value of dlh-grafana-credentials>` to read `<secret value of grafana-admin-credentials (key admin-password)>`.

- [ ] **Step 2: Also fix the stale secret name in the "See the dashboards" section**

In the `### See the dashboards` block, the line currently reads:
`# http://localhost:3001  admin / <secret value of dlh-grafana-credentials>`
Change `dlh-grafana-credentials` → `grafana-admin-credentials`.

- [ ] **Step 3: Add quickstart.sh to CLAUDE.md's local-dev section**

In `CLAUDE.md`, under "### Local-dev (laptop minikube)", change the line
"After Plan 18, only `scripts/minikube-up.sh` remains." to note the second
script, and add a bullet:

```markdown
- `scripts/quickstart.sh` — one-command bootstrap on a *running* minikube
  (CRDs → images → helm → controlplane → fixture → mysql target → lightened
  run → green VERDICT). Idempotent; `--rebuild` / `--with-kafka` flags. A
  deliberate second sanctioned script (like `minikube-up.sh`, bootstrap needs
  control flow Make can't express).
```

- [ ] **Step 4: Verify the docs render and references are consistent**

Run:
```bash
grep -n "quickstart.sh" README.md CLAUDE.md
grep -rn "dlh-grafana-credentials" README.md; echo "stale refs above should be empty"
```
Expected: `quickstart.sh` referenced in both files; no remaining `dlh-grafana-credentials` in README.md.

- [ ] **Step 5: Commit**

```bash
git add README.md CLAUDE.md
git commit -m "docs(quickstart): script-first README + CLAUDE.md, fix stale grafana secret name"
```

---

## Task 15: End-to-end verification on a live cluster

**Files:** none (verification only)

**Context:** This is the real proof. Requires a machine with minikube up (or run `scripts/minikube-up.sh` first). Uses `superpowers:verification-before-completion` discipline: run the command, observe the actual output, do not claim success without it.

- [ ] **Step 1: Fresh end-to-end run**

Run:
```bash
scripts/minikube-up.sh        # if not already up
scripts/quickstart.sh
```
Expected: staged `▶ [N/9]` progress through all steps; the run streams to completion; final output contains `VERDICT: PASS` and the Next-steps block with a real Grafana password.

- [ ] **Step 2: Idempotent re-run is fast**

Run:
```bash
time scripts/quickstart.sh
```
Expected: CRDs / images / CLI / fixture steps report `✓ … skipped`; no image rebuilds; completes much faster than Step 1; still ends `VERDICT: PASS`.

- [ ] **Step 3: Safety gate**

Run:
```bash
KUBECONFIG=/dev/null scripts/quickstart.sh; echo "exit=$?"
```
Expected: refuses with a `✗` message and `exit=1` (context/minikube gate), no cluster mutation.

- [ ] **Step 4: `--with-kafka` (optional, if validating the kafka path)**

Run:
```bash
scripts/quickstart.sh --with-kafka
```
Expected: after the mysql run, deploys the kafka target and completes `kafka-broker-partition`.

- [ ] **Step 5: Confirm no orphan port-forwards**

Run:
```bash
pgrep -fa "port-forward" || echo "no orphan port-forwards"
```
Expected: `no orphan port-forwards` (the EXIT trap cleaned them up).

- [ ] **Step 6: Final commit (if any verification-driven fixes were made)**

```bash
git add -A
git commit -m "fix(quickstart): address issues found in end-to-end verification"
```
(Skip if no fixes were needed.)

---

## Self-Review notes

- **Spec coverage:** form factor (Task 1, 13) · Plan-18 exception note (Task 1 header, Task 14) · scope = everything-but-minikube (Task 2 gate) · idempotent + `--rebuild` (Tasks 4,7) · ephemeral trap-cleaned PFs (Task 1 helper, Tasks 8,10) · context safety gate (Task 2) · all 9 steps (Tasks 3–11) · lightened→PASS run (Task 10) · Next-steps + live Grafana creds (Task 11) · `--with-kafka` (Task 12) · README/CLAUDE.md + stale-secret fix (Task 14) · verification (Task 15).
- **Deviation from spec (documented):** Step 6 seeds via ephemeral MinIO port-forward + host `mc` rather than `kubectl exec mc`, because the pinned MinIO server image has no `mc` client (Task 8 context). Still honors the core ephemeral/trap-cleaned decision.
- **Type/name consistency:** function names (`step_crds`, `step_images`, `build_and_load`, `step_platform`, `step_controlplane`, `step_cli`, `step_seed_minio`, `step_mysql_target`, `step_run`, `step_kafka`, `step_next_steps`, `port_forward`, `preflight`, `cleanup`) are defined once and referenced consistently. Secret names verified against the chart: `minio-root-credentials` (`root-user`/`root-password`), `grafana-admin-credentials` (`admin-user`/`admin-password`). Image tags match the Constants block and each component Makefile.
