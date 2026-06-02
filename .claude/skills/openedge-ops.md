---
name: openedge-ops
description: OpenEdge Operations — installa OpenEdge in produzione, risolvi problemi container/driver/DB, gestisci backup/restore, import config (on-prem self-hosted)
version: 2.0.0
tags: [industrial, iot, devops, docker, deploy, on-prem, self-hosted, scada, install, troubleshoot]
requires: [docker, docker-compose, make, curl, git]
---

# OpenEdge Ops — Skill (install + production troubleshooting)

Skill operativa per **installare OpenEdge in produzione** su un PC industriale
e **risolvere qualunque problema** in esecuzione: container in crash-loop,
DB lento, driver che non parte, restore di emergenza, import gateway/tag.

OpenEdge in master è un'installazione **on-prem single-tenant**: un solo
server (di solito in fabbrica), un'unica organizzazione, gli utenti accedono
solo a quella. Niente multi-tenant, niente cloud SaaS, niente edge agent remoti.

> Questa skill **scrive** sul sistema (avvia/ferma container, modifica config,
> ripristina backup). Per **leggere** lo stato (ci sono allarmi? gateway
> offline? quanti critical nelle 24h? impostare cron di check) usa la skill
> `openedge.md`.

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

### Passo 0 — Verifica prerequisiti (Docker + make)

#### Docker
```bash
docker info
# Se fallisce: Docker Desktop non è avviato. Avvialo e riprova.
```

#### make
```bash
make --version
# Atteso: GNU Make 4.x (o superiore)
```

Se `make` non è installato, installalo in base all'OS:

```bash
# Linux — Ubuntu / Debian
sudo apt-get update && sudo apt-get install -y make

# Linux — RHEL / Fedora / CentOS
sudo dnf install -y make

# Mac — xcode-select include make
xcode-select --install

# Mac — alternativa con Homebrew
brew install make
```

**Windows** — esegui da PowerShell (come Administrator se winget lo richiede):
```powershell
winget install GnuWin32.Make
```
Poi **chiudi e riapri** il terminale per aggiornare il PATH, quindi verifica:
```cmd
make --version
```

> Se `winget` non è disponibile (Windows < 10 1709), usa Chocolatey:
> `choco install make` — oppure usa `openedge.bat` come alternativa senza make.

Verifica finale prima di procedere:
```bash
docker info >nul 2>&1 && make --version
# Entrambi devono rispondere senza errori
```

### Passo 1 — Clona il repository

```bash
git clone https://github.com/inferis995/openedge.git
cd openedge
```

### Passo 1b — Configura .env (solo se vuoi personalizzare percorsi o credenziali)

`make start` crea automaticamente `.env` da `.env.example` e genera un `JWT_SECRET` sicuro se non è già presente. **Non devi fare nulla per il JWT.**

Se vuoi personalizzare i percorsi dati o le credenziali DB, crea prima il `.env` e modificalo:

```bash
cp .env.example .env
# Poi modifica i valori che vuoi cambiare
```

#### Percorsi dati — bind mount già implementato

I dati di PostgreSQL e Redis sono salvati su disco dell'host tramite **bind mount**, già configurato in `docker-compose.yml`:

```yaml
# Estratto da docker-compose.yml (già presente — non modificare)
postgres:
  volumes:
    - ${POSTGRES_DATA_PATH:-./data/postgres}:/var/lib/postgresql/data

redis:
  volumes:
    - ${REDIS_DATA_PATH:-./data/redis}:/data
```

**Default (test/sviluppo):** i dati vanno in `./data/postgres` e `./data/redis` dentro la cartella del repository. Nessuna configurazione necessaria.

Per produzione, imposta percorsi assoluti nel `.env` **prima** del primo `make start`:

```bash
# Linux/Mac — percorso assoluto consigliato per produzione
POSTGRES_DATA_PATH=/opt/openedge/data/postgres
REDIS_DATA_PATH=/opt/openedge/data/redis

# Windows — altra unità
POSTGRES_DATA_PATH=D:/openedge-data/postgres
REDIS_DATA_PATH=D:/openedge-data/redis
```

> ⚠️ **Imposta i percorsi PRIMA di `make start`.**
> Cambiarli dopo che i dati esistono richiede backup + restore del DB.

#### Variabili obbligatorie per produzione

| Variabile | Default nel .env | Note |
|-----------|-----------------|------|
| `JWT_SECRET` | auto-generata da `make start` | Minimo 32 caratteri, genera con `openssl rand -hex 32` |
| `POSTGRES_PASSWORD` | `CHANGE_ME_IN_PRODUCTION` | **Cambiare obbligatoriamente in produzione** |

> ⚠️ Il sistema rifiuta di avviarsi se `JWT_SECRET` è assente o inferiore a 32 caratteri.
> Se non usi `make start`, assicurati che entrambe le variabili siano nel `.env`.

#### Variabili opzionali di sicurezza (nuove)

```bash
# Origin consentite per CORS e WebSocket (default: solo localhost:3000)
# In produzione: imposta il tuo dominio reale
ALLOWED_ORIGINS=https://scada.mia-azienda.com

# Swagger UI — disabilitato per default in produzione
SWAGGER_ENABLED=false

# Formato log: "json" per produzione (compatibile con Loki, Datadog, CloudWatch)
# Lasciare vuoto per dev locale (output human-readable)
LOG_FORMAT=json
```

### Passo 2 — Build + avvio

**Linux / Mac:**
```bash
make start
```

**Windows** (doppio clic su `openedge.bat` oppure da cmd):
```cmd
openedge.bat start
```

Entrambi eseguono in sequenza:
1. Creazione `.env` da `.env.example` (se mancante) e generazione `JWT_SECRET`
2. Build di tutte le immagini Docker (core-api, web-ui, driver-manager, engine-historian)
3. Build delle 5 immagini driver (Modbus, S7, OPC UA, MQTT, Redis) ← **necessarie per i gateway**
4. Avvio di tutti i servizi
5. Applicazione delle migration DB (automatica all'avvio del backend)

**Non usare `docker-compose up -d` direttamente — salta il build delle immagini driver.**

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

Il backend rifiuta di partire se `JWT_SECRET` non è presente nel `.env` o è più corto di 32 caratteri.

```bash
# Verifica se è presente e abbastanza lungo
grep JWT_SECRET .env

# Se manca o è ancora il valore di esempio (CHANGE_ME_...), genera e aggiungi:
echo "JWT_SECRET=$(openssl rand -hex 32)" >> .env

# Riavvia il backend
docker-compose restart core-api

# Controlla che ora parta
docker-compose logs core-api | tail -20
# Deve comparire: "[AUTH] JWT secret key loaded from environment"
```

### Problema: docker-compose si blocca — "POSTGRES_PASSWORD is required"

Dalla versione con hardening sicurezza, `POSTGRES_PASSWORD` non ha più un valore di fallback.
Se avvii con `docker-compose up -d` senza un `.env` valido, ottieni:

```
variable is not set. Defaulting to a blank string.
Error: POSTGRES_PASSWORD is required — set it in .env
```

**Soluzione**: usa sempre `make start` (o `openedge.bat start` su Windows) che crea `.env` automaticamente.
Se preferisci farlo manualmente:

```bash
cp .env.example .env
# Cambia POSTGRES_PASSWORD con un valore sicuro
# make start genera il JWT_SECRET automaticamente
make start
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

## 5b. Gerarchia: Organization → Site → Area → Gateway → Tag

OpenEdge organizza i dati in una gerarchia a 5 livelli. In **on-prem
single-tenant** l'`organization` è una sola (id=1, creata dalla migration);
**site**, **area** e **gateway** li crei tu via API (o UI).

```
organization (default id=1 in on-prem)
  └── site         "Stabilimento Crotone"
        └── area   "Reparto Verniciatura"
              └── gateway   "PLC-Linea-1" (Modbus/S7/...)
                    └── tag "Temperatura_Forno"
```

Per creare un gateway servono `area_id` e per creare un'area serve `site_id`.
Crea quindi nell'ordine: site → area → gateway → tag.

### Creare un site

```
POST /api/sites
Authorization: Bearer {TOKEN}
X-Organization-ID: {ORG_ID}
Content-Type: application/json

{"name": "Stabilimento Crotone", "org_id": 1}
```

Risposta: `{"id": 3, "name": "Stabilimento Crotone", "org_id": 1, ...}` —
salva `id`, ti serve per le area.

### Creare un'area

```
POST /api/areas
Authorization: Bearer {TOKEN}
X-Organization-ID: {ORG_ID}
Content-Type: application/json

{"name": "Reparto Verniciatura", "site_id": 3}
```

Risposta: `{"id": 7, "name": "Reparto Verniciatura", "site_id": 3, ...}` —
salva `id` per il gateway.

### Creare un utente (admin o operator)

```
POST /api/users
Authorization: Bearer {TOKEN}
X-Organization-ID: {ORG_ID}
Content-Type: application/json

{
  "username": "operatore_turno_a",
  "password": "PasswordSicura123",
  "role": "user",                  // "admin" oppure "user"
  "full_name": "Mario Rossi",
  "org_id": 1,
  "i3x_write": false               // true se vuoi dargli permessi PUT i3X
}
```

| Ruolo | Cosa può fare |
|---|---|
| `admin` | CRUD su tutto (org, site, area, gateway, tag, user) + system settings |
| `user`  | Lettura + ack allarmi. Per scrivere via i3X serve `i3x_write=true` |

In on-prem single-tenant **`org_id` è sempre 1**. Lascia la chiave anche
quando crei utenti — il backend la valida.

### Creare un'organizzazione (solo se serve multi-org locale)

In on-prem single-tenant non serve mai; in casi rari (es. due aziende
gemelle su stesso server) si può fare:

```
POST /api/organizations
Authorization: Bearer {TOKEN}
Content-Type: application/json

{"name": "Acme Spa"}
```

Solo `admin` può creare org. La default `id=1` rimane comunque.

### Esempio: setup nuovo cliente in un colpo

```bash
TOKEN=$(curl -s -X POST localhost:8081/api/auth/login -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin123"}' \
  | python3 -c "import sys,json;print(json.load(sys.stdin)['token'])")
ORG=1

# 1. Site
SITE=$(curl -s -X POST -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $ORG" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Stabilimento Crotone","org_id":1}' \
  localhost:8081/api/sites | python3 -c "import sys,json;print(json.load(sys.stdin)['id'])")

# 2. Area
AREA=$(curl -s -X POST -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $ORG" \
  -H 'Content-Type: application/json' \
  -d "{\"name\":\"Reparto A\",\"site_id\":$SITE}" \
  localhost:8081/api/areas | python3 -c "import sys,json;print(json.load(sys.stdin)['id'])")

# 3. Gateway Modbus (vedi sezione 6 per gli altri driver)
GW=$(curl -s -X POST -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $ORG" \
  -H 'Content-Type: application/json' \
  -d "{\"area_id\":$AREA,\"name\":\"PLC-Linea-1\",\"driver_type\":\"MODBUS_TCP\",\
       \"scan_rate_ms\":1000,\"connection_config\":{\"host\":\"192.168.1.10\",\"port\":502,\"slave_id\":1}}" \
  localhost:8081/api/gateways | python3 -c "import sys,json;print(json.load(sys.stdin)['id'])")

# 4. Tag bulk (vedi sezione 7 per il formato)
curl -s -X POST -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $ORG" \
  -H 'Content-Type: application/json' \
  -d "{\"gateway_id\":$GW,\"historize\":true,\
       \"content\":\"Temperatura : REAL AT 40001;\nPompa : BOOL AT 00001.0;\"}" \
  localhost:8081/api/tags/import

echo "Creato: site=$SITE area=$AREA gateway=$GW"
```

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
2. (opzionale) cp .env.example .env e personalizza percorsi/credenziali
3. make start  ← crea .env e genera JWT_SECRET automaticamente
4. aspetta GET /ready == {"status":"ready",...}
5. login → ottieni TOKEN
6. POST /api/gateways (Modbus 192.168.1.10)  → ottieni gateway_id
7. POST /api/tags/import con i tag in formato PLC
8. GET /api/gateways → verifica connection_status
9. GET /api/tags/{id}/current → verifica che i valori arrivino
10. risponde con report stato
```

---

## 10. Configurazione settings da API

Tutti i settings sono in `global_settings` (tabella) ed editabili via:

- **UI** → System (cards Notifications, Backup, MQTT, ecc.)
- **API** → `GET/PUT /api/system/settings` (richiede JWT admin)

Pattern PUT generale (validato server-side):

```bash
TOKEN=$(/usr/local/bin/openedge-token.sh)   # vedi openedge.md sezione 12
curl -X PUT -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"<gruppo>": {"<chiave>": "<valore>"}}' \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/system/settings
```

Le chiavi "secret" (`*password`, `*token`, `*secret`, `*private`, `*api_key`)
in **GET** ritornano stringa vuota (mascherate) e in **PUT** se sono vuote
**preservano il valore stored**. Significa: re-mandare la password solo
quando vuoi davvero cambiarla.

### 10.1 Notifiche email — Gmail

Pre-requisito: 2FA attivo + **app password** generata da
https://myaccount.google.com/apppasswords (16 caratteri).

```bash
curl -X PUT -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{
    "notifications": {
      "notif_email_enabled": "true",
      "notif_email_smtp_host": "smtp.gmail.com",
      "notif_email_smtp_port": "587",
      "notif_email_use_tls": "false",
      "notif_email_username": "alerts@tuaazienda.it",
      "notif_email_password": "abcd efgh ijkl mnop",
      "notif_email_from": "alerts@tuaazienda.it",
      "notif_email_to": "operatore1@cliente.it, operatore2@cliente.it",
      "notif_min_severity": "high",
      "notif_on_cleared": "false",
      "notif_rate_limit_per_min": "60"
    }
  }' \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/system/settings
```

`port 587 + use_tls=false` = STARTTLS (default e raccomandato).
`port 465 + use_tls=true` = TLS implicito (per server legacy).

### 10.2 Notifiche email — Office 365

```bash
curl -X PUT -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{
    "notifications": {
      "notif_email_enabled": "true",
      "notif_email_smtp_host": "smtp.office365.com",
      "notif_email_smtp_port": "587",
      "notif_email_use_tls": "false",
      "notif_email_username": "alerts@tuaazienda.onmicrosoft.com",
      "notif_email_password": "<password-account>",
      "notif_email_from": "alerts@tuaazienda.onmicrosoft.com",
      "notif_email_to": "team@cliente.it",
      "notif_min_severity": "high"
    }
  }' \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/system/settings
```

> Office 365 richiede che SMTP AUTH sia abilitato per l'account
> (Microsoft 365 admin center → Active users → mailbox properties).

### 10.3 Notifiche email — SMTP interno aziendale (Postfix/Exim)

```bash
curl -X PUT -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{
    "notifications": {
      "notif_email_enabled": "true",
      "notif_email_smtp_host": "mail.azienda.local",
      "notif_email_smtp_port": "25",
      "notif_email_use_tls": "false",
      "notif_email_username": "",
      "notif_email_password": "",
      "notif_email_from": "openedge@azienda.local",
      "notif_email_to": "manutenzione@azienda.local",
      "notif_min_severity": "medium"
    }
  }' \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/system/settings
```

(Porta 25 senza auth è ok solo se il relay è sulla stessa LAN privata e
non autentica.)

### 10.4 Notifiche Telegram

Setup bot (una volta sola):
1. Apri Telegram, cerca `@BotFather`, manda `/newbot`, scegli nome → ricevi token.
2. Manda un messaggio al bot (DM) **o** aggiungilo a un gruppo e manda un messaggio nel gruppo.
3. Recupera `chat_id`:
```bash
curl -s "https://api.telegram.org/bot<TOKEN>/getUpdates" \
  | python3 -c "import sys,json; d=json.load(sys.stdin); print(d['result'][-1]['message']['chat']['id'])"
# Numero negativo = gruppo, positivo = DM
```

Salvalo in OpenEdge:
```bash
curl -X PUT -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{
    "notifications": {
      "notif_telegram_enabled": "true",
      "notif_telegram_bot_token": "1234567:ABCdefGHIjklMNOpqrSTUvwxYZ",
      "notif_telegram_chat_id": "-1001234567890",
      "notif_min_severity": "medium"
    }
  }' \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/system/settings
```

### 10.5 Send test — verifica end-to-end

```bash
curl -s -X POST -H "Authorization: Bearer $TOKEN" \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/system/notifications/test
```

Risposta su successo totale:
```json
{"ok": true, "errors": []}
```

Risposta su fallimento parziale:
```json
{"ok": false, "errors": ["email: connection refused", "telegram: chat not found"]}
```

Il test bypassa il rate limiter e il filtro severity — utile per
verificare le credenziali subito dopo averle salvate.

### 10.6 Backup automatici

Tre cose persistono in `global_settings` (lette dal container backup al
boot, quindi serve `docker compose restart backup` dopo un PUT):

```bash
curl -X PUT -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{
    "backup": {
      "backup_enabled": "true",
      "backup_schedule": "0 3 * * *",
      "backup_retention_days": "30",
      "backup_age_recipient": "age1abcdef..."
    }
  }' \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/system/settings
docker compose restart backup
```

`backup_schedule` è un'espressione cron (UTC). Preset comuni:
- `0 3 * * *` — ogni notte alle 03:00 (raccomandato)
- `0 */6 * * *` — ogni 6 ore
- `0 */12 * * *` — ogni 12 ore

`backup_age_recipient` è la chiave **pubblica** age (non segreta) — usala
per cifrare i dump che lasciano il server (USB, NAS off-site).
Genera il keypair OFF dal server:
```bash
age-keygen -o /percorso/sicuro/age-key.txt
grep '^# public key:' /percorso/sicuro/age-key.txt   # questa va in backup_age_recipient
# la chiave privata (age-key.txt) custodiscila — senza non si decifrano i backup
```

Lascia vuoto per backup in chiaro (ok se `./backups/` è su disco cifrato
e non esce mai dal server).

Comandi pronti:
```bash
make backup-now                                  # backup ad-hoc
make backup-to-usb USB=/media/usbkey              # copia ultimo backup su USB
make restore BACKUP=./backups/openedge-...dump   # restore (distruttivo!)
```

### 10.7 Filtri allarmi — come non spammare

Tre chiavi globali che si applicano a TUTTI i canali (email + Telegram):

| Chiave | Valori | Effetto |
|---|---|---|
| `notif_min_severity` | low / medium / high / critical | Eventi sotto la soglia → scartati silenziosamente |
| `notif_on_cleared` | true / false | Se false (default) notifica solo su ACTIVE, non su CLEARED |
| `notif_rate_limit_per_min` | 1-1000 | Cap globale token-bucket — protezione anti-flood durante alarm storm |

Esempio "no spam — solo critical + 30/min":
```bash
curl -X PUT -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{
    "notifications": {
      "notif_min_severity": "critical",
      "notif_on_cleared": "false",
      "notif_rate_limit_per_min": "30"
    }
  }' \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/system/settings
```

### 10.8 Altri settings utili da API

```bash
# Cambia retention storico
curl -X PUT -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"db_retention_days": 60}' \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/system/settings

# Cambia publish mode (dual / sparkplug_only / legacy_only)
curl -X PUT -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"publish_mode": "sparkplug_only"}' \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/system/settings

# Cambia broker MQTT da interno a esterno (es. broker cliente)
curl -X PUT -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{
    "mqtt_broker_mode": "external",
    "mqtt_external_host": "10.0.0.50",
    "mqtt_external_port": 1883,
    "mqtt_username": "openedge",
    "mqtt_password": "<segreta>"
  }' \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/system/settings
```

### 10.9 Leggere lo stato corrente

```bash
curl -s -H "Authorization: Bearer $TOKEN" \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/system/settings \
  | python3 -m json.tool
```

I valori segreti (password/token) tornano sempre come stringa vuota —
non si vedono in chiaro nemmeno via API.

---

## 11. Turni & Operatori

OpenEdge sa "che turno è adesso" e "chi è l'operatore di servizio" —
tutto configurabile da UI (pagina **Turni**) e da API.

### 11.1 Concetti

- **Turno** (`shifts` table): nome + start_time + end_time + weekdays[]
  + active. `start_time > end_time` significa turno notte che incrocia
  la mezzanotte (es. 22:00-06:00) e viene gestito automaticamente.
- **Assegnazione** (`shift_assignments` table): un utente di OpenEdge
  è responsabile di un turno tra `valid_from` e `valid_to` (null =
  indeterminato).
- **Turno corrente**: il primo turno attivo i cui weekdays includono
  oggi (o ieri per i turni notte continuati dopo mezzanotte) e il cui
  intervallo orario contiene `NOW()`.

Al primo avvio la migration seeda 3 turni default — **Mattina 06-14,
Pomeriggio 14-22, Notte 22-06**, tutti lun-ven. Modificabili/disattivabili
da subito.

### 11.2 Domande tipiche

```bash
# "Che turno è adesso? Chi è di servizio?"
curl -s -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/shifts/current \
  | python3 -c "
import sys, json
d = json.load(sys.stdin)
if not d.get('shift'):
    print('Nessun turno in corso'); sys.exit(0)
s = d['shift']
h, m = divmod(d['time_left_min'], 60)
ops = ', '.join(o['username'] for o in d['operators']) or '(nessuno designato)'
print(f'Turno: {s[\"name\"]} ({s[\"start_time\"]}-{s[\"end_time\"]})')
print(f'Resta: {h}h {m}m')
print(f'Operatori: {ops}')"
```

```bash
# Lista tutti i turni
curl -s -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/shifts | python3 -m json.tool
```

### 11.3 Creare un turno custom

```bash
# Turno notte solo nel weekend, attivo
curl -X POST -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "Notte weekend",
    "start_time": "22:00",
    "end_time": "06:00",
    "weekdays": [5, 6, 0],
    "active": true
  }' \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/shifts
```

`weekdays` è **0=Domenica, 1=Lunedì, ..., 6=Sabato**. Per turno notte
del venerdì che continua sabato mattina, metti `5` (venerdì) — il
sistema sa che si estende oltre mezzanotte.

### 11.4 Modifica/disattiva un turno

```bash
# Cambiare l'orario o disattivare temporaneamente
curl -X PUT -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "Mattina",
    "start_time": "07:00",
    "end_time": "15:00",
    "weekdays": [1,2,3,4,5],
    "active": true
  }' \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/shifts/1

# Eliminare un turno (CASCADE elimina anche le assegnazioni)
curl -X DELETE -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/shifts/3
```

### 11.5 Assegnare un operatore a un turno

```bash
# Mario Rossi (user_id=5) responsabile del turno "Mattina" (id=1) da oggi
curl -X POST -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  -H 'Content-Type: application/json' \
  -d '{"user_id": 5}' \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/shifts/1/assignments

# Con date specifiche
curl -X POST -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  -H 'Content-Type: application/json' \
  -d '{"user_id": 7, "valid_from": "2026-06-01", "valid_to": "2026-08-31"}' \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/shifts/1/assignments

# Lista assegnamenti di un turno
curl -s -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/shifts/1/assignments

# Rimuovere un'assegnazione (id ottenuto dalla lista)
curl -X DELETE -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/shifts/assignments/42
```

### 11.6 Setup completo "3 turni × 3 squadre" in uno script

```bash
TOKEN=$(/usr/local/bin/openedge-token.sh)
URL=http://$OPENEDGE_HOST:$OPENEDGE_PORT

# Assumiamo i 3 turni di default già seeded (id 1, 2, 3).
# Squadra A su Mattina, B su Pomeriggio, C su Notte.
for SHIFT_ID in 1 2 3; do
    # 4 operatori per squadra: cambia i user_id con i tuoi
    for USER_ID in 10 11 12 13; do
        curl -s -X POST -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 1" \
          -H 'Content-Type: application/json' \
          -d "{\"user_id\": $USER_ID}" \
          "$URL/api/shifts/$SHIFT_ID/assignments"
    done
done
echo "Setup turni × operatori completato."
```

Da qui in poi la dashboard di ogni utente mostra correttamente "turno
corrente: X, finisce tra Yh Zm, allarmi durante questo turno: N" e gli
operatori designati appaiono nel widget.

---

## 11b. KPI di Produzione custom

OpenEdge permette all'admin di definire metriche custom (pezzi/h,
kWh/turno, OEE component, ecc.) basate sui tag dei PLC. I KPI custom
appaiono **automaticamente nella dashboard** accanto ai 6 KPI di sistema
e sono valutati a ogni `/api/dashboard/overview` (server-side, refresh
30s).

### 11b.1 Modello

Un custom KPI è composto da:
- **nome** (visibile in dashboard)
- **tag_id** (il tag PLC sorgente)
- **aggregation** — `avg / sum / min / max / last / delta / count`
- **window_minutes** — finestra temporale (es. 480 = ultimo turno 8h)
- **multiplier** — moltiplicatore opzionale (es. 0.001 per V → kV)
- **unit** — unità per la card (es. "pz", "kWh", "°C")
- **good_when** — `up` o `down`
- **target_value** — soglia opzionale; quando settata il valore è
  colorato verde/rosso in base al target_met

### 11b.2 Esempi pratici

**Pezzi prodotti turno corrente** (counter PLC `production_count`,
delta nelle ultime 8h):
```bash
curl -X POST -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "Pezzi turno",
    "tag_id": 42,
    "aggregation": "delta",
    "window_minutes": 480,
    "unit": "pz",
    "good_when": "up",
    "target_value": 500
  }' \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/custom-kpis
```

**Temperatura media 1h** (tag `temp_forno`):
```bash
curl -X POST -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "Temp forno (1h avg)",
    "tag_id": 17,
    "aggregation": "avg",
    "window_minutes": 60,
    "unit": "°C",
    "good_when": "up",
    "target_value": 180
  }' \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/custom-kpis
```

**Energia consumata 24h in kWh** (tag `power_meter` in W, somma campioni
× 1/3600000 per W·s → kWh):
```bash
curl -X POST -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "Energia 24h",
    "tag_id": 88,
    "aggregation": "sum",
    "window_minutes": 1440,
    "multiplier": 0.0000002778,
    "unit": "kWh",
    "good_when": "down"
  }' \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/custom-kpis
```

### 11b.3 Lista / Modifica / Elimina

```bash
# Lista tutti i custom KPI configurati
curl -s -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/custom-kpis | python3 -m json.tool

# Modifica
curl -X PUT -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  -H 'Content-Type: application/json' \
  -d '{...}' \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/custom-kpis/3

# Elimina
curl -X DELETE -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/custom-kpis/3
```

### 11b.4 Quando usare quale aggregazione

| Aggregation | Quando usarla |
|---|---|
| `last` | Valore corrente (temperatura adesso, livello vasca adesso) |
| `avg` | Valore medio nel periodo (temperatura media 1h) |
| `sum` | Somma campioni — utile per consumi (energia totale, materiale usato) |
| `min` / `max` | Picchi nel periodo (massima temperatura nel turno) |
| `delta` | Variazione di un contatore monotono (pezzi prodotti = count_end − count_start) |
| `count` | Numero di campioni — diagnostica acquisizione |

---

## 12. Finestre di Manutenzione

Le finestre di manutenzione permettono di silenziare le notifiche
durante fermate pianificate. Gli allarmi continuano a essere registrati
in DB per audit, ma email/Telegram NON escono finché la finestra è attiva.

Fattore #1 di customer complaints nei deploy industriali: notifiche spam
durante una sostituzione programmata di un cuscinetto. Risolto.

### 12.1 Creare una finestra

```bash
# Manutenzione domani dalle 22 alle 02 del giorno dopo
curl -X POST -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  -H 'Content-Type: application/json' \
  -d '{
    "title": "Sostituzione cuscinetto motore linea 2",
    "start_at": "2026-06-05T22:00:00Z",
    "end_at": "2026-06-06T02:00:00Z",
    "reason": "Manutenzione programmata trimestrale"
  }' \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/maintenance
```

Timestamps in **RFC3339 UTC**. La UI converte da local time automaticamente.

### 12.2 Lista finestre

```bash
# Tutte (passate + future + attive)
curl -s -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/maintenance

# Solo quelle attive adesso
curl -s -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  "http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/maintenance?active=true"
```

### 12.3 Modifica / elimina

```bash
# Modifica orario di una finestra
curl -X PUT -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  -H 'Content-Type: application/json' \
  -d '{
    "title": "Manutenzione linea 2",
    "start_at": "2026-06-05T23:00:00Z",
    "end_at": "2026-06-06T03:00:00Z",
    "reason": "Esteso di 1h"
  }' \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/maintenance/3

# Cancellare (anche in corso — termina subito il silenziamento)
curl -X DELETE -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/maintenance/3
```

### 12.4 Manutenzione ricorrente

L'API non ha (volutamente) il concetto di "ricorrenza" — un cron locale
copre il caso d'uso senza appesantire lo schema.

```bash
# /usr/local/bin/openedge-weekly-maintenance.sh
# Crea ogni sabato una finestra notturna 22-04 per il backup esteso
#!/bin/bash
source /etc/openedge-monitor.env
TOKEN=$(/usr/local/bin/openedge-token.sh)
START=$(date -u -d 'next Saturday 22:00' +%Y-%m-%dT%H:%M:%SZ)
END=$(date -u -d 'next Saturday 22:00 + 6 hours' +%Y-%m-%dT%H:%M:%SZ)
curl -s -X POST -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  -H 'Content-Type: application/json' \
  -d "{\"title\":\"Backup esteso settimanale\",\"start_at\":\"$START\",\"end_at\":\"$END\",\"reason\":\"Automatico\"}" \
  "http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/maintenance"
```

Crontab (ogni venerdì alle 12:00 — pianifica per il sabato successivo):
```cron
0 12 * * 5 /usr/local/bin/openedge-weekly-maintenance.sh
```

---

## 13. OEE — Overall Equipment Effectiveness (multi-linea/multi-reparto)

OEE = **Availability × Performance × Quality** (standard ISO 22400).
OpenEdge supporta **N profili OEE** — ognuno è un'unità di misura
indipendente (una linea, una macchina, un reparto). La dashboard mostra
una card per profilo + un **rollup di fabbrica** (media tra i profili).

Quando 0 profili sono configurati, la dashboard usa il calcolo OEE
**legacy** con fallback euristici (compatibilità con installazioni
esistenti). Appena crei il primo profilo, la dashboard passa
automaticamente in modalità multi-profilo.

### 13.0 Quando creare un profilo OEE

- **1 sola linea / 1 sola macchina** → 1 profilo basta
- **Più linee/reparti** → 1 profilo per ognuno (es. "Linea A", "Linea B",
  "Pressa 5", "Confezionamento")
- **Macchine che il PLC già aggrega** → 1 profilo che punta al contatore
  aggregato del PLC (non serve un profilo per singola macchina)
- **5 macchine senza tag PLC aggregato** → 5 profili separati, il rollup
  in dashboard fa la media

### 13.1 Endpoint API

| Method | Path | Descrizione |
|---|---|---|
| GET | `/api/oee` | Snapshot corrente (modalità legacy o multi-profilo) |
| GET | `/api/oee/history` | Trend 7g (rollup o legacy) |
| GET | `/api/oee/test-tag/:id?role=running\|counter` | Verifica un tag prima di salvare |
| GET | `/api/oee/profiles` | Lista tutti i profili (admin) |
| POST | `/api/oee/profiles` | Crea profilo (admin) |
| PUT | `/api/oee/profiles/:id` | Aggiorna profilo (admin) |
| DELETE | `/api/oee/profiles/:id` | Elimina profilo (admin) |
| GET | `/api/oee/profiles/:id/history` | Trend 7g del singolo profilo |

### 13.1 Le due modalità di calcolo

**Modalità "fallback" (out-of-the-box, nessuna configurazione):**
- Availability = `1 - (durata totale allarmi CRITICAL / finestra)`
- Performance  = % campioni con `quality = 0` nella finestra (proxy: se i
  sensori riportano dati buoni, l'impianto sta lavorando regolarmente)
- Quality      = stesso fallback di Performance (in attesa che si configuri
  il tag "pezzi buoni")

I numeri sono "indicativi della salute generale", **non KPI di produzione veri**.

**Modalità "tag-driven" (admin configura i tag in System → OEE):**
- Availability = % campioni di `oee_run_time_tag` con valore > 0
- Performance  = `pieces_produced / (target_pph × ore_finestra)` × 100
- Quality      = `pieces_good / pieces_produced` × 100

Ogni componente porta un badge `tag` o `auto` nella card UI, così l'operatore
sa quali numeri sono "veri" e quali "indicativi".

### 13.2 Settings OEE

Tutti accessibili via PUT `/api/system/settings` con prefisso `oee_*`:

| Chiave | Default | Significato |
|---|---|---|
| `oee_window_minutes` | `480` | Finestra di calcolo in minuti (480 = 8h un turno). |
| `oee_target` | `85` | Target OEE % (ISO: 85 = world-class, 65-85 tipico). |
| `oee_run_time_tag` | `0` | tag_id "macchina in marcia" (BOOL/0-1). 0 = fallback. |
| `oee_produced_tag` | `0` | tag_id contatore pezzi prodotti (monotono). 0 = fallback. |
| `oee_good_tag` | `0` | tag_id contatore pezzi buoni (monotono). 0 = fallback. |
| `oee_target_pieces_per_hour` | `0` | Rate target (pezzi/ora) per il calcolo Performance. |

### 13.3 Creare un profilo OEE per una linea reale

Esempio: linea di assemblaggio con tag PLC:
- `Linea1.MachineRunning` (BOOL) → tag_id = 42
- `Linea1.PiecesCounter` (DINT crescente) → tag_id = 51
- `Linea1.GoodPiecesCounter` (DINT) → tag_id = 52
- Target 120 pezzi/ora, target OEE 80%, finestra 8h

```bash
source /etc/openedge-monitor.env
TOKEN=$(/usr/local/bin/openedge-token.sh)

curl -X POST -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "Linea Assemblaggio 1",
    "description": "Linea principale, turno 8h",
    "area_id": null,
    "run_time_tag_id": 42,
    "produced_tag_id": 51,
    "good_tag_id": 52,
    "target_pieces_per_hour": 120,
    "window_minutes": 480,
    "target_oee": 80,
    "display_order": 0,
    "enabled": true
  }' \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/oee/profiles
```

Verifica subito:
```bash
curl -s -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/oee | python3 -m json.tool
```

Risposta: `mode: "profiles"` con il profilo appena creato dentro
`profiles[]` + `rollup` calcolato sulla media (con 1 solo profilo, rollup
= snapshot del profilo).

### 13.4 Lista profili / aggiorna / elimina

```bash
# Lista
curl -s -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/oee/profiles | python3 -m json.tool

# Aggiorna (rinvia tutto il payload)
curl -X PUT -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Linea Assemblaggio 1","target_oee":85,"target_pieces_per_hour":130,
       "window_minutes":480,"display_order":0,"enabled":true,
       "run_time_tag_id":42,"produced_tag_id":51,"good_tag_id":52}' \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/oee/profiles/1

# Disabilita (non appare in dashboard ma resta in DB)
curl -X PUT ... -d '{"name":"...", ..., "enabled":false}' .../api/oee/profiles/1

# Elimina definitivamente
curl -X DELETE -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/oee/profiles/1
```

### 13.5 Setup completo multi-linea (esempio reale)

Fabbrica con 3 linee + 1 reparto packaging. Script per creare tutto:

```bash
#!/bin/bash
source /etc/openedge-monitor.env
TOKEN=$(/usr/local/bin/openedge-token.sh)

create_profile() {
  curl -s -X POST -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
    -H 'Content-Type: application/json' -d "$1" \
    http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/oee/profiles
  echo
}

create_profile '{"name":"Linea A","run_time_tag_id":42,"produced_tag_id":51,"good_tag_id":52,
  "target_pieces_per_hour":120,"target_oee":85,"window_minutes":480,"display_order":1,"enabled":true}'

create_profile '{"name":"Linea B","run_time_tag_id":62,"produced_tag_id":71,"good_tag_id":72,
  "target_pieces_per_hour":90, "target_oee":80,"window_minutes":480,"display_order":2,"enabled":true}'

create_profile '{"name":"Pressa 5","run_time_tag_id":81,"produced_tag_id":82,
  "target_pieces_per_hour":40, "target_oee":75,"window_minutes":480,"display_order":3,"enabled":true}'

create_profile '{"name":"Packaging","run_time_tag_id":91,"produced_tag_id":92,"good_tag_id":93,
  "target_pieces_per_hour":200,"target_oee":85,"window_minutes":480,"display_order":4,"enabled":true}'
```

### 13.6 Storico OEE persistente

Il cron worker interno (parte automatico col core-api) salva ogni ora
uno snapshot OEE per ogni profilo abilitato + un rollup fabbrica. A
mezzanotte UTC aggrega in snapshot giornaliero. Niente da configurare:
funziona out-of-the-box.

Query del rollup:

```bash
# Ultimi 7 giorni, granularità oraria, profilo specifico
curl -s -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  "http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/oee/history-v2?profile_id=1&from=2026-05-26&to=2026-06-02&bucket=hour" \
  | python3 -m json.tool

# Ultimi 30 giorni, granularità giornaliera, rollup fabbrica (omettendo profile_id)
curl -s -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  "http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/oee/history-v2?from=2026-05-03&to=2026-06-02&bucket=day"
```

Parametri:
- `profile_id`: id profilo, oppure omesso/0 per il rollup fabbrica
- `from`, `to`: RFC3339 oppure YYYY-MM-DD (mezzanotte UTC)
- `bucket`: `hour` | `day` | `shift` (shift sarà popolato dal commit successivo)

### 13.7 OEE per turno + Planned Production Time

Quando i turni sono configurati (vedi §11), ogni profilo OEE può
calcolare l'Availability rispetto al **Planned Production Time** invece
che alla wall clock window: PPT = window − pause turni − manutenzioni.
Risultato: una linea che non lavora di domenica non viene penalizzata.

Per default i profili nuovi hanno `respect_shifts=true` e
`respect_maintenance=true`. Disattivabili per profilo via UI o API.

```bash
# Crea profilo che NON considera i turni (calcolo wall clock 24/7)
curl -X POST -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Server Room","respect_shifts":false,"respect_maintenance":true,
       "window_minutes":1440,"target_oee":99,"display_order":99,"enabled":true}' \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/oee/profiles
```

Matrice OEE per turno × giorno (la KPI #1 richiesta dalla direzione):

```bash
curl -s -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  "http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/oee/by-shift?profile_id=1&from=2026-05-26&to=2026-06-02" \
  | python3 -m json.tool
```

Risposta: array di `{shift_id, shift_name, date, oee, availability, performance, quality, hours}`.
Aggregato da `oee_history` (bucket=hour) con `JOIN shifts`.

### 13.8 Loss tree + Pareto cause + MTBF/MTTR

Auto-popolato dal cron worker — niente data entry operatore. Logica:
- Allarme critical chiuso → categoria `breakdown` (un evento per profilo)
- Maintenance window con titolo contenente "setup"/"cambio"/"changeover" → `setup`
- Maintenance "pura" (titolo neutro) → esclusa dal PPT, no loss event
- Scope per profilo: profilo con `gateway_id` set vede solo allarmi su tag di quel gateway; profilo senza gateway vede tutti gli allarmi

Endpoint:

```bash
# Loss tree (Pareto cause)
curl -s -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  "http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/oee/loss-tree?profile_id=1&from=2026-05-26&to=2026-06-02" \
  | python3 -m json.tool

# MTBF/MTTR per profilo
curl -s -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  "http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/oee/profiles/1/reliability?from=2026-05-26&to=2026-06-02"

# Categorie disponibili (6 Big Losses ISO 22400-2)
curl -s -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/oee/loss-categories
```

MTBF = uptime / num_breakdowns dove uptime = planned_min(oee_history) - repair_min.
MTTR = sum(breakdown_duration) / num_breakdowns.

### 13.9 Alert rules OEE soglia

Regole "OEE Linea A < 70% per 60 min → email warning" valutate dal cron
worker ogni 5 minuti contro oee_history (bucket=hour). Anti-spam: una
regola scatta una sola volta finché resta violata.

```bash
# Crea regola "OEE Linea 1 sotto 70% per 60 minuti → warning"
curl -X POST -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  -H 'Content-Type: application/json' \
  -d '{
    "profile_id": 1,
    "name": "Linea 1 sotto target",
    "metric": "oee",
    "op": "<",
    "threshold": 70,
    "sustained_minutes": 60,
    "severity": "warning",
    "enabled": true
  }' \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/oee/alert-rules

# Lista
curl -s -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/oee/alert-rules | python3 -m json.tool

# Regola "globale" (profile_id null = rollup fabbrica)
curl -X POST ... -d '{"profile_id":null,"name":"Fabbrica giù","metric":"oee",
  "op":"<","threshold":60,"sustained_minutes":120,"severity":"critical","enabled":true}' ...
```

Parametri:
- `profile_id`: id profilo, null per rollup fabbrica
- `metric`: oee | availability | performance | quality
- `op`: `<` o `>`
- `threshold`: 0-100 (%)
- `sustained_minutes`: minimo 60 (granularità oee_history), multipli di 60 consigliati
- `severity`: info (solo log) | warning (email/Telegram) | critical (con escalation)

Le notifiche passano dal dispatcher esistente (`notif_email_*` /
`notif_telegram_*` settings già configurati). Niente nuovi canali da setuppare.

### 13.10 Gerarchia + TV-mode + Mobile

Gerarchia Site → Area → Profile con rollup multi-livello:

```bash
curl -s -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/oee/hierarchy | python3 -m json.tool
```

Risposta: albero con `sites[].areas[].profiles[]`, ogni nodo con il suo
snapshot. Site = media aritmetica delle aree figlie. Area = media dei
profili figli. Profili senza area finiscono in `unassigned`.

**TV-mode** (monitor da reparto): apri `/tv/oee` in nuova scheda dal
PC del monitor → fullscreen, no sidebar, refresh 30s, rotazione profili
ogni 10s, font enormi leggibili a 10 metri, modalità giorno/notte
auto in base all'orario (07-20 light, altrimenti scuro).

Pattern installazione: PC standalone in reparto con browser fullscreen
e bookmark a `https://openedge.local/tv/oee`. Il login resta in sessione.

**Mobile**: tabs OEE Profili wrappano su mobile, layout responsive
per caporeparto che controlla dal telefono in giro.

### 13.11 Errori comuni

- **OEE = 0** in modalità tag: controlla che `oee_produced_tag` riceva
  campioni nella finestra (probabilmente il PLC non sta inviando, vedi sezione
  "Tag con quality≠0").
- **Performance = 100% in fallback**: nessun dato nella finestra → il proxy
  "tutto OK" è scelto di proposito invece di un 0 fuorviante.
- **Counter rolling-over**: i contatori PLC che resettano a fine turno fanno
  apparire Performance/Quality negativi. Soluzione: usare un tag derivato
  monotono o un counter a 32-bit con periodo lungo.

---

*Aggiornare questo file quando vengono aggiunti nuovi endpoint operativi a OpenEdge.*
