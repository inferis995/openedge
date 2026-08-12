#!/usr/bin/env bash
# preflight.sh — refuse to start a production stack that is not ready to be one.
#
# Why this exists: every value below has a default, a placeholder, or a warning
# somewhere, and none of them stop a deployment. ENCRYPTION_KEY defaults to
# empty and core-api logs a line about storing tenant credentials in cleartext,
# then serves traffic. GRAFANA_ADMIN_PASSWORD only has to be non-empty, so the
# placeholder from this repository satisfies it — on a Grafana published to the
# internet. A cron expression with unquoted spaces aborts backup.sh before it
# dumps anything, silently, forever.
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
for var in JWT_SECRET POSTGRES_PASSWORD MQTT_ADMIN_PASSWORD ENCRYPTION_KEY \
           GRAFANA_ADMIN_PASSWORD OPENEDGE_INITIAL_ADMIN_PASSWORD; do
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
if [ -n "$HOST" ] && [ -n "$ORIGINS" ] && [[ "$ORIGINS" != *"$HOST"* ]]; then
    fail "ALLOWED_ORIGINS ($ORIGINS) does not mention PUBLIC_HOST ($HOST). The browser will \
be refused by CORS on every API call."
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
