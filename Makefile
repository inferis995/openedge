.PHONY: build up start down restart logs clean help setup-env

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
