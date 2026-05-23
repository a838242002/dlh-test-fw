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
# shellcheck disable=SC2034  # used by later bootstrap steps
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

# shellcheck disable=SC2034  # REBUILD/WITH_KAFKA used by later bootstrap steps
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
