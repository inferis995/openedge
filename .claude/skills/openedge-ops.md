---
name: openedge-ops
description: OpenEdge Operations — installa, configura e gestisce OpenEdge in produzione. Chiede sempre all'utente cosa vuole fare prima di agire. Supporta self-hosted/on-prem (Linux+Windows), VPS con Traefik, Coolify. Include driver industriali (Modbus/S7/OPC-UA/MQTT/LoRaWAN), multi-tenant, backup, monitoring, OTA updates, security.
version: 5.0.0
tags: [industrial, iot, devops, docker, deploy, coolify, traefik, vps, on-prem, self-hosted, multi-tenant, install, troubleshoot, mqtt, webhooks, backup, monitoring, health-checks, ota-updates, security, lorawan]
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

---

## File compose — struttura

| File | Uso |
|------|-----|
| `docker-compose.yml` | Self-hosted/on-prem. Include tutti i driver. TLS con `--profile tls` |
| `docker-compose.vps.yml` | VPS con Traefik + Let's Encrypt |
| `docker-compose.coolify.yml` | Coolify — standalone, no overlay |
| `docker-compose.monitoring.yml` | Overlay opzionale: Prometheus + Grafana + Loki |

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
```

`make setup-env` genera JWT_SECRET, POSTGRES_PASSWORD e MQTT_ADMIN_PASSWORD automaticamente con `openssl rand`.

---

## Monitoring (opzionale)

```bash
make monitoring-up    # avvia Prometheus + Grafana + Loki
make monitoring-down  # ferma
```

| Servizio | URL | Credenziali |
|---------|-----|-------------|
| Grafana | http://localhost:3001 | admin / admin |
| Prometheus | http://localhost:9090 | — |
| Core API metrics | http://localhost:8081/metrics | — |

Dashboard "OpenEdge Overview" e datasource già provisionati automaticamente.

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
```

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
make help            # lista completa comandi
```

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
```
