@echo off
title Start App
echo Starting Industrial Edge Middleware...
docker-compose up -d
echo.
echo [OK] Application Started via Docker.
timeout /t 5
