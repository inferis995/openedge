@echo off
setlocal
title Industrial Edge Middleware - Setup & Install

echo ===================================================
echo   INDUSTRIAL EDGE MIDDLEWARE - SETUP
echo ===================================================
echo.

:: 1. Check if Docker is running
docker info >nul 2>&1
if %errorlevel% neq 0 (
    echo [ERROR] Docker is NOT running!
    echo Please start Docker Desktop and try again.
    pause
    exit /b 1
)
echo [OK] Docker is running.
echo.

:: 2. Build Core Infrastructure
echo [INFO] Building Core Services (API, Web UI, databases)...
docker-compose build
if %errorlevel% neq 0 (
    echo [ERROR] Failed to build core services.
    pause
    exit /b 1
)
echo [OK] Core services built.
echo.

:: 3. Build Drivers (CRITICAL STEP)
echo [INFO] Building Driver Images...
echo   - Modbus Driver...
docker-compose build driver-modbus
if %errorlevel% neq 0 (
    echo [ERROR] Failed to build driver-modbus.
    pause
    exit /b 1
)

echo   - S7 Driver...
docker-compose build driver-s7
if %errorlevel% neq 0 (
    echo [ERROR] Failed to build driver-s7.
    pause
    exit /b 1
)

echo   - Redis Driver...
docker-compose build driver-redis
if %errorlevel% neq 0 (
    echo [ERROR] Failed to build driver-redis.
    pause
    exit /b 1
)
echo [OK] All drivers built successfully.
echo.

:: 4. Start Application
echo [INFO] Starting Application...
docker-compose up -d

echo.
echo ===================================================
echo   INSTALLATION COMPLETE
echo ===================================================
echo.
echo The application is running.
echo Web UI: http://localhost:3004
echo.
echo Default Credentials:
echo   Username: admin
echo   Password: admin123
echo.
pause
