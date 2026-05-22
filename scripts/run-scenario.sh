#!/usr/bin/env bash
set -euo pipefail

# ============================================================================
# DEPRECATED — local-dev only. Wraps `dlh run` (Plan 16). Phase E will
# remove this script. New code should use the dlh CLI directly:
#   dlh run <scenario> --param key=value
# ============================================================================

echo >&2 "[deprecation] scripts/run-scenario.sh is a shim around 'dlh run' since Plan 16."
echo >&2 "[deprecation] Prefer: dlh run <scenario> --param key=value --wait"

if ! command -v dlh >/dev/null 2>&1; then
  echo >&2 "error: dlh CLI not found in PATH."
  echo >&2 "  Build it: cd controlplane && make cli && cp bin/dlh /usr/local/bin/dlh"
  exit 127
fi

# Historical usage:
#   ./scripts/run-scenario.sh <scenarios/X.yaml> [-p key=value]...
# Scenario name comes from the file's metadata.generateName prefix
# (without trailing '-'). Parameters pass through as --param.
if [[ $# -lt 1 ]]; then
  echo "usage: $0 scenarios/<scenario>.yaml [-p key=value]..." >&2
  exit 2
fi

SCENARIO_FILE="$1"; shift
SCENARIO_NAME=$(awk '/^[[:space:]]*generateName:/ {print $2; exit}' "$SCENARIO_FILE" | sed 's/-$//')

if [[ -z "$SCENARIO_NAME" ]]; then
  echo >&2 "error: could not extract generateName from $SCENARIO_FILE"
  exit 1
fi

ARGS=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    -p|--param)
      ARGS+=(--param "$2")
      shift 2
      ;;
    -w|--wait)
      ARGS+=(--wait)
      shift
      ;;
    *)
      echo >&2 "[deprecation] unknown flag $1 — pass directly to 'dlh run' instead."
      exit 2
      ;;
  esac
done

exec dlh run "$SCENARIO_NAME" "${ARGS[@]}"
