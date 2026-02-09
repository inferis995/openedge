@echo off
echo ===================================================
echo [!] AVVIO SICURO APPLICAZIONE INDUSTRIAL EDGE
echo [!] Eseguire questo script come Amministratore
echo ===================================================

echo [1/4] Arresto servizi Docker esistenti...
docker-compose down

echo [2/4] Reset servizi di rete Windows (sblocco porta 3004)...
net stop winnat
net start winnat

echo [3/4] Avvio di TUTTI i container (incluso InfluxDB)...
docker-compose up -d

echo [4/4] Attesa avvio servizi (15 secondi)...
timeout /t 15 /nobreak

echo.
echo [OK] L'applicazione e' pronta!
echo Accedi a: http://localhost:3004
echo Username: admin
echo Password: admin123
echo.
pause
