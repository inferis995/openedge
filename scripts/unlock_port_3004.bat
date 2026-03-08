@echo off
echo ===================================================
echo [!] Resetting Windows NAT Service to unlock port 3004
echo [!] This script must be run as Administrator
echo ===================================================

echo Stopping WinNAT Service...
net stop winnat

echo Starting WinNAT Service...
net start winnat

echo ===================================================
echo [!] Services restarted.
echo [!] Attempting to restart industrial-web-ui container...
echo ===================================================

docker restart industrial-web-ui

echo.
echo [SUCCESS] If no errors appeared above, try accessing http://localhost:3004
echo If it failed, please ensure you ran this as Administrator.
pause
