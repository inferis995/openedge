---
name: openedge-ops
description: OpenEdge Operations — installa OpenEdge in produzione (Coolify/VPS/on-prem), gestisci organizzazioni, invita utenti, configura gateway/tag, webhook, troubleshoot container/driver/DB, backup/restore. Multi-tenant SaaS. on-prem, self-hosted, security-center, ota-updates, backup, monitoring, health-checks.
version: 4.1.0
tags: [industrial, iot, devops, docker, deploy, coolify, traefik, saas, multi-tenant, install, troubleshoot, mqtt, webhooks, invites, on-prem, self-hosted, security-center, ota-updates, backup, monitoring, health-checks]
requires: [docker, curl, git]
---

# OpenEdge Ops — Skill operativa

Skill per **installare, configurare e risolvere problemi** di OpenEdge in produzione.

OpenEdge è una piattaforma **multi-tenant SaaS**: un'unica istanza serve N clienti
(organizzazioni), ognuno isolato. Il global admin gestisce le org; ogni org admin
gestisce autonomamente i propri utenti e la propria infrastruttura dalla UI.

> Questa skill **scrive** sul sistema (deploy, config, restart, inviti).
> Per **leggere** lo stato (allarmi, valori, OEE, anomalie) usa `openedge.md`.

---

## Variabili d'ambiente attese

```bash
OPENEDGE_DIR=/home/user/openedge    # path repo locale
OPENEDGE_HOST=app.yourdomain.com    # dominio produzione
OPENEDGE_PORT=443
OPENEDGE_PROTOCOL=https
OPENEDGE_USERNAME=admin
OPENEDGE_PASSWORD=admin123
OPENEDGE_ORG_ID=1
```

---

## 1. Deploy — Prima installazione

### Opzione A: Coolify (raccomandato per SaaS)

Coolify gestisce HTTPS, Let's Encrypt, reverse proxy automaticamente.

**Passo 1 — Installa Coolify sul VPS** (Ubuntu 22+, minimo 2vCPU/4GB):
```bash
curl -fsSL https://cdn.coollabs.io/coolify/install.sh | bash
# Apri https://IP-VPS:8000 → crea account
```

**Passo 2 — Punta DNS**:
```
app.yourdomain.com → A → IP_DEL_VPS
```
Attendi propagazione (~5-30 min) prima di procedere.

**Passo 3 — Deploy in Coolify**:
1. New Project → Add Resource → Docker Compose
2. Incolla il contenuto di `docker-compose.coolify.yml` (o punta al repo)
3. In "Domains" scrivi `https://app.yourdomain.com`
4. Aggiungi le variabili d'ambiente (vedi sezione 2)
5. Deploy

**Passo 4 — MQTT/TLS porta 8883** (per edge device):
- Coolify → Settings → Traefik → Dynamic Configuration → incolla `deploy/coolify-traefik-mqtt.yml`
- Coolify → Settings → Traefik → Port Mappings → aggiungi `8883:8883`
- Sostituisci `YOUR_DOMAIN` con il tuo dominio nel config MQTT

---

### Opzione B: VPS self-hosted (script automatico)

```bash
git clone https://github.com/inferis995/openedge.git
cd openedge
bash deploy/cloud-init.sh
# Chiede: dominio, email Let's Encrypt, password admin iniziale
# Installa: Docker, ufw (22/80/443/8883), systemd service
# Genera secrets, verifica DNS, avvia tutto
```

Oppure manualmente con Traefik:
```bash
cp .env.cloud.example .env
nano .env   # imposta PUBLIC_HOST, ACME_EMAIL, JWT_SECRET, DB_PASSWORD, MQTT_ADMIN_PASSWORD
docker compose -f docker-compose.yml -f docker-compose.cloud.yml up -d
```

---

### Opzione C: Locale / on-prem (dev o uso interno)

```bash
git clone https://github.com/inferis995/openedge.git
cd openedge
make start     # genera .env, builda immagini, avvia tutto
# UI: http://localhost:3000  —  API: http://localhost:8081
```

**Windows**: doppio click su `openedge.bat` → menù interattivo.

---

## On-Prem (factory / self-hosted — raccomandato per clienti industriali)

Tutto gira sulla macchina del cliente. Nessuna dipendenza cloud. Compatibile air-gap.

### Linux (raccomandato)

```bash
# 1. Prerequisiti: Docker 24+, Docker Compose plugin
curl -fsSL https://get.docker.com | bash

# 2. Clona il repo
git clone https://github.com/inferis995/openedge.git /opt/openedge
cd /opt/openedge

# 3. Configura (auto-genera JWT_SECRET)
make setup-env
# Modifica .env: imposta POSTGRES_PASSWORD, MQTT_ADMIN_PASSWORD, PUBLIC_HOST

# 4. Build e avvio
make start
# Web UI: http://localhost:3000
# API:    http://localhost:8081
# Login di default: admin / admin123 (CAMBIA SUBITO)

# 5. Abilita HTTPS con CA interna (raccomandato)
make onprem-tls
# Caddy emette un certificato self-signed per PUBLIC_HOST (es. openedge.local)
# Esporta e installa la CA su ogni PC operatore:
make export-root-ca

# 6. Installa come servizio di sistema (auto-start al boot, sopravvive ai riavvii)
sudo make install-service
```

### Windows (PC industriale)

```powershell
# Da PowerShell elevato nella cartella del repo:
.\windows\install-service.ps1
# Registra "OpenEdge" come Windows Service usando WinSW
# Si avvia automaticamente al boot, prima del login utente
# Log: .\logs\openedge.out.log

# Verifica:
Get-Service OpenEdge
```

### Monitoring On-Prem

```bash
# Avvia Prometheus + Grafana + Loki (metriche, dashboard, log)
docker-compose -f docker-compose.yml -f docker-compose.monitoring.yml --profile monitoring up -d
# Grafana:    http://localhost:3030 (admin/admin)
# Prometheus: http://localhost:9090
# Metriche esposte su: http://localhost:8081/metrics
```

### Backup & Restore

```bash
# Backup manuale (aggiunge timestamp, rotazione 30 giorni di default)
./scripts/backup.sh 30

# Backup schedulato: aggiungi al crontab
echo "0 3 * * * /opt/openedge/scripts/backup.sh 30" | crontab -

# Restore (5 secondi di warning prima di sovrascrivere)
./scripts/restore.sh backups/openedge_20260617_030000.sql.gz
```

### Health Checks

```bash
# Liveness (sempre 200 se il processo è vivo)
curl http://localhost:8081/health

# Readiness (controlla DB + Redis)
curl http://localhost:8081/ready

# Diagnostica completa (richiede token auth)
curl -H "Authorization: Bearer TOKEN" http://localhost:8081/api/health/detailed
```

---

## 2. Variabili d'ambiente di produzione

```bash
# Sicurezza — genera con: openssl rand -base64 32
JWT_SECRET=<stringa random 32+ char>

# Database
POSTGRES_DB=industrial_edge
POSTGRES_USER=industrial_user
POSTGRES_PASSWORD=<password forte>

# MQTT
MQTT_ADMIN_USER=core-api
MQTT_ADMIN_PASSWORD=<password forte>

# Host pubblico
PUBLIC_HOST=app.yourdomain.com
ACME_EMAIL=tua@email.com

# Opzionale: password admin iniziale (default: admin123)
OPENEDGE_INITIAL_ADMIN_PASSWORD=<password>
```

Genera tutti i secret in un colpo:
```bash
echo "JWT_SECRET=$(openssl rand -base64 32)"
echo "POSTGRES_PASSWORD=$(openssl rand -base64 24)"
echo "MQTT_ADMIN_PASSWORD=$(openssl rand -base64 24)"
```

---

## 3. Primo avvio — verifica

Dopo il deploy, verifica che tutto funzioni:

```bash
# Health check
curl https://app.yourdomain.com/health
# Atteso: {"status":"ok"}

# Readiness (DB + Redis)
curl https://app.yourdomain.com/ready
# Atteso: {"status":"ready","db":"ok","redis":"ok"}

# Login
curl -s -X POST https://app.yourdomain.com/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin123"}'
# Atteso: {"token":"eyJ...","user":{...}}
```

---

## 4. Gestione organizzazioni (clienti)

### Crea una nuova organizzazione

```bash
TOKEN=<admin token>

curl -s -X POST https://app.yourdomain.com/api/organizations \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name": "Acme Corp", "description": "Cliente Acme"}'
```

Al momento della creazione il sistema **automaticamente**:
- Crea un utente MQTT dedicato (`org-{id}`) in Mosquitto DynSec
- Assegna ACL sul topic `data/acme-corp/#`
- Salva le credenziali MQTT in `org_mqtt_credentials`

Non serve fare niente manualmente per MQTT.

### Lista organizzazioni

```bash
curl -H "Authorization: Bearer $TOKEN" \
  https://app.yourdomain.com/api/organizations
```

### Aggiorna o elimina

```bash
curl -X PUT https://app.yourdomain.com/api/organizations/3 \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name": "Acme Corp Updated"}'

curl -X DELETE https://app.yourdomain.com/api/organizations/3 \
  -H "Authorization: Bearer $TOKEN"
```

---

## 5. Gestione utenti e inviti

### Invita un utente (org admin)

```bash
# L'org admin (o global admin) invita un utente via email
curl -X POST https://app.yourdomain.com/api/organizations/3/invites \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Organization-ID: 3" \
  -H "Content-Type: application/json" \
  -d '{"email": "mario@acme.com", "role": "user"}'
```

Il sistema:
1. Genera un token one-time (TTL 7 giorni)
2. Invia email con link `/accept-invite?token=...`
3. L'utente clicca, sceglie username e password → account creato

### Lista utenti di un'org

```bash
curl -H "Authorization: Bearer $TOKEN" \
  -H "X-Organization-ID: 3" \
  https://app.yourdomain.com/api/users
```

### Crea utente direttamente (admin)

```bash
curl -X POST https://app.yourdomain.com/api/users \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Organization-ID: 3" \
  -H "Content-Type: application/json" \
  -d '{"username": "mario", "password": "...", "role": "user", "org_id": 3}'
```

### Aggiorna o elimina utente

```bash
curl -X PUT https://app.yourdomain.com/api/users/5 \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"role": "admin"}'

curl -X DELETE https://app.yourdomain.com/api/users/5 \
  -H "Authorization: Bearer $TOKEN"
```

---

## 6. Configurazione SMTP (per email invite e reset password)

Imposta dalla UI: **System → Settings → Notifications** oppure via API:

```bash
curl -X PUT https://app.yourdomain.com/api/system/settings \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "notif_email_enabled": "true",
    "notif_email_smtp_host": "smtp.gmail.com",
    "notif_email_smtp_port": "587",
    "notif_email_use_tls": "false",
    "notif_email_username": "noreply@yourdomain.com",
    "notif_email_password": "gmail-app-password",
    "notif_email_from": "noreply@yourdomain.com",
    "notif_email_to": "alerts@yourdomain.com"
  }'
```

Test invio email:
```bash
curl -X POST https://app.yourdomain.com/api/system/notifications/test \
  -H "Authorization: Bearer $TOKEN"
```

---

## 7. Edge installer (download ZIP pre-configurato)

L'org admin può scaricare il pacchetto edge dalla UI (Infrastructure → Download Edge Installer) o via API:

```bash
curl -o edge-installer.zip \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Organization-ID: 3" \
  https://app.yourdomain.com/api/organizations/3/edge-installer
```

Lo ZIP contiene:
- `docker-compose.yml` — solo i servizi edge (driver-manager + driver)
- `.env` — pre-compilato con credenziali MQTT dell'org, endpoint cloud
- `install.sh` — script Linux (chmod +x && ./install.sh)
- `install.ps1` — script Windows PowerShell

Il cliente esegue solo:
```bash
unzip edge-installer.zip && cd edge && ./install.sh
```

---

## 8. Webhooks (notifiche HTTP a sistemi esterni)

### Crea webhook

```bash
curl -X POST https://app.yourdomain.com/api/organizations/3/webhooks \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Organization-ID: 3" \
  -H "Content-Type: application/json" \
  -d '{
    "url": "https://acme.com/openedge-hook",
    "events": ["alarm.active", "alarm.cleared", "edge.online", "edge.offline"]
  }'
# Risposta: {"id":1, "secret":"wh_sec_abc123..."} ← secret mostrato UNA VOLTA sola
```

**Eventi disponibili**: `alarm.active`, `alarm.cleared`, `tag.write`, `edge.online`, `edge.offline`

**Payload webhook** (firmato con HMAC-SHA256 nell'header `X-OpenEdge-Signature`):
```json
{
  "event": "alarm.active",
  "org_id": 3,
  "data": {
    "alarm_id": 45,
    "tag_id": 42,
    "tag_alias": "Portata_Ingresso",
    "severity": "critical",
    "message": "Portata massima superata",
    "value_at_trigger": 98.7,
    "occurred_at": "2026-06-13T08:15:00Z"
  }
}
```

### Lista webhook con stato

```bash
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 3" \
  https://app.yourdomain.com/api/organizations/3/webhooks
```

### Elimina webhook

```bash
curl -X DELETE https://app.yourdomain.com/api/organizations/3/webhooks/1 \
  -H "Authorization: Bearer $TOKEN"
```

---

## 9. Configurazione gateway e tag

### Crea sito → area → gateway

```bash
# Sito
curl -X POST https://app.yourdomain.com/api/sites \
  -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 3" \
  -d '{"name": "Stabilimento Milano", "org_id": 3}'

# Area
curl -X POST https://app.yourdomain.com/api/areas \
  -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 3" \
  -d '{"name": "Linea-A", "site_id": 1}'

# Gateway
curl -X POST https://app.yourdomain.com/api/gateways \
  -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 3" \
  -d '{
    "name": "PLC-Linea-A",
    "driver_type": "MODBUS_TCP",
    "ip_address": "192.168.1.10",
    "port": 502,
    "area_id": 1,
    "polling_interval": 1000
  }'
```

### Import tag (formato PLC address)

```bash
curl -X POST https://app.yourdomain.com/api/tags/import \
  -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 3" \
  -H "Content-Type: application/json" \
  -d '{
    "gateway_id": 5,
    "historize": true,
    "content": "Portata_Ingresso : REAL AT 40001;\nLivello_Vasca : REAL AT 40003;\nPompa_On : BOOL AT 00001.0;"
  }'
```

Formati supportati:

**Modbus TCP**: `Alias : DataType AT Address;`
```
Portata_Ingresso : REAL AT 40001;
Livello_Vasca    : REAL AT 40003;
Pompa_On         : BOOL AT 00001.0;
```

**Siemens S7**: `Alias : DataType AT Address;`
```
DB1_Temperatura : REAL AT DB1.DBD4;
DB1_Contatore   : INT  AT DB1.DBW0;
M_Marcia        : BOOL AT M0.0;
```

Tipi supportati: `BOOL`, `INT`, `UINT`, `DINT`, `UDINT`, `REAL`, `STRING`, `WORD`

---

## 10. Monitoring stack

```bash
make monitoring-up     # avvia Prometheus + Grafana + Loki + Promtail
make monitoring-down   # ferma
make monitoring-logs   # log
```

| Servizio | URL locale | Credenziali |
|---------|------------|-------------|
| Grafana | http://localhost:3001 | admin / admin |
| Prometheus | http://localhost:9090 | — |
| Loki | http://localhost:3100 | — |

Dashboard OpenEdge Overview già inclusa in Grafana. Datasource Prometheus e Loki auto-provisionati.

---

## 11. Backup e Restore

### Export backup (DB + config)

```bash
curl -o backup.tar.gz \
  -H "Authorization: Bearer $TOKEN" \
  https://app.yourdomain.com/api/system/backup
```

### Import restore

```bash
curl -X POST https://app.yourdomain.com/api/system/restore \
  -H "Authorization: Bearer $TOKEN" \
  -F "file=@backup.tar.gz"
```

### Impostazioni backup automatico

```bash
# Configura backup notturno (default: ore 3:00 UTC)
curl -X PUT https://app.yourdomain.com/api/system/backup/settings \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "backup_enabled": "true",
    "backup_schedule": "0 3 * * *",
    "backup_retention_days": "30"
  }'
```

---

## 12. Troubleshooting

### Verifica stato container

```bash
docker compose ps
docker compose logs -f core-api
docker compose logs -f mosquitto
docker compose logs -f postgres
```

### Edge offline — diagnostica

```bash
# 1. Verifica heartbeat MQTT
docker compose logs mosquitto | grep "org-3"

# 2. Verifica Redis TTL heartbeat
docker compose exec redis redis-cli get "edge:ping:3"

# 3. Edge status via API
curl -H "Authorization: Bearer $TOKEN" \
  https://app.yourdomain.com/api/organizations/3/edge-status
```

### Driver non parte

```bash
# Verifica immagini driver disponibili
docker images | grep industrial-driver

# Se mancano — rebuild
make start

# Log driver-manager
docker compose logs -f driver-manager

# Log del driver specifico per gateway_id=5
docker logs industrial-driver-5
```

### DB lento / connessioni esaurite

```bash
# Verifica connessioni attive
docker compose exec postgres psql -U industrial_user -d industrial_edge \
  -c "SELECT count(*), state FROM pg_stat_activity GROUP BY state;"

# Restart core-api (libera pool)
docker compose restart core-api
```

### Webhook non arriva

```bash
# 1. Verifica URL raggiungibile dall'esterno
curl -X POST https://your-receiver.com/hook -d '{"test":true}'

# 2. Controlla ultimo errore via API
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 3" \
  https://app.yourdomain.com/api/organizations/3/webhooks
# Guarda: last_status_code, last_error

# 3. Verifica firma HMAC nel receiver
# Header: X-OpenEdge-Signature: sha256=<hex>
```

### Reset completo (cancella tutti i dati)

```bash
make clean && make start
# ATTENZIONE: distrugge tutti i dati. Usare SOLO in dev.
```

### Comandi utili

```bash
make start         # build + start (prima volta)
make up            # start (immagini già presenti)
make down          # stop
make restart       # stop + start
make logs          # tutti i log
make monitoring-up # avvia stack monitoraggio
```

---

## Security Center

La dashboard Security Center (solo admin, `/security`) fornisce:
- Punteggio di sicurezza 0-100 con breakdown
- Checklist di conformità NIS2 Art. 21 (12 controlli)
- Feed eventi di sicurezza (blocchi account, tentativi di login falliti)
- Inventario infrastruttura con stato TLS/auth

### Blocco account
5 tentativi di login falliti → blocco di 30 minuti. Si resetta automaticamente al login corretto.
L'admin può sbloccare tramite: `PUT /api/users/:id` (impostare `locked_until: null`).

### OTA Edge Updates (super admin)

1. Il super admin pubblica una release: UI → Releases → "Pubblica Release"
   - Richiede: versione, URL artifact, checksum SHA256 (64 caratteri hex)
   - Crea automaticamente un'approvazione `pending` per ogni org

2. L'org admin approva: banner dashboard "Aggiornamento vX.X.X disponibile" → "Rivedi & Approva"

3. L'edge agent (driver-manager) controlla `/api/edge/update-check` ogni 5 minuti
   - Se `approved=true`: scarica l'artifact, verifica SHA256, applica l'aggiornamento
   - Health check post-aggiornamento → rollback automatico in caso di errore
   - Riporta lo stato: `updating` → `success` oppure `rolled_back`

Stato della flotta: UI → Releases → tabella Fleet Status

---

## Amministrazione Multi-Tenant

### Ciclo di vita tenant

```bash
# 1. Crea org (super admin)
POST /api/organizations
{"name": "Acme Industries"}

# 2. Invita org admin
POST /api/organizations/:id/invites
{"email": "admin@acme.com", "role": "admin"}
# L'utente riceve un'email con il link di registrazione

# 3. Deploy edge: l'org admin scarica l'installer
GET /api/organizations/:id/edge-installer
# Restituisce un ZIP con docker-compose pre-configurato per l'org

# 4. Monitora tutte le org (super admin)
GET /api/fleet/status          # tutti gli edge online/offline
GET /api/infrastructure        # tutti i gateway con IP/stato TLS
GET /api/security/overview     # punteggio di sicurezza globale
```

### Isolamento Org
- Tutti i dati filtrati per `org_id` — nessuna fuga di dati cross-tenant
- Credenziali MQTT per-org (DynSec ACL)
- Header X-Organization-ID validato contro JWT — spoofing bloccato a livello middleware
- Admin globale: `role=admin` AND `org_id IS NULL` nel JWT

### Retention Storico (per deployment)
Configurabile da UI → System → Database → Retention Days (default 365).
Un worker gira ogni giorno: cancella le righe vecchie + VACUUM ANALYZE automatico.

---

## Prompt di esempio per l'agente

```
"Installa OpenEdge su questo VPS con Coolify — dominio: app.acme.com"
"Crea una nuova organizzazione per il cliente Rossi Srl"
"Invita mario@rossi.com come admin dell'org 5"
"Configura SMTP con Gmail per l'invio di email"
"Importa questi tag Modbus nel gateway PLC-Linea-A: ..."
"Scarica l'edge installer per l'org 3"
"Configura un webhook su https://slack.hook/xxx per alarm.active nell'org 3"
"Il driver del gateway 7 non parte — diagnostica e risolvi"
"Fai un backup del DB e scaricalo"
"L'edge dell'org 4 è offline da 10 minuti — indaga"
"Quanti utenti ha l'org 2? Aggiungine uno read-only"
```
