@echo off
setlocal enabledelayedexpansion
cd /d "%~dp0"

:: ─────────────────────────────────────────────────────────────────────────────
::  OpenEdge Industrial Edge Middleware — Windows management script
::  Usage:  openedge.bat [start|stop|restart|logs|status|clean]
::          Double-click to open the interactive menu.
:: ─────────────────────────────────────────────────────────────────────────────

if "%~1"==""         goto :menu
if /i "%~1"=="start"   goto :do_start
if /i "%~1"=="stop"    goto :do_stop
if /i "%~1"=="restart" goto :do_restart
if /i "%~1"=="logs"    goto :do_logs
if /i "%~1"=="status"  goto :do_status
if /i "%~1"=="clean"   goto :do_clean
echo Unknown command: %~1
goto :usage

:: ── Interactive menu ────────────────────────────────────────────────────────

:menu
cls
echo.
echo   ╔══════════════════════════════════════════════╗
echo   ║     OpenEdge Industrial Edge Middleware      ║
echo   ╠══════════════════════════════════════════════╣
echo   ║  1. Start    (build + launch all services)   ║
echo   ║  2. Stop                                     ║
echo   ║  3. Restart                                  ║
echo   ║  4. Logs     (live, Ctrl+C to exit)          ║
echo   ║  5. Status                                   ║
echo   ║  6. Clean    (WARNING: deletes all data)     ║
echo   ║  7. Exit                                     ║
echo   ╚══════════════════════════════════════════════╝
echo.
set /p "choice=  Select [1-7]: "
if "%choice%"=="1" goto :do_start
if "%choice%"=="2" goto :do_stop
if "%choice%"=="3" goto :do_restart
if "%choice%"=="4" goto :do_logs
if "%choice%"=="5" goto :do_status
if "%choice%"=="6" goto :do_clean
if "%choice%"=="7" exit /b 0
goto :menu

:: ── Helpers ─────────────────────────────────────────────────────────────────

:check_docker
docker info >nul 2>&1
if errorlevel 1 (
    echo.
    echo  ERROR: Docker is not running.
    echo         Please start Docker Desktop and try again.
    echo.
    pause
    exit /b 1
)
exit /b 0

:setup_env
:: Uses PowerShell (built into Windows 7+) only for cryptographic random generation.
:: Creates .env from .env.example if missing; generates JWT_SECRET if unset or placeholder.
powershell -NoProfile -ExecutionPolicy Bypass -Command ^
    "$envFile = '.env'; $envExample = '.env.example';" ^
    "if (-not (Test-Path $envFile)) { Copy-Item $envExample $envFile; Write-Host '[setup] Created .env from .env.example' };" ^
    "$content = Get-Content $envFile -Raw;" ^
    "$needsSecret = (-not ($content -match '(?m)^JWT_SECRET=')) -or ($content -match '(?m)^JWT_SECRET=CHANGE_ME');" ^
    "if ($needsSecret) {" ^
    "  $bytes = New-Object byte[] 32;" ^
    "  [System.Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($bytes);" ^
    "  $secret = ($bytes | ForEach-Object { $_.ToString('x2') }) -join '';" ^
    "  $lines = Get-Content $envFile | Where-Object { $_ -notmatch '^JWT_SECRET=' };" ^
    "  ($lines + \"JWT_SECRET=$secret\") | Set-Content $envFile;" ^
    "  Write-Host '[setup] Generated JWT_SECRET and saved to .env'" ^
    "}"
exit /b 0

:: ── Commands ────────────────────────────────────────────────────────────────

:do_start
call :check_docker || exit /b 1
echo.
echo  [1/3] Configuring environment...
call :setup_env
if errorlevel 1 ( echo  ERROR: Environment setup failed. & pause & exit /b 1 )
echo.
echo  [2/3] Building images (first run may take several minutes)...
docker-compose -f docker-compose.yml -f docker-compose.build.yml build
if errorlevel 1 ( echo  ERROR: Build failed. Check the output above. & pause & exit /b 1 )
echo.
echo  [3/3] Starting services...
docker-compose up -d
if errorlevel 1 ( echo  ERROR: Failed to start services. & pause & exit /b 1 )
echo.
echo  ┌─────────────────────────────────────────────┐
echo  │  OpenEdge is running!                       │
echo  │                                             │
echo  │  Web UI:   http://localhost:3000            │
echo  │  Core API: http://localhost:8081            │
echo  │  Login:    admin / admin123                 │
echo  │                                             │
echo  │  Wait ~30s for all services to be healthy.  │
echo  │  Change the default password after login.   │
echo  └─────────────────────────────────────────────┘
echo.
pause
exit /b 0

:do_stop
call :check_docker || exit /b 1
echo  Stopping all services...
docker-compose down
echo  Done.
echo.
pause
exit /b 0

:do_restart
call :check_docker || exit /b 1
echo  Restarting services...
call :setup_env
docker-compose down
docker-compose up -d
if errorlevel 1 ( echo  ERROR: Failed to restart services. & pause & exit /b 1 )
echo  Done. Services are back up.
echo.
pause
exit /b 0

:do_logs
docker-compose logs -f
exit /b 0

:do_status
echo.
docker-compose ps
echo.
pause
exit /b 0

:do_clean
call :check_docker || exit /b 1
echo.
echo  ╔══════════════════════════════════════════════╗
echo  ║  WARNING — This will DELETE all data:        ║
echo  ║  database, tag history, alarms, backups.     ║
echo  ║  This action is IRREVERSIBLE.                ║
echo  ╚══════════════════════════════════════════════╝
echo.
set /p "confirm=  Type YES to confirm: "
if not "%confirm%"=="YES" ( echo  Aborted. & pause & exit /b 0 )
docker-compose down -v
echo  All data deleted. Run 'openedge.bat start' to begin fresh.
echo.
pause
exit /b 0

:usage
echo.
echo  Usage: openedge.bat [start^|stop^|restart^|logs^|status^|clean]
echo         Run without arguments to open the interactive menu.
echo.
pause
exit /b 0
