#!/usr/bin/env bash
# Safe on-prem upgrade: takes a snapshot, pulls the new images, brings
# the stack back up, runs a quick health probe. If anything fails, the
# previous snapshot is the recovery point — restore with:
#   make restore BACKUP=./backups/openedge-pre-update-<ts>.dump
#
# Designed to be re-runnable. Does NOT trigger Docker image pulls of
# images you haven't tagged locally — operators control which image
# refs are deployed via the .env / compose files.
#
# Usage:
#   ./scripts/update-onprem.sh
#   ./scripts/update-onprem.sh --check     # show what would change
#   ./scripts/update-onprem.sh --no-backup # skip the snapshot (NOT recommended)

set -euo pipefail

CHECK=false
DO_BACKUP=true
for arg in "$@"; do
    case "$arg" in
        --check)     CHECK=true ;;
        --no-backup) DO_BACKUP=false ;;
        *) echo "unknown flag: $arg" >&2; exit 2 ;;
    esac
done

step() { printf "\n\033[1;34m▶ %s\033[0m\n" "$*"; }

# 1. Pre-flight: docker present + compose v2 plugin -------------------------
step "1/5 pre-flight"
command -v docker >/dev/null 2>&1 || { echo "docker not in PATH"; exit 1; }
docker compose version >/dev/null 2>&1 || { echo "docker compose v2 plugin missing"; exit 1; }

# 2. Snapshot (skip with --no-backup) ---------------------------------------
if $DO_BACKUP; then
    step "2/5 snapshot (pg_dump)"
    if ! docker compose --profile backup ps backup >/dev/null 2>&1; then
        echo "  backup service is not running. Enable the 'backup' profile in .env" >&2
        echo "  or start it ad-hoc:"
        echo "    docker compose --profile backup run --rm -e BACKUP_RUN_NOW=true backup" >&2
        exit 1
    fi
    PRE_BACKUP="openedge-pre-update-$(date -u +%Y%m%dT%H%M%SZ).dump"
    docker compose --profile backup run --rm \
      -e BACKUP_RUN_NOW=true \
      -e BACKUP_DIR=/backups \
      backup
    # The backup container names files openedge-<ts>.dump; tag the newest
    # one as our recovery point.
    LATEST=$(ls -t ./backups/openedge-*.dump* 2>/dev/null | head -n1 || true)
    if [[ -z "$LATEST" ]]; then
        echo "WARN: snapshot completed but no file found in ./backups" >&2
    else
        cp -v "$LATEST" "./backups/$PRE_BACKUP"
        echo "  recovery point: ./backups/$PRE_BACKUP"
    fi
else
    step "2/5 snapshot SKIPPED (--no-backup)"
fi

# 3. Pull new images --------------------------------------------------------
step "3/5 docker compose pull"
if $CHECK; then
    echo "(check mode) skipping actual pull; running --dry-run if available"
    docker compose pull --dry-run 2>/dev/null || echo "  dry-run not supported on this docker compose version"
else
    docker compose pull
fi

if $CHECK; then
    echo
    echo "Check mode: no changes applied. Re-run without --check to upgrade."
    exit 0
fi

# 4. Bring the stack back up -----------------------------------------------
step "4/5 docker compose up -d --remove-orphans"
docker compose --profile local --profile backup up -d --remove-orphans

# 5. Health probe — give services up to 90s to settle ----------------------
step "5/5 health probe"
DEADLINE=$(( $(date +%s) + 90 ))
HEALTH_URL="${HEALTH_URL:-http://localhost:8081/health}"
OK=false
while (( $(date +%s) < DEADLINE )); do
    if curl -fsS -m 2 "$HEALTH_URL" >/dev/null 2>&1; then
        OK=true
        break
    fi
    sleep 3
done

if $OK; then
    echo
    echo "✅ Upgrade complete. Health endpoint at $HEALTH_URL is responding."
    if $DO_BACKUP; then
        echo "   Recovery point kept at ./backups/$PRE_BACKUP"
        echo "   Delete it once you're sure the upgrade is good."
    fi
else
    echo
    echo "❌ Health probe FAILED after 90s. The new stack is not responding."
    echo "   Inspect: docker compose logs --tail 200 core-api"
    if $DO_BACKUP; then
        echo "   Roll back with:"
        echo "     make restore BACKUP=./backups/$PRE_BACKUP"
    fi
    exit 1
fi
