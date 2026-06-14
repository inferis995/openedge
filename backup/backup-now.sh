#!/usr/bin/env bash
# Take ONE backup snapshot. Called by cron OR manually.
#
#  1. Disk-space pre-flight check (abort if < BACKUP_MIN_FREE_MB free)
#  2. pg_dump --custom (compressed, restorable with pg_restore)
#  3. Optional age encryption if BACKUP_AGE_RECIPIENT is set
#  4. SHA-256 sidecar file (<filename>.sha256) for integrity verification
#  5. Optional S3/MinIO upload (requires aws-cli + AWS_S3_BUCKET)
#  6. Retention pruning (BACKUP_RETENTION_DAYS, default 30)
#  7. MQTT heartbeat on sys/health/backup with status, size, sha256
#
# Required env: PGHOST  PGUSER  PGPASSWORD  PGDATABASE
# Optional env:
#   BACKUP_DIR              default /backups
#   BACKUP_RETENTION_DAYS   default 30
#   BACKUP_MIN_FREE_MB      default 500
#   BACKUP_AGE_RECIPIENT    age public key; if set, encrypts dump
#   AWS_S3_BUCKET           if set, uploads to S3/MinIO after local save
#   AWS_S3_ENDPOINT         custom endpoint for MinIO (e.g. http://minio:9000)
#   AWS_S3_REGION           default us-east-1
#   AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY
#   MQTT_HOST / MQTT_PORT / MQTT_TOPIC

set -euo pipefail

: "${PGHOST:?PGHOST is required}"
: "${PGUSER:?PGUSER is required}"
: "${PGPASSWORD:?PGPASSWORD is required}"
: "${PGDATABASE:?PGDATABASE is required}"

BACKUP_DIR="${BACKUP_DIR:-/backups}"
BACKUP_RETENTION_DAYS="${BACKUP_RETENTION_DAYS:-30}"
BACKUP_MIN_FREE_MB="${BACKUP_MIN_FREE_MB:-500}"
MQTT_HOST="${MQTT_HOST:-mosquitto}"
MQTT_PORT="${MQTT_PORT:-1883}"
MQTT_TOPIC="${MQTT_TOPIC:-sys/health/backup}"

mkdir -p "$BACKUP_DIR"

TS=$(date -u +%Y%m%dT%H%M%SZ)
TMP="$BACKUP_DIR/.openedge-$TS.dump.partial"
FINAL="$BACKUP_DIR/openedge-$TS.dump"

publish_health() {
    local status="$1" message="$2" sha256="${3:-}" size_kb="${4:-0}"
    if command -v mosquitto_pub >/dev/null 2>&1; then
        mosquitto_pub -h "$MQTT_HOST" -p "$MQTT_PORT" -t "$MQTT_TOPIC" -r \
            -m "{\"status\":\"$status\",\"timestamp\":\"$TS\",\"message\":\"$message\",\"sha256\":\"$sha256\",\"size_kb\":$size_kb}" \
            2>/dev/null || true
    fi
}

cleanup_failed() {
    [[ -f "$TMP" ]] && rm -f "$TMP"
    publish_health "error" "$1"
    exit 1
}

# ── 1. Disk-space pre-flight ──────────────────────────────────────────────────
FREE_MB=$(df -m "$BACKUP_DIR" | awk 'NR==2{print $4}')
if (( FREE_MB < BACKUP_MIN_FREE_MB )); then
    echo "[backup] ERROR: only ${FREE_MB}MB free in $BACKUP_DIR (min ${BACKUP_MIN_FREE_MB}MB required)"
    cleanup_failed "insufficient disk space (${FREE_MB}MB free)"
fi
echo "[backup] disk pre-flight OK: ${FREE_MB}MB free"

# ── 2. pg_dump ────────────────────────────────────────────────────────────────
echo "[backup] pg_dump $PGDATABASE → $TMP"
if ! pg_dump --no-owner --no-privileges --format=custom --file="$TMP" "$PGDATABASE"; then
    cleanup_failed "pg_dump failed"
fi

# ── 3. Optional encryption ────────────────────────────────────────────────────
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
echo "[backup] OK — $FINAL (${SIZE_KB} KB)"

# ── 4. SHA-256 checksum ───────────────────────────────────────────────────────
SHA256=$(sha256sum "$FINAL" | awk '{print $1}')
echo "$SHA256  $FINAL" > "${FINAL}.sha256"
echo "[backup] SHA-256: $SHA256"

# ── 5. Optional S3/MinIO upload ───────────────────────────────────────────────
if [[ -n "${AWS_S3_BUCKET:-}" ]]; then
    S3_KEY="$(basename "$FINAL")"
    S3_DEST="s3://${AWS_S3_BUCKET}/${S3_KEY}"
    echo "[backup] uploading → $S3_DEST"
    S3_ARGS=()
    [[ -n "${AWS_S3_ENDPOINT:-}" ]] && S3_ARGS+=(--endpoint-url "$AWS_S3_ENDPOINT")
    S3_ARGS+=(--region "${AWS_S3_REGION:-us-east-1}")
    if aws s3 cp "$FINAL" "$S3_DEST" "${S3_ARGS[@]}" 2>&1; then
        echo "[backup] S3 upload OK"
        aws s3 cp "${FINAL}.sha256" "${S3_DEST}.sha256" "${S3_ARGS[@]}" 2>/dev/null || true
    else
        echo "[backup] WARNING: S3 upload failed — backup saved locally only"
    fi
fi

# ── 6. Retention pruning ──────────────────────────────────────────────────────
echo "[backup] pruning backups older than ${BACKUP_RETENTION_DAYS}d"
find "$BACKUP_DIR" -maxdepth 1 -type f \
    \( -name 'openedge-*.dump' -o -name 'openedge-*.dump.age' \
       -o -name 'openedge-*.dump.sha256' -o -name 'openedge-*.dump.age.sha256' \) \
    -mtime "+${BACKUP_RETENTION_DAYS}" -print -delete || true

# ── 7. MQTT heartbeat ─────────────────────────────────────────────────────────
publish_health "ok" "backup complete (${SIZE_KB} KB)" "$SHA256" "$SIZE_KB"
