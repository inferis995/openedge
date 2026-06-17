#!/usr/bin/env bash
# OpenEdge database backup — usage: ./scripts/backup.sh [retention_days]
set -euo pipefail

RETENTION_DAYS=${1:-30}
BACKUP_DIR="$(cd "$(dirname "$0")/.." && pwd)/backups"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
BACKUP_FILE="${BACKUP_DIR}/openedge_${TIMESTAMP}.sql.gz"

mkdir -p "$BACKUP_DIR"
echo "[BACKUP] Starting at $(date)"

CONTAINER_ID=$(docker ps --filter "name=postgres" --filter "status=running" -q | head -1)
if [ -z "$CONTAINER_ID" ]; then
  echo "[BACKUP] ERROR: No running postgres container found"; exit 1
fi

docker exec "$CONTAINER_ID" pg_dump \
  -U "${POSTGRES_USER:-openedge}" "${POSTGRES_DB:-openedge}" \
  | gzip -9 > "$BACKUP_FILE"

SIZE=$(du -sh "$BACKUP_FILE" | cut -f1)
echo "[BACKUP] Created: $BACKUP_FILE ($SIZE)"

find "$BACKUP_DIR" -name "openedge_*.sql.gz" -mtime "+${RETENTION_DAYS}" -delete
COUNT=$(find "$BACKUP_DIR" -name "openedge_*.sql.gz" | wc -l)
echo "[BACKUP] Done. ${COUNT} backup(s) retained (keeping last ${RETENTION_DAYS} days)."
