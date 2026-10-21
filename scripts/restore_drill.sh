#!/usr/bin/env bash
# Prove that backups restore: back up, restore into a scratch database, compare
# every table's row count and the migration version, then drop the scratch copy.
# A backup that has never been restored is a hope, not a backup. Run on a schedule.
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
DB="${PGDATABASE:-criaisis}"
SCRATCH="criaisis_drill_$(date -u +%s)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

pg() {
  if [[ -n "${PG_DOCKER_CONTAINER:-}" ]]; then
    docker exec -i -e PGPASSWORD="${PGPASSWORD:-}" "$PG_DOCKER_CONTAINER" "$@"
  else
    "$@"
  fi
}
user_flag=(--username="${PGUSER:-postgres}")

counts() {
  pg psql "${user_flag[@]}" --dbname="$1" -tA -v ON_ERROR_STOP=1 <<'SQL'
SELECT 'schema_migrations=' || version || ' dirty=' || dirty FROM schema_migrations;
SELECT format('%s=%s', c.relname,
       (xpath('/row/n/text()', query_to_xml(format('SELECT count(*) AS n FROM %I', c.relname), false, true, '')))[1]::text)
FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = 'public' AND c.relkind = 'r'
ORDER BY c.relname;
SQL
}

KEEP_DAILY=1 KEEP_MONTHLY=0 "$HERE/backup.sh" "$WORK" > /dev/null
dump="$(ls -1 "$WORK"/criaisis-*.dump)"
"$HERE/restore.sh" "$dump" "$SCRATCH" > /dev/null

before="$(counts "$DB")"
after="$(counts "$SCRATCH")"
pg psql "${user_flag[@]}" --dbname="${PGADMIN_DATABASE:-postgres}" -qc "DROP DATABASE $SCRATCH WITH (FORCE)"

if [[ "$before" != "$after" ]]; then
  echo "RESTORE DRILL FAILED: restored copy differs from the source" >&2
  diff <(echo "$before") <(echo "$after") >&2 || true
  exit 1
fi
echo "restore drill passed: $(echo "$before" | wc -l | tr -d ' ') checks identical"
echo "$before"
