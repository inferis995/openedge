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

# Verify the dynamic-security plugin is where mosquitto.conf expects it.
#
# When the path is wrong mosquitto logs "Unable to load plugin" and EXITS; with
# `restart: always` that becomes a silent crash loop in which no per-org ACL is
# ever enforced. Failing here instead, with the path actually found, turns a
# confusing loop into one actionable line.
PLUGIN_PATH=$(grep -E "^plugin[[:space:]]" /mosquitto/config/mosquitto.conf | awk '{print $2}' | head -1)
if [ -n "$PLUGIN_PATH" ] && [ ! -f "$PLUGIN_PATH" ]; then
    echo "[MQTT-INIT] ERROR: dynamic-security plugin not found at $PLUGIN_PATH" >&2
    FOUND=$(find /usr -name 'mosquitto_dynamic_security.so' 2>/dev/null | head -1)
    if [ -n "$FOUND" ]; then
        echo "[MQTT-INIT] It is at: $FOUND — update the 'plugin' line in mosquitto/config/mosquitto.conf" >&2
    else
        echo "[MQTT-INIT] Not present in this image at all; per-client ACLs cannot work." >&2
    fi
    exit 1
fi

exec /docker-entrypoint.sh "$@"
