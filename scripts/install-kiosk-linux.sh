#!/usr/bin/env bash
# Configure an HMI / kiosk-style display: Chromium boots straight into
# the OpenEdge UI in fullscreen, no menus or task switcher.
#
# Target: a dedicated Linux PC (Ubuntu/Debian/Raspberry Pi OS) wired
# to a touchscreen on the factory floor. The operator never sees the
# desktop — power-on lands them on the live dashboard.
#
# Usage (as the user who'll own the kiosk session):
#   ./scripts/install-kiosk-linux.sh https://openedge.local
#   ./scripts/install-kiosk-linux.sh https://openedge.local --uninstall

set -euo pipefail

URL="${1:-http://localhost:3000}"
ACTION="${2:-install}"

AUTOSTART_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/autostart"
DESKTOP_FILE="$AUTOSTART_DIR/openedge-kiosk.desktop"

if [[ "$ACTION" == "--uninstall" ]]; then
    rm -f "$DESKTOP_FILE"
    echo "Kiosk autostart removed. The operator desktop is restored on next login."
    exit 0
fi

if [[ ! "$URL" =~ ^https?:// ]]; then
    echo "first argument must be a full URL (http(s)://...)" >&2
    exit 1
fi

# Pick the first available browser. Chromium is the canonical industrial
# choice (Chrome's kiosk flags + manifest support); we fall back to
# Firefox for distros where only it is preinstalled.
BROWSER=""
for cmd in chromium chromium-browser google-chrome firefox; do
    if command -v "$cmd" >/dev/null 2>&1; then
        BROWSER="$cmd"
        break
    fi
done
if [[ -z "$BROWSER" ]]; then
    echo "No supported browser found. Install chromium-browser or firefox first." >&2
    exit 1
fi

case "$BROWSER" in
    firefox)
        EXEC_LINE="$BROWSER --kiosk \"$URL\""
        ;;
    *)
        EXEC_LINE="$BROWSER --kiosk --no-first-run --start-fullscreen --noerrdialogs --disable-translate \"$URL\""
        ;;
esac

mkdir -p "$AUTOSTART_DIR"
cat > "$DESKTOP_FILE" <<EOF
[Desktop Entry]
Type=Application
Name=OpenEdge Kiosk
Comment=Boot straight into the OpenEdge UI
Exec=$EXEC_LINE
X-GNOME-Autostart-enabled=true
StartupNotify=false
EOF
chmod 644 "$DESKTOP_FILE"

echo "Installed kiosk autostart:"
echo "  Browser: $BROWSER"
echo "  URL:     $URL"
echo "  File:    $DESKTOP_FILE"
echo
echo "On the next login the kiosk will start automatically."
echo "Recommended companion settings on this PC:"
echo "  - disable screen lock / screensaver"
echo "  - set this user to auto-login (via the display manager)"
echo "  - disable system notifications you don't want on the dashboard"
