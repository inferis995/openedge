#!/usr/bin/env bash
# Install OpenEdge as a systemd service so it starts at boot and survives
# reboots / power cuts. Idempotent — safe to re-run.
#
# Usage (as root or via sudo):
#   sudo ./systemd/install.sh
#   sudo ./systemd/install.sh --uninstall
#
# By default the unit assumes the install lives at /opt/openedge. If your
# clone is elsewhere, pass the path:
#   sudo ./systemd/install.sh /home/operator/openedge

set -euo pipefail

UNIT_NAME="openedge.service"
UNIT_SRC="$(cd "$(dirname "$0")" && pwd)/$UNIT_NAME"
UNIT_DST="/etc/systemd/system/$UNIT_NAME"

if [[ $EUID -ne 0 ]]; then
  echo "must be run as root: sudo $0 $*" >&2
  exit 1
fi

# Uninstall path -------------------------------------------------------------
if [[ "${1:-}" == "--uninstall" ]]; then
  if systemctl is-enabled "$UNIT_NAME" >/dev/null 2>&1; then
    systemctl disable --now "$UNIT_NAME" || true
  fi
  rm -f "$UNIT_DST"
  systemctl daemon-reload
  echo "openedge.service uninstalled."
  exit 0
fi

# Resolve install dir (default /opt/openedge, override with first arg) -------
INSTALL_DIR="${1:-/opt/openedge}"
if [[ ! -d "$INSTALL_DIR" ]]; then
  echo "install dir '$INSTALL_DIR' does not exist. Pass the right path:" >&2
  echo "  sudo $0 /full/path/to/openedge" >&2
  exit 1
fi
if [[ ! -f "$INSTALL_DIR/docker-compose.yml" ]]; then
  echo "no docker-compose.yml in '$INSTALL_DIR' — is this an OpenEdge checkout?" >&2
  exit 1
fi

# Preflight: docker present + .env present -----------------------------------
if ! command -v docker >/dev/null 2>&1; then
  echo "Docker not installed. Install Docker Engine first." >&2
  exit 1
fi
if ! docker compose version >/dev/null 2>&1; then
  echo "Docker Compose plugin missing. Install 'docker compose' v2." >&2
  exit 1
fi
if [[ ! -f "$INSTALL_DIR/.env" ]]; then
  echo "WARNING: $INSTALL_DIR/.env not found. Generate it with: make setup-env" >&2
  echo "Continuing — the service will fail to start until .env exists." >&2
fi

# Install --------------------------------------------------------------------
# Customise WorkingDirectory + EnvironmentFile to the resolved install dir
# so the unit works wherever the user cloned the repo.
sed \
  -e "s|^WorkingDirectory=.*$|WorkingDirectory=${INSTALL_DIR}|" \
  -e "s|^EnvironmentFile=.*$|EnvironmentFile=-${INSTALL_DIR}/.env|" \
  "$UNIT_SRC" > "$UNIT_DST"
chmod 644 "$UNIT_DST"

systemctl daemon-reload
systemctl enable --now "$UNIT_NAME"

echo
echo "Installed openedge.service (install dir: $INSTALL_DIR)."
echo "  status:  sudo systemctl status openedge"
echo "  logs:    sudo journalctl -u openedge -f"
echo "  restart: sudo systemctl restart openedge"
echo "  remove:  sudo $0 --uninstall"
