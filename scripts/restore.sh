#!/usr/bin/env bash
# OpenEdge database restore — usage: ./scripts/restore.sh <backup_file.sql.gz>
set -euo pipefail

BACKUP_FILE="${1:-}"
if [ -z "$BACKUP_FILE" ] || [ ! -f "$BACKUP_FILE" ]; then
  echo "Usage: $0 <backup_file.sql.gz>"
  echo "Available backups:"; ls -lh "$(dirname "$0")/../backups/"*.sql.gz 2>/dev/null || echo "  (none)"
  exit 1
fi

CONTAINER_ID=$(docker ps --filter "name=postgres" --filter "status=running" -q | head -1)
[ -z "$CONTAINER_ID" ] && { echo "ERROR: No running postgres container"; exit 1; }

echo "[RESTORE] Restoring from: $BACKUP_FILE"
echo "[RESTORE] WARNING: This overwrites the current database. Ctrl+C within 5s to cancel."
sleep 5

docker exec "$CONTAINER_ID" psql -U "${POSTGRES_USER:-openedge}" -c \
  "DROP DATABASE IF EXISTS ${POSTGRES_DB:-openedge}; CREATE DATABASE ${POSTGRES_DB:-openedge};"

gunzip -c "$BACKUP_FILE" | docker exec -i "$CONTAINER_ID" psql \
  -U "${POSTGRES_USER:-openedge}" "${POSTGRES_DB:-openedge}"

echo "[RESTORE] Completed successfully."
