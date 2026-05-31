# Industrial Edge - Setup Windows Task Scheduler for Auto-Start
#
# DEPRECATED for industrial / unattended use: this script registers a
# Task Scheduler trigger that fires "at user logon" — it needs a user
# to log in for the stack to come up, which is wrong for a headless
# industrial PC that may reboot unattended.
#
# For production use prefer:
#     .\windows\install-service.ps1
# which registers a proper Windows Service that starts at boot before
# any login. See windows\README.md for details.
#
# This script stays for developer-laptop convenience.

param(
    [switch]$Uninstall
)

$ErrorActionPreference = "Stop"
$TaskName = "IndustrialEdge-AutoStart"
$ProjectPath = Split-Path -Parent $PSScriptRoot
$ScriptPath = Join-Path $PSScriptRoot "start-industrial-edge.bat"

if ($Uninstall) {
    Write-Host "Removing scheduled task..."
    Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false -ErrorAction SilentlyContinue
    Write-Host "Task removed successfully!"
    exit 0
}

# Check if running as admin
$isAdmin = ([Security.Principal.WindowsPrincipal] [Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $isAdmin) {
    Write-Host "ERROR: This script must be run as Administrator!" -ForegroundColor Red
    Write-Host "Right-click PowerShell and select 'Run as Administrator'" -ForegroundColor Yellow
    exit 1
}

# Create the scheduled task
Write-Host "Creating scheduled task: $TaskName"

# Task trigger - At log on
$trigger = New-ScheduledTaskTrigger -AtLogOn

# Task action - Run the startup script
$action = New-ScheduledTaskAction -Execute $ScriptPath -WorkingDirectory (Split-Path $ScriptPath)

# Task settings
$settings = New-ScheduledTaskSettingsSet `
    -AllowStartIfOnBatteries `
    -DontStopIfGoingOnBatteries `
    -StartWhenAvailable `
    -RunOnlyIfNetworkAvailable `
    -ExecutionTimeLimit (New-TimeSpan -Minutes 30)

# Task principal - Run as current user
$principal = New-ScheduledTaskPrincipal -UserId $env:USERNAME -LogonType Interactive -RunLevel Highest

# Register the task
Register-ScheduledTask `
    -TaskName $TaskName `
    -Trigger $trigger `
    -Action $action `
    -Settings $settings `
    -Principal $principal `
    -Description "Automatically starts Industrial Edge Docker services at user logon" `
    -Force

Write-Host ""
Write-Host "=========================================="
Write-Host "Task created successfully!" -ForegroundColor Green
Write-Host "=========================================="
Write-Host ""
Write-Host "The Industrial Edge services will now start automatically when you log in to Windows."
Write-Host ""
Write-Host "To start services immediately, run:"
Write-Host "  $ScriptPath"
Write-Host ""
Write-Host "To remove the auto-start task, run:"
Write-Host "  powershell -ExecutionPolicy Bypass -File `$PSScriptRoot\install-autostart.ps1 -Uninstall"
Write-Host ""
