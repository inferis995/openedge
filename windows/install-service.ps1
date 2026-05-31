# OpenEdge — register the stack as a Windows Service that starts at boot.
#
# What it does (idempotent — safe to re-run):
#   1. Verifies Docker + docker compose are available.
#   2. Downloads WinSW (one ~6 MB file) if not already in this folder,
#      verifying its SHA-256.
#   3. Copies windows\openedge.xml next to it.
#   4. Patches the XML's WorkingDirectory to point at the repo root.
#   5. Installs the "OpenEdge" Windows Service (runs as LocalSystem,
#      starts at boot, restarts on failure, logs under .\logs).
#
# Run from an elevated PowerShell (Right-click PowerShell -> Run as
# Administrator), from the repo root:
#
#   PS C:\OpenEdge> Set-ExecutionPolicy -Scope Process Bypass -Force
#   PS C:\OpenEdge> .\windows\install-service.ps1
#
# Uninstall:
#   PS C:\OpenEdge> .\windows\install-service.ps1 -Uninstall

[CmdletBinding()]
param(
    [switch]$Uninstall,
    [string]$WinSWVersion = "v2.12.0",
    [string]$WinSWSha256  = "61cb55ccd9c46e6dca2fbe0fad9cd1bd57aae723d76fc1aaf9d627d7a3cbe7e3"
    # ^ pinned hash for WinSW v2.12.0 net4 build (winsw.exe). Update both
    #   $WinSWVersion and $WinSWSha256 together if you bump the release.
)

$ErrorActionPreference = "Stop"
$ServiceName    = "OpenEdge"
$RepoRoot       = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$WindowsDir     = Join-Path $RepoRoot "windows"
$ServiceExe     = Join-Path $WindowsDir "openedge-service.exe"
$ServiceXml     = Join-Path $WindowsDir "openedge-service.xml"
$XmlTemplate    = Join-Path $WindowsDir "openedge.xml"
$LogsDir        = Join-Path $RepoRoot "logs"

function Assert-Admin {
    $current = [Security.Principal.WindowsPrincipal]::new(
        [Security.Principal.WindowsIdentity]::GetCurrent())
    if (-not $current.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        Write-Host "ERROR: must be run as Administrator." -ForegroundColor Red
        exit 1
    }
}

function Stop-AndRemoveService {
    if (Get-Service -Name $ServiceName -ErrorAction SilentlyContinue) {
        if ((Get-Service $ServiceName).Status -ne 'Stopped') {
            Write-Host "Stopping service $ServiceName ..."
            Stop-Service $ServiceName -Force -ErrorAction SilentlyContinue
            # Wait up to 30s for graceful stop.
            for ($i=0; $i -lt 30; $i++) {
                if ((Get-Service $ServiceName).Status -eq 'Stopped') { break }
                Start-Sleep -Seconds 1
            }
        }
        # `sc.exe delete` works even if the service binary is gone.
        & sc.exe delete $ServiceName | Out-Null
        Start-Sleep -Seconds 2
    }
}

# ---------------------------------------------------------------------------
Assert-Admin

if ($Uninstall) {
    Write-Host "Uninstalling Windows service '$ServiceName' ..."
    Stop-AndRemoveService
    Remove-Item -Force $ServiceExe, $ServiceXml -ErrorAction SilentlyContinue
    Write-Host "Done. (logs under '$LogsDir' preserved.)" -ForegroundColor Green
    exit 0
}

# ---------------------------------------------------------------------------
# 1. Prereq check.
$dockerCmd = Get-Command docker -ErrorAction SilentlyContinue
if (-not $dockerCmd) {
    Write-Host "ERROR: docker not found in PATH. Install Docker Desktop or Docker Engine." -ForegroundColor Red
    exit 1
}
& docker compose version *>$null
if ($LASTEXITCODE -ne 0) {
    Write-Host "ERROR: 'docker compose' v2 plugin not installed." -ForegroundColor Red
    exit 1
}

# 2. Ensure WinSW is present, downloading + verifying if necessary.
if (-not (Test-Path $ServiceExe)) {
    $url  = "https://github.com/winsw/winsw/releases/download/$WinSWVersion/WinSW.NET4.exe"
    Write-Host "Downloading WinSW $WinSWVersion ..."
    try {
        Invoke-WebRequest -Uri $url -OutFile $ServiceExe -UseBasicParsing
    } catch {
        Write-Host "ERROR: failed to download $url" -ForegroundColor Red
        Write-Host "If this host is offline, download manually on another machine and copy to:" -ForegroundColor Yellow
        Write-Host "  $ServiceExe" -ForegroundColor Yellow
        exit 1
    }
    $actual = (Get-FileHash $ServiceExe -Algorithm SHA256).Hash.ToLower()
    if ($actual -ne $WinSWSha256.ToLower()) {
        Remove-Item $ServiceExe -Force
        Write-Host "ERROR: SHA-256 mismatch for WinSW download. Expected $WinSWSha256, got $actual." -ForegroundColor Red
        Write-Host "Refusing to install an unverified binary. Update the pinned hash if WinSW released a new build." -ForegroundColor Yellow
        exit 1
    }
    Write-Host "WinSW downloaded and verified." -ForegroundColor Green
}

# 3. Generate the per-install XML by patching the WorkingDirectory.
if (-not (Test-Path $XmlTemplate)) {
    Write-Host "ERROR: $XmlTemplate not found (run from a fresh checkout)." -ForegroundColor Red
    exit 1
}
$xml = Get-Content $XmlTemplate -Raw
$xml = $xml -replace '<workingdirectory>%BASE%</workingdirectory>',
                     "<workingdirectory>$RepoRoot</workingdirectory>"
$xml | Set-Content -Path $ServiceXml -Encoding UTF8

# 4. Make sure the logs dir exists (WinSW won't create parent dirs).
New-Item -ItemType Directory -Force -Path $LogsDir | Out-Null

# 5. (Re)install the service.
Stop-AndRemoveService
Write-Host "Installing Windows service '$ServiceName' ..."
& $ServiceExe install
if ($LASTEXITCODE -ne 0) {
    Write-Host "ERROR: WinSW install failed (exit $LASTEXITCODE)." -ForegroundColor Red
    exit 1
}

# Start now so the operator sees immediate result.
Write-Host "Starting service ..."
& $ServiceExe start
if ($LASTEXITCODE -ne 0) {
    Write-Host "WARN: service installed but failed to start. Check '$LogsDir\openedge.err.log'." -ForegroundColor Yellow
    exit 0
}

Write-Host ""
Write-Host "OpenEdge installed as Windows Service." -ForegroundColor Green
Write-Host "  status:    Get-Service $ServiceName"
Write-Host "  logs:      Get-Content '$LogsDir\openedge.out.log' -Wait"
Write-Host "  restart:   Restart-Service $ServiceName"
Write-Host "  remove:    .\windows\install-service.ps1 -Uninstall"
