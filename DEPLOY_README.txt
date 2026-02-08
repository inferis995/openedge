INDUSTRIAL EDGE MIDDLEWARE - GUIDA ALL'INSTALLAZIONE
======================================================

REQUISITI
---------
1. Sistema Operativo: Windows 10/11 Professional
2. Docker Desktop installato e avviato (Icona della balena verde in basso a destra).

INSTALLAZIONE DA ZERO (Nuovo PC)
--------------------------------
1. Copia l'intera cartella del progetto sul nuovo PC (es. in C:\IndustrialEdge).
2. Fai doppio click sul file "setup.bat".
3. Attendi il caricamento (ci vorranno circa 5-10 minuti la prima volta).
4. Alla fine, si aprirà un messaggio di conferma.

ACCESSO
-------
Web UI: Apri Chrome/Edge e vai su http://localhost:3004
Utente: admin
Password: admin123

OPERAZIONI COMUNI
-----------------
- AVVIARE: Fare doppio click su "start.bat"
- FERMARE: Fare click destro su Docker -> Quit, oppure apri il terminale e scrivi "docker-compose down"
- RESET TOTALE (CANCELLA TUTTI I DATI!): Fai doppio click su "reset_factory.bat". Usare con cautela.

TROUBLESHOOTING
---------------
Se i dati non arrivano (Qualità BAD):
- Controlla che il PLC sia raggiungibile.
- Se usi un Simulatore sul PC, imposta la sua connessione su "0.0.0.0" o "Any Interface" (non localhost).
