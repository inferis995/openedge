# PRD — OpenEdge: da On-Premises a Piattaforma IIoT Enterprise
**Versione:** 2.0  
**Data:** 2026-06-13  
**Branch:** `claude/ciao-Cvmm2`

---

## Contesto e Visione

OpenEdge è oggi un sistema IIoT on-premises: un'istanza per cliente, deploy manuale, nessuna CI/CD, 6 soli test Go su ~33.000 righe di codice. L'obiettivo è trasformarlo in una **piattaforma SaaS multi-tenant enterprise** comparabile a Siemens Industrial Edge, AWS IoT Greengrass, Azure IoT Hub — con:
- Codice pulito e testato (lint + test + CI/CD)
- Isolamento completo tra tenant (org-level)
- Edge manager distribuibile on-premises dai clienti
- Funzionalità enterprise: SSO, MFA, observability, digital twin
- Infrastruttura container production-grade (multi-arch, Kubernetes)
- Ecosistema aperto: webhook, connettori cloud, SDK

**Stato attuale rilevante:**
- ✅ Gerarchia dati org→site→area→gateway→tag già multi-tenant
- ✅ API già isolata per org_id (middleware/organization.go)
- ✅ JWT con org_id, ruoli admin/user
- ✅ `slog` con JSON format (pronto per Loki)
- ✅ OpenTelemetry in go.mod (non attivato)
- ✅ Interfacce mockabili: `MQTTClient`, `RedisClient`, `Channel`
- ✅ Swagger/OpenAPI spec parziale in docs/
- ❌ MQTT anonimo (nessun ACL), 0 test frontend, 0 CI/CD, no SSO, no Helm

---

## Goal 0 — Code Quality Foundation
> *Prima di ogni nuova feature: il codice deve essere verificabile e manutenibile*

### Deliverable
- `.golangci.yml` — 13 linter attivi (errcheck, staticcheck, gocritic, gosec, dupl, gocyclo…)
- `services/web-ui/eslint.config.js` — ESLint 9 flat config (TypeScript + React Hooks)
- `services/web-ui/.prettierrc` — Prettier per formattazione uniforme
- `services/web-ui/vitest.config.ts` — Vitest + jsdom + coverage thresholds (60% lines)
- `.github/workflows/ci.yml` — Pipeline: lint → test → build Docker (su ogni PR)
- `lefthook.yml` — Pre-commit: go vet + gofmt + tsc; pre-push: go test + vitest

### Test aggiunti
| File | Cosa testa |
|------|-----------|
| `internal/middleware/auth_test.go` | RequireAuth: token mancante, invalido, scaduto, valido |
| `internal/middleware/organization_test.go` | OrganizationContext: global admin, org user, cross-org forbidden |
| `internal/auth/auth_test.go` | SecretKey init, token sign + verify |
| `services/web-ui/src/lib/utils.test.ts` | cn(): merge classi, conflitti Tailwind, valori falsy |

### Makefile targets aggiunti
```
make lint            # golangci-lint + eslint + tsc
make test            # go test -race + vitest run
make test-coverage   # coverage report Go + frontend
make swagger         # rigenera docs/openapi.yaml
make hooks-install   # installa lefthook
```

### Target coverage
- Go `internal/`: ≥ 70% su handlers/, middleware/, auth/
- Frontend: ≥ 60% su lib/, hooks/, stores/

### AC
- [ ] `make lint` → 0 errori su CI
- [ ] `make test` → tutti verdi localmente e su CI
- [ ] PR senza test per nuova feature → CI fallisce (soglia coverage)
- [ ] `gofmt -l .` → 0 file non formattati

---

## Goal 1 — MQTT Authentication + ACL + TLS
> *Nessun tenant può leggere i dati di un altro tenant*

### Problema attuale
Il broker Mosquitto accetta connessioni anonime su porta 18830. Qualsiasi client può iscriversi a `data/#` e leggere i dati di tutti gli org.

### Deliverable
**Mosquitto:**
- Listener interno 1883: rimane anonymous (Docker network trusted)
- Listener esterno 8883: `allow_anonymous false`, `password_file`, `acl_file`

**ACL template per org:**
```
user acme-corp
topic readwrite data/acme-corp/#
topic readwrite spBv1.0/acme-corp-#
topic write sys/health/#
topic read sys/write/#
topic write sys/write_ack/#
```

**API — auto-generazione credenziali:**
- `POST /api/organizations` → genera password MQTT random (32 chars) → `mosquitto_passwd -b` + append ACL → SIGHUP per reload
- Tabella: `org_mqtt_credentials(org_id, username, password_hash, created_at, revoked_at)`
- `POST /api/organizations/{id}/rotate-mqtt-credentials` → rigenera credenziali
- File: `internal/handlers/organizations.go` (aggiungere post-create hook)

**TLS esterno:**
- nginx stream proxy → porta 8883 con cert Let's Encrypt (riusa quello del dominio)
- WSS per browser: `wss://yourdomain.com/mqtt` (nginx location block)
- `docker-compose.yml`: esporre 8883 verso internet

### AC
- [ ] Edge manager con cred org-A → non può SUBSCRIBE a `data/org-b/#` (CONNACK refused)
- [ ] Connessioni Docker interne su 1883 non toccate
- [ ] Creazione org → credenziali MQTT pronte entro 1s
- [ ] TLS: `openssl s_client -connect yourdomain.com:8883` → certificato valido

---

## Goal 2 — Config Pull API (Edge Manager → API)
> *Edge manager legge configurazione dalla API centrale, non da DB diretto*

### Problema attuale
`driver-manager` fa `SELECT * FROM gateways` direttamente su PostgreSQL. In SaaS il DB non è accessibile dall'edge del cliente.

### Deliverable
**Nuova tabella:**
```sql
CREATE TABLE org_api_keys (
    id SERIAL PRIMARY KEY,
    org_id INT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    key_hash CHAR(64) NOT NULL UNIQUE,  -- SHA-256
    key_prefix VARCHAR(10) NOT NULL,     -- "oe_5f2a" per identificazione visiva
    name VARCHAR(100),
    last_used_at TIMESTAMP,
    revoked_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT NOW()
);
```

**Nuovo endpoint:**
```
GET /api/edge/config
Header: X-API-Key: oe_5f2a_<random64>
Response: { org_id, org_name, mqtt: {host,port,tls,username,password}, gateways: [...+tags] }
```
- `password` MQTT in chiaro solo qui (mai più visibile dopo)
- Middleware: `internal/middleware/api_key.go`
- File: `internal/handlers/edge_config.go` (nuovo)

**driver-manager:**
- Se env `EDGE_API_KEY` presente → chiama API ogni 10s invece del DB
- Se assente → DB locale (retrocompatibile, single-tenant funziona ancora)
- Reload immediato via MQTT `sys/config/{org_id}/reload`

### AC
- [ ] `curl -H "X-API-Key: oe_xxx" .../api/edge/config` → JSON corretto
- [ ] API key org-A non può leggere config org-B → 403
- [ ] driver-manager con `EDGE_API_KEY` avvia driver correttamente
- [ ] driver-manager senza `EDGE_API_KEY` funziona come prima

---

## Goal 3 — Edge Manager Packaging
> *Il cliente scarica un installer dalla UI e in 5 minuti è operativo*

### Deliverable
**Endpoint installer:**
```
GET /api/organizations/{id}/edge-installer
→ ZIP pre-configurato per quell'org
```

**Contenuto ZIP:**
```
openedge-edge-acme-corp-v1.2.zip
├── docker-compose.yml      # solo driver-manager + driver images
├── .env                    # pre-compilato: API key, MQTT creds, endpoint
├── install.sh              # Linux: systemd service (adatta systemd/install.sh)
├── install.ps1             # Windows: NSSM service
├── update.sh               # docker compose pull && docker compose up -d
└── README.txt
```

**Heartbeat:**
- driver-manager pubblica `sys/edge/{org_id}/ping` ogni 30s
- core-api salva in Redis `edge_ping:{org_id}` con TTL 90s
- `GET /api/organizations/{id}` aggiunge campo `edge_status: "online"|"offline"|"never"`

**UI — card "Edge Manager":**
- Status badge (🟢/🔴) con ultimo ping timestamp
- Bottone "Scarica Installer"
- Bottone "Rigenera API Key" (con conferma)

### AC
- [ ] ZIP generato in < 2s e contiene tutti i file
- [ ] `bash install.sh` su Ubuntu 22 → servizio avviato e status "online" entro 30s
- [ ] `install.ps1` su Windows 11 → Docker service avviato
- [ ] Reboot → servizio si riavvia automaticamente
- [ ] UI mostra "Offline" dopo 90s senza heartbeat

---

## Goal 4 — Customer Self-Service + RBAC Granulare
> *Il cliente gestisce la propria infra autonomamente, senza intervento del platform admin*

### Deliverable
**Invite utenti:**
```sql
CREATE TABLE user_invites (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id INT NOT NULL REFERENCES organizations(id),
    email VARCHAR(255) NOT NULL,
    role VARCHAR(20) NOT NULL DEFAULT 'user',
    invited_by INT REFERENCES users(id),
    accepted_at TIMESTAMP,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP DEFAULT NOW()
);
```
- `POST /api/invites` → genera link `/register?token=<uuid>`, invia email
- `GET /register?token=<uuid>` → pagina React per completare registrazione

**RBAC granulare:**
```sql
CREATE TABLE user_permissions (
    user_id INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    permission VARCHAR(50) NOT NULL,
    PRIMARY KEY (user_id, permission)
);
-- Permessi: can_configure_gateways, can_write_tags, can_manage_users,
--           can_view_alarms, can_acknowledge_alarms, can_export_data
```
- Org admin ha tutti i permessi per la sua org
- Middleware: `RequirePermission("can_write_tags")` su endpoint sensibili

**Config real-time:**
- Creazione gateway → pubblica `sys/config/{org_id}/reload`
- edge manager forza sync immediata (<5s vs 10s di polling)

### AC
- [ ] Cliente admin invita operatore via email → operatore completa registrazione → accede con org corretta
- [ ] Operatore senza `can_configure_gateways` → 403 su POST /api/gateways
- [ ] Nuovo gateway in UI → visibile nell'edge manager entro 10s
- [ ] Link invite scaduto → errore chiaro in UI

---

## Goal 5 — Write Commands + Audit Trail Completo
> *Scrittura PLC dalla UI con doppia validazione e tracciabilità totale*

### Deliverable
**Endpoint:**
```
POST /api/tags/{id}/write
Body: { "value": 75.0, "comment": "setpoint linea 1" }
Auth: JWT con permesso can_write_tags
```
Validazioni: tag appartiene all'org dell'utente, tipo dato compatibile, range min/max tag (se configurato), rate limit 10/min per utente.

**MQTT publish:**
```
Topic: sys/write/{tag_id}
Payload: { "value": 75.0, "user_id": 12, "username": "mario", "ts": ..., "req_id": "uuid" }
```

**Acknowledgement:**
- Driver risponde su `sys/write_ack/{tag_id}`: `{ "req_id": "uuid", "status": "ok"|"error", "msg": "..." }`
- UI: spinner → ✓ verde / ✗ rosso con messaggio (timeout 5s)

**Tabella audit:**
```sql
CREATE TABLE write_commands (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    req_id UUID NOT NULL,
    tag_id INT NOT NULL,
    org_id INT NOT NULL,
    user_id INT NOT NULL,
    value_sent TEXT NOT NULL,
    status VARCHAR(20) DEFAULT 'pending',  -- pending|ack|nack|timeout
    ack_ts TIMESTAMP,
    error_msg TEXT,
    ip_address VARCHAR(45),
    comment TEXT,
    created_at TIMESTAMP DEFAULT NOW()
);
```

**Routing per driver:**
- S7/Modbus/OPC-UA: driver riceve e scrive direttamente sul device
- MQTT driver: forwarda su broker locale `connection_config.write_topic`

### AC
- [ ] Scrittura → PLC aggiornato entro 2s (happy path)
- [ ] Scrittura cross-org → 403
- [ ] Utente senza `can_write_tags` → 403, icona matita non visibile in UI
- [ ] Ogni scrittura appare in audit table con user, IP, timestamp, status
- [ ] PLC offline → "timeout" in UI entro 5s

---

## Goal 6 — Observability Stack Enterprise
> *Visibilità totale su performance, errori, metriche — come AWS CloudWatch o Azure Monitor*

### Contesto
OpenTelemetry è già in go.mod ma non configurato. `slog` è già in JSON mode. La base è pronta.

### Deliverable
**OpenTelemetry — attivare traces + metrics:**
- `internal/telemetry/telemetry.go` — init OTel con OTLP exporter
- Traces su: ogni HTTP request (gin middleware), ogni query MQTT in/out, ogni DB query > 100ms
- Metrics: `mqtt_messages_received_total{org}`, `api_request_duration_seconds{endpoint}`, `active_gateways_total{org}`, `write_commands_total{status}`

**Prometheus endpoint:**
- `GET /metrics` (no auth) → Prometheus scrape
- Standard Go runtime metrics + custom business metrics

**Stack monitoring** (docker-compose profile `monitoring`):
```yaml
# docker-compose.monitoring.yml
services:
  prometheus:    # localhost:9090
  grafana:       # localhost:3001 — dashboard pre-caricata
  loki:          # log aggregation via promtail sidecar
  alertmanager:  # alert su edge offline, errori > soglia
```

**Dashboard Grafana pre-configurata** (`monitoring/grafana/dashboards/openedge.json`):
- Pannelli: messaggi MQTT/s per org, latenza API p50/p95/p99, gateway online/offline, scritture PLC/ora

**Alerting rules:**
- Edge manager offline > 5min → email/Slack al platform admin
- Error rate API > 5% → PagerDuty
- Nessun dato da gateway > 15min → notifica in-app

### AC
- [ ] `docker compose --profile monitoring up` → Grafana raggiungibile con dati reali
- [ ] `/metrics` espone >20 metriche custom
- [ ] Trace di una request HTTP visibile in Grafana Tempo/Jaeger
- [ ] Alert "edge offline" si attiva entro 6 min dall'interruzione

---

## Goal 7 — Enterprise Auth: SSO + MFA + API Versioning
> *Autenticazione enterprise: login con Azure AD, Google, SAML. MFA obbligatorio per admin*

### Deliverable
**SSO/OIDC:**
```sql
CREATE TABLE sso_providers (
    id SERIAL PRIMARY KEY,
    org_id INT REFERENCES organizations(id),  -- NULL = platform-level
    provider VARCHAR(20) NOT NULL,  -- google|azure|github|generic-oidc
    client_id VARCHAR(255) NOT NULL,
    client_secret_enc TEXT NOT NULL,  -- cifrato con internal/crypto
    issuer_url TEXT NOT NULL,
    domain_hint VARCHAR(100),  -- es. "acme.com" per auto-detect
    enabled BOOLEAN DEFAULT true
);
```
- Libreria: `golang.org/x/oauth2` (già transitivamente disponibile)
- Flow: `/api/auth/sso/{provider}/login` → redirect → callback → JWT generato
- Org admin può configurare il proprio provider OIDC

**MFA/TOTP:**
```sql
ALTER TABLE users ADD COLUMN mfa_enabled BOOLEAN DEFAULT false;
CREATE TABLE user_mfa (
    user_id INT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    totp_secret TEXT NOT NULL,  -- cifrato con internal/crypto
    backup_codes TEXT[],
    enrolled_at TIMESTAMP DEFAULT NOW()
);
```
- Libreria: `github.com/pquerna/otp`
- Flow: login → se mfa_enabled → `/api/auth/mfa/verify` con codice 6 cifre
- Org admin può rendere MFA obbligatorio per tutti gli utenti dell'org

**API versioning:**
- Prefisso `/api/v1/` su tutti gli endpoint (backward compat: `/api/` redirige a `/api/v1/`)
- Header `X-API-Version` come alternativa

**Personal Access Tokens (PAT):**
- `POST /api/v1/tokens` → genera token long-lived (come GitHub PAT)
- Usabili come `Authorization: Bearer oe_pat_...` al posto del JWT
- Tabella: `personal_access_tokens(user_id, name, token_hash, scopes[], expires_at)`

**OpenAPI completato:**
- `make swagger` → genera `docs/openapi.yaml` completo e aggiornato
- Hosted su `/api/docs` (Swagger UI)

### AC
- [ ] Login con Google → JWT valido → accesso UI
- [ ] MFA: login senza codice TOTP se mfa_enabled → 401
- [ ] `/api/v1/gateways` risponde identico a `/api/gateways`
- [ ] PAT funziona in curl senza JWT

---

## Goal 8 — Container & Infra Enterprise
> *Production-grade: multi-arch ARM, Kubernetes, immagini firmate, sicurezza container*

### Problema attuale
- Tutti i Dockerfile girano come root
- Nessun multi-arch build (edge su Raspberry Pi / PC ARM non supportati)
- Nessun Helm chart (deploy Kubernetes non documentato)
- Mosquitto non scala oltre ~100k connessioni

### Deliverable
**Rootless containers:**
- Ogni Dockerfile aggiunge `USER nonroot:nonroot` (o equivalente per ogni immagine base)
- Volumes montati con UID corretto
- Test: `docker run --user 1000:1000 openedge/core-api` non crasha

**Multi-arch builds:**
```yaml
# .github/workflows/build.yml
strategy:
  matrix:
    platform: [linux/amd64, linux/arm64]
```
- Usare `docker buildx` con QEMU emulation
- Tag: `openedge/core-api:1.2.0-amd64`, `openedge/core-api:1.2.0-arm64`, manifest `1.2.0`
- Abilitato per: core-api, engine-historian, driver-manager, web-ui, tutti i driver

**Kubernetes Helm chart:**
```
helm/openedge/
├── Chart.yaml
├── values.yaml           # domain, replicas, storage, ingress
├── templates/
│   ├── deployment-core-api.yaml
│   ├── deployment-historian.yaml
│   ├── deployment-web-ui.yaml
│   ├── statefulset-postgres.yaml
│   ├── statefulset-redis.yaml
│   ├── deployment-mosquitto.yaml
│   ├── ingress.yaml
│   ├── configmap.yaml
│   └── secrets.yaml
```
```bash
helm install openedge ./helm/openedge \
  --set global.domain=yourdomain.com \
  --set global.storageClass=standard
```

**EMQX (opzionale, produzione ad alta scala):**
- Sostituisce Mosquitto per ambienti con >1000 org o >100k connessioni
- Plugin `emqx-auth-http`: valida credenziali tramite callback a core-api
- docker-compose profile `emqx` come alternativa a `mosquitto`

**Image signing:**
- `cosign sign` su ogni immagine pushata in CI
- `cosign verify` documentato nel README

**Health checks migliorati:**
```dockerfile
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
  CMD wget -qO- http://localhost:8081/health || exit 1
```

### AC
- [ ] `docker run openedge/core-api:latest whoami` → output non-root
- [ ] `docker buildx build --platform linux/arm64` → build OK
- [ ] `helm install openedge ./helm` su k3s locale → tutti i pod Running
- [ ] `cosign verify openedge/core-api:1.2.0` → verificato

---

## Goal 9 — Digital Twin + Fleet Management
> *Come Siemens Industrial Edge: gestione centralizzata della flotta, shadow sempre disponibile*

### Deliverable
**Tag Shadows (Digital Twin):**
- Redis: `tag_shadow:{tag_id}` = `{value, quality, ts, source: "live|historic|unknown"}`
- Aggiornato da: engine-historian su ogni valore, edge manager su ogni publish
- `GET /api/v1/tags/{id}/shadow` → sempre un valore (mai 404, al massimo `source: "unknown"`)
- UI: indicatore "live" / "cached" / "offline" su ogni tag

**Fleet Management:**
- `GET /api/v1/admin/fleet` → tabella org con: edge_status, gateway_count, tag_count, last_data_ts, messages_today
- `POST /api/v1/organizations/{id}/restart-edge` → pubblica `sys/command/{org_id}/restart`
- OTA edge manager: `POST /api/v1/organizations/{id}/update-edge` → pubblica `sys/update/{org_id}` con nuova versione
- Edge manager riceve → `docker compose pull && docker compose up -d`

**Alert Rules Engine:**
```sql
CREATE TABLE alert_rules (
    id SERIAL PRIMARY KEY,
    org_id INT NOT NULL REFERENCES organizations(id),
    tag_id INT NOT NULL REFERENCES tags(id),
    name VARCHAR(100) NOT NULL,
    condition VARCHAR(20) NOT NULL,  -- gt|lt|eq|ne|change
    threshold FLOAT,
    duration_seconds INT DEFAULT 0,
    channels TEXT[],  -- ['email','slack','pagerduty']
    enabled BOOLEAN DEFAULT true
);
```
- Valutazione ogni scan cycle nell'engine-historian
- Deduplicazione: non ripete l'alert se la condizione è ancora attiva

**Data Transform Rules:**
```sql
CREATE TABLE tag_transforms (
    tag_id INT PRIMARY KEY REFERENCES tags(id) ON DELETE CASCADE,
    formula TEXT NOT NULL,  -- es. "value * 0.001 + 273.15"
    enabled BOOLEAN DEFAULT true
);
```
- Applicato nell'engine-historian prima dell'archiviazione
- Safe eval: solo operatori matematici, no codice arbitrario

### AC
- [ ] Tag online → shadow aggiornato in <2s
- [ ] Edge offline → shadow mantiene ultimo valore con `source: "cached"`
- [ ] Alert rule "temp > 80 per 5min" → notifica Slack entro 5min+epsilon
- [ ] OTA update inviato → edge manager aggiornato entro 2 minuti
- [ ] Tag con formula `value * 0.001` → valore storicizzato trasformato correttamente

---

## Goal 10 — Ecosystem & Integrazioni
> *Connettori verso il mondo esterno: webhook, cloud bridge, SDK — come i marketplace dei colossi*

### Deliverable
**Webhooks:**
```sql
CREATE TABLE webhooks (
    id SERIAL PRIMARY KEY,
    org_id INT NOT NULL REFERENCES organizations(id),
    url TEXT NOT NULL,
    secret VARCHAR(64),  -- HMAC-SHA256 per verifica firma
    events TEXT[],  -- ['tag.value', 'alarm.triggered', 'edge.offline', 'write.ack']
    enabled BOOLEAN DEFAULT true,
    failure_count INT DEFAULT 0,
    last_delivery_ts TIMESTAMP
);
```
- Retry con backoff esponenziale (5 tentativi, 2→4→8→16→32s)
- Payload firmato con `X-OpenEdge-Signature: sha256=<hmac>`
- UI: log ultimi 50 delivery con status e response code

**Cloud connectors** (`internal/connectors/`):
```
connectors/
├── aws_iot.go        # MQTT bridge → AWS IoT Core
├── azure_iot.go      # AMQP bridge → Azure IoT Hub
├── influxdb.go       # Line protocol push → InfluxDB v2
└── aveva_pi.go       # REST → AVEVA Data Hub / OSIsoft PI
```
Configurabili per-org via `global_settings` (tipo: `connector_aws_enabled`, `connector_aws_endpoint`…)

**Notification channels aggiunti:**
- Slack (webhook URL) — già parzialmente presente
- Microsoft Teams (Adaptive Card)
- PagerDuty (Events API v2)
- Telegram — già presente
- Email — già presente

**REST API SDK:**
```
sdk/
├── python/     # openedge-sdk (PyPI)
│   └── openedge/client.py  -- auth, tags, history, write
└── nodejs/     # @openedge/sdk (npm)
    └── src/client.ts
```
Generati/mantenuti a partire dalla spec OpenAPI (Goal 7).

**Rate limiting API migliorato:**
- Per-org (non solo per-IP): `300 req/min per org` oltre al per-IP esistente
- Header response: `X-RateLimit-Remaining`, `X-RateLimit-Reset`

### AC
- [ ] Webhook riceve payload firmato entro 1s da evento tag.value
- [ ] Verifica firma HMAC nel webhook consumer di test → valida
- [ ] `POST /api/v1/connectors/influxdb/test` → scrittura di prova in InfluxDB OK
- [ ] SDK Python: `client.tags.get_history(tag_id, start, end)` → dati corretti
- [ ] Rate limit per-org: 301° request nello stesso minuto → 429 con header

---

## Roadmap

```
Q3 2026 (Sprint 1-2)
├── Goal 0  — Code Quality (lint, test, CI)        2 settimane
├── Goal 1  — MQTT Auth + ACL + TLS                2 settimane
└── Goal 2  — Config Pull API                      2 settimane

Q4 2026 (Sprint 3-4)
├── Goal 3  — Edge Manager Packaging               2 settimane
├── Goal 4  — Customer Self-Service + RBAC         2 settimane
└── Goal 5  — Write Commands + Audit               2 settimane

Q1 2027 (Sprint 5-6)
├── Goal 6  — Observability Stack                  3 settimane
├── Goal 7  — Enterprise Auth (SSO + MFA)          3 settimane
└── Goal 8  — Container Enterprise (multi-arch, Helm) 2 settimane

Q2 2027 (Sprint 7-8)
├── Goal 9  — Digital Twin + Fleet                 3 settimane
└── Goal 10 — Ecosystem + SDK                      3 settimane
```

---

## Confronto con i colossi

| Feature | Siemens IE | AWS IoT | Azure IoT | **OpenEdge target** |
|---------|-----------|---------|-----------|---------------------|
| Multi-tenant isolato | ✅ | ✅ | ✅ | ✅ Goal 1-2 |
| Edge installer 1-click | ✅ | ✅ | ✅ | ✅ Goal 3 |
| SSO / Azure AD | ✅ | ✅ | ✅ | ✅ Goal 7 |
| MFA | ✅ | ✅ | ✅ | ✅ Goal 7 |
| Observability (metrics, traces) | ✅ | ✅ | ✅ | ✅ Goal 6 |
| Digital Twin / Device Shadow | ✅ | ✅ | ✅ | ✅ Goal 9 |
| OTA update edge | ✅ | ✅ | ✅ | ✅ Goal 9 |
| Kubernetes / Helm | ✅ | ✅ | ✅ | ✅ Goal 8 |
| Multi-arch ARM | ✅ | ✅ | ✅ | ✅ Goal 8 |
| Webhook | ✅ | ✅ | ✅ | ✅ Goal 10 |
| Cloud connectors | ✅ | n/a | n/a | ✅ Goal 10 |
| Alert Rules Engine | ✅ | ✅ | ✅ | ✅ Goal 9 |
| API SDK (Python/Node) | ✅ | ✅ | ✅ | ✅ Goal 10 |
| Protocolli IIoT | S7/OPC/MB | MQTT | MQTT/AMQP | **S7+OPC+MB+MQTT** ✅ |
| Write PLC dalla UI | ✅ | ❌ | ❌ | ✅ Goal 5 (vantaggio!) |
| Historian integrato | ✅ | ❌ | ❌ | ✅ già presente (vantaggio!) |
| OEE integrato | ❌ | ❌ | ❌ | ✅ già presente (vantaggio!) |

**Vantaggi differenziali unici di OpenEdge** (già presenti, da valorizzare):
- Write PLC nativamente dalla UI (AWS/Azure non lo fanno)
- Historian TimescaleDB integrato nel prodotto (non serve InfluxDB Cloud separato)
- OEE/KPI industriali nativi
- Sparkplug B supportato out-of-the-box
- Open source (MIT) → clienti possono self-host senza costi di licenza

---

## Verifica end-to-end finale

```bash
# 1. CI passa
make lint && make test

# 2. Onboarding nuovo cliente
# Admin crea org "Test Corp" → scarica installer
curl -X POST https://yourdomain.com/api/v1/organizations \
  -H "Authorization: Bearer <admin_jwt>" \
  -d '{"name": "Test Corp"}'
# → edge_installer_url in response

# 3. Edge manager in fabbrica
unzip openedge-edge-test-corp-v1.2.zip
bash install.sh
# → servizio avviato, status "online" nella UI entro 30s

# 4. Crea gateway dalla UI
# Cliente admin aggiunge gateway S7 a 192.168.1.10
# → driver avviato nell'edge entro 10s

# 5. Dati visibili nel Trend historian

# 6. Scrittura PLC
curl -X POST https://yourdomain.com/api/v1/tags/42/write \
  -H "Authorization: Bearer <user_jwt_with_write_perm>" \
  -d '{"value": 75.0}'
# → ack entro 2s, audit log registrato

# 7. Alert rule triggers notifica Slack/email

# 8. SSO login funziona con account Azure AD del cliente
```
