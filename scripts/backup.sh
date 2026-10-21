#!/usr/bin/env bash
# Logical backup of the criAIsis database, verified and pruned.
#
#   scripts/backup.sh [backup-dir]
#
# Connection comes from the usual libpq variables (PGHOST, PGPORT, PGUSER,
# PGPASSWORD, PGDATABASE). Set PG_DOCKER_CONTAINER to run the Postgres client
# tools inside that container instead of on the host (local development).
#
# Retention (grandfather-father-son, by file name, no date arithmetic): keep the
# newest $KEEP_DAILY dumps, plus the newest dump of each of the last
# $KEEP_MONTHLY calendar months.
#
# A dump is only as useful as the key that decrypts the credentials inside it:
# CRIAISIS_SECURITY_CREDENTIAL_ENCRYPTION_KEY must be backed up separately, and
# never next to the dumps.
set -euo pipefail

DIR="${1:-./backups}"
KEEP_DAILY="${KEEP_DAILY:-7}"
KEEP_MONTHLY="${KEEP_MONTHLY:-6}"
DB="${PGDATABASE:-criaisis}"

pg() {
  if [[ -n "${PG_DOCKER_CONTAINER:-}" ]]; then
    docker exec -i -e PGPASSWORD="${PGPASSWORD:-}" "$PG_DOCKER_CONTAINER" "$@"
  else
    "$@"
  fi
}

mkdir -p "$DIR"
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
final="$DIR/criaisis-$stamp.dump"
partial="$final.partial"

# Custom format: compressed, and restorable selectively and in parallel.
pg pg_dump --format=custom --no-owner --dbname="$DB" ${PGUSER:+"--username=$PGUSER"} > "$partial"

# Verify before trusting it: a truncated dump fails to list its contents.
if ! pg pg_restore --list < "$partial" > /dev/null; then
  rm -f "$partial"
  echo "backup verification failed; nothing kept" >&2
  exit 1
fi
mv "$partial" "$final" # atomic: a reader never sees a half-written dump
echo "backup: $final ($(du -h "$final" | cut -f1))"

# Prune. Names sort chronologically, so newest-first is a reverse sort.
# Written for bash 3.2 (macOS) as well as Linux: no mapfile, no assoc arrays.
keep=" "
months=" "
n=0
nmonths=0
for f in $(ls -1 "$DIR"/criaisis-*.dump 2>/dev/null | sort -r); do
  n=$((n + 1))
  month="$(basename "$f" | cut -c10-15)" # YYYYMM from criaisis-YYYYMMDDT...
  if [ "$n" -le "$KEEP_DAILY" ]; then
    keep="$keep$f "
  fi
  case "$months" in
    *" $month "*) ;;
    *)
      if [ "$nmonths" -lt "$KEEP_MONTHLY" ]; then
        months="$months$month "
        nmonths=$((nmonths + 1))
        keep="$keep$f "
      fi
      ;;
  esac
done
for f in $(ls -1 "$DIR"/criaisis-*.dump 2>/dev/null); do
  case "$keep" in
    *" $f "*) ;;
    *) rm -f "$f"; echo "pruned: $f" ;;
  esac
done
