#!/usr/bin/env bash
# Container entrypoint:
#   - if BACKUP_RUN_NOW=true, runs once and exits (useful for ad-hoc backups
#     via `docker compose run --rm backup`)
#   - otherwise installs the cron schedule and stays alive tailing /var/log/cron
#
# Cron expression is BACKUP_SCHEDULE (default "0 3 * * *" → daily at 03:00 UTC).

set -euo pipefail

# One-shot mode: run a backup right now and exit.
if [[ "${BACKUP_RUN_NOW:-false}" == "true" ]]; then
    exec backup-now
fi

SCHEDULE="${BACKUP_SCHEDULE:-0 3 * * *}"

# Build a crontab on the fly so env vars are available to the cron job.
# Alpine's cron strips most of the environment, so we re-export here.
{
    echo "PGHOST=${PGHOST:-}"
    echo "PGPORT=${PGPORT:-5432}"
    echo "PGUSER=${PGUSER:-}"
    echo "PGPASSWORD=${PGPASSWORD:-}"
    echo "PGDATABASE=${PGDATABASE:-}"
    echo "BACKUP_DIR=${BACKUP_DIR:-/backups}"
    echo "BACKUP_RETENTION_DAYS=${BACKUP_RETENTION_DAYS:-30}"
    echo "BACKUP_AGE_RECIPIENT=${BACKUP_AGE_RECIPIENT:-}"
    echo "MQTT_HOST=${MQTT_HOST:-mosquitto}"
    echo "MQTT_PORT=${MQTT_PORT:-1883}"
    echo "MQTT_TOPIC=${MQTT_TOPIC:-sys/health/backup}"
    echo "${SCHEDULE} /usr/local/bin/backup-now >> /var/log/openedge-backup.log 2>&1"
} > /etc/crontabs/root

# Make sure the log file exists so `tail -f` doesn't fail.
touch /var/log/openedge-backup.log

echo "[backup] cron installed: ${SCHEDULE}"
echo "[backup] backups go to: ${BACKUP_DIR:-/backups}"
echo "[backup] retention: ${BACKUP_RETENTION_DAYS:-30} days"
if [[ -n "${BACKUP_AGE_RECIPIENT:-}" ]]; then
    echo "[backup] encryption: age (recipient configured)"
else
    echo "[backup] encryption: NONE (set BACKUP_AGE_RECIPIENT to enable)"
fi

# Start crond in foreground so the container stays alive and SIGTERM works.
crond -f -l 8 &
CROND=$!

# Tail the backup log so `docker logs` shows every run.
tail -F /var/log/openedge-backup.log &
TAIL=$!

# Forward SIGTERM/SIGINT cleanly.
trap 'kill -TERM "$CROND" "$TAIL" 2>/dev/null || true' TERM INT
wait "$CROND"
