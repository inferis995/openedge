#!/usr/bin/env bash
# Installa le skill OpenEdge per Claude Code e OpenCode.
#
#   ./install-skills.sh                  → installa in Claude Code + OpenCode (utente corrente)
#   ./install-skills.sh --claude-only    → solo Claude Code (~/.claude/skills)
#   ./install-skills.sh --opencode-only  → solo OpenCode (~/.config/opencode/skills)
#   ./install-skills.sh --repo           → repo aperto: niente da fare, gia' auto-scoperte
#
# Le skill vivono in .claude/skills/ nel repo (forma directory, niente duplicati):
# Claude Code e OpenCode le auto-scoprono quando il repo e' aperto.
# Questo script serve per averle globali, fuori dal repo.
set -euo pipefail

SKILLS="openedge openedge-ops"
SRC_DIR="$(cd "$(dirname "$0")/.claude/skills" && pwd)"
DO_CLAUDE=1
DO_OPENCODE=1

for arg in "$@"; do
    case "$arg" in
        --claude-only) DO_OPENCODE=0 ;;
        --opencode-only) DO_CLAUDE=0 ;;
        --repo) echo "Repo aperto: Claude Code e OpenCode scoprono gia' .claude/skills/ da sole."; exit 0 ;;
        -h|--help) sed -n '2,10p' "$0"; exit 0 ;;
        *) echo "Opzione sconosciuta: $arg (usa --help)"; exit 1 ;;
    esac
done

install_to() {
    local dest="$1" skill="$2"
    mkdir -p "$dest"
    rm -rf "$dest/$skill"
    cp -r "$SRC_DIR/$skill" "$dest/$skill"
    echo "  ok: $skill -> $dest/$skill"
}

if [ "$DO_CLAUDE" = 1 ]; then
    echo "Claude Code (~/.claude/skills):"
    for s in $SKILLS; do install_to "$HOME/.claude/skills" "$s"; done
fi
if [ "$DO_OPENCODE" = 1 ]; then
    echo "OpenCode (~/.config/opencode/skills):"
    for s in $SKILLS; do install_to "$HOME/.config/opencode/skills" "$s"; done
fi
echo "Fatto."
