#!/usr/bin/env bash
# Take ONE backup snapshot. Designed to be called by cron OR manually.
#
# What it does:
#   1. pg_dump --custom (compressed, restorable with pg_restore)
#   2. encrypts with age if BACKUP_AGE_RECIPIENT is set (recommended for
#      backups that may leave the host on a USB key / external NAS)
#   3. writes to /backups/openedge-<UTC-timestamp>.dump[.age]
#   4. prunes backups older than BACKUP_RETENTION_DAYS (default 30)
#   5. publishes a heartbeat to MQTT topic sys/health/backup so the
#      alarm engine can fire when nightly backups stop happening
#
# Required env:
#   PGHOST, PGUSER, PGPASSWORD, PGDATABASE   - libpq vars; pg_dump uses them
# Optional env:
#   BACKUP_DIR                  default /backups
#   BACKUP_RETENTION_DAYS       default 30
#   BACKUP_AGE_RECIPIENT        age public key. If set, encrypts. If empty,
#                                writes plaintext (acceptable when /backups
#                                never leaves the host's encrypted disk).
#   MQTT_HOST, MQTT_PORT        default mosquitto:1883
#   MQTT_TOPIC                  default sys/health/backup

set -euo pipefail

: "${PGHOST:?PGHOST is required}"
: "${PGUSER:?PGUSER is required}"
: "${PGPASSWORD:?PGPASSWORD is required}"
: "${PGDATABASE:?PGDATABASE is required}"

BACKUP_DIR="${BACKUP_DIR:-/backups}"
BACKUP_RETENTION_DAYS="${BACKUP_RETENTION_DAYS:-30}"
MQTT_HOST="${MQTT_HOST:-mosquitto}"
MQTT_PORT="${MQTT_PORT:-1883}"
MQTT_TOPIC="${MQTT_TOPIC:-sys/health/backup}"

mkdir -p "$BACKUP_DIR"

TS=$(date -u +%Y%m%dT%H%M%SZ)
TMP="$BACKUP_DIR/.openedge-$TS.dump.partial"
FINAL="$BACKUP_DIR/openedge-$TS.dump"

publish_health() {
    local status="$1" message="$2"
    # Best-effort MQTT publish — never fail the backup just because the
    # broker is momentarily unavailable. Mosquitto's pub client is in
    # alpine's mosquitto-clients pkg; we install it only if MQTT is needed.
    if command -v mosquitto_pub >/dev/null 2>&1; then
        mosquitto_pub -h "$MQTT_HOST" -p "$MQTT_PORT" -t "$MQTT_TOPIC" -r \
            -m "{\"status\":\"$status\",\"timestamp\":\"$TS\",\"message\":\"$message\"}" 2>/dev/null || true
    fi
}

cleanup_failed() {
    if [[ -f "$TMP" ]]; then rm -f "$TMP"; fi
    publish_health "error" "$1"
    exit 1
}

# 1. Dump. --format=custom is the only format pg_restore can do per-table
# or selective restores from; also it compresses by default.
echo "[backup] pg_dump $PGDATABASE → $TMP"
if ! pg_dump --no-owner --no-privileges --format=custom \
        --file="$TMP" "$PGDATABASE"; then
    cleanup_failed "pg_dump failed"
fi

# 2. Optional encryption.
if [[ -n "${BACKUP_AGE_RECIPIENT:-}" ]]; then
    echo "[backup] encrypting with age (recipient: ${BACKUP_AGE_RECIPIENT:0:16}…)"
    if ! age --encrypt -r "$BACKUP_AGE_RECIPIENT" -o "${FINAL}.age" "$TMP"; then
        cleanup_failed "age encryption failed"
    fi
    rm -f "$TMP"
    FINAL="${FINAL}.age"
else
    mv "$TMP" "$FINAL"
fi

SIZE_KB=$(du -k "$FINAL" | awk '{print $1}')
echo "[backup] OK — $FINAL ($SIZE_KB KB)"

# 3. Retention — delete dumps older than the configured cutoff.
echo "[backup] pruning backups older than ${BACKUP_RETENTION_DAYS}d"
find "$BACKUP_DIR" -maxdepth 1 -type f \
    \( -name 'openedge-*.dump' -o -name 'openedge-*.dump.age' \) \
    -mtime "+${BACKUP_RETENTION_DAYS}" -print -delete || true

publish_health "ok" "backup complete (${SIZE_KB} KB)"
