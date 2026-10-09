---
name: openedge-ops
description: OpenEdge Operations — installa, configura e gestisce OpenEdge in produzione. Chiede sempre all'utente cosa vuole fare prima di agire. Supporta self-hosted/on-prem (Linux+Windows), on-prem multi-tenant dietro VPN Tailscale, VPS con Traefik, Coolify, edge profile. Commissioning da CLI (org, siti, aree, gateway, import tag da foglio, decoder MQTT, utenti). Data logger MQTT con payload binari (preset MiDo, storico nel pacchetto). Include driver industriali (Modbus/S7/OPC-UA/MQTT/LoRaWAN), multi-tenant, backup, monitoring professionale (Prometheus+Grafana+AlertManager+Loki+4 exporter), OTA updates, security, MFA TOTP con recovery codes e mfa_required per org.
version: 9.0.0
tags: [industrial, iot, commissioning, tailscale, vpn, mqtt-decoder, mido, data-logger, tag-import, devops, docker, deploy, coolify, traefik, vps, on-prem, self-hosted, multi-tenant, install, troubleshoot, mqtt, webhooks, backup, monitoring, health-checks, ota-updates, security, lorawan, mfa, totp, 2fa, recovery-codes]
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
1b. On-prem + VPN Tailscale — una VM tua, clienti che entrano via Tailscale con il loro
                            utente (multi-tenant). HTTPS valido senza dominio, nessuna
                            porta aperta su internet. → Deploy E
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
  Login:    admin / <password generata>
```

La password dell'admin è generata da `make setup-env` e sta in `.env`
(`OPENEDGE_INITIAL_ADMIN_PASSWORD`). È un **global admin**: vede ogni organizzazione.

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

## Deploy E — On-prem multi-tenant dietro VPN Tailscale

Quando: una VM tua (cloud o ufficio) e dei clienti che devono vedere **solo la
loro organizzazione**, senza esporre niente su internet e senza dominio.

L'app è la stessa (multi-tenant sempre attivo). Cambia solo chi fa l'HTTPS:
`tailscale serve` al posto di Traefik/Let's Encrypt.

```bash
# 1. Prerequisiti: Docker, git, make, openssl, jq, mosquitto-clients; Tailscale già nella tailnet
curl -fsSL https://get.docker.com | sh
apt-get install -y git make openssl jq mosquitto-clients

# 2. Nella console Tailscale: DNS → MagicDNS ON e "HTTPS Certificates" ON.
sudo tailscale set --hostname=openedge
TS_HOST=$(tailscale status --json | jq -r '.Self.DNSName' | sed 's/\.$//')   # openedge.<tailnet>.ts.net

# 3. OpenEdge, con UI e broker solo su localhost
git clone https://github.com/inferis995/openedge.git /opt/openedge && cd /opt/openedge
make setup-env
cat >> .env <<EOF
WEB_UI_PORT=127.0.0.1:3000
MQTT_BIND=127.0.0.1
PUBLIC_HOST=$TS_HOST
OPENEDGE_PUBLIC_URL=https://$TS_HOST
EOF
sed -i "s|^ALLOWED_ORIGINS=.*|ALLOWED_ORIGINS=https://$TS_HOST|" .env
make start
sudo make install-service INSTALL_DIR=/opt/openedge

# 4. HTTPS via Tailscale verso l'interfaccia
sudo tailscale serve --bg 3000
tailscale serve status        # → https://openedge.<tailnet>.ts.net → http://127.0.0.1:3000
```

Verifica che solo Tailscale esponga OpenEdge:

```bash
ss -tlnp | grep -v '127.0.0.1\|\[::1\]'   # restano solo sshd e tailscaled
```

Accesso dei clienti:
1. Console Tailscale → Machines → `openedge` → **Share** → invito via email/link.
   Il cliente accetta con il suo account Tailscale (gratuito) e vede **solo
   quella macchina**, non la tua rete.
2. In OpenEdge: crea l'organizzazione del cliente e i suoi utenti
   (`openedge users create --org <id> --role user`). Entrano su
   `https://openedge.<tailnet>.ts.net` con le **loro** credenziali.

Note:
- `ALLOWED_ORIGINS` deve essere esattamente `https://<TS_HOST>`. Se manca, il
  browser carica la pagina ma ogni chiamata all'API viene bloccata (CORS).
- `MQTT_BIND=127.0.0.1` tiene il broker interno (18830, 9001) fuori dalla rete:
  con i driver sulla stessa VM nessun edge esterno ne ha bisogno, e il browser
  arriva a 9001 tramite il proxy `/mqtt` dell'interfaccia.
- Il servizio systemd (`make install-service`) avvia lo stack base, che
  include già `driver-manager`: i driver dei gateway ripartono al riavvio.

---

## Deploy C — VPS con Traefik + Let's Encrypt

**Prerequisiti**: VPS con IP pubblico, dominio puntato all'IP, porte 80/443/8883 aperte

> **Prima di avviare qualsiasi cosa in produzione**: `make preflight`.
> Rifiuta l'avvio se un segreto è ancora il segnaposto del repository, se
> `ENCRYPTION_KEY` non è esattamente 32 byte (a qualsiasi altra lunghezza viene
> ignorata e le credenziali dei broker finiscono in chiaro nel database), se
> `BACKUP_SCHEDULE` non è quotato (il backup non parte mai) o se `PUBLIC_HOST`
> è ancora quello d'esempio. `make vps-up` lo esegue da solo.
>
> **DNS**: un record A per `PUBLIC_HOST`. Il monitoring (Prometheus, Grafana,
> Loki, exporter) **non parte per default** — sta dietro `profiles: ["monitoring"]`.
> Se lo attivi con `make vps-up-monitoring` serve un secondo record A per
> `grafana.PUBLIC_HOST`, altrimenti Traefik ritenta fino al rate limit di
> Let's Encrypt e ritarda anche il certificato del dominio principale.

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

Se vuoi che i driver girino direttamente sul VPS (senza edge fisici separati):

```bash
make vps-up-edge
# Avvia: Traefik + core + driver-manager, e costruisce anche le immagini dei driver
```

Per l'avvio al boot il servizio systemd deve usare lo stesso profilo:
`docker compose -f docker-compose.yml -f docker-compose.vps.yml --profile edge up -d`.
Quello scritto da `deploy/cloud-init.sh` **non** ha `--profile edge`: dopo un
riavvio `driver-manager` non partirebbe e i gateway smetterebbero di leggere.

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
  New Project → New Resource → Public Repository
    https://github.com/inferis995/openedge, branch master,
    Build Pack "Docker Compose", file /docker-compose.coolify.yml
  NON incollare il compose: le immagini si costruiscono dai sorgenti e
  servono mosquitto/ e migrations/ del repository.
  Variabili: PUBLIC_HOST (senza https://), POSTGRES_PASSWORD,
    MQTT_ADMIN_PASSWORD, JWT_SECRET, ENCRYPTION_KEY (esattamente 32
    caratteri: openssl rand -hex 16), OPENEDGE_INITIAL_ADMIN_PASSWORD, TZ
  web-ui instrada https://PUBLIC_HOST con le sue label Traefik.
  Deploy. Devono girare anche engine-historian (storico) e, con il profilo
  edge, driver-manager; i servizi driver-* restano fermi (replicas: 0):
  sono solo le immagini da cui driver-manager avvia i driver.

Passo 4 — MQTT/TLS porta 8883
  Coolify → Settings → Traefik → Dynamic Configuration
  Incolla: deploy/coolify-traefik-mqtt.yml (sostituisci YOUR_DOMAIN)
  Coolify → Settings → Traefik → Port Mappings → aggiungi 8883:8883
```

### All-in-one su Coolify

Per avere driver-manager sullo stesso server Coolify, attiva il profilo `edge`:
- In Coolify → Resource → Environment: aggiungi variable `COMPOSE_PROFILES=edge`
- Oppure testa in locale: `make coolify-up-edge`

Il profilo `edge` costruisce anche le immagini `industrial-driver-*`. I driver
si attaccano alla rete del `driver-manager` (quella del progetto Coolify, dove
sta mosquitto): una rete diversa è isolata da Docker e il driver non potrebbe
pubblicare nulla. `DRIVER_NETWORK=<nome>` la forza se serve.

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

### Crea nuova org (solo global admin)

```bash
openedge orgs create "Acme Corp" --json
# oppure: openedge api POST /api/organizations '{"name":"Acme Corp"}'
```

Un org admin riceve 403: crea e gestisce utenti, gateway e tag della **sua**
organizzazione, non nuove organizzazioni.

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
# Ruoli: "admin" (dell'organizzazione) o "user" (sola lettura). Altri valori → 400.
# Da CLI: openedge orgs invite 3 --email mario@acme.com --role admin
```

### Crea un utente senza email (niente SMTP)

```bash
openedge users create --org 3 --username mario.rossi --password '<almeno 12 caratteri>' --role user
```

| Ruolo | Vede | Può |
|---|---|---|
| global admin (`org_id` NULL) | tutte le org, Sistema, backup | tutto |
| `admin` con org | solo la sua org | gateway, tag, allarmi, utenti della sua org |
| `user` con org | solo la sua org | sola lettura; riconosce gli allarmi |

Le pagine di sistema sono rifiutate dal server (`RequireGlobalAdmin`), non solo
nascoste dal menu. L'accesso per **area/sito** di un utente (`site_ids`,
`area_ids`) oggi filtra gli elenchi di siti e aree, **non** tag, trend e
dashboard: non presentarlo come separazione dei dati. La separazione garantita
è tra organizzazioni.

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

## Data logger MQTT con payload binari (decoder, MiDo)

Per dispositivi che non mandano JSON — frame binari/base64/hex, più letture in
un messaggio, invio ogni poche ore con lo storico — il gateway MQTT ha un
**decoder**: un preset (`mido`) o una funzione JavaScript `decode(msg)`.

```bash
openedge decoders presets
# Prova su un messaggio vero, nulla viene salvato:
mosquitto_sub -h <broker> -u <user> -P <pass> -t '<topic>' -C 1 > msg.txt
openedge decoders test --preset mido --payload-file msg.txt --topic '<topic>'
```

Gateway (broker esterno + preset + storico):

```bash
openedge gateways create --area <id> --name "MiDo Cariati" --driver MQTT --config '{
  "broker_host":"broker.example.com","broker_port":1883,
  "broker_username":"<user>","broker_password":"<pass>",
  "broker_client_id":"openedge-<org>-<area>",
  "decoder_preset":"mido","stale_after_minutes":1440,"alarms_on_history":true}'
```

| Chiave | Effetto |
|---|---|
| `decoder_preset` / `decoder_script` | Come leggere il payload. Validati al salvataggio. |
| `stale_after_minutes` | Minuti senza dati prima che i tag diventino "non aggiornati". Mettilo ben sopra l'intervallo di invio, così un invio saltato non fa scattare nulla (invio ogni 4–6 h → 1440, un giorno). 0/assente con decoder = nessun controllo. |
| `alarms_on_history` | Allarmi valutati su tutte le letture del pacchetto con i loro orari; notificato solo ciò che è ancora attivo all'ultima lettura (o la fine di uno già notificato). |
| `broker_client_id` | Unico per gateway: due client con lo stesso id si scollegano a vicenda. |

Tag: più tag sullo **stesso topic**, distinti dal **percorso JSON** (il campo
della lettura decodificata). Importali da foglio:

```
Nome;Indirizzo;Tipo;Storicizza;Unità;Decimali;Percorso JSON
P1_AN1;site/863266053566249/dataup/up;REAL;true;;2;in7
P1_AN2;site/863266053566249/dataup/up;REAL;true;;2;in8
P1_BATTERIA;site/863266053566249/dataup/up;REAL;true;V;2;batteria
```

```bash
openedge tags import <gateway-id> foglio.csv            # anteprima riga per riga
openedge tags import <gateway-id> foglio.csv --apply    # tutto o niente
```

Preset MiDo: `in<N>` per ogni ingresso (AN1/AN2 sono gli ingressi annunciati
con quell'etichetta nel messaggio AN — verifica con `decoders test` su un AN
prima di importare), `batteria`, `rssi`, `sqi`, bit di stato e di allarme.
Ogni lettura è storicizzata al suo orario; il valore corrente resta il più
recente anche se un pacchetto vecchio arriva dopo.

---

## Troubleshooting

### Driver MQTT: niente dati

```bash
docker ps --format '{{.Names}}' | grep driver-mqtt
docker logs --since 1h <container> 2>&1 | grep -iE 'connect|subscrib|BRIDGED|ERROR'
```

- `not authorized` → credenziali del broker nel gateway.
- timeout/refused → uscita verso `broker:porta` bloccata (`nc -zv broker 1883`).
- `ERROR decoding message` → prova quel payload con `openedge decoders test`.
- nessun `BRIDGED` ma connesso → i topic dei tag non sono quelli su cui il
  dispositivo pubblica (`mosquitto_sub -t '<prefisso>/#' -v`).

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
make vps-up-edge     # VPS — all-in-one (server + driver-manager + immagini driver)
make build-cli       # CLI → bin/openedge  (make install-cli → /usr/local/bin)
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
# Struttura — lettura
openedge orgs list
openedge sites list --org 1
openedge areas list --site 2
openedge gateways list [--org 1]

# Struttura — creazione (ogni create con --json stampa l'oggetto, id incluso)
openedge orgs create "Cliente Srl"                        # solo global admin
openedge sites create "Stabilimento" --org 1
openedge areas create <site-id> "Reparto A"
openedge gateways create --area 3 --name PLC1 --driver MODBUS_TCP --config '{"ip":"192.168.1.10","port":502}'
openedge tags import <gateway-id> foglio.csv [--apply]
openedge users create --org 1 --username mario --password '<12+>' --role user
openedge decoders test --preset mido --payload-file msg.txt --topic '<topic>'

# Qualsiasi endpoint, con le credenziali della CLI
openedge api GET /api/sites
openedge api PUT /api/organizations/3/mfa-required '{"mfa_required":true}'

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

> `users.token_version` è nei claim e `middleware.RequireAuth` lo confronta con
> la colonna. Cambiare la password (propria o da admin), o il ruolo/l'org di un
> utente, lo incrementa e ritira **tutte** le sessioni di quell'utente.

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
"Installa OpenEdge on-prem e dai accesso al cliente via Tailscale"
"Collega 68 data logger MiDo dal broker MQTT del cliente"
"Importa i tag da questo foglio sul gateway 12"
"Come configuro la CLI per on-prem con certificato self-signed?"
"Aggiungi openedge come MCP server in Claude Code"

```
