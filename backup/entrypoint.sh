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

# Pull settings from the database when available (the UI writes here),
# falling back to the env values otherwise. Operator edits via the
# Backup panel take effect at the next container restart.
#
# Why not poll continuously: cron is started ONCE per container; changing
# the schedule mid-run would need a `crond` reload. Restart-on-edit is
# simple and matches operator mental model ("save, then restart backup").
db_setting() {
    local key="$1" fallback="$2"
    if [[ -n "${PGHOST:-}" && -n "${PGUSER:-}" && -n "${PGDATABASE:-}" ]]; then
        # 2s timeout — if Postgres is slow at boot we just fall back to env.
        local v
        v=$(PGPASSWORD="${PGPASSWORD:-}" psql -h "$PGHOST" -p "${PGPORT:-5432}" \
            -U "$PGUSER" -d "$PGDATABASE" -tA \
            -c "SELECT value FROM global_settings WHERE key='${key}' LIMIT 1" \
            2>/dev/null || true)
        if [[ -n "$v" ]]; then echo "$v"; return; fi
    fi
    echo "$fallback"
}

# Resolve the operative values: DB > env > hard-coded default.
ENABLED=$(db_setting backup_enabled "${BACKUP_ENABLED:-true}")
SCHEDULE=$(db_setting backup_schedule "${BACKUP_SCHEDULE:-0 3 * * *}")
RETENTION=$(db_setting backup_retention_days "${BACKUP_RETENTION_DAYS:-30}")
AGE_RECIP=$(db_setting backup_age_recipient "${BACKUP_AGE_RECIPIENT:-}")
# Re-export so the cron environment + child scripts see the resolved values.
export BACKUP_RETENTION_DAYS="$RETENTION"
export BACKUP_AGE_RECIPIENT="$AGE_RECIP"

if [[ "$ENABLED" != "true" ]]; then
    echo "[backup] DISABLED (backup_enabled=$ENABLED). Container stays idle."
    # Sleep forever so the compose service stays UP and `docker compose
    # run --rm backup` (one-shot mode) still works for ad-hoc backups.
    exec tail -f /dev/null
fi

# Build a crontab on the fly so env vars are available to the cron job.
# Alpine's cron strips most of the environment, so we re-export here.
{
    echo "PGHOST=${PGHOST:-}"
    echo "PGPORT=${PGPORT:-5432}"
    echo "PGUSER=${PGUSER:-}"
    echo "PGPASSWORD=${PGPASSWORD:-}"
    echo "PGDATABASE=${PGDATABASE:-}"
    echo "BACKUP_DIR=${BACKUP_DIR:-/backups}"
    echo "BACKUP_RETENTION_DAYS=${BACKUP_RETENTION_DAYS}"
    echo "BACKUP_AGE_RECIPIENT=${BACKUP_AGE_RECIPIENT}"
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
