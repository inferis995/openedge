#!/usr/bin/env bash
# Copy the most recent OpenEdge backup to a mounted external volume —
# typically a USB key the operator inserts before running.
#
# Usage:
#   ./scripts/backup-to-usb.sh                # autodetect first /media/* mount
#   ./scripts/backup-to-usb.sh /media/usbkey  # explicit mount point
#
# Safe to schedule via cron — copies only the NEWEST backup, doesn't
# touch the rest. Also writes a SHA-256 sidecar so the operator can
# verify the copy on arrival.

set -euo pipefail

# 1. Locate the latest backup -----------------------------------------------
BACKUP_DIR="${BACKUP_DIR:-./backups}"
# scripts/backup.sh writes openedge_<ts>.sql.gz. This used to glob
# openedge-*.dump* — a hyphen and a format no producer in this repo has
# ever written — so it matched nothing and exited with "no backups found"
# on a machine with a directory full of them.
LATEST=$(ls -t "$BACKUP_DIR"/openedge_*.sql.gz 2>/dev/null | head -n1 || true)
if [[ -z "$LATEST" ]]; then
  echo "no backups found in $BACKUP_DIR — run the backup container first" >&2
  exit 1
fi

# 2. Resolve the USB mount point --------------------------------------------
if [[ $# -ge 1 ]]; then
  USB="$1"
else
  # Naive autodetect: first writable mount under /media or /mnt.
  for candidate in /media/*/. /mnt/*/.; do
    [[ -d "$candidate" ]] || continue
    if [[ -w "$candidate" ]]; then
      USB="$(dirname "$candidate")"
      break
    fi
  done
fi
if [[ -z "${USB:-}" || ! -d "$USB" ]]; then
  echo "no USB mount found. Insert the key and run:" >&2
  echo "  $0 /media/<your-usb>" >&2
  exit 1
fi

# 3. Copy + checksum --------------------------------------------------------
DEST_DIR="$USB/openedge-backups"
mkdir -p "$DEST_DIR"
FNAME=$(basename "$LATEST")

echo "[usb] copying $FNAME to $DEST_DIR …"
cp -v "$LATEST" "$DEST_DIR/"
sha256sum "$LATEST" | awk -v f="$FNAME" '{print $1"  "f}' > "$DEST_DIR/${FNAME}.sha256"

sync
echo
echo "OK — backup on USB at $DEST_DIR/$FNAME"
echo "  verify on another machine with:"
echo "    cd $DEST_DIR && sha256sum -c ${FNAME}.sha256"
echo
echo "Safely eject the key with: sudo umount $USB"
