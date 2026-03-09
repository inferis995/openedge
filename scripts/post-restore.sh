#!/bin/bash
# Post-restore script for OpenEdge Industrial Edge Middleware
# This script restarts services in the correct order after a database restore
# Usage: ./scripts/post-restore.sh

echo "🔄 OpenEdge - Post-Restore Script"
echo "================================"
echo ""

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Function to restart a service
restart_service() {
    local service=$1
    echo -e "${YELLOW}Restarting ${service}...${NC}"
    docker restart $service

    # Wait for service to be healthy
    echo -e "${YELLOW}Waiting for ${service} to be healthy...${NC}"
    local max_attempts=30
    local attempt=0

    while [ $attempt -lt $max_attempts ]; do
        if docker inspect --format='{{.State.Health.Status}}' $service 2>/dev/null | grep -q "healthy"; then
            echo -e "${GREEN}✓ ${service} is healthy${NC}"
            return 0
        fi
        sleep 2
        attempt=$((attempt + 1))
    done

    echo -e "${RED}✗ ${service} failed to become healthy${NC}"
    return 1
}

# Step 1: Restart database (if it was restored)
echo "Step 1: Restarting PostgreSQL..."
restart_service "industrial-postgres" || exit 1
echo ""

# Step 2: Restart Redis (clears cache and ensures fresh state)
echo "Step 2: Restarting Redis..."
restart_service "industrial-redis" || exit 1
echo ""

# Step 3: Restart MQTT Broker
echo "Step 3: Restarting Mosquitto MQTT Broker..."
restart_service "industrial-mosquitto" || exit 1
echo ""

# Step 4: Restart Core API (reconnects to all services)
echo "Step 4: Restarting Core API..."
restart_service "industrial-core-api" || exit 1
echo ""

# Step 5: Restart Driver Manager (will recreate all driver containers)
echo "Step 5: Restarting Driver Manager..."
restart_service "industrial-driver-manager" || exit 1
echo ""

# Step 6: Restart Engine Historian
echo "Step 6: Restarting Engine Historian..."
restart_service "industrial-engine-historian" || exit 1
echo ""

# Step 7: Restart Web UI
echo "Step 7: Restarting Web UI..."
restart_service "industrial-web-ui" || exit 1
echo ""

echo "================================"
echo -e "${GREEN}✓ Post-Restore completed successfully!${NC}"
echo ""
echo "🌐 Web UI: http://localhost:3000"
echo "📊 API: http://localhost:9090"
echo ""
echo "💡 Tip: Wait 30-60 seconds for all drivers to reconnect to PLCs"
