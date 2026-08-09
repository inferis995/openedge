#!/bin/sh
# Bootstrap Mosquitto Dynamic Security on first start.
# Subsequent starts detect the existing config and skip init.
set -e

DYNSEC_FILE="/mosquitto/config/dynamic-security.json"
ADMIN_USER="${MQTT_ADMIN_USER:-core-api}"
ADMIN_PASS="${MQTT_ADMIN_PASSWORD:-}"
MOSQ_USER="mosquitto"

# Role granting the platform services access to the data topics. See the block
# that creates it for why it has to exist.
PLATFORM_ROLE="openedge-platform"

if [ -z "$ADMIN_PASS" ]; then
    echo "[MQTT-INIT] ERROR: MQTT_ADMIN_PASSWORD env var is required" >&2
    exit 1
fi

if [ ! -f "$DYNSEC_FILE" ]; then
    mosquitto_ctrl dynsec init "$DYNSEC_FILE" "$ADMIN_USER" "$ADMIN_PASS"
    echo "[MQTT-INIT] Dynamic security initialized (admin: $ADMIN_USER)"
    NEEDS_PLATFORM_ROLE=1
else
    echo "[MQTT-INIT] Dynamic security config exists — skipping init"
    NEEDS_PLATFORM_ROLE=0
    grep -q "\"$PLATFORM_ROLE\"" "$DYNSEC_FILE" || NEEDS_PLATFORM_ROLE=1
fi

# Ownership, every start — not just on create.
#
# This script runs as root, so the file dynsec init writes is root-owned; the
# broker then drops to the mosquitto user and cannot read it. mosquitto does NOT
# refuse to start in that case. It logs one line —
#   "Error loading Dynamic security plugin config: File is not readable"
# — and carries on with an EMPTY security config, so every client (core-api, the
# historian, driver-manager, every driver) is refused with "not authorised"
# while the TCP healthcheck stays green and nothing upstream notices.
#
# Applied on every start because a deployment that already booted once carries
# the bad ownership on its volume and would never recover otherwise.
if [ "$(id -u)" = "0" ]; then
    chown "$MOSQ_USER:$MOSQ_USER" "$DYNSEC_FILE"
fi
chmod 600 "$DYNSEC_FILE"
if [ "$(stat -c '%U' "$DYNSEC_FILE")" != "$MOSQ_USER" ]; then
    echo "[MQTT-INIT] ERROR: $DYNSEC_FILE is not owned by $MOSQ_USER — the broker" >&2
    echo "[MQTT-INIT] would run but deny every client. Refusing to start." >&2
    exit 1
fi

# Give the platform account access to the data topics.
#
# `dynsec init` says it plainly: "This client is configured to allow you to
# administer the dynamic security plugin only. It does not have access to
# publish messages to normal topics." Every OpenEdge service authenticates as
# that account, so with the stock ACLs they connect and then have every publish
# and subscribe denied — the entire data path is dead while the broker looks
# healthy.
#
# Per-tenant isolation is unaffected: organizations get their own restricted
# roles (internal/mqtt/dynsec.go, orgRoleACLs). This role is for the trusted
# components, which already hold database credentials, and it is strictly less
# powerful than the dynsec admin rights the same account carries — that account
# can mint any client it likes.
#
# It is done here, before the real listener accepts anything, so no service can
# race ahead of the grant and sit on a subscription that was silently denied.
if [ "$NEEDS_PLATFORM_ROLE" = "1" ]; then
    BOOT_CONF=$(mktemp)
    BOOT_PORT=11883
    cat > "$BOOT_CONF" <<EOF
listener $BOOT_PORT 127.0.0.1
allow_anonymous false
plugin /usr/lib/mosquitto_dynamic_security.so
plugin_opt_config_file $DYNSEC_FILE
EOF
    chmod 644 "$BOOT_CONF"

    /usr/sbin/mosquitto -c "$BOOT_CONF" >/tmp/mosquitto-bootstrap.log 2>&1 &
    BOOT_PID=$!

    ready=0
    i=0
    while [ $i -lt 30 ]; do
        if mosquitto_ctrl -h 127.0.0.1 -p "$BOOT_PORT" -u "$ADMIN_USER" -P "$ADMIN_PASS" \
             dynsec listRoles >/dev/null 2>&1; then
            ready=1
            break
        fi
        i=$((i + 1))
        sleep 1
    done
    if [ "$ready" != "1" ]; then
        echo "[MQTT-INIT] ERROR: the bootstrap broker never accepted the admin account." >&2
        cat /tmp/mosquitto-bootstrap.log >&2
        kill "$BOOT_PID" 2>/dev/null || true
        exit 1
    fi

    ctrl() {
        mosquitto_ctrl -h 127.0.0.1 -p "$BOOT_PORT" -u "$ADMIN_USER" -P "$ADMIN_PASS" "$@"
    }

    ctrl dynsec createRole "$PLATFORM_ROLE" >/dev/null 2>&1 || true
    # '#' does not match topics beginning with $SYS, so the broker's own status
    # tree needs its own grant — the healthcheck reads it to prove that
    # authentication AND authorization work, which is the check that would have
    # caught this whole class of failure.
    for acl in publishClientSend publishClientReceive subscribePattern unsubscribePattern; do
        ctrl dynsec addRoleACL "$PLATFORM_ROLE" "$acl" '#' 1 allow >/dev/null
    done
    ctrl dynsec addRoleACL "$PLATFORM_ROLE" subscribePattern '$SYS/#' 1 allow >/dev/null
    ctrl dynsec addClientRole "$ADMIN_USER" "$PLATFORM_ROLE" 1 >/dev/null

    kill "$BOOT_PID" 2>/dev/null || true
    wait "$BOOT_PID" 2>/dev/null || true
    rm -f "$BOOT_CONF"

    if ! grep -q "\"$PLATFORM_ROLE\"" "$DYNSEC_FILE"; then
        echo "[MQTT-INIT] ERROR: role $PLATFORM_ROLE was not persisted; services would be denied." >&2
        exit 1
    fi
    echo "[MQTT-INIT] Granted $ADMIN_USER the $PLATFORM_ROLE role (data topics + \$SYS)"

    if [ "$(id -u)" = "0" ]; then
        chown "$MOSQ_USER:$MOSQ_USER" "$DYNSEC_FILE"
    fi
    chmod 600 "$DYNSEC_FILE"
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
