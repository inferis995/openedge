---
name: openedge-ops
description: OpenEdge Operations — deploy, container fix, gateway/tag import via shell and API
version: 1.0.0
tags: [industrial, iot, devops, docker, deploy, import, scada]
requires: [docker, docker-compose, make, curl, git]
---

# OpenEdge Ops — Skill

Skill operativa per installare, deployare, fixare e configurare un'istanza OpenEdge.
Richiede accesso shell al server (docker, make, curl).

Per il monitoraggio dati usa la skill `openedge.md` (sola lettura, no shell).

## Compatibilità

- **Claude Code** — `.claude/skills/openedge-ops.md`
- **OpenClaw** — `~/.openclaw/skills/openedge-ops/SKILL.md`

---

## Variabili d'ambiente attese

```bash
OPENEDGE_DIR=/home/user/openedge   # path repo locale
OPENEDGE_HOST=localhost
OPENEDGE_PORT=8081
OPENEDGE_USERNAME=admin
OPENEDGE_PASSWORD=admin123
OPENEDGE_ORG_ID=1
```

---

## 1. Deploy — Prima installazione

Segui questi passi **nell'ordine esatto**. Non procedere al passo successivo
se il precedente non è verificato con successo.

### Passo 1 — Clona il repository

```bash
git clone https://github.com/inferis995/openedge.git
cd openedge
```

### Passo 1b — Configura .env (percorsi dati, credenziali e JWT secret)

```bash
cp .env.example .env
```

#### ⚠️ OBBLIGATORIO — Genera e imposta JWT_SECRET

Il backend **si rifiuta di avviarsi** se `JWT_SECRET` non è presente nel `.env`.
Genera il secret e aggiungilo al file:

```bash
# Linux / Mac
echo "JWT_SECRET=$(openssl rand -hex 32)" >> .env

# Windows (PowerShell)
Add-Content .env "JWT_SECRET=$(-join ((48..57+65..90+97..122) | Get-Random -Count 64 | % {[char]$_}))"
```

Verifica che sia presente:
```bash
grep JWT_SECRET .env
# Atteso: JWT_SECRET=<stringa lunga 64 caratteri hex>
```

> Non usare mai il valore di esempio (`CHANGE_ME_...`) in produzione.
> Se il backend si avvia con il valore di esempio si tratta di un `.env` non configurato correttamente.

#### Percorsi dati (PostgreSQL e Redis)

Scegli dove salvare i dati storici. Se non imposti nulla finiscono in `./data/` dentro la cartella del repo.

```bash
# Default — ok per test/sviluppo
POSTGRES_DATA_PATH=./data/postgres
REDIS_DATA_PATH=./data/redis

# Linux/Mac — percorso assoluto consigliato per produzione
POSTGRES_DATA_PATH=/opt/openedge/data/postgres
REDIS_DATA_PATH=/opt/openedge/data/redis

# Windows — altra unità
POSTGRES_DATA_PATH=D:/openedge-data/postgres
REDIS_DATA_PATH=D:/openedge-data/redis
```

> ⚠️ **Imposta i percorsi PRIMA di `make start`.**
> Cambiarli dopo che i dati esistono richiede backup + restore.

Opzionale: cambia password e credenziali nel blocco `DATABASE CONFIGURATION` di `.env`.

### Passo 2 — Build + avvio con make start

```bash
make start
```

`make start` esegue in sequenza:
1. Build di tutte le immagini Docker (core-api, web-ui, driver-manager, engine-historian)
2. Build delle 5 immagini driver (Modbus, S7, OPC UA, MQTT, Redis) ← **necessarie per i gateway**
3. Avvio di tutti i servizi
4. Applicazione delle migration DB

**Non usare `docker-compose up -d` — salta il build delle immagini driver.**

### Passo 3 — Verifica immagini driver (OBBLIGATORIO prima di creare gateway)

```bash
docker images | grep industrial-driver
```

L'output deve mostrare **tutte e 5** queste immagini:

```
industrial-driver-modbus    latest   ...
industrial-driver-s7        latest   ...
industrial-driver-opcua     latest   ...
industrial-driver-mqtt      latest   ...
industrial-driver-redis     latest   ...
```

Se mancano alcune immagini, NON procedere alla creazione del gateway.
Esegui prima:

```bash
docker-compose -f docker-compose.yml -f docker-compose.build.yml build
```

Poi ricontrolla con `docker images | grep industrial-driver`.

### Passo 4 — Verifica sistema operativo

```bash
curl http://localhost:8081/ready
```

Atteso: `{"status":"ready","db":"ok","redis":"ok"}`

Se risponde con errore, attendi 30 secondi e riprova (i container stanno ancora avviandosi).

### Passo 5 — Solo ora puoi creare gateway

Apri `http://localhost:3000` — Login: `admin / admin123`

Crea un gateway dalla UI. Il driver-manager avvierà automaticamente
il container driver corretto (es. `industrial-driver-modbus`) non appena
il gateway viene salvato.

Verifica che il driver sia partito:

```bash
docker ps | grep driver
# Deve comparire: openedge-driver-modbus-<gateway_id>   Up X seconds
```

Verifica stato connessione via API:

```bash
TOKEN=$(curl -s -X POST http://localhost:8081/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin123"}' \
  | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])")

curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 1" \
  http://localhost:8081/api/gateways
# connection_status: "online"  → driver connesso al PLC ✓
# connection_status: "offline" → driver partito ma PLC non raggiungibile
# connection_status: "unknown" → driver sta ancora avviandosi, attendi
```

---

## 2. Comandi Makefile

```bash
make start    # Prima volta: build + avvio (raccomandato)
make up       # Avvia servizi (immagini già buildate)
make down     # Ferma tutti i servizi
make restart  # Stop + start
make logs     # Segui i log in tempo reale
make clean    # Stop + cancella tutti i dati (reset completo)

make migrate-status  # Stato migration DB
make migrate         # Applica migration pendenti
make migrate-down    # Rollback ultima migration
```

---

## 3. Diagnostica container

### Stato tutti i container
```bash
docker-compose ps
docker ps --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}" | grep industrial
```

### Logs di un servizio
```bash
docker-compose logs -f core-api
docker-compose logs -f postgres
docker-compose logs -f driver-manager
docker-compose logs --tail 50 <service>
```

### Container crashati / restart loop
```bash
# Identifica il container problematico
docker ps -a --format "table {{.Names}}\t{{.Status}}" | grep industrial

# Leggi logs dell'ultimo crash
docker logs industrial-core-api --tail 100

# Restart singolo servizio
docker-compose restart core-api
docker restart industrial-core-api
```

### Health check API
```bash
curl http://localhost:8081/health   # {"status":"ok"} — server risponde
curl http://localhost:8081/ready    # {"status":"ready","db":"ok","redis":"ok"} — tutto up
```

---

## 4. Fix problemi comuni

### Problema: core-api non si avvia — "JWT_SECRET environment variable is required"

Il backend rifiuta di partire se `JWT_SECRET` non è presente nel `.env`.

```bash
# Verifica se è presente
grep JWT_SECRET .env

# Se manca o è ancora il valore di esempio (CHANGE_ME_...), genera e aggiungi:
echo "JWT_SECRET=$(openssl rand -hex 32)" >> .env

# Riavvia il backend
docker-compose restart core-api

# Controlla che ora parta
docker-compose logs core-api | tail -20
# Deve comparire: "[AUTH] JWT secret key loaded from environment"
```

---

### Problema: gateway creato dalla UI ma il driver non parte

Sintomo: crei un gateway Modbus/S7/OPC UA dalla UI ma il container driver non appare in `docker ps`.

Causa più comune: le immagini driver non sono state buildate (hai usato `docker-compose up` invece di `make start`).

```bash
# 1. Verifica se le immagini esistono
docker images | grep industrial-driver
# Se l'output è vuoto → le immagini non ci sono → builda subito:

# 2. Builda le immagini driver
docker-compose -f docker-compose.yml -f docker-compose.build.yml build

# 3. Controlla i log del driver-manager per conferma
docker logs industrial-driver-manager --tail 30
# Cerca: "starting driver for gateway X" oppure errori "image not found"

# 4. Non serve ricreare il gateway — il driver-manager riprova automaticamente
#    dopo che l'immagine è disponibile. Attendi ~10 secondi.
docker ps | grep driver
```

### Problema: driver container in crash-loop "GATEWAY_ID required"
Causa: driver avviato manualmente invece che da driver-manager.
Soluzione: i driver NON vanno avviati manualmente — li gestisce driver-manager quando crei un gateway.
```bash
# Non fare: docker-compose up driver-modbus
# Usa solo:
make start
```

### Problema: core-api non si connette al DB
```bash
docker-compose logs core-api | grep -i "error\|failed"
docker-compose ps postgres
# Se postgres non è healthy:
docker-compose restart postgres
docker-compose restart core-api
```

### Problema: migration non applicate
```bash
make migrate-status
make migrate
docker-compose restart core-api
```

### Problema: reset completo (dati persi — irreversibile)
```bash
make clean    # cancella volumi Docker e tutti i dati
make start    # riparte da zero
```

---

## 5. Autenticazione API (necessaria per import)

```bash
TOKEN=$(curl -s -X POST http://localhost:8081/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin123"}' \
  | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])")
```

Usa `$TOKEN` e `X-Organization-ID: $OPENEDGE_ORG_ID` in tutte le chiamate successive.

---

## 6. Creare un Gateway

Un gateway rappresenta un PLC o dispositivo fisico. Deve esistere prima di poter importare tag.

### Struttura richiesta

```
POST /api/gateways
Authorization: Bearer {TOKEN}
X-Organization-ID: {ORG_ID}
Content-Type: application/json
```

### Modbus TCP
```json
{
  "area_id": 1,
  "name": "PLC-Serbatoio1",
  "driver_type": "MODBUS_TCP",
  "scan_rate_ms": 1000,
  "connection_config": {
    "host": "192.168.1.10",
    "port": 502,
    "slave_id": 1,
    "timeout_ms": 3000
  }
}
```

### Siemens S7
```json
{
  "area_id": 1,
  "name": "PLC-S7-300",
  "driver_type": "S7",
  "scan_rate_ms": 500,
  "connection_config": {
    "host": "192.168.1.20",
    "rack": 0,
    "slot": 2
  }
}
```

### OPC UA
```json
{
  "area_id": 1,
  "name": "Server-OPCUA",
  "driver_type": "OPC_UA",
  "scan_rate_ms": 1000,
  "connection_config": {
    "endpoint": "opc.tcp://192.168.1.30:4840",
    "security_mode": "None"
  }
}
```

### MQTT
```json
{
  "area_id": 1,
  "name": "Gateway-MQTT",
  "driver_type": "MQTT",
  "scan_rate_ms": 1000,
  "connection_config": {
    "host": "192.168.1.40",
    "port": 1883
  }
}
```

### Risposta attesa
```json
{"id": 3, "name": "PLC-Serbatoio1", "driver_type": "MODBUS_TCP", ...}
```

Salva l'`id` del gateway — serve per importare i tag.

### Verifica gateway attivo
```bash
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 1" \
  http://localhost:8081/api/gateways
# connection_status: "online" = driver connesso al PLC
# connection_status: "offline" = driver non raggiunge il PLC
# connection_status: "unknown" = driver appena avviato
```

---

## 7. Importare Tag

### Metodo 1 — Import bulk da testo PLC (raccomandato)

Formato testo: `Alias : Tipodato AT Indirizzo;`

```
POST /api/tags/import
Authorization: Bearer {TOKEN}
X-Organization-ID: {ORG_ID}
Content-Type: application/json

{
  "gateway_id": 3,
  "historize": true,
  "content": "Portata_Ingresso : REAL AT 40001;\nLivello_Vasca : REAL AT 40003;\nPompa_On : BOOL AT 00001.0;\nPressione : REAL AT 40005;"
}
```

Tipi dati supportati: `BOOL`, `INT`, `UINT`, `DINT`, `UDINT`, `REAL`, `STRING`, `WORD`

Risposta:
```json
{"created": 4, "updated": 0, "errors": []}
```

Se `errors` contiene voci, quelle righe sono state saltate (le altre sono comunque importate).

### Metodo 2 — Creazione singola via API

```
POST /api/tags
Authorization: Bearer {TOKEN}
X-Organization-ID: {ORG_ID}
Content-Type: application/json

{
  "gateway_id": 3,
  "code": "40001",
  "alias": "Portata_Ingresso",
  "data_type": "REAL",
  "historize": true,
  "historize_deadband": 0.1
}
```

Tipi: `INT`, `REAL`, `BOOL`, `DINT`

### Esportare tag esistenti (per backup o migrazione)

```bash
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 1" \
  "http://localhost:8081/api/tags/export?gateway_id=3"
# Ritorna il testo PLC di tutti i tag del gateway
```

---

## 8. Verifica post-configurazione

```bash
# 1. Sistema healthy
curl http://localhost:8081/ready

# 2. Gateway online
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 1" \
  http://localhost:8081/api/gateways

# 3. Tag importati
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 1" \
  "http://localhost:8081/api/tags?gateway_id=3" | python3 -m json.tool | grep '"alias"'

# 4. Nessun allarme anomalo
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 1" \
  http://localhost:8081/api/alarms/active

# 5. Valori real-time che arrivano (dopo qualche secondo)
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 1" \
  http://localhost:8081/api/tags/1/current
# q=0 = GOOD (dato affidabile), q=1 = BAD (problema comunicazione)
```

---

## 9. Flusso completo — deploy + import automatico

```
agente riceve task:
"Installa OpenEdge, crea gateway Modbus su 192.168.1.10, importa questi tag:
 Portata:REAL:40001, Livello:REAL:40003, Pompa:BOOL:00001.0"

1. git clone
2. cp .env.example .env
3. echo "JWT_SECRET=$(openssl rand -hex 32)" >> .env   ← OBBLIGATORIO
4. imposta POSTGRES_DATA_PATH e REDIS_DATA_PATH se richiesto
5. make start
6. aspetta GET /ready == {"status":"ready",...}
7. login → ottieni TOKEN
8. POST /api/gateways (Modbus 192.168.1.10)  → ottieni gateway_id
9. POST /api/tags/import con i tag in formato PLC
10. GET /api/gateways → verifica connection_status
11. GET /api/tags/{id}/current → verifica che i valori arrivino
12. risponde con report stato
```

---

*Aggiornare questo file quando vengono aggiunti nuovi endpoint operativi a OpenEdge.*
