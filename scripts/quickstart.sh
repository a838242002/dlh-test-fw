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
# shellcheck disable=SC2034  # used by later bootstrap steps
DLH_TOKEN='fake:dev:dev@example.com:dlh-admins'
REBUILD=false
# shellcheck disable=SC2034  # used by later bootstrap steps
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

# shellcheck disable=SC2034  # REBUILD/WITH_KAFKA used by later bootstrap steps
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

main() {
  printf '%sQuickstart: running minikube → green VERDICT: PASS%s\n' "$C_BLUE" "$C_RESET"
  preflight
  step_crds
  step_images
  step_platform
}

main "$@"
