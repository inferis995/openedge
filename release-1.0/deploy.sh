#!/bin/bash
###############################################################################
# OpenEdge v1.0 - Production Deployment Script
###############################################################################
# This script automates the deployment of OpenEdge Industrial Edge
# Middleware in a production environment.
#
# Usage:
#   ./deploy.sh [options]
#
# Options:
#   -h, --help           Show this help message
#   -c, --check          Check prerequisites only
#   -b, --build          Build Docker images
#   -s, --start          Start all services
#   -x, --stop           Stop all services
#   -r, --restart        Restart all services
#   -l, --logs           Show service logs
#   -p, --status         Show service status
#   -u, --update         Update and restart services
#   --backup             Backup database before deployment
#   --restore            Restore from backup
#
# Examples:
#   ./deploy.sh --check          # Check prerequisites
#   ./deploy.sh --backup --build # Backup and build
#   ./deploy.sh --start          # Start all services
###############################################################################

set -e  # Exit on error

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_NAME="OpenEdge"
VERSION="1.0.0"
BACKUP_DIR="$SCRIPT_DIR/backups"
LOG_DIR="$SCRIPT_DIR/logs"
COMPOSE_FILE="$SCRIPT_DIR/docker-compose.yml"

# Functions
print_header() {
    echo -e "${BLUE}"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "  $1"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo -e "${NC}"
}

print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

print_error() {
    echo -e "${RED}✗ $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}⚠ $1${NC}"
}

print_info() {
    echo -e "${BLUE}ℹ $1${NC}"
}

check_prerequisites() {
    print_header "Checking Prerequisites"

    local missing=0

    # Check Docker
    if command -v docker &> /dev/null; then
        print_success "Docker is installed"
    else
        print_error "Docker is not installed"
        missing=$((missing + 1))
    fi

    # Check Docker Compose
    if command -v docker-compose &> /dev/null; then
        print_success "Docker Compose is installed"
    else
        print_error "Docker Compose is not installed"
        missing=$((missing + 1))
    fi

    # Check .env file
    if [ -f "$SCRIPT_DIR/.env" ]; then
        print_success "Environment file (.env) exists"
    else
        print_warning "Environment file (.env) not found"
        if [ -f "$SCRIPT_DIR/.env.example" ]; then
            print_info "Copying .env.example to .env"
            cp "$SCRIPT_DIR/.env.example" "$SCRIPT_DIR/.env"
            print_warning "Please edit .env with your configuration before starting services"
        else
            print_error ".env.example not found"
            missing=$((missing + 1))
        fi
    fi

    # Create necessary directories
    mkdir -p "$BACKUP_DIR" "$LOG_DIR" "$SCRIPT_DIR/backups"
    print_success "Created necessary directories"

    if [ $missing -gt 0 ]; then
        print_error "Missing prerequisites. Please install required software."
        exit 1
    fi

    print_success "All prerequisites checked"
}

build_images() {
    print_header "Building Docker Images"

    print_info "Building OpenEdge services..."
    docker-compose -f "$COMPOSE_FILE" build

    print_success "Docker images built successfully"
}

backup_database() {
    print_header "Backing Up Database"

    local timestamp=$(date +%Y%m%d_%H%M%S)
    local backup_file="$BACKUP_DIR/openedge_backup_$timestamp.sql"

    print_info "Creating backup: $backup_file"

    docker-compose -f "$COMPOSE_FILE" exec -T postgres \
        pg_dump -U industrial_user industrial_edge > "$backup_file"

    if [ $? -eq 0 ]; then
        print_success "Database backed up successfully"
        print_info "Backup file: $backup_file"
    else
        print_error "Database backup failed"
        exit 1
    fi
}

restore_database() {
    local backup_file="$1"

    if [ -z "$backup_file" ]; then
        print_error "Please specify backup file"
        exit 1
    fi

    if [ ! -f "$backup_file" ]; then
        print_error "Backup file not found: $backup_file"
        exit 1
    fi

    print_header "Restoring Database from Backup"

    print_warning "This will replace the current database. Are you sure? (y/N)"
    read -r confirmation

    if [ "$confirmation" != "y" ] && [ "$confirmation" != "Y" ]; then
        print_info "Restore cancelled"
        return
    fi

    print_info "Restoring from: $backup_file"

    docker-compose -f "$COMPOSE_FILE" exec -T postgres \
        psql -U industrial_user industrial_edge < "$backup_file"

    print_success "Database restored successfully"
}

start_services() {
    print_header "Starting OpenEdge Services"

    print_info "Starting all services..."
    docker-compose -f "$COMPOSE_FILE" up -d

    print_success "Services started"

    print_info "Waiting for services to be healthy..."
    sleep 5

    show_status
}

stop_services() {
    print_header "Stopping OpenEdge Services"

    print_info "Stopping all services..."
    docker-compose -f "$COMPOSE_FILE" down

    print_success "Services stopped"
}

restart_services() {
    print_header "Restarting OpenEdge Services"

    stop_services
    sleep 2
    start_services
}

show_logs() {
    local service="$1"

    if [ -z "$service" ]; then
        docker-compose -f "$COMPOSE_FILE" logs -f --tail=100
    else
        docker-compose -f "$COMPOSE_FILE" logs -f --tail=100 "$service"
    fi
}

show_status() {
    print_header "OpenEdge Service Status"

    docker-compose -f "$COMPOSE_FILE" ps

    echo ""
    print_info "Service Health:"

    # Check PostgreSQL
    if docker-compose -f "$COMPOSE_FILE" exec -T postgres pg_isready -U industrial_user &> /dev/null; then
        print_success "PostgreSQL: Healthy"
    else
        print_error "PostgreSQL: Unhealthy"
    fi

    # Check Redis
    if docker-compose -f "$COMPOSE_FILE" exec -T redis redis-cli ping &> /dev/null; then
        print_success "Redis: Healthy"
    else
        print_error "Redis: Unhealthy"
    fi

    # Check Mosquitto
    if docker-compose -f "$COMPOSE_FILE" exec -T mosquitto mosquitto_pub -h localhost -t '$$SYS/broker/version' -C 1 &> /dev/null; then
        print_success "Mosquitto: Healthy"
    else
        print_error "Mosquitto: Unhealthy"
    fi

    echo ""
    print_info "Access URLs:"
    echo "  Web UI:       http://localhost:80"
    echo "  API:          http://localhost:8081"
    echo "  MQTT:         mqtt://localhost:18830"
}

update_services() {
    print_header "Updating OpenEdge Services"

    print_warning "This will pull latest updates and restart services."
    print_warning "Make sure you have committed any local changes."

    print_info "Pulling latest images..."
    docker-compose -f "$COMPOSE_FILE" pull

    print_info "Rebuilding custom images..."
    docker-compose -f "$COMPOSE_FILE" build

    restart_services

    print_success "Services updated successfully"
}

show_help() {
    cat << EOF
${GREEN}OpenEdge v${VERSION} - Production Deployment Script${NC}

${YELLOW}Usage:${NC}
  $0 [options]

${YELLOW}Options:${NC}
  -h, --help           Show this help message
  -c, --check          Check prerequisites only
  -b, --build          Build Docker images
  -s, --start          Start all services
  -x, --stop           Stop all services
  -r, --restart        Restart all services
  -l, --logs [service] Show service logs (all services if none specified)
  -p, --status         Show service status
  -u, --update         Update and restart services
  --backup             Backup database
  --restore <file>     Restore database from backup

${YELLOW}Examples:${NC}
  $0 --check           # Check prerequisites
  $0 --backup --build  # Backup and build
  $0 --start           # Start all services
  $0 --logs core-api   # Show core-api logs
  $0 --status          # Show service status

${YELLOW}For more information:${NC}
  https://docs.openedge.io/deployment
EOF
}

# Main script logic
main() {
    local command=""

    # Parse arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            -h|--help)
                show_help
                exit 0
                ;;
            -c|--check)
                command="check"
                shift
                ;;
            -b|--build)
                command="build"
                shift
                ;;
            -s|--start)
                command="start"
                shift
                ;;
            -x|--stop)
                command="stop"
                shift
                ;;
            -r|--restart)
                command="restart"
                shift
                ;;
            -l|--logs)
                command="logs"
                shift
                ;;
            -p|--status)
                command="status"
                shift
                ;;
            -u|--update)
                command="update"
                shift
                ;;
            --backup)
                command="backup"
                shift
                ;;
            --restore)
                command="restore"
                shift
                ;;
            *)
                # Unknown option
                if [ "$command" == "logs" ]; then
                    # For logs, the next argument is the service name
                    shift
                    show_logs "$@"
                    exit 0
                else
                    print_error "Unknown option: $1"
                    show_help
                    exit 1
                fi
                ;;
        esac
    done

    # Execute command
    case $command in
        check)
            check_prerequisites
            ;;
        build)
            check_prerequisites
            build_images
            ;;
        backup)
            check_prerequisites
            backup_database
            ;;
        restore)
            check_prerequisites
            restore_database "$2"
            ;;
        start)
            check_prerequisites
            start_services
            ;;
        stop)
            stop_services
            ;;
        restart)
            check_prerequisites
            restart_services
            ;;
        logs)
            check_prerequisites
            show_logs
            ;;
        status)
            check_prerequisites
            show_status
            ;;
        update)
            check_prerequisites
            update_services
            ;;
        "")
            # No command specified, show help
            show_help
            ;;
        *)
            show_help
            exit 1
            ;;
    esac
}

# Run main function
main "$@"
