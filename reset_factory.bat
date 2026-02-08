@echo off
setlocal
title ALLARME - FACTORY RESET

color 4F
echo ===================================================
echo   !!! ATTENZIONE - FACTORY RESET !!!
echo ===================================================
echo.
echo QUESTA OPERAZIONE CANCELLERA' TUTTI I DATI.
echo.
echo Verranno rimossi:
echo  - Tutti gli utenti (tranne admin)
echo  - Tutte le configurazioni (Siti, Aree, Gateway)
echo  - Tutto lo storico dati
echo.
echo L'applicazione tornera' allo STATO 0 (Come nuova).
echo.
set /p confirmation="Sei SICURO? Scrivi 'SI' per procedere: "

if /i "%confirmation%" neq "SI" (
    echo Operazione annullata.
    color 07
    pause
    exit /b 0
)

echo.
echo [INFO] Stopping containers...
docker-compose down

echo [INFO] Destroying data volumes...
docker-compose down -v

echo [INFO] Clearing MQTT retained messages...
del /q ".\mosquitto\data\*" 2>nul
rmdir /s /q ".\mosquitto\data" 2>nul
mkdir ".\mosquitto\data"

echo.
echo [OK] RESET COMPLETATO.
echo Ora puoi eseguire 'setup.bat' per reinstallare da zero.
echo.
color 07
pause
