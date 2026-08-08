#!/usr/bin/env bash
# OpenEdge database backup — usage: ./scripts/backup.sh [retention_days]
#
# Guarantees:
#   * the .gz that lands in backups/ is only ever a dump that pg_dump finished
#     successfully AND that gzip -t accepts AND that is not suspiciously small;
#   * old backups are rotated ONLY after a new good one exists — a failed run
#     never destroys history;
#   * the postgres container is matched exactly, so sibling containers such as
#     openedge-postgres-exporter can never be picked instead.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

# Pick up POSTGRES_USER / POSTGRES_DB / POSTGRES_CONTAINER from .env when present,
# so the defaults below match what docker-compose.yml actually started.
if [ -f "${REPO_ROOT}/.env" ]; then
  # shellcheck disable=SC1091
  set -a; . "${REPO_ROOT}/.env"; set +a
fi

RETENTION_DAYS=${1:-${BACKUP_RETENTION_DAYS:-30}}
BACKUP_DIR="${REPO_ROOT}/backups"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
BACKUP_FILE="${BACKUP_DIR}/openedge_${TIMESTAMP}.sql.gz"

# Defaults must match docker-compose.yml (POSTGRES_USER=industrial_user,
# POSTGRES_DB=industrial_edge). The previous "openedge" defaults meant every
# unconfigured run authenticated as a role that does not exist.
PGUSER="${POSTGRES_USER:-industrial_user}"
PGDB="${POSTGRES_DB:-industrial_edge}"
CONTAINER_NAME="${POSTGRES_CONTAINER:-openedge-postgres}"

# A dump of even an empty OpenEdge schema is several KB gzipped; anything under
# this is a truncated/aborted dump masquerading as a valid archive.
MIN_BACKUP_BYTES="${MIN_BACKUP_BYTES:-1024}"

mkdir -p "$BACKUP_DIR"
echo "[BACKUP] Starting at $(date)"

# Exact match. Docker's --filter name= is a substring regex, so "name=postgres"
# also matched openedge-postgres-exporter (and head -1 then picked whichever the
# daemon listed first). Anchoring on the optional leading slash pins it.
CONTAINER_ID=$(docker ps --filter "name=^/?${CONTAINER_NAME}$" --filter "status=running" -q)
if [ -z "$CONTAINER_ID" ]; then
  echo "[BACKUP] ERROR: no running container named '${CONTAINER_NAME}'" >&2
  echo "[BACKUP] Set POSTGRES_CONTAINER if your deployment renames it." >&2
  exit 1
fi
if [ "$(printf '%s\n' "$CONTAINER_ID" | wc -l)" -gt 1 ]; then
  echo "[BACKUP] ERROR: '${CONTAINER_NAME}' matched more than one running container" >&2
  exit 1
fi

# Dump to a temp file next to the target (same filesystem => atomic mv) and only
# publish it once we know it is good. Nothing writes to $BACKUP_FILE until then,
# so a crash mid-dump cannot leave a valid-looking empty archive behind.
TMP_FILE=$(mktemp "${BACKUP_DIR}/.openedge_${TIMESTAMP}.sql.gz.XXXXXX")
cleanup() { rm -f "$TMP_FILE"; }
trap cleanup EXIT

if ! docker exec "$CONTAINER_ID" pg_dump -U "$PGUSER" "$PGDB" | gzip -9 > "$TMP_FILE"; then
  echo "[BACKUP] ERROR: pg_dump failed — no backup written, nothing rotated." >&2
  exit 1
fi

if ! gzip -t "$TMP_FILE" 2>/dev/null; then
  echo "[BACKUP] ERROR: produced archive is not valid gzip — discarding." >&2
  exit 1
fi

ACTUAL_BYTES=$(wc -c < "$TMP_FILE" | tr -d ' ')
if [ "$ACTUAL_BYTES" -lt "$MIN_BACKUP_BYTES" ]; then
  echo "[BACKUP] ERROR: dump is only ${ACTUAL_BYTES} bytes (< ${MIN_BACKUP_BYTES}) — treating as failed." >&2
  echo "[BACKUP] Nothing was written and nothing was rotated." >&2
  exit 1
fi

mv "$TMP_FILE" "$BACKUP_FILE"
trap - EXIT
chmod 600 "$BACKUP_FILE"

SIZE=$(du -h "$BACKUP_FILE" | cut -f1)
echo "[BACKUP] Created: $BACKUP_FILE ($SIZE)"

# Rotation is reached only on the success path above, so a broken run can never
# prune the last good backups.
find "$BACKUP_DIR" -maxdepth 1 -name "openedge_*.sql.gz" -mtime "+${RETENTION_DAYS}" -delete
COUNT=$(find "$BACKUP_DIR" -maxdepth 1 -name "openedge_*.sql.gz" | wc -l | tr -d ' ')
echo "[BACKUP] Done. ${COUNT} backup(s) retained (keeping last ${RETENTION_DAYS} days)."
