#!/usr/bin/env bash
#
# Reproduce an incident's conditions in throwaway containers, so a hypothesis or a
# candidate fix can be tested without touching production.
#
#   scripts/sandbox_reproduce.sh db_connection_exhaustion
#   scripts/sandbox_reproduce.sh oom_crashloop --keep
#
# Every scenario maps to a Docker Compose profile in
# deployments/docker-compose.sandbox.yml. The sandbox shares no volume, port or
# network with the development stack, and is torn down on exit unless --keep is given.

set -euo pipefail

COMPOSE_FILE="deployments/docker-compose.sandbox.yml"
SCENARIO_DIR="testdata/scenarios"
KEEP=0
OBSERVE_SECONDS="${SANDBOX_OBSERVE_SECONDS:-25}"

usage() {
  cat <<USAGE
usage: $(basename "$0") <scenario> [--keep]

Available scenarios:
$(find "$SCENARIO_DIR" -name '*.json' -exec basename {} .json \; 2>/dev/null | sed 's/^/  /')

  --keep   leave the sandbox running for manual inspection
USAGE
}

[ $# -ge 1 ] || { usage; exit 2; }

SCENARIO="$1"; shift
for arg in "$@"; do
  case "$arg" in
    --keep) KEEP=1 ;;
    *) echo "unknown option: $arg" >&2; usage; exit 2 ;;
  esac
done

FIXTURE="$SCENARIO_DIR/$SCENARIO.json"
[ -f "$FIXTURE" ] || { echo "error: no fixture at $FIXTURE" >&2; usage; exit 2; }
[ -f "$COMPOSE_FILE" ] || { echo "error: $COMPOSE_FILE not found (run from the repository root)" >&2; exit 2; }
# Compose ships either as a docker plugin ("docker compose") or as a standalone
# binary ("docker-compose"). Accept whichever this machine has.
if docker compose version >/dev/null 2>&1; then
  COMPOSE_CMD=(docker compose)
elif command -v docker-compose >/dev/null 2>&1; then
  COMPOSE_CMD=(docker-compose)
else
  echo "error: Docker Compose is required (neither 'docker compose' nor 'docker-compose' found)" >&2
  exit 2
fi

# A scenario without a profile has no reproducible condition yet. Say so rather
# than starting an empty sandbox and implying the incident was recreated.
if ! grep -q "\"$SCENARIO\"" "$COMPOSE_FILE"; then
  echo "error: no sandbox profile defined for '$SCENARIO'." >&2
  echo "       add a service with profiles: [\"$SCENARIO\"] to $COMPOSE_FILE" >&2
  exit 1
fi

# Fail before the trap is installed, so an unreachable daemon cannot produce a
# misleading "sandbox removed" message for a sandbox that never started.
if ! docker info >/dev/null 2>&1; then
  echo "error: the Docker daemon is not reachable." >&2
  echo "       start it first (for example: colima start, or open Docker Desktop)." >&2
  exit 2
fi

compose() { "${COMPOSE_CMD[@]}" -f "$COMPOSE_FILE" --profile "$SCENARIO" "$@"; }

teardown() {
  if [ "$KEEP" -eq 1 ]; then
    echo
    echo "sandbox left running. Tear it down with:"
    echo "  ${COMPOSE_CMD[*]} -f $COMPOSE_FILE --profile $SCENARIO down -v"
    return
  fi
  echo
  echo "tearing down sandbox..."
  compose down -v --remove-orphans >/dev/null 2>&1 || true
  echo "sandbox removed."
}
trap teardown EXIT

echo
echo "  criAIsis sandbox reproduction"
echo "  ─────────────────────────────────────────────────────"
echo "  scenario   $SCENARIO"
echo "  expected   $(grep -o '"expected_classification"[^,]*' "$FIXTURE" | cut -d'"' -f4) root cause"
echo "  profile    $COMPOSE_FILE"
echo

echo "starting sandbox containers..."
compose up -d --remove-orphans

echo
echo "observing for ${OBSERVE_SECONDS}s while the failure condition develops..."
sleep "$OBSERVE_SECONDS"

echo
echo "  container state"
echo "  ─────────────────────────────────────────────────────"
compose ps

echo
echo "  container logs (tail)"
echo "  ─────────────────────────────────────────────────────"
compose logs --tail=25

echo
echo "  read-only diagnostics"
echo "  ─────────────────────────────────────────────────────"
echo "These are the commands a specialist would hand an engineer. They only read:"
case "$SCENARIO" in
  db_connection_exhaustion)
    echo "  psql 'postgres://postgres:sandbox@localhost:55432/sandbox' \\"
    echo "    -c \"SELECT pid, state, query FROM pg_stat_activity ORDER BY state_change;\""
    echo "  psql 'postgres://postgres:sandbox@localhost:55432/sandbox' -c 'SHOW max_connections;'"
    ;;
  oom_crashloop)
    echo "  ${COMPOSE_CMD[*]} -f $COMPOSE_FILE --profile $SCENARIO ps"
    echo "  docker inspect --format '{{.State.OOMKilled}} {{.State.ExitCode}}' \\"
    echo "    \$(compose ps -q memory-capped-worker)"
    ;;
  checkout_packet_loss)
    echo "  ${COMPOSE_CMD[*]} -f $COMPOSE_FILE --profile $SCENARIO exec lossy-network tc qdisc show dev eth0"
    echo "  ${COMPOSE_CMD[*]} -f $COMPOSE_FILE --profile $SCENARIO exec lossy-network ping -c 10 1.1.1.1"
    ;;
esac
echo
