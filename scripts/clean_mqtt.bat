@echo off
title Clear MQTT Retained Messages

echo ===================================================
echo   CLEAR MQTT RETAINED MESSAGES
echo ===================================================
echo.
echo This will stop Mosquitto, delete its database file,
echo and restart it. This clears ALL retained messages.
echo.
echo Real-time data will reappear as soon as drivers publish it.
echo.

set /p confirmation="Are you sure? (Y/N): "
if /i "%confirmation%" neq "Y" exit /b

echo.
echo [INFO] Stopping Mosquitto...
docker-compose stop mosquitto

echo [INFO] Removing persistence file...
docker-compose run --rm mosquitto sh -c "rm -f /mosquitto/data/mosquitto.db"

echo [INFO] Starting Mosquitto...
docker-compose start mosquitto

echo.
echo [OK] MQTT Cleared.
pause
