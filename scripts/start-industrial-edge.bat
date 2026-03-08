@echo off
REM Industrial Edge - Windows Startup Script
REM This batch file calls the PowerShell startup script

cd /d "%~dp0.."
powershell.exe -ExecutionPolicy Bypass -File "%~dp0start-industrial-edge.ps1" -WaitForHealthy -TimeoutSeconds 300
