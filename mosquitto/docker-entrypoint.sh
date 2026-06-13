#!/bin/sh
# Bootstrap Mosquitto Dynamic Security on first start.
# Subsequent starts detect the existing config and skip init.
set -e

DYNSEC_FILE="/mosquitto/config/dynamic-security.json"
ADMIN_USER="${MQTT_ADMIN_USER:-core-api}"
ADMIN_PASS="${MQTT_ADMIN_PASSWORD:-}"

if [ -z "$ADMIN_PASS" ]; then
    echo "[MQTT-INIT] ERROR: MQTT_ADMIN_PASSWORD env var is required" >&2
    exit 1
fi

if [ ! -f "$DYNSEC_FILE" ]; then
    mosquitto_ctrl dynsec init "$DYNSEC_FILE" "$ADMIN_USER" "$ADMIN_PASS"
    chmod 600 "$DYNSEC_FILE"
    echo "[MQTT-INIT] Dynamic security initialized (admin: $ADMIN_USER)"
else
    echo "[MQTT-INIT] Dynamic security config exists — skipping init"
fi

exec /docker-entrypoint.sh "$@"
