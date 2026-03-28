.PHONY: build up start down restart logs clean help

## Build all images (main services + all driver images)
build:
	docker-compose -f docker-compose.yml -f docker-compose.build.yml build

## Start all services in background (images must already be built)
up:
	docker-compose up -d

## Build all images then start services  [RECOMMENDED for first run]
start: build up
	@echo ""
	@echo "OpenEdge is starting up. Check status with: make logs"
	@echo "Web UI will be available at: http://localhost:3000"
	@echo "Login: admin / admin123"

## Stop all running services
down:
	docker-compose down

## Restart all services
restart: down up

## Follow logs of all services
logs:
	docker-compose logs -f

## Full reset - DESTROYS ALL DATA (volumes included)
clean:
	docker-compose down -v

## Show this help
help:
	@echo "Available targets:"
	@echo "  make start    - Build all images and start services (first run)"
	@echo "  make build    - Build all images (main + driver images)"
	@echo "  make up       - Start services (if images already built)"
	@echo "  make down     - Stop all services"
	@echo "  make restart  - Stop then start all services"
	@echo "  make logs     - Follow logs of all services"
	@echo "  make clean    - Stop services and DELETE all data volumes"
