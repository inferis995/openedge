# OpenEdge — Windows kiosk setup.
#
# Configures the current user to:
#   - skip the lock screen,
#   - launch Microsoft Edge in --kiosk mode at logon pointing at the
#     OpenEdge UI URL.
#
# The "assigned access" (Shell Launcher) approach Microsoft recommends
# is only on Windows Enterprise/IoT; this script uses the cross-edition
# Registry + Run-key path so it works on Windows Pro too.
#
# Run as Administrator:
#   .\windows\install-kiosk.ps1 -Url https://openedge.local
#   .\windows\install-kiosk.ps1 -Uninstall

[CmdletBinding()]
param(
    [string]$Url = "https://openedge.local",
    [switch]$Uninstall
)

$ErrorActionPreference = "Stop"
$RegRun  = "HKCU:\Software\Microsoft\Windows\CurrentVersion\Run"
$Name    = "OpenEdgeKiosk"

if ($Uninstall) {
    if (Test-Path "$RegRun\$Name") { Remove-ItemProperty -Path $RegRun -Name $Name -ErrorAction SilentlyContinue }
    Write-Host "Kiosk autostart removed. Re-enable the Start menu / browser yourself if needed." -ForegroundColor Green
    exit 0
}

# Edge is preinstalled on Windows 10+; --kiosk is the documented flag.
$edge = "$env:ProgramFiles\Microsoft\Edge\Application\msedge.exe"
if (-not (Test-Path $edge)) {
    Write-Host "ERROR: Microsoft Edge not found at $edge." -ForegroundColor Red
    Write-Host "Install Edge (or adjust this script to point at chrome.exe)." -ForegroundColor Yellow
    exit 1
}

# --kiosk + --edge-kiosk-type=fullscreen → no toolbars, no F11 to exit.
# --no-first-run skips the welcome flow that would appear on a brand new profile.
$cmd = "`"$edge`" --kiosk `"$Url`" --edge-kiosk-type=fullscreen --no-first-run --disable-features=PreloadMediaEngagementData"

if (-not (Test-Path $RegRun)) { New-Item -Path $RegRun -Force | Out-Null }
Set-ItemProperty -Path $RegRun -Name $Name -Value $cmd

Write-Host ""
Write-Host "Kiosk autostart installed for user '$env:USERNAME'." -ForegroundColor Green
Write-Host "  URL:    $Url"
Write-Host "  Edge:   $edge"
Write-Host ""
Write-Host "Recommended companion settings on this PC:"
Write-Host "  - enable auto-login for this user (netplwiz)"
Write-Host "  - disable screen saver and lock screen (Settings > Personalization)"
Write-Host "  - disable notifications you don't want over the dashboard"
Write-Host "  - if shipped to a customer, also disable Windows Update auto-reboot"
