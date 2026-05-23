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
  for pid in "${PF_PIDS[@]+"${PF_PIDS[@]}"}"; do
    kill "$pid" 2>/dev/null || true
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
    *)            usage >&2; die "unknown argument: $1" ;;
  esac
  shift
done

cd "$REPO_ROOT"

# --- preflight -------------------------------------------------------------
preflight() {
  printf '%s▶ Preflight checks%s\n' "$C_BLUE" "$C_RESET"

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

step_platform() {
  log_step 3 "Installing the platform (helm upgrade --install)"
  helm dependency update helm/dlh-test-fw >/dev/null
  helm upgrade --install dlh helm/dlh-test-fw \
    -f helm/dlh-test-fw/values.yaml \
    -f helm/dlh-test-fw/values-minikube.yaml \
    --namespace "$NS" --create-namespace --wait --timeout 5m
  log_ok "platform installed"
}

step_controlplane() {
  log_step 4 "Deploying the controlplane"
  kubectl -n "$NS" apply -f controlplane/deploy/
  kubectl -n "$NS" set env deployment/dlh-controlplane DLH_AUTH_DISABLED=true
  kubectl -n "$NS" rollout status deployment/dlh-controlplane --timeout=120s
  log_ok "controlplane ready"
}

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

step_run() {
  log_step 8 "Submitting mysql-pod-delete (lightened → PASS)"
  port_forward dlh-controlplane 8080 80
  export DLH_ENDPOINT="http://localhost:8080"
  export DLH_TOKEN
  dlh run mysql-pod-delete --wait \
    -p load_duration=180s -p chaos_duration=15s -p chaos_start_after=60s
  log_ok "run complete"
}

step_next_steps() {
  log_step "$TOTAL_STEPS" "Done"
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

step_kafka() {
  [[ "$WITH_KAFKA" == true ]] || return 0
  log_step 9 "Optional: kafka scenario"
  # Reuses the controlplane port-forward + DLH_ENDPOINT that step_run opens;
  # step_run always runs immediately before step_kafka in main().
  kubectl apply -f targets/kafka/deploy.yaml
  kubectl -n kafka-sys rollout status statefulset/kafka --timeout=240s
  dlh run kafka-broker-partition --wait
  log_ok "kafka run complete"
}

step_mysql_target() {
  log_step 7 "Deploying the mysql target"
  kubectl apply -f targets/mysql/deploy.yaml
  kubectl -n mysql-sys rollout status deploy/mysql --timeout=120s
  log_ok "mysql target ready"
}

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
  kubectl wait --for=condition=Established -f /tmp/dlh-crds.yaml --timeout=120s
  kubectl label -f /tmp/dlh-crds.yaml app.kubernetes.io/managed-by=Helm --overwrite
  kubectl annotate -f /tmp/dlh-crds.yaml \
    meta.helm.sh/release-name=dlh \
    meta.helm.sh/release-namespace=dlh-test-fw --overwrite
  log_ok "CRDs installed and stamped for Helm ownership"
}

main() {
  [[ "$WITH_KAFKA" == true ]] && TOTAL_STEPS=10
  printf '%sQuickstart: running minikube → green VERDICT: PASS%s\n' "$C_BLUE" "$C_RESET"
  preflight
  step_crds
  step_images
  step_platform
  step_controlplane
  step_cli
  step_seed_minio
  step_mysql_target
  step_run
  step_kafka
  step_next_steps
}

main "$@"
