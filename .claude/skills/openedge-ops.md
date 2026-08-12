---
name: openedge-ops
description: OpenEdge Operations — installa, configura e gestisce OpenEdge in produzione. Chiede sempre all'utente cosa vuole fare prima di agire. Supporta self-hosted/on-prem (Linux+Windows), VPS con Traefik, Coolify, edge profile. Include driver industriali (Modbus/S7/OPC-UA/MQTT/LoRaWAN), modellazione impianto con UDT (tipi riusabili con propagazione alle istanze), multi-tenant, backup, monitoring professionale (Prometheus+Grafana+AlertManager+Loki+4 exporter), OTA updates, security, NIS2/IEC62443 compliance automatica (asset sync da gateway, CSIRT Art.23 countdown, vendor risk Art.18, 30-item checklist auto-assessed), MFA TOTP con recovery codes e mfa_required per org.
version: 8.0.0
tags: [industrial, iot, udt, commissioning, devops, docker, deploy, coolify, traefik, vps, on-prem, self-hosted, multi-tenant, install, troubleshoot, mqtt, webhooks, backup, monitoring, health-checks, ota-updates, security, lorawan, nis2, compliance, csirt, vendor-risk, iec62443, mfa, totp, 2fa, recovery-codes]
requires: [docker, curl, git]
---

# OpenEdge Ops — Skill operativa

Skill per **installare, configurare e risolvere problemi** di OpenEdge in produzione.

> **Regola #1**: prima di fare qualsiasi cosa, chiedi all'utente cosa vuole installare/fare.
> Non assumere nulla. Usa il flusso di onboarding qui sotto.

---

## Onboarding — Chiedi sempre queste domande prima di procedere

Se l'utente non specifica esattamente cosa vuole, fai queste domande nell'ordine:

### Step 1 — Modalità di deployment

```
Dove vuoi installare OpenEdge?

1. Self-hosted / On-prem  — tutto gira sulla tua macchina (fabbrica, ufficio, PC industriale)
                            Nessuna dipendenza cloud. Funziona anche air-gap.
2. VPS / Cloud            — server dedicato con IP pubblico (Hetzner, OVH, DigitalOcean, ecc.)
                            HTTPS automatico con Let's Encrypt + Traefik.
3. Coolify                — piattaforma PaaS self-hosted. Gestisce HTTPS e deploy automaticamente.
                            Raccomandato per chi vuole il cloud senza gestire Traefik a mano.
```

### Step 1b — Server-only o All-in-one (solo se VPS o Coolify)

```
Vuoi che i driver industriali girino sullo stesso server (all-in-one)
oppure su macchine di fabbrica separate (server-only)?

1. Server-only (raccomandato per SaaS multi-tenant)
   — il server cloud gestisce solo API, UI e database
   — gli edge fisici (driver-manager) girano su macchine separate in fabbrica
   — ogni org scarica il proprio edge installer ZIP dalla UI

2. All-in-one (utile per demo, dev, o deployment piccoli)
   — driver-manager gira direttamente sul VPS/Coolify
   — non servono macchine edge separate
   — attiva il profilo `edge` nel compose
```

### Step 2 — Sistema operativo (solo se on-prem)

```
Che sistema operativo ha la macchina?

1. Linux  (Ubuntu 22+, Debian 12+, RHEL 8+) — raccomandato
2. Windows (Windows 10/11 o Windows Server 2019+)
```

### Step 3 — HTTPS (solo se on-prem Linux)

```
Hai bisogno di HTTPS?

1. Sì — rete locale o fabbrica (Caddy genera CA interna, nessun internet richiesto)
2. No  — sviluppo locale, HTTP su :3000 è sufficiente
```

### Step 4 — Dominio / hostname

```
Qual è il nome host o dominio che vuoi usare?

- On-prem LAN:  openedge.local  (o l'IP della macchina, es. 192.168.1.100)
- VPS/cloud:    app.tuazienda.com
- Coolify:      openedge.tuazienda.com
```

### Step 5 — Monitoraggio (opzionale)

```
Vuoi abilitare il monitoring (Prometheus + Grafana + Loki)?
- Grafana dashboard per metriche container, DB, MQTT, API response time
- Richiede ~500 MB RAM aggiuntivi
```

---

Una volta raccolte le risposte, segui la sezione corrispondente qui sotto.

---

## Deploy A — Self-hosted Linux (raccomandato per on-prem)

**Prerequisiti**: Docker 24+, Docker Compose plugin, `make`, `git`, `openssl`

```bash
# 1. Installa Docker (se non presente)
curl -fsSL https://get.docker.com | bash
newgrp docker   # oppure rilogga

# 2. Clona il repo
git clone https://github.com/inferis995/openedge.git /opt/openedge
cd /opt/openedge

# 3. Setup automatico .env (genera JWT, DB password, MQTT password)
make setup-env
# Opzionale: nano .env per personalizzare PUBLIC_HOST, OPENEDGE_INITIAL_ADMIN_PASSWORD

# 4. Build e avvio (include TUTTI i driver: Modbus/S7/OPC-UA/MQTT/Redis/LoRaWAN)
make start
```

Output atteso:
```
OpenEdge is starting up.
  Web UI:   http://localhost:3000
  Core API: http://localhost:8081
  Login:    admin / admin123
```

**Con HTTPS (raccomandato anche in LAN)**:
```bash
# Imposta il nome host in .env prima di avviare
echo "PUBLIC_HOST=openedge.local" >> .env   # o IP: 192.168.1.100

make onprem-tls
# Caddy emette un certificato dalla sua CA interna (nessun internet richiesto)

# Esporta la CA e installala una volta su ogni PC operatore
make export-root-ca
# Crea: openedge-root-ca.crt
# Windows: doppio click → Installa in "Autorità di certificazione radice attendibili"
# macOS:   doppio click → Portachiavi di Sistema → fidati del certificato
# Linux:   sudo cp openedge-root-ca.crt /usr/local/share/ca-certificates/ && sudo update-ca-certificates
```

**Auto-start al boot**:
```bash
sudo make install-service
# Registra come servizio systemd — riavvia automaticamente dopo crash o reboot
```

**Verifica**:
```bash
curl http://localhost:8081/health    # {"status":"ok"}
curl http://localhost:8081/ready     # {"status":"ready","db":"ok","redis":"ok"}
docker compose ps                    # tutti i container Up (healthy)
```

---

## Deploy B — Self-hosted Windows

**Prerequisiti**: Docker Desktop, PowerShell 5+, Git for Windows

```powershell
# 1. Clona il repo
git clone https://github.com/inferis995/openedge.git C:\OpenEdge
cd C:\OpenEdge

# 2. Avvia (menu interattivo — no make richiesto)
.\openedge.bat
# Seleziona "Start" dal menu

# 3. Installa come Windows Service (auto-start al boot, prima del login)
#    Da PowerShell elevato (Esegui come amministratore):
Set-ExecutionPolicy -Scope Process Bypass -Force
.\windows\install-service.ps1
# Scarica WinSW (hash SHA256 verificato), registra servizio "OpenEdge"

# Verifica:
Get-Service OpenEdge   # Status: Running
# UI: http://localhost:3000
```

**Disinstalla**:
```powershell
.\windows\install-service.ps1 -Uninstall
```

---

## Deploy C — VPS con Traefik + Let's Encrypt

**Prerequisiti**: VPS con IP pubblico, dominio puntato all'IP, porte 80/443/8883 aperte

```bash
# Opzione 1 — Script automatico (raccomandato)
git clone https://github.com/inferis995/openedge.git
cd openedge
bash deploy/cloud-init.sh
# Chiede: dominio, email Let's Encrypt, password admin
# Fa tutto: installa Docker, ufw, systemd, genera secrets, avvia stack

# Opzione 2 — Manuale
cp .env.cloud.example .env
nano .env  # imposta: PUBLIC_HOST, ACME_EMAIL, POSTGRES_PASSWORD, MQTT_ADMIN_PASSWORD, JWT_SECRET
make vps-up
```

**Verifica**:
```bash
make vps-status
# oppure:
curl https://app.tuazienda.com/health
```

**MQTT TLS** (per edge device che si connettono al cloud):
```
Porta: 8883 (mqtts://app.tuazienda.com:8883)
Traefik gestisce TLS automaticamente.
```

### All-in-one (server + edge sulla stessa macchina)

Se vuoi che i driver girano direttamente sul VPS (senza edge fisici separati):

```bash
make vps-up-edge
# Avvia: Traefik + monitoring + core + driver-manager
```

### Server only (raccomandato per SaaS multi-tenant)

```bash
make vps-up
# Gli edge fisici si connettono via MQTT TLS :8883
# Ogni org scarica il proprio edge installer ZIP dalla UI
```

---

## Deploy D — Coolify

**Prerequisiti**: istanza Coolify su VPS, dominio puntato, porte 80/443/8883 aperte

```
Passo 1 — Installa Coolify (se non presente)
  curl -fsSL https://cdn.coollabs.io/coolify/install.sh | bash
  Apri https://IP-VPS:8000 → crea account

Passo 2 — Punta DNS
  app.tuazienda.com → A → IP_DEL_VPS

Passo 3 — Deploy in Coolify
  New Project → Add Resource → Docker Compose
  Incolla il contenuto di docker-compose.coolify.yml
  In "Domains": https://app.tuazienda.com
  Aggiungi le variabili d'ambiente (vedi sezione "Variabili")
  Deploy

Passo 4 — MQTT/TLS porta 8883
  Coolify → Settings → Traefik → Dynamic Configuration
  Incolla: deploy/coolify-traefik-mqtt.yml (sostituisci YOUR_DOMAIN)
  Coolify → Settings → Traefik → Port Mappings → aggiungi 8883:8883
```

### All-in-one su Coolify

Per avere driver-manager sullo stesso server Coolify, attiva il profilo `edge`:
- In Coolify → Resource → Environment: aggiungi variable `COMPOSE_PROFILES=edge`
- Oppure testa in locale: `make coolify-up-edge`

---

## File compose — struttura

| File | Uso |
|------|-----|
| `docker-compose.yml` | Self-hosted/on-prem. Tutti i driver inclusi. TLS con `--profile tls`. Monitoring con `--profile monitoring` |
| `docker-compose.vps.yml` | VPS con Traefik + Let's Encrypt. Monitoring sempre attivo. Driver-manager con `--profile edge` (`make vps-up-edge`) |
| `docker-compose.coolify.yml` | Coolify — standalone. Driver-manager con `--profile edge` |

**Driver inclusi automaticamente** (nessun file extra necessario):
Modbus TCP · Siemens S7 · OPC-UA · MQTT · Redis · **LoRaWAN** — tutti buildati con `make start`.
Il driver-manager li spawna via Docker socket quando crei un Gateway nella UI.

---

## Variabili d'ambiente chiave

```bash
# Sicurezza — auto-generati da 'make setup-env'
JWT_SECRET=<stringa 32+ char>
POSTGRES_PASSWORD=<password forte>
MQTT_ADMIN_PASSWORD=<password forte>

# Database
POSTGRES_DB=industrial_edge
POSTGRES_USER=industrial_user

# Host
PUBLIC_HOST=app.tuazienda.com     # dominio o IP
ACME_EMAIL=tua@email.com          # solo VPS/Coolify

# Admin iniziale (default: admin123 — cambia dopo il primo login)
OPENEDGE_INITIAL_ADMIN_PASSWORD=<password>

# URL pubblico. Finisce nel documento di discovery OAuth e nei redirect del
# login: se non è impostato viene ricavato dalla richiesta, che è giusto dietro
# un proxy che imposta X-Forwarded-Proto/Host e produce link irraggiungibili
# dietro uno che non lo fa. Impostalo appena hai un dominio.
OPENEDGE_PUBLIC_URL=https://app.tuazienda.com
```

`make setup-env` genera JWT_SECRET, POSTGRES_PASSWORD e MQTT_ADMIN_PASSWORD automaticamente con `openssl rand`.

---

## Monitoring

### Architettura

| Deploy | Monitoring |
|--------|-----------|
| Self-hosted / On-prem | Opzionale — `make monitoring-up` |
| VPS / Cloud | **Sempre attivo** — incluso in `docker-compose.vps.yml` |
| Coolify | Configurabile manualmente post-deploy |

### On-prem — avvio monitoring

```bash
make monitoring-up
# Avvia: Prometheus, Grafana, AlertManager, Loki, Promtail
#        + postgres-exporter, redis-exporter, node-exporter, mosquitto-exporter
```

| Servizio | URL | Credenziali |
|---------|-----|-------------|
| Grafana | http://localhost:3001 | admin / `GRAFANA_ADMIN_PASSWORD` da .env |
| Prometheus | http://localhost:9090 | — |
| AlertManager | http://localhost:9093 | — |

Dashboard provisionati automaticamente:
- **OpenEdge Operations** — API rate/latency/errors, DB connections, Redis, MQTT msg/s, log panel
- **Infrastructure** — CPU, RAM, disco, rete, disk I/O, table size PostgreSQL

### Metriche raccolte

| Exporter | Cosa misura |
|----------|------------|
| `core-api` | HTTP rate, latency p50/p95/p99, errors, goroutines |
| `postgres-exporter` | Connessioni, query lente, table size, vacuum lag |
| `redis-exporter` | Memoria, hit rate, eviction, commands/s |
| `node-exporter` | CPU, RAM, disco, rete host |
| `mosquitto-exporter` | Client connessi, messages/s |

### Alert rules attivi

| Alert | Trigger | Severity |
|-------|---------|---------|
| `CoreAPIDown` | API irraggiungibile >1 min | critical |
| `APIHighLatency` | p99 >2s per 3 min | warning |
| `DBConnectionsHigh` | >20 connessioni per 5 min | warning |
| `PostgresDown` | exporter irraggiungibile | critical |
| `RedisDown` | exporter irraggiungibile | critical |
| `DiskSpaceLow` | disco <15% | warning |
| `DiskSpaceCritical` | disco <5% | critical |
| `HighMemoryUsage` | RAM >90% per 5 min | warning |
| `MQTTBrokerDown` | Mosquitto irraggiungibile >2 min | critical |
| `RedisEvictingKeys` | eviction >10 chiavi/s | warning |

### Configura AlertManager (routing notifiche)

Modifica `monitoring/alertmanager.yml`:
```yaml
# Email (già configurato, imposta ALERTMANAGER_EMAIL_TO in .env)
# Slack: decommenta slack_configs e imposta ALERTMANAGER_SLACK_WEBHOOK
# PagerDuty: decommenta pagerduty_configs e imposta ALERTMANAGER_PAGERDUTY_KEY
```

Ricarica senza restart:
```bash
curl -X POST http://localhost:9093/-/reload
```

### VPS — Grafana accessibile via HTTPS

In VPS mode, Grafana è esposto via Traefik su `https://grafana.tuadominio.com`.
Password configurata in `.env` → `GRAFANA_ADMIN_PASSWORD` (auto-generata da `make setup-env`).

---

## Backup & Restore

```bash
# Backup immediato (pg_dump nel container postgres)
make backup-now
# oppure: ./scripts/backup.sh 30  (mantieni 30 giorni)

# Backup schedulato — aggiungi al crontab
echo "0 3 * * * /opt/openedge/scripts/backup.sh 30" | crontab -

# Copia backup su USB
make backup-to-usb
# oppure: make backup-to-usb USB=/media/chiavetta

# Restore (ATTENZIONE: sovrascrive tutto — 5 secondi di warning)
make restore BACKUP=./backups/openedge_20260617_030000.sql.gz
```

---

## Upgrade sicuro

```bash
make update          # snapshot → git pull → build → up → health check
make update-check    # mostra cosa cambia senza applicare
```

Se l'health check fallisce dopo l'upgrade, lo script propone il rollback.

---

## Gestione organizzazioni (multi-tenant)

### Crea nuova org

```bash
TOKEN=<admin token>

curl -s -X POST https://app.tuazienda.com/api/organizations \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name": "Acme Corp", "description": "Cliente Acme"}'
```

Al momento della creazione il sistema crea automaticamente:
- Utente MQTT dedicato (`org-{id}`) in Mosquitto DynSec
- ACL sul topic `data/{org-slug}/#`
- Credenziali MQTT salvate in `org_mqtt_credentials`

### Invita org admin

```bash
curl -X POST https://app.tuazienda.com/api/organizations/3/invites \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Organization-ID: 3" \
  -H "Content-Type: application/json" \
  -d '{"email": "mario@acme.com", "role": "admin"}'
# L'utente riceve email con link di registrazione (valido 7 giorni)
```

### Lista / aggiorna / elimina

```bash
curl -H "Authorization: Bearer $TOKEN" https://app.tuazienda.com/api/organizations
curl -X PUT  https://app.tuazienda.com/api/organizations/3 -H "Authorization: Bearer $TOKEN" -d '{"name":"Acme Aggiornato"}'
curl -X DELETE https://app.tuazienda.com/api/organizations/3 -H "Authorization: Bearer $TOKEN"

# Forza MFA per tutti gli utenti di un'org (solo global admin)
curl -X PUT https://app.tuazienda.com/api/organizations/3/mfa-required \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"mfa_required": true}'
# → utenti senza MFA configurato vengono bloccati al login con messaggio "configura MFA dal browser"
```

---

## MFA / Autenticazione a due fattori

### Per org (admin enforcement)

| Scenario | Comportamento |
|----------|--------------|
| `mfa_required=false` (default) | MFA opzionale per ogni utente |
| `mfa_required=true` + utente ha TOTP | Login normale con codice OTP |
| `mfa_required=true` + utente senza TOTP | Login bloccato, messaggio "configura MFA" |
| SSO (Google/Azure AD) | MFA delegato al provider — TOTP OpenEdge non richiesto |

### Gestione MFA utente via API

```bash
BASE=https://app.tuazienda.com
AUTH="-H 'Authorization: Bearer $TOKEN'"

# Stato MFA dell'utente corrente
curl $AUTH $BASE/api/auth/me/mfa/status

# Setup — genera secret e QR URL
curl -X POST $AUTH $BASE/api/auth/me/mfa/setup

# Attiva — verifica primo codice, restituisce 8 codici di recupero
curl -X POST $AUTH $BASE/api/auth/me/mfa/enable \
  -H "Content-Type: application/json" -d '{"code":"123456"}'
# Risposta: {"message":"MFA attivato","recovery_codes":["A1B2-C3D4-E5F6",...]}

# Rigenera codici di recupero (brucia i vecchi)
curl -X POST $AUTH $BASE/api/auth/me/mfa/recovery-codes

# Disattiva (richiede password)
curl -X DELETE $AUTH $BASE/api/auth/me/mfa/disable \
  -H "Content-Type: application/json" -d '{"password":"mia-password"}'
```

### Codici di recupero

- Generati automaticamente all'attivazione MFA: **8 codici** formato `XXXX-XXXX-XXXX`
- Mostrati **una sola volta** — salvare in un posto sicuro
- Ognuno è **usa-e-getta**: una volta usato viene marcato come consumato
- Rigenera con `POST /api/auth/me/mfa/recovery-codes` (invalida tutti i vecchi)
- Funzionano sia dal browser che dalla CLI

---

## Edge Installer (deploy edge remoto)

L'org admin scarica un ZIP pre-configurato dalla UI (Infrastructure → Download Edge Installer) o via API:

```bash
curl -o edge-installer.zip \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Organization-ID: 3" \
  https://app.tuazienda.com/api/organizations/3/edge-installer
```

Il cliente esegue solo:
```bash
unzip edge-installer.zip && cd edge && ./install.sh
```

ZIP contiene: `docker-compose.yml`, `.env` pre-compilato con credenziali MQTT org, `install.sh`, `install.ps1`.

---

## OTA Edge Updates

```
Super admin → UI → Releases → "Pubblica Release"
  Richiede: versione, URL artifact, SHA256 checksum (64 char hex)
  Crea approvazione "pending" per ogni org automaticamente

Org admin → banner dashboard "Aggiornamento disponibile" → "Rivedi & Approva"

Edge agent (driver-manager) → ogni 5 min controlla /api/edge/update-check
  Se approved=true: scarica → verifica SHA256 → applica → health check
  Se health check fallisce: rollback automatico → riporta rolled_back

Stato flotta: UI → Releases → Fleet Status
```

---

## Configurazione SMTP

```bash
# Da UI: System → Settings → Notifications → Email
# oppure via API:
curl -X PUT https://app.tuazienda.com/api/system/settings \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "notif_email_enabled": "true",
    "notif_email_smtp_host": "smtp.gmail.com",
    "notif_email_smtp_port": "587",
    "notif_email_use_tls": "false",
    "notif_email_username": "noreply@tuazienda.com",
    "notif_email_password": "gmail-app-password",
    "notif_email_from": "noreply@tuazienda.com",
    "notif_email_to": "alerts@tuazienda.com"
  }'
```

---

## Notifiche (Slack / Teams / PagerDuty)

```bash
# Slack
curl -X PUT https://app.tuazienda.com/api/system/settings \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"notif_slack_enabled":"true","notif_slack_webhook_url":"https://hooks.slack.com/..."}'

# Microsoft Teams
curl -X PUT https://app.tuazienda.com/api/system/settings \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"notif_teams_enabled":"true","notif_teams_webhook_url":"https://outlook.office.com/webhook/..."}'

# PagerDuty
curl -X PUT https://app.tuazienda.com/api/system/settings \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"notif_pagerduty_enabled":"true","notif_pagerduty_routing_key":"abc123..."}'

# Test canale
curl -X POST https://app.tuazienda.com/api/system/notifications/test \
  -H "Authorization: Bearer $TOKEN"
```

---

## Modellazione impianto: UDT prima dei tag

Quando devi mettere in servizio piu' apparecchiature dello stesso genere —
dieci pompe, venti valvole, otto inverter — **non creare i tag uno per uno**.
Definisci un tipo e istanzialo. La differenza si vede alla prima modifica: con
i tag piatti spostare una soglia su cinquanta motori sono cinquanta modifiche,
e quella dimenticata la scopri da un allarme che non scatta.

```bash
# 1. Il tipo, una volta sola
openedge api POST /api/udt/types '{
  "name":"Motore",
  "members":[
    {"name":"Run","address_suffix":"+0","data_type":"BOOL","historize":true},
    {"name":"Fault","address_suffix":"+1","data_type":"BOOL","historize":true,
     "alarms":[{"alarm_type":"high","threshold":1,"severity":"critical",
                "message":"guasto motore","enabled":true}]},
    {"name":"Speed","address_suffix":"+2","data_type":"REAL","historize":true,
     "scaling_enabled":true,"scaling_raw_min":0,"scaling_raw_max":27648,
     "scaling_eu_min":0,"scaling_eu_max":1500,"eu_unit":"rpm"}]}'

# 2. Un'istanza per apparecchiatura
openedge api POST /api/udt/instances \
  '{"type_id":7,"gateway_id":3,"name":"Pompa01","base_address":"40001"}'
```

Il `base_address` dell'istanza piu' l'`address_suffix` del membro danno
l'indirizzo del tag. Lo stesso tipo funziona su Modbus (`40001` + `+2`) e su S7
(`DB10` + `.DBX0.1`): il suffisso porta il separatore che serve al linguaggio di
indirizzamento, cosi' il tipo non deve sapere su quale protocollo finira'.

### In fase di commissioning

Se il cliente ha uno schema di indirizzamento regolare — Pompa01 a 40001,
Pompa02 a 40011, e cosi' via — istanziare dieci pompe e' un ciclo. Verifica
sempre l'indirizzo generato del **primo** tag sul PLC reale prima di crearne
altri nove: un errore nel `base_address` si moltiplica per dieci in silenzio.

### Dalla UI

Menu **Tipi (UDT)** e **Istanze**. Stessa semantica dell'API: l'editor del tipo
salva tutti i membri insieme e riporta la riconciliazione, e la rimozione di un
membro apre la finestra che quantifica la perdita.

### La cosa da non fare in automatico

Rimuovere un membro da un tipo cancella quel tag su **ogni** istanza e con esso
**tutto lo storico** (`tag_history` ha `ON DELETE CASCADE`). L'API rifiuta con
`409` e dice quanti tag e quante righe sono in gioco.

**Non reinviare con `confirm_data_loss: true` per conto tuo.** Riporta il numero
all'operatore e aspetta. Quel rifiuto e' l'unica cosa che separa una modifica di
configurazione da una perdita di dati irreversibile.

---

## Troubleshooting

### Container non parte

```bash
docker compose ps                      # stato tutti i container
docker compose logs -f core-api        # log API
docker compose logs -f mosquitto       # log MQTT
docker compose logs -f postgres        # log DB
```

### Driver non parte

```bash
docker images | grep industrial-driver  # verifica immagini presenti
make start                              # rebuild tutto se mancano immagini
docker compose logs -f driver-manager  # log driver-manager
docker logs openedge-driver-5          # log driver specifico (container ID dal DB)
```

### Edge offline

```bash
# 1. Verifica heartbeat Redis
docker compose exec redis redis-cli get "edge:ping:3"

# 2. Edge status via API
curl -H "Authorization: Bearer $TOKEN" https://app.tuazienda.com/api/organizations/3/edge-status

# 3. Log MQTT
docker compose logs mosquitto | grep "org-3"
```

### DB lento

```bash
docker compose exec postgres psql -U industrial_user -d industrial_edge \
  -c "SELECT count(*), state FROM pg_stat_activity GROUP BY state;"
docker compose restart core-api   # libera connection pool
```

### Reset completo (SOLO DEV — cancella tutti i dati)

```bash
make clean && make start
```

---

## Comandi rapidi

```bash
make start           # prima installazione — build + start
make onprem-tls      # HTTPS con CA interna
make up              # start (immagini già presenti)
make down            # stop
make restart         # stop + start
make logs            # tutti i log in real-time
make install-service # systemd auto-start al boot
make export-root-ca  # esporta CA per browser operatori
make backup-now      # backup immediato
make update          # upgrade sicuro
make monitoring-up   # Prometheus + Grafana + Loki
make vps-up          # VPS — server only (edge su macchine separate)
make vps-up-edge     # VPS — all-in-one (server + driver-manager stesso VPS)
make coolify-up      # Coolify — server only
make coolify-up-edge # Coolify — all-in-one
make help            # lista completa comandi
```

---

---

## OpenEdge CLI — installazione e configurazione

Il CLI `openedge` è il binario da riga di comando che si usa per gestire la piattaforma
senza aprire il browser. Si installa **una volta sola** sulla macchina dell'operatore.

### Installazione da sorgente (su macchina con il repo clonato)

```bash
cd /path/to/openedge
make build-cli        # compila → bin/openedge
make install-cli      # copia in /usr/local/bin/openedge (richiede sudo)
```

### Installazione binario pre-compilato (su qualsiasi macchina)

```bash
# Linux amd64
curl -sL https://github.com/inferis995/openedge/releases/latest/download/openedge-linux-amd64 \
  -o /usr/local/bin/openedge && chmod +x /usr/local/bin/openedge

# macOS arm64 (Apple Silicon)
curl -sL https://github.com/inferis995/openedge/releases/latest/download/openedge-darwin-arm64 \
  -o /usr/local/bin/openedge && chmod +x /usr/local/bin/openedge
```

### Login — on-prem HTTP

```bash
openedge login --url http://192.168.1.100:3000
# → chiede username e password interattivamente
# → salva token in ~/.openedge/config.json
```

### Login — on-prem HTTPS con Caddy internal CA (self-signed)

```bash
# Opzione A: importa il CA cert nel sistema (soluzione corretta per uso continuativo)
make export-root-ca          # dalla macchina server → crea caddy-root-ca.crt
# poi sul client:
# macOS:  doppio click → Portachiavi di Sistema → fidati per TLS
# Linux:  sudo cp caddy-root-ca.crt /usr/local/share/ca-certificates/ && sudo update-ca-certificates
# Windows: doppio click → Installa in "Autorità di Certificazione Radice Attendibili"
openedge login --url https://myedge.local

# Opzione B: skip TLS una-tantum (solo per reti fidate, es. LAN factory)
openedge login --url https://myedge.local --insecure
# il flag insecure=true viene SALVATO nel config — non serve ripassarlo mai più
```

### Login — SaaS / VPS (HTTPS Let's Encrypt, certificato trusted)

```bash
openedge login --url https://app.miazienda.com
```

### Login — CI/CD o script non interattivo

```bash
openedge login --url https://app.miazienda.com --username admin --password "$OPENEDGE_PASSWORD"
# oppure con env vars (no config file, ideale per container):
export OPENEDGE_URL=https://app.miazienda.com
export OPENEDGE_TOKEN=eyJ...
export OPENEDGE_ORG_ID=1
openedge gateways list   # funziona senza login
```

### Login con MFA attivo (TOTP / Google Authenticator)

Il CLI gestisce automaticamente il flusso MFA a 2 step:

```bash
openedge login --url https://app.miazienda.com
# Username: admin
# Password: ••••••••
# Codice MFA (6 cifre o codice di recupero): 123456   ← prompt automatico se MFA è attivo
# ✓ Logged in as admin (global admin)
```

**Se l'org ha `mfa_required=true` e l'utente non ha ancora configurato MFA:**
```bash
# Errore: "Il tuo amministratore richiede MFA. Configuralo da browser: https://app.../profile"
# → l'utente deve andare sul browser e attivare MFA dal profilo prima di usare la CLI
```

**Codici di recupero** (se l'utente ha perso il telefono):
```bash
openedge login --url https://app.miazienda.com
# Codice MFA (6 cifre o codice di recupero): A1B2-C3D4-E5F6
# ✓ Il codice di recupero viene bruciato (usa-e-getta)
```

### Verifica connessione

```bash
openedge whoami          # mostra utente, ruolo, org, URL, TLS mode
openedge health          # verifica che API + DB + Redis siano up
```

### Gestione configurazione

```bash
openedge config show                     # mostra tutto (token mascherato)
openedge config set url https://nuovo.com  # cambia URL senza re-login
openedge config set org 3               # cambia org attiva
openedge config set insecure true       # abilita skip TLS permanente
openedge logout                         # cancella credenziali (equivale a config reset)
```

### Comandi operativi più usati

```bash
# Struttura
openedge orgs list
openedge gateways list [--org 1]

# Lettura tag
openedge tags list --gateway 2
openedge tags get 42
openedge tags shadows --gateway 2       # digital twin — funziona anche edge offline

# Scrittura tag (richiede admin o can_write_tags=true)
openedge tags write 43 1

# Storico
openedge tags history 42 --from 2026-06-19T00:00:00Z --to 2026-06-20T00:00:00Z

# Allarmi
openedge alarms list --active
openedge alarms list --severity critical
openedge alarms ack 45

# Fleet
openedge fleet status                   # tutte le org: online/offline, versione
openedge fleet restart 1               # riavvia edge org 1
openedge fleet update 1 --version v2.1.0  # OTA update edge org 1

# AI-Ops
openedge aiops summary --hours 24
openedge aiops anomalies --tag 42
openedge aiops digest

# Output JSON per script/jq
openedge tags list --gateway 2 --json | jq '.[].alias'
```

### MCP server per Claude Code / Cursor

```bash
# Aggiungi a ~/.claude/settings.json
{
  "mcpServers": {
    "openedge": {
      "command": "openedge",
      "args": ["mcp"],
      "env": {
        "OPENEDGE_URL": "https://app.miazienda.com",
        "OPENEDGE_TOKEN": "eyJ...",
        "OPENEDGE_ORG_ID": "1"
      }
    }
  }
}
```

Dopo il riavvio di Claude Code, i tool OpenEdge appaiono direttamente nella chat
(`list_gateways`, `get_tag_value`, `list_active_alarms`, ecc.).

### MCP server remoto (per client che non girano su questa macchina)

La configurazione qui sopra lancia un processo locale che usa **il tuo** token.
Per un server condiviso serve il trasporto HTTP, dove l'identità arriva con
ogni richiesta:

```bash
# systemd unit, accanto al core API
openedge mcp --http 127.0.0.1:9090 \
  --url http://127.0.0.1:8081 \
  --auth-server https://app.miazienda.com \
  --public-url https://mcp.miazienda.com
```

Poi esponi `https://mcp.miazienda.com` col reverse proxy verso `127.0.0.1:9090`.
Verifica prima di collegare qualsiasi client:

```bash
curl -s https://mcp.miazienda.com/healthz
curl -s https://mcp.miazienda.com/.well-known/oauth-protected-resource | jq .
curl -si -X POST https://mcp.miazienda.com/mcp -d '{}' | grep -i www-authenticate
```

L'ultimo comando deve rispondere `401` con un header `WWW-Authenticate` che
punta al `resource_metadata`. Se manca, `--auth-server` non è impostato e i
client dovranno portarsi dietro un token statico.

**Cosa questo NON fa**: un server MCP dà a un agente degli *strumenti*. Non
mostra i sinottici né l'interfaccia dentro il client. La schermata di login
OAuth, invece, è la tua.

### OAuth — dare accesso a un client senza dargli un token

`OPENEDGE_PUBLIC_URL` deve essere impostato: finisce nel documento di discovery
e nei redirect del login.

```bash
curl -s https://app.miazienda.com/.well-known/oauth-authorization-server | jq .
```

- Scope `openedge:read` / `openedge:write`. Un token read-only viene rifiutato
  con 403 su ogni POST/PUT/PATCH/DELETE.
- Access token 1 ora, refresh 30 giorni con rotazione.
- La registrazione dei client è aperta (RFC 7591) e passa dal rate limiter del
  login. Un client registrato senza un utente che lo approva non accede a nulla.
- Per vedere chi ha autorizzato cosa:

```sql
SELECT c.client_name, u.username, r.scope, r.created_at, r.revoked_at
  FROM oauth_refresh_tokens r
  JOIN oauth_clients c ON c.client_id = r.client_id
  JOIN users u ON u.id = r.user_id
 ORDER BY r.created_at DESC LIMIT 20;
```

Per togliere l'accesso a un client:

```sql
UPDATE oauth_refresh_tokens SET revoked_at = NOW()
 WHERE client_id = '<client_id>' AND revoked_at IS NULL;
```

L'access token già emesso resta valido fino alla scadenza (max un'ora): è un
JWT autoconsistente, verificato solo sulla firma e sulla scadenza, e non c'è
modo di richiamarlo prima. Se serve una revoca immediata l'unica leva è
cambiare `JWT_SECRET` e riavviare — che invalida **tutte** le sessioni di
tutti, non solo quel client.

> `users.token_version` esiste ed è nei claim, ma `middleware.RequireAuth` non
> lo confronta con la colonna: oggi non revoca nulla. Vedi il TODO in
> `internal/auth/auth.go`.

---

## NIS2 / IEC 62443 Compliance — Monitoraggio automatico per org

OpenEdge implementa **"From Visibility to Compliance in Four Steps"** con dati automatici
estratti dai gateway già configurati di ogni org. Nessuna doppia inserzione dati.

### Come funziona l'automatismo

```
Org configura gateway → sync automatico ogni ora → asset inventory popolato
                      → risk score calcolato da protocollo + stato online
                      → auto-assessment NIS2 aggiornato dai dati reali
```

| Cosa si popola automaticamente | Da dove |
|-------------------------------|---------|
| OT Asset inventory | Tabella `gateways` di ogni org (IP, protocollo, nome, stato online) |
| Risk score per asset | Protocollo non cifrato (+3), offline >48h (+2), TLS attivo (-2) |
| Vendor inventory (Art.18) | Campo `vendor` in `connection_config` di ogni gateway |
| 12 requisiti NIS2 auto-assessed | Dati reali: scan jobs, threat events, protocolli, vendor scored |

### Step 1 — Asset Discovery (automatico)

```bash
# Gli asset si sincronizzano automaticamente ogni ora.
# Per forzare la sincronizzazione manuale:
curl -X POST https://app.miazienda.com/api/compliance/sync-assets \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Organization-ID: 3"
# → {"created": 4, "updated": 2}
# Risponde con quanti gateway sono stati importati come OT asset

# Lista asset per org:
curl -H "Authorization: Bearer $TOKEN" \
  -H "X-Organization-ID: 3" \
  https://app.miazienda.com/api/compliance/assets
```

### Step 2 — Risk Posture + NIS2 Auto-Valutazione

```bash
# Risk posture dell'org:
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 3" \
  https://app.miazienda.com/api/compliance/risk-posture
# → {total_assets, avg_risk_score, critical_cves, by_type, top_risky}

# Score NIS2/IEC62443 (0-100 pesato):
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 3" \
  https://app.miazienda.com/api/compliance/score

# Auto-valuta i 12 requisiti NIS2 auto-assessable dai dati reali:
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 3" \
  https://app.miazienda.com/api/compliance/auto-assess
# → lista di {req_code, status, evidence} aggiornati automaticamente

# Checklist completa (30 item NIS2 Art.21 a-j):
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 3" \
  https://app.miazienda.com/api/compliance/frameworks/NIS2/assessment

# Aggiorna singolo requisito (item manuale):
curl -X PUT https://app.miazienda.com/api/compliance/frameworks/NIS2/assessment/15 \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Organization-ID: 3" \
  -d '{"status":"compliant","evidence":"Piano BCP approvato CdA 2026-01-15","notes":""}'
```

**Requisiti NIS2 auto-assessed** (si valorizzano da soli):
| Codice | Controllo Art.21 | Come viene valutato |
|--------|-----------------|---------------------|
| NIS2-A2 | Analisi del rischio | Scan eseguito negli ultimi 30gg? |
| NIS2-B1 | Rilevamento incidenti | Threat events registrati? |
| NIS2-B3 | CSIRT 24h | Almeno un CSIRT incident aperto? |
| NIS2-D1 | Inventario fornitori | `ot_vendors` count > 0? |
| NIS2-D2 | Valutazione fornitori | Vendor con score calcolato? |
| NIS2-D3 | Clausole contratti | % vendor con security_clauses |
| NIS2-E2 | Protocolli sicuri OT | % gateway con OPC-UA/MQTT vs Modbus raw |
| NIS2-H1 | Crittografia in transito | Stessa metrica E2 |
| NIS2-J3 | Segregazione OT/IT | Asset inventory presente? |
| NIS2-J4 | Audit log | Sempre compliant (sistema ha audit trail) |

### Step 3 — CSIRT Art.23 (incidenti con countdown legali)

```bash
# Lista incidenti CSIRT:
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 3" \
  https://app.miazienda.com/api/compliance/csirt

# Summary (quanti aperti, scaduti):
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 3" \
  https://app.miazienda.com/api/compliance/csirt/summary

# Crea nuovo incidente (deadline impostate automaticamente: +24h, +72h, +30gg):
curl -X POST https://app.miazienda.com/api/compliance/csirt \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Organization-ID: 3" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Accesso non autorizzato gateway Linea 3",
    "severity": "high",
    "description": "Rilevato tentativo di accesso anomalo al gateway Modbus 192.168.1.45",
    "affected_systems": "Gateway Linea 3, Tag temperatura/pressione"
  }'
# → risponde con id, early_warning_due (now+24h), notification_due (now+72h), final_report_due (now+30d)

# Marca Early Warning inviata (Art.23 — entro 24h):
curl -X PUT https://app.miazienda.com/api/compliance/csirt/7/early-warning \
  -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 3"

# Marca Notifica formale inviata (Art.23 — entro 72h):
curl -X PUT https://app.miazienda.com/api/compliance/csirt/7/notify \
  -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 3"

# Chiudi incidente con root cause (Art.23 — entro 30gg):
curl -X PUT https://app.miazienda.com/api/compliance/csirt/7/close \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Organization-ID: 3" \
  -d '{"root_cause":"Credenziali MQTT compromesse","remediation":"Rotazione credenziali, whitelist IP"}'
```

**Flusso CSIRT obbligatorio NIS2:**
```
Incidente rilevato
  ├── entro 24h → Early Warning ad ACN/CSIRT-IT  (notif. preliminare)
  ├── entro 72h → Notifica formale con impatto stimato
  └── entro 30gg → Rapporto finale con root cause + remediation
```

### Step 4 — Vendor Risk Art.18 (supply chain)

```bash
# Sincronizza vendor automaticamente dai gateway configurati:
curl -X POST https://app.miazienda.com/api/compliance/vendors/sync \
  -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 3"
# → {"imported": 3, "updated": 1}
# Legge connection_config->'vendor' di ogni gateway e crea record

# Lista vendor con score:
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 3" \
  https://app.miazienda.com/api/compliance/vendors

# Summary (critical, high, avg_score):
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 3" \
  https://app.miazienda.com/api/compliance/vendors/summary

# Aggiorna vendor (aggiungi certificazioni per migliorare score):
curl -X PUT https://app.miazienda.com/api/compliance/vendors/2 \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Organization-ID: 3" \
  -d '{
    "has_iso27001": true,
    "last_audit_date": "2025-11-01",
    "security_clauses": true,
    "data_access_level": "write"
  }'

# Ricalcola score dopo aggiornamento:
curl -X POST https://app.miazienda.com/api/compliance/vendors/2/score \
  -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 3"
```

**Formula score vendor** (0-100, più alto = meno rischioso):
```
Base: 50
+15  ISO 27001 certificato
+10  SOC 2 certificato
+15  IEC 62443 certificato
+10  Ultimo audit < 1 anno (+5 se < 2 anni)
+10  Clausole di sicurezza nel contratto
-20  Accesso admin ai sistemi
-10  Accesso write ai sistemi / remote access
-10  Fornitore non-EU (paese non in lista EU-27)
→ Criticità: 0-25=critical, 26-50=high, 51-75=medium, 76-100=low
```

### Report compliance

```bash
# Genera report NIS2 per org (asincrono — ritorna id subito):
curl -X POST https://app.miazienda.com/api/compliance/reports \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Organization-ID: 3" \
  -d '{"report_type":"nis2_assessment","title":"NIS2 Q2 2026","period_from":"2026-01-01T00:00:00Z","period_to":"2026-06-30T23:59:59Z"}'

# Lista report generati:
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 3" \
  https://app.miazienda.com/api/compliance/reports

# Scarica report specifico:
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 3" \
  https://app.miazienda.com/api/compliance/reports/12 | jq '.content'
```

Tipi report: `nis2_assessment`, `iec62443_assessment`, `asset_inventory`, `incident_timeline`, `full_compliance`

### Via MCP server (Claude Code / Cursor)

Con `openedge mcp` attivo, Claude può monitorare la compliance direttamente in chat:

```
"Qual è il risk score medio dell'org 3?"
"Ci sono incidenti CSIRT scaduti per la notifica?"
"Sincronizza i vendor dell'org 2 dai gateway e mostrami quelli critici"
"Genera un report NIS2 per il trimestre Q2 2026"
"Quanti asset OT hanno risk score > 7?"
"Auto-valuta la compliance NIS2 dell'org 1 e dimmi cosa manca"
```

### UI — pagine compliance (per ogni org)

| Rotta | Cosa mostra |
|-------|------------|
| `/compliance/assets` | Asset inventory auto-popolato, CVE, scan, bottone "Sincronizza da Gateway" |
| `/compliance/risk` | Score NIS2/IEC62443, checklist 30 item, bottone "Auto-valuta ⚡" |
| `/compliance/threats` | Threat events con severità, risoluzione, auto-refresh 30s |
| `/compliance/csirt` | Incidenti Art.23 con countdown timer 24h/72h/30gg, azioni legali |
| `/compliance/vendors` | Vendor risk Art.18 con score bar, certificazioni, sync da gateway |
| `/compliance/reports` | Generazione asincrona + download JSON |

---

## Esempi di prompt per l'agente

```
"Voglio installare OpenEdge sulla mia macchina Linux in fabbrica"
"Come installo OpenEdge su Windows per uso interno?"
"Installa OpenEdge su questo VPS con dominio app.miazienda.com"
"Voglio usare Coolify — come lo deplopo?"
"Crea una nuova organizzazione per il cliente Rossi Srl"
"Invita mario@rossi.com come admin dell'org 5"
"Configura Slack per le notifiche di allarme"
"Il driver del gateway 7 non parte — diagnostica"
"Fai un backup del DB"
"L'edge dell'org 4 è offline — cosa faccio?"
"Abilita il monitoring con Grafana"
"Aggiorna OpenEdge all'ultima versione"
"Installa la CLI sulla mia macchina"
"Come configuro la CLI per on-prem con certificato self-signed?"
"Aggiungi openedge come MCP server in Claude Code"

"Sincronizza gli asset OT dell'org 3 dai gateway configurati"
"Qual è il risk score NIS2 dell'org 2?"
"Auto-valuta la compliance NIS2 dell'org 1"
"Crea un incidente CSIRT per accesso non autorizzato al gateway Linea 3"
"Quanto tempo manca alla scadenza Early Warning dell'incidente 7?"
"Sincronizza i vendor dell'org 3 dai gateway e mostrami quelli critici"
"Genera un report NIS2 Q2 2026 per l'org 4"
"Quali requisiti NIS2 dell'org 2 sono non conformi?"
"L'org 5 ha incidenti CSIRT con notifica scaduta?"
"Come funziona l'auto-sync degli asset OT da gateway?"
```
