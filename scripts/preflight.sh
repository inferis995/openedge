#!/usr/bin/env bash
# preflight.sh — refuse to start a production stack that is not ready to be one.
#
# Why this exists: every value below has a default, a placeholder, or a warning
# somewhere, and none of them stop a deployment. ENCRYPTION_KEY defaults to
# empty and core-api logs a line about storing tenant credentials in cleartext,
# then serves traffic. A cron expression with unquoted spaces aborts backup.sh
# before it dumps anything, silently, forever. GRAFANA_ADMIN_PASSWORD only has
# to be non-empty, so the placeholder from this repository satisfies it — which
# matters only once the monitoring profile is on and Grafana is published, and
# is why that check waits for --with-monitoring.
#
# Each of those is survivable in development and none of them should reach a
# customer. This script turns them into a failed start, which is the only
# failure mode an operator actually sees.
#
#   bash scripts/preflight.sh [path-to-env]   # default: .env
#
# Exit 0 = safe to start. Exit 1 = a list of what to fix.

set -uo pipefail

ENV_FILE="${1:-.env}"
# Grafana only exists when the monitoring profile is enabled. Demanding its
# password from every deployment would be a check that cries wolf, and those
# get ignored — including on the deployment where it does matter.
WITH_MONITORING=0
for arg in "$@"; do
    [ "$arg" = "--with-monitoring" ] && WITH_MONITORING=1
done
PROBLEMS=()
NOTES=()

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BOLD='\033[1m'; NC='\033[0m'

fail() { PROBLEMS+=("$1"); }
note() { NOTES+=("$1"); }

if [ ! -f "$ENV_FILE" ]; then
    echo -e "${RED}${BOLD}✗ $ENV_FILE does not exist.${NC}"
    echo "  Create it first:  cp .env.cloud.example .env  (then fill in every CHANGE_ME)"
    exit 1
fi

# Read the file as a shell would, because that is what backup.sh does. A value
# that breaks here breaks there.
if ! SOURCE_OUT=$(set -a; . "$ENV_FILE" >/dev/null 2>&1; set +a; echo ok); then
    fail "$ENV_FILE cannot be sourced by a shell. A value containing spaces needs quotes — \
scripts/backup.sh sources this file under 'set -e' and will abort before dumping anything."
fi
[ "${SOURCE_OUT:-}" = "ok" ] || true

# Read values without executing the file, so a broken line does not hide the rest.
get() { sed -n "s/^$1=//p" "$ENV_FILE" | tail -1 | sed 's/^"\(.*\)"$/\1/; s/^'\''\(.*\)'\''$/\1/'; }

# ── Secrets that must not be placeholders ────────────────────────────────────
#
# The check is on the VALUE, not on whether the key exists: a key present with
# the repository's placeholder is worse than a missing one, because every other
# check treats it as configured.
REQUIRED=(JWT_SECRET POSTGRES_PASSWORD MQTT_ADMIN_PASSWORD ENCRYPTION_KEY
          OPENEDGE_INITIAL_ADMIN_PASSWORD)
[ "$WITH_MONITORING" = "1" ] && REQUIRED+=(GRAFANA_ADMIN_PASSWORD)

for var in "${REQUIRED[@]}"; do
    value=$(get "$var")
    if [ -z "$value" ]; then
        fail "$var is empty or missing."
    elif [[ "$value" == *CHANGE_ME* ]]; then
        fail "$var still holds the placeholder from the repository. Anyone who has read \
this project knows it."
    fi
done

# ── Lengths that are not advisory ────────────────────────────────────────────
JWT=$(get JWT_SECRET)
if [ -n "$JWT" ] && [[ "$JWT" != *CHANGE_ME* ]] && [ ${#JWT} -lt 32 ]; then
    fail "JWT_SECRET is ${#JWT} characters; core-api requires at least 32. Generate: openssl rand -hex 32"
fi

ENC=$(get ENCRYPTION_KEY)
if [ -n "$ENC" ] && [[ "$ENC" != *CHANGE_ME* ]] && [ ${#ENC} -ne 32 ]; then
    fail "ENCRYPTION_KEY is ${#ENC} bytes; AES-256 needs EXACTLY 32. At any other length the \
key is ignored and credentials are stored in cleartext. Generate: openssl rand -hex 16"
fi

ADMIN=$(get OPENEDGE_INITIAL_ADMIN_PASSWORD)
if [ -n "$ADMIN" ] && [[ "$ADMIN" != *CHANGE_ME* ]] && [ ${#ADMIN} -lt 12 ]; then
    fail "OPENEDGE_INITIAL_ADMIN_PASSWORD is ${#ADMIN} characters. That account is a global \
admin: it reads every tenant's data and writes setpoints on every PLC."
fi

# ── The domain, which everything public is derived from ──────────────────────
HOST=$(get PUBLIC_HOST)
case "$HOST" in
    ""|app.yourcompany.com|app.yourdomain.com|openedge.local|localhost)
        fail "PUBLIC_HOST is '$HOST' — set it to the domain whose DNS points at this server. \
It becomes the TLS certificate, the CORS origin, the MQTT endpoint published to every edge, \
and the OAuth issuer." ;;
esac

MAIL=$(get ACME_EMAIL)
case "$MAIL" in
    ""|ops@yourcompany.com) fail "ACME_EMAIL is '$MAIL' — Let's Encrypt requires a real address." ;;
esac

ORIGINS=$(get ALLOWED_ORIGINS)
# .env.cloud.example ships ALLOWED_ORIGINS=https://${PUBLIC_HOST}, and Compose
# expands that when it reads the file. Comparing the raw text against the host
# therefore failed on a correctly filled-in .env — the one case this check is
# supposed to wave through — and told the operator their CORS was broken when
# it was not. Expand the reference the same way Compose does before comparing.
ORIGINS=${ORIGINS//\$\{PUBLIC_HOST\}/$HOST}
ORIGINS=${ORIGINS//\$PUBLIC_HOST/$HOST}
if [ -n "$HOST" ] && [ -n "$ORIGINS" ] && [[ "$ORIGINS" != *"$HOST"* ]]; then
    fail "ALLOWED_ORIGINS ($ORIGINS) does not mention PUBLIC_HOST ($HOST). The browser will \
be refused by CORS on every API call."
fi

# ── The tool that will run this, not just the file it will read ──────────────
#
# docker-compose.vps.yml removes the host port bindings with `ports: !override
# []`. It has to: a plain `ports: []` merges with the base list instead of
# replacing it, which is how the web UI and the plaintext MQTT port spent this
# project's whole history published on public VPS addresses while the file said
# they were not. The tag needs Compose v2.24 (January 2024) or later. Older
# versions fail on the unknown tag rather than quietly republishing the ports —
# but a YAML parse error is a poor way to learn this, so say it here.
if command -v docker >/dev/null 2>&1; then
    CV=$(docker compose version --short 2>/dev/null | sed 's/^v//')
    CV_MAJOR=${CV%%.*}
    CV_REST=${CV#*.}
    CV_MINOR=${CV_REST%%.*}
    if [ -n "${CV_MAJOR:-}" ] && [ "$CV_MAJOR" -eq 2 ] 2>/dev/null && [ "${CV_MINOR:-0}" -lt 24 ] 2>/dev/null; then
        fail "Docker Compose is $CV; the cloud overlay needs 2.24 or later for the \
\`!override\` tag that takes the internal services off the host. Upgrade the plugin: \
https://docs.docker.com/compose/install/linux/"
    elif [ -n "${CV_MAJOR:-}" ] && [ "$CV_MAJOR" -lt 2 ] 2>/dev/null; then
        fail "Docker Compose is $CV. This is the standalone v1 (docker-compose), which does \
not support overlay files the way this deployment needs. Install the v2 plugin: \
https://docs.docker.com/compose/install/linux/"
    fi
fi

# ── Things that are wrong but not fatal ──────────────────────────────────────
if [ "$(get SWAGGER_ENABLED)" != "false" ]; then
    note "SWAGGER_ENABLED is not 'false' — the API documentation will be public."
fi

SCHEDULE=$(sed -n 's/^BACKUP_SCHEDULE=//p' "$ENV_FILE" | tail -1)
if [ -n "$SCHEDULE" ] && [[ "$SCHEDULE" != \"* ]] && [[ "$SCHEDULE" != \'* ]] && [[ "$SCHEDULE" == *" "* ]]; then
    fail "BACKUP_SCHEDULE is not quoted. Sourced by backup.sh under 'set -e', \
'0 3 * * *' reads as the command '0' with arguments — the script exits before pg_dump runs, \
and no backup is ever written."
fi

if [ "$WITH_MONITORING" = "0" ] && [[ "$(get GRAFANA_ADMIN_PASSWORD)" == *CHANGE_ME* ]]; then
    note "GRAFANA_ADMIN_PASSWORD is still the placeholder. Harmless while the monitoring \
profile is off, and a public Grafana with a published password the moment you turn it on."
fi

if [ "$WITH_MONITORING" = "1" ]; then
    note "Grafana will be published at grafana.$(get PUBLIC_HOST) — it needs its own DNS A record."
fi

if [ -z "$(get REDIS_PASSWORD)" ]; then
    note "REDIS_PASSWORD is empty — Redis accepts any client on the Docker network."
fi

# ── Report ───────────────────────────────────────────────────────────────────
echo ""
if [ ${#PROBLEMS[@]} -gt 0 ]; then
    echo -e "${RED}${BOLD}✗ Not ready for production — ${#PROBLEMS[@]} problem(s) in $ENV_FILE${NC}"
    echo ""
    for p in "${PROBLEMS[@]}"; do
        echo -e "  ${RED}•${NC} $p"
        echo ""
    done
    echo -e "  Generate what is missing:  ${BOLD}make setup-env${NC}"
    echo ""
    exit 1
fi

echo -e "${GREEN}${BOLD}✓ $ENV_FILE is ready for production${NC}"
for n in "${NOTES[@]}"; do
    echo -e "  ${YELLOW}note:${NC} $n"
done
echo ""
exit 0
