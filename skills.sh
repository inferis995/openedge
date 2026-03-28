#!/usr/bin/env bash
# OpenEdge Skills Installer
# Usage:
#   ./skills.sh                  # installa tutte le skill
#   ./skills.sh openedge         # installa solo openedge.md
#   ./skills.sh openedge-ops     # installa solo openedge-ops.md
#   ./skills.sh openedge-dashboard

set -e

SKILL=${1:-all}
BASE_URL="https://raw.githubusercontent.com/inferis995/openedge/master/.claude/skills"

# Destinazione: .claude/skills/ nella directory corrente, o CLAUDE_SKILLS_DIR se definita
DEST="${CLAUDE_SKILLS_DIR:-.claude/skills}"
mkdir -p "$DEST"

install_skill() {
  local name=$1
  echo -n "  Downloading $name.md ... "
  if curl -sf "$BASE_URL/$name.md" -o "$DEST/$name.md"; then
    echo "✓"
  else
    echo "✗ (not found)"
    return 1
  fi
}

echo ""
echo "OpenEdge Skills Installer"
echo "→ destination: $DEST"
echo ""

case "$SKILL" in
  all)
    install_skill openedge
    install_skill openedge-ops
    install_skill openedge-dashboard
    ;;
  openedge|openedge-ops|openedge-dashboard)
    install_skill "$SKILL"
    ;;
  *)
    echo "Unknown skill: $SKILL"
    echo "Available: openedge, openedge-ops, openedge-dashboard"
    exit 1
    ;;
esac

echo ""
echo "Done. Skills installed in: $DEST"
