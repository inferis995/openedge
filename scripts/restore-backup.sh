#!/usr/bin/env bash
# Restore the OpenEdge database from a backup file produced by the
# nightly backup container. Designed for the operator-runs-once-after-
# disaster path, NOT for routine use.
#
# Usage:
#   ./scripts/restore-backup.sh <backup-file>
#   ./scripts/restore-backup.sh ./backups/openedge-20250604T030000Z.dump
#   ./scripts/restore-backup.sh ./backups/openedge-20250604T030000Z.dump.age
#
# The script:
#   1. Refuses to run unless --yes-i-have-a-recent-backup is passed
#      (small foot-gun guard — the running DB is about to be wiped).
#   2. Decrypts the dump with age if necessary (asks for the identity
#      file path).
#   3. Stops the application containers so they don't write while we
#      restore.
#   4. Drops and recreates the database from scratch.
#   5. Runs pg_restore.
#   6. Starts the application back up.

set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: $0 <backup-file> [--yes-i-have-a-recent-backup]" >&2
  exit 2
fi

BACKUP="$1"
CONFIRM="${2:-}"

if [[ ! -f "$BACKUP" ]]; then
  echo "backup file not found: $BACKUP" >&2
  exit 1
fi

if [[ "$CONFIRM" != "--yes-i-have-a-recent-backup" ]]; then
  cat >&2 <<EOF
RESTORE IS DESTRUCTIVE.

This will:
  - stop core-api / web-ui / drivers (data stops flowing in)
  - DROP the current database (every row is lost)
  - recreate it from $BACKUP

If something goes wrong, the only way back is another backup. Make sure
you have a recent one (or that you don't care about the current state)
and re-run with:

  $0 $BACKUP --yes-i-have-a-recent-backup

EOF
  exit 1
fi

# Decrypt if needed --------------------------------------------------------
WORK_DIR=$(mktemp -d)
trap 'rm -rf "$WORK_DIR"' EXIT

if [[ "$BACKUP" == *.age ]]; then
  read -rp "age identity file (private key) path: " IDENTITY
  if [[ ! -f "$IDENTITY" ]]; then
    echo "identity file not found: $IDENTITY" >&2
    exit 1
  fi
  echo "[restore] decrypting $BACKUP ..."
  DECRYPTED="$WORK_DIR/dump.bin"
  age --decrypt -i "$IDENTITY" -o "$DECRYPTED" "$BACKUP"
  BACKUP="$DECRYPTED"
fi

# Stop writers so the DB is quiescent during restore -----------------------
echo "[restore] stopping application containers..."
docker compose --profile local stop \
  core-api web-ui engine-historian driver-manager 2>/dev/null || true

# Read DB creds from .env --------------------------------------------------
if [[ -f .env ]]; then
  # shellcheck disable=SC1091
  set -a; . .env; set +a
fi
PGUSER="${POSTGRES_USER:-industrial_user}"
PGDB="${POSTGRES_DB:-industrial_edge}"
PGPASS="${POSTGRES_PASSWORD:-industrial_pass}"

# Drop + recreate + restore ------------------------------------------------
echo "[restore] dropping and recreating $PGDB ..."
docker compose exec -T -e PGPASSWORD="$PGPASS" postgres \
  psql -U "$PGUSER" -d postgres -c "DROP DATABASE IF EXISTS $PGDB;"
docker compose exec -T -e PGPASSWORD="$PGPASS" postgres \
  psql -U "$PGUSER" -d postgres -c "CREATE DATABASE $PGDB;"

echo "[restore] running pg_restore ..."
docker compose exec -T -e PGPASSWORD="$PGPASS" postgres \
  pg_restore --no-owner --no-privileges --dbname "$PGDB" < "$BACKUP"

# Restart writers ---------------------------------------------------------
echo "[restore] starting application containers..."
docker compose --profile local up -d core-api web-ui engine-historian driver-manager

echo
echo "OK — restored from $1 (decrypted: $BACKUP)"
echo "Verify the UI is up at http://localhost:3000 and check a few historical tags."
