#!/usr/bin/env bash
# OpenEdge database restore (plain SQL .gz produced by scripts/backup.sh).
#
# Usage:
#   ./scripts/restore.sh <backup_file.sql.gz> --yes-i-have-a-recent-backup
#
# This DROPs the live database. The confirmation flag is mandatory and mirrors
# scripts/restore-backup.sh; the previous "Ctrl+C within 5s" countdown was not a
# confirmation at all — under cron, nohup, CI or any non-interactive shell there
# was nobody to press it, and the production database went away regardless.
#
# Before dropping anything the script takes a safety dump of the CURRENT database
# into backups/pre-restore_<timestamp>.sql.gz, so a restore of the wrong file is
# recoverable.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BACKUP_DIR="${REPO_ROOT}/backups"

usage() {
  echo "Usage: $0 <backup_file.sql.gz> --yes-i-have-a-recent-backup" >&2
  echo "Available backups:" >&2
  ls -lh "${BACKUP_DIR}"/*.sql.gz 2>/dev/null >&2 || echo "  (none)" >&2
}

BACKUP_FILE="${1:-}"
CONFIRM="${2:-}"

if [ -z "$BACKUP_FILE" ] || [ ! -f "$BACKUP_FILE" ]; then
  usage
  exit 1
fi

if [ -f "${REPO_ROOT}/.env" ]; then
  # shellcheck disable=SC1091
  set -a; . "${REPO_ROOT}/.env"; set +a
fi

PGUSER="${POSTGRES_USER:-industrial_user}"
PGDB="${POSTGRES_DB:-industrial_edge}"
CONTAINER_NAME="${POSTGRES_CONTAINER:-openedge-postgres}"

if [ "$CONFIRM" != "--yes-i-have-a-recent-backup" ]; then
  cat >&2 <<EOF
RESTORE IS DESTRUCTIVE.

This will:
  - DROP the database '${PGDB}' on container '${CONTAINER_NAME}' (every row is lost)
  - recreate it from ${BACKUP_FILE}

A safety dump of the current database is taken first, into
  ${BACKUP_DIR}/pre-restore_<timestamp>.sql.gz
but do not rely on it alone. Re-run with:

  $0 ${BACKUP_FILE} --yes-i-have-a-recent-backup

EOF
  exit 1
fi

# Second gate when a human is actually at the keyboard: type the database name.
# Skipped automatically when stdin is not a TTY so scripted/DR use still works
# with the explicit flag above.
if [ -t 0 ]; then
  printf 'Type the database name to confirm (%s): ' "$PGDB" >&2
  read -r TYPED
  if [ "$TYPED" != "$PGDB" ]; then
    echo "[RESTORE] Aborted — '$TYPED' does not match '$PGDB'." >&2
    exit 1
  fi
fi

# Exact container match (see scripts/backup.sh) — "name=postgres" also matched
# openedge-postgres-exporter.
CONTAINER_ID=$(docker ps --filter "name=^/?${CONTAINER_NAME}$" --filter "status=running" -q)
if [ -z "$CONTAINER_ID" ]; then
  echo "[RESTORE] ERROR: no running container named '${CONTAINER_NAME}'" >&2
  exit 1
fi
if [ "$(printf '%s\n' "$CONTAINER_ID" | wc -l)" -gt 1 ]; then
  echo "[RESTORE] ERROR: '${CONTAINER_NAME}' matched more than one running container" >&2
  exit 1
fi

# Refuse to feed an archive we cannot even decompress.
if ! gzip -t "$BACKUP_FILE" 2>/dev/null; then
  echo "[RESTORE] ERROR: ${BACKUP_FILE} is not a valid gzip archive." >&2
  exit 1
fi

# --- Safety dump of the current state ---------------------------------------
mkdir -p "$BACKUP_DIR"
SAFETY_TS=$(date +%Y%m%d_%H%M%S)
SAFETY_FILE="${BACKUP_DIR}/pre-restore_${SAFETY_TS}.sql.gz"
SAFETY_TMP=$(mktemp "${BACKUP_DIR}/.pre-restore_${SAFETY_TS}.sql.gz.XXXXXX")
SAFETY_OK=0

echo "[RESTORE] Taking safety dump of '${PGDB}' first..."
if docker exec "$CONTAINER_ID" pg_dump -U "$PGUSER" "$PGDB" | gzip -9 > "$SAFETY_TMP" \
   && gzip -t "$SAFETY_TMP" 2>/dev/null; then
  mv "$SAFETY_TMP" "$SAFETY_FILE"
  chmod 600 "$SAFETY_FILE"
  SAFETY_OK=1
  echo "[RESTORE] Safety dump: $SAFETY_FILE"
else
  rm -f "$SAFETY_TMP"
  echo "[RESTORE] WARNING: safety dump failed (new/empty database?)." >&2
  if [ -t 0 ]; then
    printf '[RESTORE] Continue WITHOUT a safety dump? (yes/no): ' >&2
    read -r GO
    [ "$GO" = "yes" ] || { echo "[RESTORE] Aborted."; exit 1; }
  elif [ "${ALLOW_RESTORE_WITHOUT_SAFETY_DUMP:-0}" = "1" ]; then
    echo "[RESTORE] ALLOW_RESTORE_WITHOUT_SAFETY_DUMP=1 — continuing without one." >&2
  else
    echo "[RESTORE] Aborting: refusing to drop '${PGDB}' non-interactively without a safety dump." >&2
    echo "[RESTORE] Set ALLOW_RESTORE_WITHOUT_SAFETY_DUMP=1 to override." >&2
    exit 1
  fi
fi

echo "[RESTORE] Restoring from: $BACKUP_FILE"

docker exec "$CONTAINER_ID" psql -U "$PGUSER" -d postgres -v ON_ERROR_STOP=1 -c \
  "DROP DATABASE IF EXISTS \"${PGDB}\";"
docker exec "$CONTAINER_ID" psql -U "$PGUSER" -d postgres -v ON_ERROR_STOP=1 -c \
  "CREATE DATABASE \"${PGDB}\";"

gunzip -c "$BACKUP_FILE" | docker exec -i "$CONTAINER_ID" psql \
  -U "$PGUSER" -v ON_ERROR_STOP=1 "$PGDB"

echo "[RESTORE] Completed successfully."
if [ "$SAFETY_OK" = "1" ]; then
  echo "[RESTORE] Previous state was saved to ${SAFETY_FILE}"
fi
