# OpenEdge Industrial Edge Middleware - Windows Startup Script
# This script builds all images (including driver images) then starts all services.

param(
    [switch]$Build = $false,
    [switch]$Clean = $false,
    [switch]$Stop = $false,
    [switch]$Logs = $false
)

$ErrorActionPreference = "Stop"
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$RootDir = Split-Path -Parent $ScriptDir

Set-Location $RootDir

function Write-Header {
    param([string]$Message)
    Write-Host ""
    Write-Host "=================================================" -ForegroundColor Cyan
    Write-Host "  $Message" -ForegroundColor Cyan
    Write-Host "=================================================" -ForegroundColor Cyan
}

function Test-DockerRunning {
    try {
        docker info 2>&1 | Out-Null
        return $true
    } catch {
        return $false
    }
}

# Handle flags
if ($Stop) {
    Write-Header "Stopping OpenEdge"
    docker-compose down
    Write-Host "All services stopped." -ForegroundColor Green
    exit 0
}

if ($Logs) {
    docker-compose logs -f
    exit 0
}

if ($Clean) {
    Write-Header "Full Reset (deletes all data!)"
    $confirm = Read-Host "This will DELETE all data. Type 'yes' to confirm"
    if ($confirm -ne "yes") { Write-Host "Aborted."; exit 0 }
    docker-compose down -v
    Write-Host "Done. Run this script again to start fresh." -ForegroundColor Green
    exit 0
}

# --- Main startup ---
Write-Header "OpenEdge Industrial Edge Middleware"

if (-not (Test-DockerRunning)) {
    Write-Host "ERROR: Docker is not running. Please start Docker Desktop first." -ForegroundColor Red
    exit 1
}

Write-Host "Step 1/2: Building all images (main services + driver images)..." -ForegroundColor Yellow
Write-Host "This may take several minutes on first run." -ForegroundColor Gray
docker-compose -f docker-compose.yml -f docker-compose.build.yml build
if ($LASTEXITCODE -ne 0) {
    Write-Host "ERROR: Build failed. Check the output above." -ForegroundColor Red
    exit 1
}

Write-Host "" 
Write-Host "Step 2/2: Starting services..." -ForegroundColor Yellow
docker-compose up -d
if ($LASTEXITCODE -ne 0) {
    Write-Host "ERROR: Failed to start services." -ForegroundColor Red
    exit 1
}

Write-Header "OpenEdge Started Successfully"
Write-Host "  Web UI:   http://localhost:3000" -ForegroundColor Green
Write-Host "  Core API: http://localhost:8081" -ForegroundColor Green
Write-Host "  Login:    admin / admin123" -ForegroundColor Green
Write-Host ""
Write-Host "Tip: It may take 30-60 seconds for all services to become healthy."
Write-Host "Run '.\scripts\start-industrial-edge.ps1 -Logs' to follow the logs."
