#!/usr/bin/env bash
# cloud-init.sh — Bootstrap OpenEdge on a fresh VPS in one command.
#
# Usage (run as root or with sudo):
#   curl -fsSL https://raw.githubusercontent.com/inferis995/openedge/master/deploy/cloud-init.sh | bash
#
# Or interactively:
#   git clone https://github.com/inferis995/openedge /opt/openedge
#   cd /opt/openedge && sudo bash deploy/cloud-init.sh
#
# Tested on: Ubuntu 22.04 LTS, Debian 12, Hetzner CX32 (€6/mo)
# Minimum specs: 2 vCPU, 4 GB RAM, 40 GB SSD

set -euo pipefail

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BOLD='\033[1m'; NC='\033[0m'
info()  { echo -e "${GREEN}[OK]${NC} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*" >&2; exit 1; }
step()  { echo -e "\n${BOLD}▶ $*${NC}"; }

# ── 1. Prerequisites ──────────────────────────────────────────────────────────
step "Checking prerequisites"

if [ "$(id -u)" -ne 0 ]; then
  error "Run this script as root (sudo bash deploy/cloud-init.sh)"
fi

OS=$(. /etc/os-release && echo "$ID")
if [[ "$OS" != "ubuntu" && "$OS" != "debian" ]]; then
  warn "Only Ubuntu/Debian tested. Proceed with caution on $OS."
fi

# ── 2. Install Docker ─────────────────────────────────────────────────────────
if ! command -v docker &>/dev/null; then
  step "Installing Docker"
  curl -fsSL https://get.docker.com | sh
  systemctl enable --now docker
  info "Docker installed"
else
  info "Docker already installed: $(docker --version)"
fi

if ! docker compose version &>/dev/null; then
  step "Installing Docker Compose plugin"
  apt-get install -y docker-compose-plugin 2>/dev/null || \
    error "Install docker-compose-plugin manually: https://docs.docker.com/compose/install/"
fi

# ── 3. Firewall ───────────────────────────────────────────────────────────────
step "Configuring firewall (ufw)"
if command -v ufw &>/dev/null; then
  ufw allow 22/tcp    comment "SSH"
  ufw allow 80/tcp    comment "HTTP (redirect to HTTPS)"
  ufw allow 443/tcp   comment "HTTPS"
  ufw allow 8883/tcp  comment "MQTT/TLS"
  ufw --force enable
  info "Firewall configured: 22, 80, 443, 8883 allowed"
else
  warn "ufw not found — configure firewall manually"
fi

# ── 4. Install directory ──────────────────────────────────────────────────────
INSTALL_DIR="${INSTALL_DIR:-/opt/openedge}"
step "Preparing install directory: $INSTALL_DIR"

if [ ! -d "$INSTALL_DIR" ]; then
  # Running from a pipe: clone the repo
  if ! command -v git &>/dev/null; then
    apt-get install -y git
  fi
  git clone https://github.com/inferis995/openedge "$INSTALL_DIR"
  info "Repository cloned to $INSTALL_DIR"
fi

cd "$INSTALL_DIR"

# ── 5. Environment setup ──────────────────────────────────────────────────────
step "Setting up environment"

if [ ! -f .env ]; then
  cp .env.cloud.example .env
  info "Created .env from .env.cloud.example"
fi

# Auto-generate secrets if still placeholders.
#
# ENCRYPTION_KEY must be EXACTLY 32 bytes: at any other length core-api ignores
# it and stores every tenant's broker password in cleartext, saying so once in a
# log line. It was missing from this loop and from .env.cloud.example, so every
# install made here came up that way.
#
# GRAFANA_ADMIN_PASSWORD was missing too, and its compose entry only refuses an
# EMPTY value — so the placeholder satisfied it, and Grafana went onto the
# public internet with a password published in this repository.
for VAR in JWT_SECRET POSTGRES_PASSWORD MQTT_ADMIN_PASSWORD; do
  if ! grep -q "^${VAR}=" .env || grep -q "^${VAR}=CHANGE_ME" .env; then
    sed -i "s|^${VAR}=.*|${VAR}=$(openssl rand -hex 32)|" .env
    info "Auto-generated $VAR"
  fi
done

if ! grep -q "^ENCRYPTION_KEY=" .env || grep -q "^ENCRYPTION_KEY=CHANGE_ME" .env; then
  ENC_KEY=$(openssl rand -hex 16)   # 32 hex characters = 32 bytes
  grep -v "^ENCRYPTION_KEY=" .env > .env.tmp && mv .env.tmp .env
  printf 'ENCRYPTION_KEY=%s\n' "$ENC_KEY" >> .env
  info "Auto-generated ENCRYPTION_KEY (AES-256, exactly 32 bytes)"
fi

if ! grep -q "^GRAFANA_ADMIN_PASSWORD=" .env || grep -q "^GRAFANA_ADMIN_PASSWORD=CHANGE_ME" .env; then
  GF_PASS=$(openssl rand -base64 24 | tr -d '/+=')
  grep -v "^GRAFANA_ADMIN_PASSWORD=" .env > .env.tmp && mv .env.tmp .env
  printf 'GRAFANA_ADMIN_PASSWORD=%s\n' "$GF_PASS" >> .env
  info "Auto-generated GRAFANA_ADMIN_PASSWORD (Grafana is internet-facing here)"
fi

# Prompt for domain and email if not set
PUBLIC_HOST=$(grep "^PUBLIC_HOST=" .env | cut -d= -f2 || true)
if [[ -z "$PUBLIC_HOST" || "$PUBLIC_HOST" == "app.yourcompany.com" ]]; then
  echo ""
  read -rp "  Enter your domain (e.g. app.openedge.io): " PUBLIC_HOST
  sed -i "s|^PUBLIC_HOST=.*|PUBLIC_HOST=${PUBLIC_HOST}|" .env
  # Also update ALLOWED_ORIGINS
  sed -i "s|^ALLOWED_ORIGINS=.*|ALLOWED_ORIGINS=https://${PUBLIC_HOST}|" .env
  sed -i "s|^MQTT_PUBLIC_HOST=.*|MQTT_PUBLIC_HOST=${PUBLIC_HOST}|" .env
fi

ACME_EMAIL=$(grep "^ACME_EMAIL=" .env | cut -d= -f2 || true)
if [[ -z "$ACME_EMAIL" || "$ACME_EMAIL" == "ops@yourcompany.com" ]]; then
  echo ""
  read -rp "  Enter email for Let's Encrypt (TLS cert notifications): " ACME_EMAIL
  sed -i "s|^ACME_EMAIL=.*|ACME_EMAIL=${ACME_EMAIL}|" .env
fi

ADMIN_PASS=$(grep "^OPENEDGE_INITIAL_ADMIN_PASSWORD=" .env | cut -d= -f2 || true)
if [[ -z "$ADMIN_PASS" || "$ADMIN_PASS" == "CHANGE_ME"* ]]; then
  echo ""
  read -rsp "  Set initial admin password (min 12 chars): " ADMIN_PASS; echo
  sed -i "s|^OPENEDGE_INITIAL_ADMIN_PASSWORD=.*|OPENEDGE_INITIAL_ADMIN_PASSWORD=${ADMIN_PASS}|" .env
fi

info "Environment configured"

# ── 6. DNS check ──────────────────────────────────────────────────────────────
step "Checking DNS for $PUBLIC_HOST"
SERVER_IP=$(curl -fsSL ifconfig.me 2>/dev/null || echo "unknown")
DNS_IP=$(dig +short "$PUBLIC_HOST" 2>/dev/null | tail -1 || true)

if [ "$DNS_IP" = "$SERVER_IP" ]; then
  info "DNS OK: $PUBLIC_HOST → $SERVER_IP"
else
  warn "DNS may not be pointing here yet."
  warn "  This server IP: $SERVER_IP"
  warn "  $PUBLIC_HOST resolves to: ${DNS_IP:-<not found>}"
  warn "  Let's Encrypt will fail until DNS is correct."
  read -rp "  Continue anyway? [y/N] " CONT
  [[ "$CONT" =~ ^[Yy]$ ]] || error "Aborted — fix DNS first"
fi

# ── 7. Install systemd service ────────────────────────────────────────────────
step "Installing systemd service"
COMPOSE_CMD="docker compose -f docker-compose.yml -f docker-compose.vps.yml"
cat > /etc/systemd/system/openedge-cloud.service << EOF
[Unit]
Description=OpenEdge Cloud Stack
After=docker.service network-online.target
Requires=docker.service

[Service]
Type=oneshot
RemainAfterExit=yes
WorkingDirectory=${INSTALL_DIR}
EnvironmentFile=${INSTALL_DIR}/.env
ExecStart=${COMPOSE_CMD} up -d
ExecStop=${COMPOSE_CMD} down
Restart=on-failure
RestartSec=15s

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable openedge-cloud
info "Systemd service openedge-cloud installed and enabled"

# ── 7b. Preflight ─────────────────────────────────────────────────────────────
step "Checking the configuration is fit for production"
if ! bash scripts/preflight.sh .env; then
  error "Fix the problems above and run this script again."
fi

# ── 8. Build and start ────────────────────────────────────────────────────────
step "Building and starting OpenEdge"
# The base compose carries the build definitions; docker-compose.build.yml was
# merged into it and no longer exists.
$COMPOSE_CMD build
$COMPOSE_CMD up -d

# ── 9. Health check ───────────────────────────────────────────────────────────
step "Waiting for services to be healthy"
sleep 10
ATTEMPTS=0
until curl -fsSL "https://${PUBLIC_HOST}/health" &>/dev/null || [ $ATTEMPTS -ge 30 ]; do
  sleep 3
  ATTEMPTS=$((ATTEMPTS+1))
  echo -n "."
done
echo ""

if curl -fsSL "https://${PUBLIC_HOST}/health" &>/dev/null; then
  info "Health check passed"
else
  warn "Health check failed — check logs: docker compose -f docker-compose.yml -f docker-compose.vps.yml logs"
fi

# ── Done ──────────────────────────────────────────────────────────────────────
echo ""
echo -e "${GREEN}${BOLD}✓ OpenEdge is live!${NC}"
echo ""
echo "  Web UI:       https://${PUBLIC_HOST}"
echo "  MQTT broker:  mqtts://${PUBLIC_HOST}:8883"
echo "  Admin login:  admin / (your password)"
echo ""
echo "  Next steps:"
echo "  1. Log in and change the admin password"
echo "  2. Create your first organization"
echo "  3. Click 'Infrastructure' → 'Download Edge Package'"
echo "  4. Deploy the ZIP on the customer's machine: bash install.sh"
echo ""
echo "  Manage with: make vps-logs | make vps-down | make vps-up"
