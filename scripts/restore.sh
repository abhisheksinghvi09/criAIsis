#!/usr/bin/env bash
# Restore a dump from scripts/backup.sh into a NEW database.
#
#   scripts/restore.sh <dump-file> <target-database>
#
# It refuses to restore into a database that already exists: recovering by
# overwriting the live database destroys the evidence of what went wrong and
# leaves no way back. Restore beside it, verify, then repoint the application
# (CRIAISIS_DATABASE_NAME) at the restored copy.
set -euo pipefail

DUMP="${1:?usage: restore.sh <dump-file> <target-database>}"
TARGET="${2:?usage: restore.sh <dump-file> <target-database>}"
[[ "$TARGET" =~ ^[a-z_][a-z0-9_]*$ ]] || { echo "target database name must be lowercase [a-z0-9_]" >&2; exit 1; }

pg() {
  if [[ -n "${PG_DOCKER_CONTAINER:-}" ]]; then
    docker exec -i -e PGPASSWORD="${PGPASSWORD:-}" "$PG_DOCKER_CONTAINER" "$@"
  else
    "$@"
  fi
}
admin_db="${PGADMIN_DATABASE:-postgres}"
user_flag=(--username="${PGUSER:-postgres}")

exists="$(pg psql "${user_flag[@]}" --dbname="$admin_db" -tAc "SELECT 1 FROM pg_database WHERE datname = '$TARGET'")"
if [[ "$exists" == "1" ]]; then
  echo "database $TARGET already exists; restore into a new name" >&2
  exit 1
fi

pg psql "${user_flag[@]}" --dbname="$admin_db" -v ON_ERROR_STOP=1 -qc "CREATE DATABASE $TARGET"
# --exit-on-error: a partial restore must fail loudly, never look like success.
pg pg_restore "${user_flag[@]}" --dbname="$TARGET" --no-owner --exit-on-error < "$DUMP"
echo "restored $DUMP into $TARGET"
