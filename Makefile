.PHONY: build up start down restart logs clean help setup-env install-service uninstall-service onprem-tls onprem-tls-down backup-now backup-to-usb restore export-root-ca

COMPOSE_ONPREM_TLS = docker-compose -f docker-compose.yml -f docker-compose.onprem-tls.yml --profile backup

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

## Show available targets
help:
	@echo ""
	@echo "  make start    Build all images and start services (first run)"
	@echo "  make up       Start services (images already built)"
	@echo "  make down     Stop all services"
	@echo "  make restart  Stop then start"
	@echo "  make logs     Follow logs"
	@echo "  make clean    Stop and DELETE all data (irreversible)"
	@echo ""
