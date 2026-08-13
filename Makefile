.PHONY: build up start down restart logs clean help setup-env install-service uninstall-service onprem-tls onprem-tls-down backup-now backup-to-usb restore export-root-ca update update-check kiosk-linux lint lint-go lint-frontend test test-go test-frontend test-coverage swagger hooks-install vps-up vps-up-edge vps-down vps-logs vps-status coolify-build coolify-up coolify-down monitoring-up monitoring-down monitoring-logs build-cli install-cli

COMPOSE         = docker compose
COMPOSE_TLS     = docker compose --profile tls
COMPOSE_VPS          = docker compose -f docker-compose.yml -f docker-compose.vps.yml
COMPOSE_VPS_EDGE     = docker compose -f docker-compose.yml -f docker-compose.vps.yml --profile edge
COMPOSE_VPS_MON      = docker compose -f docker-compose.yml -f docker-compose.vps.yml --profile monitoring
COMPOSE_COOLIFY = docker compose -f docker-compose.coolify.yml
COMPOSE_MONITORING = docker compose --profile monitoring

## Create .env from example if missing; auto-generate JWT_SECRET if unset or placeholder
setup-env:
	@if [ ! -f .env ]; then \
		cp .env.example .env; \
		echo "[setup] Created .env from .env.example"; \
	fi
	@if ! grep -q "^JWT_SECRET=" .env || grep -q "^JWT_SECRET=CHANGE_ME" .env; then \
		SECRET=$$(openssl rand -hex 32); \
		grep -v "^JWT_SECRET=" .env > .env.tmp && mv .env.tmp .env; \
		printf "JWT_SECRET=$$SECRET\n" >> .env; \
		echo "[setup] Generated JWT_SECRET"; \
	fi
	@if grep -q "^POSTGRES_PASSWORD=CHANGE_ME" .env; then \
		PG_PASS=$$(openssl rand -hex 16); \
		grep -v "^POSTGRES_PASSWORD=" .env > .env.tmp && mv .env.tmp .env; \
		printf "POSTGRES_PASSWORD=$$PG_PASS\n" >> .env; \
		echo "[setup] Generated POSTGRES_PASSWORD"; \
	fi
	@if grep -q "^MQTT_ADMIN_PASSWORD=CHANGE_ME" .env; then \
		MQTT_PASS=$$(openssl rand -hex 16); \
		grep -v "^MQTT_ADMIN_PASSWORD=" .env > .env.tmp && mv .env.tmp .env; \
		printf "MQTT_ADMIN_PASSWORD=$$MQTT_PASS\n" >> .env; \
		echo "[setup] Generated MQTT_ADMIN_PASSWORD"; \
	fi
	@if ! grep -q "^GRAFANA_ADMIN_PASSWORD=" .env || grep -q "^GRAFANA_ADMIN_PASSWORD=CHANGE_ME" .env; then \
		GF_PASS=$$(openssl rand -hex 12); \
		grep -v "^GRAFANA_ADMIN_PASSWORD=" .env > .env.tmp && mv .env.tmp .env; \
		printf "GRAFANA_ADMIN_PASSWORD=$$GF_PASS\n" >> .env; \
		echo "[setup] Generated GRAFANA_ADMIN_PASSWORD"; \
	fi
	@if ! grep -q "^ENCRYPTION_KEY=" .env || grep -q "^ENCRYPTION_KEY=CHANGE_ME" .env; then \
		ENC_KEY=$$(openssl rand -hex 16); \
		grep -v "^ENCRYPTION_KEY=" .env > .env.tmp && mv .env.tmp .env; \
		printf "ENCRYPTION_KEY=$$ENC_KEY\n" >> .env; \
		echo "[setup] Generated ENCRYPTION_KEY (AES-256, exactly 32 bytes)"; \
	fi
	@if ! grep -q "^OPENEDGE_INITIAL_ADMIN_PASSWORD=" .env || grep -q "^OPENEDGE_INITIAL_ADMIN_PASSWORD=CHANGE_ME" .env; then \
		ADMIN_PASS=$$(openssl rand -base64 18 | tr -d '/+=' | cut -c1-20); \
		grep -v "^OPENEDGE_INITIAL_ADMIN_PASSWORD=" .env > .env.tmp && mv .env.tmp .env; \
		printf "OPENEDGE_INITIAL_ADMIN_PASSWORD=$$ADMIN_PASS\n" >> .env; \
		echo "[setup] Generated the initial admin password (shown by 'make start')"; \
	fi
	@echo "[setup] .env ready — all secrets auto-generated"

## Build all images — core services + all drivers (modbus/s7/opcua/mqtt/redis/lorawan)
build: setup-env
	$(COMPOSE) --profile drivers build

## Start services in background (images must already be built)
up: setup-env
	$(COMPOSE) up -d

## Build all images then start services  [RECOMMENDED for first run]
start: setup-env
	$(COMPOSE) --profile drivers build
	$(COMPOSE) up -d
	@echo ""
	@echo "OpenEdge is starting up."
	@echo "  Web UI:   http://localhost:${WEB_UI_PORT:-3000}"
	@echo "  Core API: http://localhost:8081"
	@echo "  Login:    admin / $$(grep '^OPENEDGE_INITIAL_ADMIN_PASSWORD=' .env | cut -d= -f2-)"
	@echo ""
	@echo "  This password was generated for this installation and is stored in .env."
	@echo "  It is a GLOBAL ADMIN: it can read every tenant and write to every PLC."
	@echo ""
	@echo "Tip: run 'make logs' to follow startup logs."

## Stop all running services
down:
	$(COMPOSE) down

## Stop then start all services
restart: down up

## Follow logs of all services
logs:
	$(COMPOSE) logs -f

## Full reset — DESTROYS ALL DATA including volumes (irreversible)
clean:
	$(COMPOSE) down -v

# ── On-prem service install (industrial PCs, runs at boot, survives reboots) ─
## Install OpenEdge as a systemd service on Linux. Idempotent.
##   sudo make install-service           # uses /opt/openedge (default)
##   sudo make install-service INSTALL_DIR=/home/user/openedge
install-service:
	./systemd/install.sh $(INSTALL_DIR)

## Remove the systemd service. Data and config are NOT touched.
uninstall-service:
	./systemd/install.sh --uninstall

# ── On-prem TLS (internal CA, no internet needed) ───────────────────────────
## Start the on-prem stack with internal TLS (Caddy `tls internal`, no internet needed).
## After first start: make export-root-ca → install cert on each operator PC once.
onprem-tls: setup-env
	$(COMPOSE) --profile drivers build
	$(COMPOSE_TLS) up -d
	@echo ""
	@echo "OpenEdge started with internal TLS."
	@echo "  Web UI:  https://$$(grep ^PUBLIC_HOST .env 2>/dev/null | cut -d= -f2 || echo openedge.local)"
	@echo "  Next:    make export-root-ca → install openedge-root-ca.crt on each operator PC."

## Stop the TLS stack
onprem-tls-down:
	$(COMPOSE_TLS) down

## Export Caddy's internal root CA so operators can trust it in their browsers.
export-root-ca:
	@docker exec openedge-caddy cat /data/caddy/pki/authorities/local/root.crt > openedge-root-ca.crt
	@echo "Wrote openedge-root-ca.crt — install it on each operator PC:"
	@echo "  Windows: import to 'Trusted Root Certification Authorities' (certmgr.msc)"
	@echo "  macOS:   double-click → Keychain → trust this cert"
	@echo "  Linux:   sudo cp openedge-root-ca.crt /usr/local/share/ca-certificates/ && sudo update-ca-certificates"

# ── Backup / restore ────────────────────────────────────────────────────────
## Take a backup right now — runs pg_dump inside the running postgres container.
backup-now:
	./scripts/backup.sh

## Copy the most recent backup to a USB key (autodetected under /media/*).
##   make backup-to-usb USB=/media/myusb   to override the destination.
backup-to-usb:
	./scripts/backup-to-usb.sh $(USB)

## Restore from a backup file. DESTRUCTIVE — wipes the live database.
##   make restore BACKUP=./backups/openedge_20250604T030000Z.sql.gz
##
## Takes a safety dump of the CURRENT database first, so restoring the wrong
## file is recoverable.
restore:
	@: $${BACKUP:?BACKUP is required (path to a .sql.gz from scripts/backup.sh)}
	./scripts/restore.sh $(BACKUP) --yes-i-have-a-recent-backup

# ── Controlled on-prem upgrade ──────────────────────────────────────────────
## Safe upgrade: snapshot → pull → up -d → health probe. Rolls back path
## documented; the snapshot file path is printed on success.
update:
	./scripts/update-onprem.sh

## Show what an upgrade would change, without applying anything.
update-check:
	./scripts/update-onprem.sh --check

# ── Operator workstation setup ─────────────────────────────────────────────
## Configure a Linux PC as an HMI kiosk: browser autostarts in fullscreen.
##   make kiosk-linux URL=https://openedge.local
kiosk-linux:
	@: $${URL:?URL is required (e.g. URL=https://openedge.local)}
	./scripts/install-kiosk-linux.sh $(URL)

# ── VPS / Cloud deployment (Traefik + Let's Encrypt) ────────────────────────
## First-time VPS setup (interactive): installs Docker, configures TLS, starts stack.
## Prefer Coolify for a managed experience.
vps-init: setup-env
	bash deploy/cloud-init.sh

## Refuse to start production with placeholder or missing secrets
preflight:
	@bash scripts/preflight.sh

## Same check, plus what only matters when Grafana is published
preflight-monitoring:
	@bash scripts/preflight.sh .env --with-monitoring

## Start the VPS stack (Traefik + Let's Encrypt + all services)
vps-up: setup-env preflight
	$(COMPOSE_VPS) build
	$(COMPOSE_VPS) up -d
	@echo ""
	@echo "OpenEdge VPS stack started."
	@echo "  Web UI: https://$$(grep ^PUBLIC_HOST .env 2>/dev/null | cut -d= -f2 || echo 'your-domain')"
	@echo "  MQTT:   mqtts://$$(grep ^PUBLIC_HOST .env 2>/dev/null | cut -d= -f2 || echo 'your-domain'):8883"

## Start the VPS stack WITH monitoring (Prometheus + Grafana + Loki + exporters).
## Grafana is published at grafana.$$PUBLIC_HOST, so it needs its own DNS A
## record and a real password — which is why this has its own preflight.
vps-up-monitoring: setup-env preflight-monitoring
	$(COMPOSE_VPS_MON) build
	$(COMPOSE_VPS_MON) up -d
	@echo ""
	@echo "OpenEdge VPS stack + monitoring started."
	@echo "  Grafana: https://grafana.$$(grep ^PUBLIC_HOST .env 2>/dev/null | cut -d= -f2 || echo 'your-domain')"

## Start the VPS stack + edge (driver-manager on the same VPS — all-in-one cloud)
vps-up-edge: setup-env preflight
	$(COMPOSE_VPS_EDGE) build
	$(COMPOSE_VPS_EDGE) up -d
	@echo ""
	@echo "OpenEdge VPS + Edge stack started (server + drivers on same machine)."
	@echo "  Web UI: https://$$(grep ^PUBLIC_HOST .env 2>/dev/null | cut -d= -f2 || echo 'your-domain')"
	@echo "  MQTT:   mqtts://$$(grep ^PUBLIC_HOST .env 2>/dev/null | cut -d= -f2 || echo 'your-domain'):8883"

## Stop the VPS stack
vps-down:
	$(COMPOSE_VPS) down

## Follow VPS logs
vps-logs:
	$(COMPOSE_VPS) logs -f

## Show VPS service health
vps-status:
	$(COMPOSE_VPS) ps
	@echo ""
	@DOMAIN=$$(grep ^PUBLIC_HOST .env 2>/dev/null | cut -d= -f2 || echo 'localhost'); \
	 echo "  Checking https://$$DOMAIN/health ..."; \
	 curl -fsSLo /dev/null -w "  HTTP %%{http_code}  %%{time_total}s\n" "https://$$DOMAIN/health" 2>/dev/null || \
	 echo "  Not reachable yet (TLS cert may still be issuing)"

# ── Coolify deployment (recommended for cloud) ────────────────────────────────
## Build images locally — used when Coolify builds from git
coolify-build: setup-env
	$(COMPOSE_COOLIFY) build

## Test Coolify compose locally before pushing (server only)
coolify-up: setup-env
	$(COMPOSE_COOLIFY) up -d

## Test Coolify compose locally + driver-manager (all-in-one)
coolify-up-edge: setup-env
	$(COMPOSE_COOLIFY) --profile edge up -d

## Stop local Coolify test stack
coolify-down:
	$(COMPOSE_COOLIFY) down

# ── Observability (Prometheus + Grafana + Loki) ──────────────────────────────
## Start full stack + monitoring (Prometheus, Grafana, AlertManager, Loki + 4 exporters)
monitoring-up: setup-env
	$(COMPOSE) --profile drivers --profile monitoring build
	$(COMPOSE_MONITORING) up -d
	@echo ""
	@echo "Monitoring stack started."
	@echo "  Grafana:      http://localhost:3001  (admin / $$(grep ^GRAFANA_ADMIN_PASSWORD .env 2>/dev/null | cut -d= -f2 || echo admin))"
	@echo "  Prometheus:   http://localhost:9090"
	@echo "  AlertManager: http://localhost:9093"

## Stop the monitoring stack (leaves core services running)
monitoring-down:
	$(COMPOSE_MONITORING) down

## Follow monitoring logs
monitoring-logs:
	$(COMPOSE_MONITORING) logs -f prometheus grafana alertmanager loki

# ── Code quality ─────────────────────────────────────────────────────────────
## Run all linters (Go + frontend)
lint: lint-go lint-frontend

## Run golangci-lint on Go code
lint-go:
	golangci-lint run ./...

## Run ESLint + TypeScript type-check on frontend
lint-frontend:
	cd services/web-ui && npm run lint && npx tsc --noEmit

## Run all tests (Go + frontend)
test: test-go test-frontend

## Run Go tests with race detector
test-go:
	JWT_SECRET=local-test-secret-key-minimum-32-chars go test -race ./internal/... ./services/...

## Run end-to-end acceptance tests against a RUNNING stack.
## Start it first (make start), then: make test-e2e
## These cross process boundaries — they catch the "both halves correct, wired
## to nothing" defects that unit tests structurally cannot see.
test-e2e:
	@echo "Expecting the stack at $${E2E_API_URL:-http://127.0.0.1:8081} — run 'make start' first."
	go test -tags=e2e -v -timeout=10m ./test/e2e/...

## Run frontend unit tests (single run, no watch)
test-frontend:
	cd services/web-ui && npm run test:run

## Run all tests with coverage reports
test-coverage:
	JWT_SECRET=local-test-secret-key-minimum-32-chars go test -race -coverprofile=coverage.out ./internal/... && \
	go tool cover -func=coverage.out | tail -1 && \
	cd services/web-ui && npm run test:coverage

## Build the OpenEdge CLI binary → bin/openedge
build-cli:
	@mkdir -p bin
	go build -ldflags="-s -w" -o bin/openedge ./services/cli/
	@echo "[cli] Built → bin/openedge"

## Install the OpenEdge CLI to /usr/local/bin (requires sudo)
install-cli: build-cli
	sudo cp bin/openedge /usr/local/bin/openedge
	@echo "[cli] Installed → /usr/local/bin/openedge"
	@echo "      Run: openedge login --url https://your-openedge.com"

## Regenerate Swagger/OpenAPI docs
swagger:
	swag init -g services/core-api/main.go -o docs/ --parseDependency

## Install git hooks via lefthook
hooks-install:
	lefthook install

## Show available targets
help:
	@echo ""
	@echo "  ── Self-Hosted / On-Prem ──────────────────────────────────"
	@echo "  make start           Build + start (first run, HTTP :3000)"
	@echo "  make onprem-tls      Build + start with HTTPS :443 (internal CA)"
	@echo "  make up              Start (images already built)"
	@echo "  make down            Stop all services"
	@echo "  make restart         Stop then start"
	@echo "  make logs            Follow logs"
	@echo "  make clean           Stop and DELETE all data (irreversible)"
	@echo "  make install-service Register as systemd service (auto-start at boot)"
	@echo "  make export-root-ca  Export Caddy CA cert for operator PCs"
	@echo ""
	@echo "  ── Cloud / VPS (Traefik + Let's Encrypt) ──────────────────"
	@echo "  make vps-up          Start VPS stack (server only)"
	@echo "  make vps-up-edge     Start VPS stack + driver-manager (all-in-one)"
	@echo "  make vps-down        Stop VPS stack"
	@echo "  make vps-logs        Follow VPS logs"
	@echo "  make vps-status      Health check"
	@echo ""
	@echo "  ── Coolify (recommended for cloud) ─────────────────────────"
	@echo "  make coolify-build   Build images for Coolify"
	@echo "  make coolify-up      Test Coolify compose locally (server only)"
	@echo "  make coolify-up-edge Test Coolify compose locally + driver-manager"
	@echo ""
	@echo "  ── Backup / Restore ────────────────────────────────────────"
	@echo "  make backup-now      Take a backup immediately"
	@echo "  make restore BACKUP=./backups/file.sql.gz"
	@echo "  make backup-to-usb   Copy latest backup to USB"
	@echo "  make update          Safe upgrade with rollback"
	@echo ""
	@echo "  ── Monitoring ──────────────────────────────────────────────"
	@echo "  make monitoring-up   Start Prometheus + Grafana + Loki"
	@echo "  make monitoring-down Stop monitoring stack"
	@echo ""
	@echo "  ── CLI ─────────────────────────────────────────────────────"
	@echo "  make build-cli       Build openedge CLI → bin/openedge"
	@echo "  make install-cli     Install openedge CLI → /usr/local/bin/openedge"
	@echo ""
	@echo "  ── Quality ─────────────────────────────────────────────────"
	@echo "  make lint            Run all linters"
	@echo "  make test            Run all tests"
	@echo ""
