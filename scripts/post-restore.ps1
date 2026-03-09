# Post-Restore Script for OpenEdge Industrial Edge Middleware (PowerShell)
# This script restarts services in the correct order after a database restore
# Usage: .\scripts\post-restore.ps1

Write-Host "🔄 OpenEdge - Post-Restore Script" -ForegroundColor Cyan
Write-Host "================================" -ForegroundColor Cyan
Write-Host ""

# Function to restart a service and wait for it to be healthy
function Restart-Service {
    param(
        [string]$ServiceName
    )

    Write-Host "Restarting $ServiceName..." -ForegroundColor Yellow

    # Restart the service
    docker restart $ServiceName | Out-Null

    # Wait for service to be healthy
    Write-Host "Waiting for $ServiceName to be healthy..." -ForegroundColor Yellow
    $maxAttempts = 30
    $attempt = 0

    while ($attempt -lt $maxAttempts) {
        $healthStatus = docker inspect --format='{{.State.Health.Status}}' $ServiceName 2>$null

        if ($healthStatus -eq "healthy") {
            Write-Host "✓ $ServiceName is healthy" -ForegroundColor Green
            return $true
        }

        Start-Sleep -Seconds 2
        $attempt++
    }

    Write-Host "✗ $ServiceName failed to become healthy" -ForegroundColor Red
    return $false
}

# Step 1: Restart PostgreSQL
Write-Host "Step 1: Restarting PostgreSQL..." -ForegroundColor Cyan
if (!(Restart-Service "industrial-postgres")) { exit 1 }
Write-Host ""

# Step 2: Restart Redis
Write-Host "Step 2: Restarting Redis..." -ForegroundColor Cyan
if (!(Restart-Service "industrial-redis")) { exit 1 }
Write-Host ""

# Step 3: Restart Mosquitto MQTT Broker
Write-Host "Step 3: Restarting Mosquitto MQTT Broker..." -ForegroundColor Cyan
if (!(Restart-Service "industrial-mosquitto")) { exit 1 }
Write-Host ""

# Step 4: Restart Core API
Write-Host "Step 4: Restarting Core API..." -ForegroundColor Cyan
if (!(Restart-Service "industrial-core-api")) { exit 1 }
Write-Host ""

# Step 5: Restart Driver Manager
Write-Host "Step 5: Restarting Driver Manager..." -ForegroundColor Cyan
if (!(Restart-Service "industrial-driver-manager")) { exit 1 }
Write-Host ""

# Step 6: Restart Engine Historian
Write-Host "Step 6: Restarting Engine Historian..." -ForegroundColor Cyan
if (!(Restart-Service "industrial-engine-historian")) { exit 1 }
Write-Host ""

# Step 7: Restart Web UI
Write-Host "Step 7: Restarting Web UI..." -ForegroundColor Cyan
if (!(Restart-Service "industrial-web-ui")) { exit 1 }
Write-Host ""

Write-Host "================================" -ForegroundColor Cyan
Write-Host "✓ Post-Restore completed successfully!" -ForegroundColor Green
Write-Host ""
Write-Host "🌐 Web UI: http://localhost:3000" -ForegroundColor Cyan
Write-Host "📊 API: http://localhost:8081" -ForegroundColor Cyan
Write-Host ""
Write-Host "💡 Tip: Wait 30-60 seconds for all drivers to reconnect to PLCs" -ForegroundColor Yellow
