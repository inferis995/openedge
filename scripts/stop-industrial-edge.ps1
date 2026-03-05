# Industrial Edge - Graceful Shutdown Script

param(
    [switch]$StopDocker
)

$ErrorActionPreference = "Stop"
$ProjectPath = Split-Path -Parent $PSScriptRoot
$LogFile = Join-Path $ProjectPath "logs\startup.log"

function Write-Log {
    param([string]$Message)
    $timestamp = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
    $logEntry = "[$timestamp] $Message"
    Write-Host $logEntry
    if (Test-Path (Split-Path $LogFile)) {
        Add-Content -Path $LogFile -Value $logEntry
    }
}

Write-Log "=========================================="
Write-Log "Industrial Edge - Shutting down"
Write-Log "=========================================="

Set-Location $ProjectPath

Write-Log "Stopping Docker Compose services..."
docker compose down --timeout 30 2>&1 | ForEach-Object { Write-Log $_ }

if ($StopDocker) {
    Write-Log "Stopping Docker Desktop..."
    Stop-Process -Name "Docker Desktop" -Force -ErrorAction SilentlyContinue
}

Write-Log "Shutdown completed!"
