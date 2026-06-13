.PHONY: build up start down restart logs clean help setup-env install-service uninstall-service onprem-tls onprem-tls-down backup-now backup-to-usb restore export-root-ca update update-check kiosk-linux lint lint-go lint-frontend test test-go test-frontend test-coverage swagger hooks-install cloud-up cloud-down cloud-logs cloud-status monitoring-up monitoring-down monitoring-logs

COMPOSE_ONPREM_TLS  = docker-compose -f docker-compose.yml -f docker-compose.onprem-tls.yml --profile backup
COMPOSE_CLOUD       = docker compose -f docker-compose.yml -f docker-compose.cloud.yml
COMPOSE_COOLIFY     = docker compose -f docker-compose.coolify.yml
COMPOSE_MONITORING  = docker compose -f docker-compose.yml -f docker-compose.monitoring.yml --profile monitoring

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
		echo "[setup] Generated JWT_SECRET and saved to .env"; \
	fi

## Build all images (main services + all driver images)
build:
	docker-compose -f docker-compose.yml -f docker-compose.build.yml build

## Start services in background — setup-env runs automatically
up: setup-env
	docker-compose up -d

## Build all images then start services  [RECOMMENDED for first run]
start: build up
	@echo ""
	@echo "OpenEdge is starting up."
	@echo "  Web UI:   http://localhost:3000"
	@echo "  Core API: http://localhost:8081"
	@echo "  Login:    admin / admin123"
	@echo ""
	@echo "Tip: run 'make logs' to follow startup progress."
	@echo "     Change the default password after first login."

## Stop all running services
down:
	docker-compose down

## Stop then start all services
restart: down up

## Follow logs of all services
logs:
	docker-compose logs -f

## Full reset - DESTROYS ALL DATA (volumes included)
clean:
	docker-compose down -v

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
## Start the on-prem stack with internal TLS (Caddy `tls internal`).
## After first start, export the CA with `make export-root-ca` and install
## it on each operator PC.
onprem-tls: setup-env
	$(COMPOSE_ONPREM_TLS) up -d
	@echo ""
	@echo "OpenEdge on-prem started with internal TLS."
	@echo "  Web UI:  https://$$(grep ^PUBLIC_HOST .env 2>/dev/null | cut -d= -f2 || echo openedge.local)"
	@echo "  Next: 'make export-root-ca' then install openedge-root-ca.crt on each operator PC."

## Stop the on-prem TLS stack
onprem-tls-down:
	$(COMPOSE_ONPREM_TLS) down

## Export Caddy's internal root CA so operators can trust it in their browsers.
export-root-ca:
	@docker exec openedge-caddy cat /data/caddy/pki/authorities/local/root.crt > openedge-root-ca.crt
	@echo "Wrote openedge-root-ca.crt — install it on each operator PC:"
	@echo "  Windows: import to 'Trusted Root Certification Authorities' (certmgr.msc)"
	@echo "  macOS:   double-click → Keychain → trust this cert"
	@echo "  Linux:   sudo cp openedge-root-ca.crt /usr/local/share/ca-certificates/ && sudo update-ca-certificates"

# ── Backup / restore ────────────────────────────────────────────────────────
## Take a backup right now (out-of-schedule, useful before risky changes).
backup-now:
	docker compose --profile backup run --rm \
	  -e BACKUP_RUN_NOW=true backup

## Copy the most recent backup to a USB key (autodetected under /media/*).
##   make backup-to-usb USB=/media/myusb   to override the destination.
backup-to-usb:
	./scripts/backup-to-usb.sh $(USB)

## Restore from a backup file. DESTRUCTIVE — wipes the live database.
##   make restore BACKUP=./backups/openedge-20250604T030000Z.dump
restore:
	@: $${BACKUP:?BACKUP is required (path to dump file)}
	./scripts/restore-backup.sh $(BACKUP)

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

# ── Cloud / SaaS deployment ──────────────────────────────────────────────────
## First-time VPS setup (interactive): installs Docker, configures TLS, starts stack.
## Prefer Coolify for a managed experience (make coolify-build).
cloud-init: setup-env
	bash deploy/cloud-init.sh

# ── Coolify deployment (recommended for cloud) ────────────────────────────────
## Build images locally then push — used when Coolify builds from git
coolify-build: setup-env
	$(COMPOSE_COOLIFY) build

## Start coolify stack locally for testing before pushing
coolify-up: setup-env
	$(COMPOSE_COOLIFY) up -d

## Stop coolify local stack
coolify-down:
	$(COMPOSE_COOLIFY) down

## Start the cloud stack (Traefik + Let's Encrypt + all services)
cloud-up: setup-env
	$(COMPOSE_CLOUD) up -d
	@echo ""
	@echo "OpenEdge cloud stack started."
	@echo "  Web UI: https://$$(grep ^PUBLIC_HOST .env 2>/dev/null | cut -d= -f2 || echo 'your-domain')"
	@echo "  MQTT:   mqtts://$$(grep ^PUBLIC_HOST .env 2>/dev/null | cut -d= -f2 || echo 'your-domain'):8883"

## Stop the cloud stack
cloud-down:
	$(COMPOSE_CLOUD) down

## Follow logs of the cloud stack
cloud-logs:
	$(COMPOSE_CLOUD) logs -f

## Show health status of cloud services
cloud-status:
	$(COMPOSE_CLOUD) ps
	@echo ""
	@DOMAIN=$$(grep ^PUBLIC_HOST .env 2>/dev/null | cut -d= -f2 || echo 'localhost'); \
	 echo "  Checking https://$$DOMAIN/api/health ..."; \
	 curl -fsSLo /dev/null -w "  HTTP %%{http_code}  %%{time_total}s\n" "https://$$DOMAIN/api/health" 2>/dev/null || \
	 echo "  ⚠ Not reachable yet (TLS cert may still be issued)"

# ── Observability (Prometheus + Grafana + Loki) ──────────────────────────────
## Start OpenEdge + Prometheus + Grafana + Loki monitoring stack.
##   Grafana: http://localhost:3001  (admin / admin)
##   Prometheus: http://localhost:9090
monitoring-up: setup-env
	$(COMPOSE_MONITORING) up -d
	@echo ""
	@echo "Monitoring stack started."
	@echo "  Grafana:    http://localhost:3001  (admin / admin)"
	@echo "  Prometheus: http://localhost:9090"

## Stop the monitoring stack
monitoring-down:
	$(COMPOSE_MONITORING) down

## Follow monitoring logs
monitoring-logs:
	$(COMPOSE_MONITORING) logs -f prometheus grafana loki

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
	JWT_SECRET=local-test-secret-key-minimum-32-chars go test -race ./internal/...

## Run frontend unit tests (single run, no watch)
test-frontend:
	cd services/web-ui && npm run test:run

## Run all tests with coverage reports
test-coverage:
	JWT_SECRET=local-test-secret-key-minimum-32-chars go test -race -coverprofile=coverage.out ./internal/... && \
	go tool cover -func=coverage.out | tail -1 && \
	cd services/web-ui && npm run test:coverage

## Regenerate Swagger/OpenAPI docs
swagger:
	swag init -g services/core-api/main.go -o docs/ --parseDependency

## Install git hooks via lefthook
hooks-install:
	lefthook install

## Show available targets
help:
	@echo ""
	@echo "  ── Local / on-prem ────────────────────────────────────────"
	@echo "  make start         Build images and start services (first run)"
	@echo "  make up            Start services (images already built)"
	@echo "  make down          Stop all services"
	@echo "  make restart       Stop then start"
	@echo "  make logs          Follow logs"
	@echo "  make clean         Stop and DELETE all data (irreversible)"
	@echo "  make onprem-tls    Start with internal TLS (self-signed Caddy CA)"
	@echo ""
	@echo "  ── Cloud / SaaS ────────────────────────────────────────────"
	@echo "  make coolify-build Build images for Coolify deployment (recommended)"
	@echo "  make coolify-up    Test Coolify compose locally"
	@echo "  make coolify-down  Stop local Coolify stack"
	@echo "  make cloud-init    Manual VPS setup without Coolify (Traefik direct)"
	@echo "  make cloud-up      Start manual cloud stack"
	@echo "  make cloud-logs    Follow cloud logs"
	@echo "  make cloud-status  Health check"
	@echo ""
	@echo "  ── Monitoring (Prometheus + Grafana + Loki) ────────────────"
	@echo "  make monitoring-up   Start monitoring stack alongside OpenEdge"
	@echo "  make monitoring-down Stop monitoring stack"
	@echo ""
	@echo "  ── Quality ─────────────────────────────────────────────────"
	@echo "  make lint          Run all linters"
	@echo "  make test          Run all tests"
	@echo ""
