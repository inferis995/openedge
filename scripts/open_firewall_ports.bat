@echo off
echo ========================================================
echo   Industrial Edge - Firewall Configuration Utility
echo ========================================================
echo.
echo This script opens the necessary ports in Windows Firewall
echo to allow external access to the Industrial Edge platform.
echo.
echo Ports to open:
echo   - 3004 (Web UI)
echo   - 1883 (MQTT Broker)
echo   - 8081 (Core API - Optional direct access)
echo.
echo NOTE: This script must be run as Administrator!
echo.
pause

echo.
echo [1/3] Allow Web UI (Port 3004)...
netsh advfirewall firewall add rule name="Industrial Edge Web UI" dir=in action=allow protocol=TCP localport=3004 profile=any
if %errorlevel% neq 0 (
    echo FAILED. Please run as Administrator.
    pause
    exit /b
) else (
    echo OK.
)

echo.
echo [2/3] Allow MQTT Broker (Port 1883)...
netsh advfirewall firewall add rule name="Industrial Edge MQTT" dir=in action=allow protocol=TCP localport=1883 profile=any
echo OK.

echo.
echo [3/3] Allow Core API (Port 8081)...
netsh advfirewall firewall add rule name="Industrial Edge Core API" dir=in action=allow protocol=TCP localport=8081 profile=any
echo OK.

echo.
echo ========================================================
echo   Configuration Complete!
echo ========================================================
echo You should now be able to access the Web UI from other devices
echo using this computer's IP address (e.g., http://192.168.1.X:3004).
echo.
pause
