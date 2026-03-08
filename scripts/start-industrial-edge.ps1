# Industrial Edge - Auto-start Script for Windows
# This script ensures all Docker services are running

param(
    [switch]$WaitForHealthy,
    [int]$TimeoutSeconds = 300
)

$ErrorActionPreference = "Stop"
$ProjectPath = Split-Path -Parent $PSScriptRoot
$LogFile = Join-Path $ProjectPath "logs\startup.log"

# Ensure log directory exists
$logDir = Split-Path $LogFile -Parent
if (-not (Test-Path $logDir)) {
    New-Item -ItemType Directory -Path $logDir -Force | Out-Null
}

function Write-Log {
    param([string]$Message)
    $timestamp = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
    $logEntry = "[$timestamp] $Message"
    Write-Host $logEntry
    Add-Content -Path $LogFile -Value $logEntry
}

function Test-DockerRunning {
    try {
        docker info | Out-Null
        return $true
    } catch {
        return $false
    }
}

function Start-DockerIfNotRunning {
    if (Test-DockerRunning) {
        Write-Log "Docker is already running"
        return $true
    }

    Write-Log "Starting Docker Desktop..."

    # Try to start Docker Desktop
    $dockerPaths = @(
        "${env:ProgramFiles}\Docker\Docker\Docker Desktop.exe",
        "${env:LOCALAPPDATA}\Docker\Docker Desktop.exe"
    )

    foreach ($path in $dockerPaths) {
        if (Test-Path $path) {
            Start-Process $path
            break
        }
    }

    # Wait for Docker to start
    $maxWait = 120
    $waited = 0
    while (-not (Test-DockerRunning) -and $waited -lt $maxWait) {
        Start-Sleep -Seconds 5
        $waited += 5
        Write-Log "Waiting for Docker to start... ($waited/$maxWait seconds)"
    }

    if (Test-DockerRunning) {
        Write-Log "Docker started successfully"
        return $true
    } else {
        Write-Log "ERROR: Docker failed to start within $maxWait seconds"
        return $false
    }
}

function Wait-ServicesHealthy {
    param([int]$Timeout)

    Write-Log "Waiting for all services to become healthy (timeout: ${Timeout}s)..."

    $services = @(
        "industrial-postgres",
        "industrial-redis",
        "industrial-mosquitto",
        "industrial-core-api",
        "industrial-web-ui",
        "industrial-engine-historian",
        "industrial-driver-manager"
    )

    $startTime = Get-Date
    $allHealthy = $false

    while (-not $allHealthy) {
        $elapsed = ((Get-Date) - $startTime).TotalSeconds
        if ($elapsed -gt $Timeout) {
            Write-Log "ERROR: Timeout waiting for services to become healthy"
            return $false
        }

        $allHealthy = $true
        $statusReport = @()

        foreach ($service in $services) {
            $status = docker inspect --format='{{.State.Health.Status}}' $service 2>$null
            if ($status -ne "healthy") {
                $allHealthy = $false
                $statusReport += "$service`: $status"
            }
        }

        if (-not $allHealthy) {
            Write-Log "Services status: $($statusReport -join ', ')"
            Start-Sleep -Seconds 5
        }
    }

    Write-Log "All services are healthy!"
    return $true
}

# Main execution
Write-Log "=========================================="
Write-Log "Industrial Edge - Starting up"
Write-Log "=========================================="

# Step 1: Ensure Docker is running
if (-not (Start-DockerIfNotRunning)) {
    Write-Log "FATAL: Cannot start Docker"
    exit 1
}

# Step 2: Navigate to project directory
Set-Location $ProjectPath
Write-Log "Project directory: $ProjectPath"

# Step 3: Pull and start all services
Write-Log "Starting Docker Compose services..."
docker compose up -d --pull always 2>&1 | ForEach-Object { Write-Log $_ }

# Step 4: Wait for healthy status if requested
if ($WaitForHealthy) {
    if (-not (Wait-ServicesHealthy -Timeout $TimeoutSeconds)) {
        Write-Log "WARNING: Some services may not be fully healthy"
    }
}

# Step 5: Show status
Write-Log "=========================================="
Write-Log "Services Status:"
docker ps --format "table {{.Names}}`t{{.Status}}`t{{.Ports}}" 2>&1 | ForEach-Object { Write-Log $_ }
Write-Log "=========================================="
Write-Log "Web UI: http://localhost:9090"
Write-Log "API: http://localhost:8081"
Write-Log "Default login: admin / admin123"
Write-Log "=========================================="

Write-Log "Startup completed successfully!"
