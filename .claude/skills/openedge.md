---
name: openedge
description: OpenEdge Industrial IoT — monitor allarmi, leggi dati real-time, storico, anomalie, OEE via REST + i3X. Multi-tenant SaaS. UDT (tipi definiti dall'utente) con propagazione alle istanze. CLI (openedge) con MCP server mode: 39 tool, l'agente costruisce l'impianto oltre a leggerlo. LoRaWAN auto-discovery + tag import + downlink. Tag Shadows (digital twin). Fleet OTA. SSO/OIDC. RBAC granulare. Slack/Teams/PagerDuty. InfluxDB connector. Usa openedge-ops per deploy/configurazione.
version: 6.0.0
tags: [industrial, iot, udt, provisioning, alarms, historian, timeseries, scada, i3x, cesmii, monitoring, multi-tenant, saas, oee, webhooks, security, nis2, infrastructure, health, ota, fleet, lorawan, shadows, sso, oidc, rbac, cli, mcp, influxdb, slack, teams, pagerduty]
---

# OpenEdge — Skill di monitoraggio e controllo

Questo documento descrive come un agente AI **legge** un'istanza OpenEdge:
valori real-time, storico, allarmi, OEE, anomalie, stato edge, webhook status.

OpenEdge è una piattaforma **multi-tenant SaaS**: ogni organizzazione (cliente)
è isolata — dati, MQTT, utenti separati. Il global admin (org_id=NULL) vede tutto;
gli org admin e gli utenti vedono solo la propria org.

> Per **installare / configurare / risolvere problemi** usa la skill `openedge-ops.md`.
> Questa è solo per "leggere cosa sta succedendo" e inviare comandi di scrittura.

Leggi tutto prima di fare qualsiasi chiamata API.

---

## Variabili d'ambiente attese

```bash
OPENEDGE_HOST=app.yourdomain.com   # dominio pubblico (o localhost per dev)
OPENEDGE_PORT=443                  # 443 in produzione, 8081 in dev locale
OPENEDGE_PROTOCOL=https            # https in produzione, http in dev
OPENEDGE_USERNAME=admin
OPENEDGE_PASSWORD=admin123         # SOLO se l'installazione non ha impostato
                                   # OPENEDGE_INITIAL_ADMIN_PASSWORD prima del
                                   # primo avvio. In quel caso core-api stampa un
                                   # avviso di sicurezza a ogni bootstrap: quel
                                   # conto e' global admin, legge i dati di ogni
                                   # tenant e scrive setpoint su ogni PLC.
OPENEDGE_ORG_ID=1                  # l'org_id del cliente da monitorare
```

**Multi-tenant**: in produzione ogni org ha il suo `org_id`. Ottienilo da `GET /api/organizations` (global admin) oppure dall'URL della dashboard.

---

## 1. Autenticazione

Tutti gli endpoint (eccetto `/health`, `/ready`, `/api/auth/login`) richiedono un JWT Bearer token e l'header `X-Organization-ID`.

### Login

```
POST {OPENEDGE_PROTOCOL}://{OPENEDGE_HOST}/api/auth/login
Content-Type: application/json

{"username": "{OPENEDGE_USERNAME}", "password": "{OPENEDGE_PASSWORD}"}
```

Risposta:
```json
{
  "token": "eyJ...",
  "user": {
    "id": 1,
    "username": "admin",
    "role": "admin",
    "org_id": null,
    "full_name": "System Administrator"
  }
}
```

`org_id: null` → global admin (vede tutte le org).
`org_id: 5` → org-scoped admin/user (vede solo la sua org).

### Header richiesti su ogni chiamata

```
Authorization: Bearer {TOKEN}
X-Organization-ID: {OPENEDGE_ORG_ID}
Content-Type: application/json   (solo per POST/PUT)
```

### Profilo utente corrente

```
GET /api/auth/me
```
Risposta: `{id, username, role, org_id, full_name, email}`

---

## 2. Stato sistema

### Health e readiness

```
GET /health   → {"status": "ok"}
GET /ready    → {"status": "ready", "db": "ok", "redis": "ok"}
```

### Metriche Prometheus

```
GET /metrics
```
Metriche chiave:
- `openedge_orgs_total` — numero organizzazioni
- `openedge_gateways_total` — gateway totali
- `openedge_tags_total` — tag totali
- `openedge_active_alarms_total` — allarmi attivi
- `openedge_users_total` — utenti totali
- `http_requests_total{method,path,status}` — richieste HTTP
- `http_request_duration_seconds` — latenza HTTP
- `mqtt_messages_received_total` — messaggi MQTT ricevuti

### Diagnostics (admin only)

```
GET /api/system/diagnostics
```
Risposta: CPU%, RAM%, disco%, ping DB, ping Redis, versioni servizi.

---

## 3. Gateway

### Lista gateway con stato

```
GET /api/gateways
X-Organization-ID: {OPENEDGE_ORG_ID}
```

Risposta:
```json
[
  {
    "id": 2,
    "name": "PLC-Serbatoio1",
    "driver_type": "MODBUS_TCP",
    "connection_status": "online",
    "ip_address": "192.168.1.10",
    "port": 502,
    "area_id": 7
  }
]
```

`connection_status`: `"online"` | `"offline"` | `"unknown"`

### Stato edge (online/offline dell'edge agent remoto)

```
GET /api/organizations/{org_id}/edge-status
```

Risposta:
```json
{"online": true, "last_ping": "2026-06-13T18:42:00Z"}
```

L'edge agent pubblica un heartbeat ogni 30s su `sys/edge/{org_id}/ping`.
Se mancano più di 90s → `"online": false`.

---

## 4. Tag e valori real-time

### Lista tag

```
GET /api/tags?gateway_id={gw_id}
```

Risposta:
```json
[
  {
    "id": 42,
    "alias": "Portata_Ingresso",
    "data_type": "REAL",
    "gateway_id": 2,
    "address": "40001",
    "historize": true,
    "json_path": null
  }
]
```

### Valore corrente di un tag

```
GET /api/tags/{tag_id}/current
```

Risposta:
```json
{
  "tag_id": 42,
  "value": 42.5,
  "quality": 0,
  "timestamp": "2026-06-13T18:43:00Z"
}
```

`quality`: `0=Good`, `1=Bad` (API standard). L'i3X usa `192=Good, 0=Bad`.

### WebSocket real-time

```
wss://{OPENEDGE_HOST}/ws/realtime
Authorization: Bearer {TOKEN}
```

Ogni messaggio:
```json
{
  "tag_id": 42,
  "value": 43.1,
  "quality": 0,
  "timestamp": "2026-06-13T18:43:01Z",
  "org_id": 1
}
```

---

## 5. Storico (Historian)

### Statistiche aggregate

```
GET /api/history/stats?tag_id=42&from=2026-06-12T00:00:00Z&to=2026-06-13T00:00:00Z&interval=1h
```

Risposta:
```json
[
  {
    "bucket": "2026-06-12T10:00:00Z",
    "avg": 41.2,
    "min": 38.0,
    "max": 45.7,
    "sample_count": 360
  }
]
```

### Export CSV

```
GET /api/reports/history.csv?gateway_id=2&from=...&to=...
GET /api/reports/alarms.csv?from=...&to=...
```

---

## 6. Allarmi

### Allarmi attivi

```
GET /api/alarms/active
```

Risposta:
```json
[
  {
    "id": 45,
    "tag_id": 42,
    "tag_alias": "Portata_Ingresso",
    "gateway_name": "PLC-Serbatoio1",
    "alarm_type": "high",
    "severity": "critical",
    "message": "Portata massima superata",
    "value_at_trigger": 98.7,
    "threshold": 90.0,
    "trigger_time": "2026-06-13T08:15:00Z",
    "acknowledged": false
  }
]
```

### Storico allarmi

```
GET /api/alarms/history?from=2026-06-12T00:00:00Z&to=2026-06-13T00:00:00Z&severity=critical
```

### Acknowledge allarme (admin)

```
POST /api/alarms/{alarm_id}/ack
```

### Conteggio per gateway

```
GET /api/gateways/{id}/alarms/count
```

---

## 7. i3X Access API (CESMII Standard)

Base path: `/api/i3x/v1/` — usa sempre `X-Organization-ID`.

### ID Format

| Tipo | Formato | Esempio |
|------|---------|---------|
| Org | `org-{n}` | `org-1` |
| Site | `site-{n}` | `site-3` |
| Area | `area-{n}` | `area-7` |
| Gateway | `gw-{n}` | `gw-2` |
| Tag | `tag-{n}` | `tag-42` |

### Quality codes i3X

| Valore | Significato |
|--------|-------------|
| `192` | Good |
| `64` | Uncertain |
| `0` | Bad |

### Gerarchia equipment

```
GET /api/i3x/v1/equipment
```

Risposta:
```json
{
  "items": [
    {
      "id": "gw-2",
      "name": "PLC-Serbatoio1",
      "type": "Equipment",
      "parentId": "area-7",
      "path": "Acme/Sito-Milano/Zona-A/PLC-Serbatoio1",
      "attributes": {
        "driver_type": "MODBUS_TCP",
        "connection_status": "online"
      }
    }
  ],
  "total": 5
}
```

### Properties (tag) con valore live

```
GET /api/i3x/v1/equipment/gw-2/properties
```

Risposta:
```json
{
  "items": [
    {
      "id": "tag-42",
      "name": "Portata_Ingresso",
      "equipmentId": "gw-2",
      "dataType": "Float",
      "historize": true,
      "current": {
        "value": 42.5,
        "quality": 192,
        "timestamp": "2026-06-13T18:43:00Z"
      }
    }
  ],
  "total": 12
}
```

### Write value (richiede i3x_write=true o role=admin)

```
PUT /api/i3x/v1/properties/tag-43/value
{"value": 1}

→ {"message": "Write command sent"}
```

### Allarmi i3X

```
GET /api/i3x/v1/alarms           # attivi
GET /api/i3x/v1/alarms/history   # storico
```

---

## 8. AI-Ops

### Snapshot org

```
GET /api/aiops/summary?hours=24
```

Ritorna: conteggio tag attivi, avg/min/max per tag, conteggio allarmi per severity, totale gateway online/offline.

### Anomaly detection (Z-score)

```
GET /api/aiops/anomalies?tag_id=42&window_hours=168&baseline_days=30
```

Ritorna: campioni con `z_score >= 2.5` (anomalie) nella window.

### Alarm digest

```
GET /api/aiops/alarms/digest?hours=24
```

Ritorna: raggruppamento allarmi per severity con conteggio e lista eventi.

---

## 9. OEE

### Lista profili OEE

```
GET /api/oee/profiles
```

### Calcolo OEE per profilo

```
GET /api/oee/{profile_id}/calculate?from=2026-06-12T06:00:00Z&to=2026-06-12T14:00:00Z
```

Risposta:
```json
{
  "profile_id": 1,
  "oee": 78.5,
  "availability": 92.0,
  "performance": 88.2,
  "quality": 96.7,
  "planned_min": 480,
  "downtime_min": 38.4
}
```

### Storico OEE (rollup orario/giornaliero/turno)

```
GET /api/oee/history?profile_id=1&bucket_size=hour&from=...&to=...
```

---

## 10. Webhooks (stato)

### Lista webhook e ultimo stato

```
GET /api/organizations/{org_id}/webhooks
```

Risposta:
```json
[
  {
    "id": 1,
    "url": "https://acme.com/hook",
    "events": ["alarm.active", "alarm.cleared"],
    "enabled": true,
    "last_triggered_at": "2026-06-13T18:00:00Z",
    "last_status_code": 200,
    "last_error": null
  }
]
```

---

## 11. Audit log (global admin)

```
GET /api/audit/logs?from=2026-06-12T00:00:00Z&to=2026-06-13T00:00:00Z&action=login&success=true&limit=100
```

Risposta:
```json
[
  {
    "id": 1234,
    "username": "admin",
    "action": "login",
    "ip_address": "1.2.3.4",
    "success": true,
    "details": {"org_id": 1},
    "created_at": "2026-06-13T09:00:00Z"
  }
]
```

Azioni disponibili:
```
GET /api/audit/actions
```

---

## 12. Security Center API

```
GET /api/security/overview     # punteggio, NIS2 passed/total, eventi 24h
GET /api/security/events       # feed eventi di sicurezza (?limit=50)
GET /api/security/compliance   # checklist NIS2 a 12 punti

GET /api/infrastructure        # tutti i gateway: IP, porta, TLS, stato online
GET /api/db/stats              # dimensione DB, righe historian, dimensioni tabelle

GET /api/releases              # release edge disponibili (super admin)
GET /api/releases/fleet        # stato aggiornamento per-org (super admin)
GET /api/organizations/:id/update  # aggiornamento pending per questa org
```

---

## 13. Health Endpoints (non autenticati)

```
GET /health           # liveness: sempre 200 {"status":"ok","ts":...}
GET /ready            # readiness: 200 se DB+Redis ok, 503 se non ok
GET /api/health/detailed   # diagnostica completa (richiede auth)
```

Risposta `/api/health/detailed`:
```json
{
  "db": {"ok": true, "latency_ms": 2, "open_connections": 5, "in_use": 1},
  "redis": {"ok": true, "latency_ms": 1},
  "memory": {"alloc_mb": 45.2, "sys_mb": 120.0, "num_gc": 12},
  "goroutines": 42,
  "uptime_seconds": 86400
}
```

---

## 14. Edge Heartbeat & Stato

```
POST /api/edge/heartbeat          # l'edge agent fa PING ogni 30s
POST /api/edge/update-status      # l'edge riporta lo stato dell'aggiornamento OTA
GET  /api/edge/update-check       # l'edge controlla se ci sono aggiornamenti approvati
```

---

---

## 15. LoRaWAN

### Lista dispositivi auto-scoperti

```
GET /api/gateways/{gw_id}/lorawan/devices
```

Risposta:
```json
[
  {
    "id": 1,
    "device_id": "eui-0123456789abcdef",
    "dev_eui": "0123456789ABCDEF",
    "last_seen": "2026-06-20T10:00:00Z",
    "last_rssi": -85.0,
    "last_snr": 7.2,
    "last_f_port": 1,
    "uplink_count": 142,
    "available_fields": {
      "temperature": "23.4",
      "humidity": "61.5",
      "battery": "3.2"
    },
    "existing_tags": ["eui-0123456789abcdef/temperature"]
  }
]
```

I dispositivi vengono auto-scoperti dal driver LoRaWAN al primo uplink ricevuto.
`existing_tags` contiene i codici già importati come tag in OpenEdge.

### Import tag da dispositivi LoRaWAN (admin)

```
POST /api/gateways/{gw_id}/lorawan/devices/import
{
  "devices": [
    {
      "device_id": "eui-0123456789abcdef",
      "fields": [
        {"name": "temperature", "alias": "Temperatura Stanza", "data_type": "REAL", "historize": true},
        {"name": "humidity",    "alias": "Umidità Stanza",    "data_type": "REAL", "historize": true}
      ]
    }
  ]
}
```

Risposta:
```json
{"created": 2, "skipped": 0, "message": "2 tag(s) created, 0 already existed"}
```

I tag vengono creati con codice `{device_id}/{field_name}`.
Tipi supportati: `REAL`, `INT`, `BOOL`, `STRING`.

### Downlink LoRaWAN (admin)

```
POST /api/gateways/{gw_id}/lorawan/downlink
{
  "device_id": "eui-0123456789abcdef",
  "f_port": 2,
  "payload_hex": "0102FF",
  "confirmed": false
}
```

Il payload viene inoltrato via MQTT al driver LoRaWAN che lo trasmette al LNS (TTN v3 o ChirpStack v4).

---

## 16. Tag Shadows (Digital Twin)

Il shadow è l'ultimo valore noto di un tag — disponibile anche quando l'edge è offline.
Fonte: `"live"` = edge online; `"historic"` = edge offline, dato da DB.

### Shadow di un singolo tag

```
GET /api/tags/{tag_id}/shadow
```

Risposta:
```json
{
  "tag_id": 42,
  "value": 41.2,
  "quality": 192,
  "timestamp": "2026-06-20T09:58:00Z",
  "source": "live"
}
```

### Shadow batch (tutti i tag di un gateway)

```
GET /api/tags/shadows?gateway_id=2
```

Risposta:
```json
[
  {
    "tag_id": 42,
    "value": 41.2,
    "quality": 192,
    "timestamp": "2026-06-20T09:58:00Z",
    "source": "live"
  },
  {
    "tag_id": 43,
    "value": 1,
    "quality": 192,
    "timestamp": "2026-06-20T08:00:00Z",
    "source": "historic"
  }
]
```

---

## 17. Fleet Management (OTA)

### Stato fleet (global admin)

```
GET /api/fleet/status
```

Risposta:
```json
[
  {
    "org_id": 1,
    "org_name": "Acme SpA",
    "online": true,
    "last_ping": "2026-06-20T10:00:00Z",
    "version": "v2.0.0",
    "gateway_count": 5
  }
]
```

### Restart edge di un'org (admin)

```
POST /api/organizations/{org_id}/edge-restart
```

Pubblica `sys/restart/{org_id}` via MQTT → il driver-manager riavvia tutti i driver.

### OTA update edge (admin)

```
POST /api/organizations/{org_id}/edge-update
{"version": "v2.1.0", "image": "ghcr.io/inferis995/openedge-driver-manager:v2.1.0"}
```

Pubblica `sys/update/{org_id}` → il driver-manager fa `docker pull` + `docker restart`.

---

## 18. SSO / OIDC

### Login con Google

```
GET /api/auth/sso/google/login   → redirect a Google OAuth
GET /api/auth/sso/google/callback
```

### Login con Azure AD / Entra ID

```
GET /api/auth/sso/azure/login    → redirect a Microsoft
GET /api/auth/sso/azure/callback
```

Dopo callback: l'utente viene creato automaticamente se non esiste (`role=user`).
L'org viene assegnata per domain hint (configurabile da Global Admin).

### Configurazione provider SSO (global admin)

```
GET  /api/sso/providers
POST /api/sso/providers          # {"org_id":1,"provider":"google","client_id":"...","client_secret":"...","enabled":true}
PUT  /api/sso/providers/{id}
DELETE /api/sso/providers/{id}
```

---

## 19. RBAC Granulare

Permessi aggiuntivi rispetto ai ruoli base (admin/user):

| Flag | Significato |
|------|-------------|
| `can_write_tags` | Può scrivere valori sui tag |
| `can_ack_alarms` | Può acknowledger gli allarmi |
| `can_export_data` | Può esportare CSV storico |
| `can_manage_recipes` | Può gestire ricette |
| `can_manage_shifts` | Può gestire turni |
| `can_view_audit` | Può vedere l'audit log |
| `can_download_installer` | Può scaricare l'edge installer |

### Leggi permessi utente

```
GET /api/users/{user_id}/permissions
```

### Aggiorna permessi (admin)

```
PUT /api/users/{user_id}/permissions
{
  "can_write_tags": true,
  "can_ack_alarms": true,
  "can_export_data": false
}
```

---

## 19b. UDT — tipi definiti dall'utente

I tag sono piatti: gateway, indirizzo, alias, tipo di dato. Cinquanta motori
uguali significano cinquanta volte N tag inseriti a mano, e spostare una soglia
di allarme significa cinquanta modifiche — di cui una viene dimenticata, e la si
scopre da un allarme che non scatta.

Un **tipo** dichiara la forma una volta sola. Ogni **istanza** viene generata da
lui e **resta legata**: modifichi il tipo e tutte le istanze seguono.

### Il modello

- Un **membro** e' un tag futuro. Porta `address_suffix`, che viene **accodato**
  al `base_address` dell'istanza. E' cosi' che lo stesso tipo funziona su Modbus
  (base `40001`, suffisso `+2`) e su S7 (base `DB10`, suffisso `.DBX0.1`) senza
  che il tipo sappia su quale protocollo finira'.
- Scalatura, storicizzazione e allarmi si dichiarano **sul membro**, quindi
  «alta pressione a 8 bar» esiste una volta per ogni motore che mai esistera'.
- Il tag generato si chiama `{istanza}_{membro}`, cosi' un allarme dice da quale
  macchina arriva senza aprire nulla.

### Creare un tipo

```bash
curl -X POST "$BASE/api/udt/types" -H "$AUTH" -H "$ORG" -d '{
  "name": "Motore",
  "description": "Motore asincrono con inverter",
  "members": [
    {"name":"Run","address_suffix":"+0","data_type":"BOOL","historize":true},
    {"name":"Fault","address_suffix":"+1","data_type":"BOOL","historize":true,
     "alarms":[{"alarm_type":"high","threshold":1,"severity":"critical",
                "message":"guasto motore","enabled":true}]},
    {"name":"Speed","address_suffix":"+2","data_type":"REAL","historize":true,
     "scaling_enabled":true,"scaling_raw_min":0,"scaling_raw_max":27648,
     "scaling_eu_min":0,"scaling_eu_max":1500,"eu_unit":"rpm"}
  ]}'
```

I nomi dei membri accettano lettere, cifre, `-` e `_`. Non e' pignoleria: il nome
finisce nell'alias del tag e quindi in un topic MQTT, e un nome con spazi o
slash produce stringhe diverse ai due lati della slugificazione — che e'
esattamente il modo in cui un tag smette di essere storicizzato senza che
nessuno se ne accorga.

### Istanziare

```bash
curl -X POST "$BASE/api/udt/instances" -H "$AUTH" -H "$ORG" -d '{
  "type_id": 7, "gateway_id": 3, "name": "Pompa01", "base_address": "40001"}'
# -> {"id": 12, "tags_created": 3}
```

Genera `Pompa01_Run` a `40001+0`, `Pompa01_Fault` a `40001+1`,
`Pompa01_Speed` a `40001+2`, con la scalatura e l'allarme del tipo.

### Modificare un tipo: la propagazione

`PUT /api/udt/types/{id}` prende la lista **completa** dei membri — un tipo e'
una forma, e applicarla un membro alla volta farebbe passare le istanze per
stati che nessuno ha chiesto (un motore momentaneamente senza il bit di guasto e'
un motore il cui allarme momentaneamente non puo' scattare).

La risposta dice cosa ha toccato:

```json
{"status":"updated","reconciled":{"tags_created":3,"tags_updated":9,"tags_deleted":0}}
```

I membri sono confrontati **per nome**, non per id: e' il nome che un tecnico
usa quando riscrive la forma. Un tag esistente viene riscritto sul posto, non
sostituito, cosi' lo storico resta attaccato all'apparecchiatura.

### ATTENZIONE — rimuovere un membro cancella lo storico

`tag_history` ha `ON DELETE CASCADE` sul tag. Togliere un membro da un tipo
elimina quel tag su **ogni** istanza e con esso **ogni valore mai registrato**.

Per questo la chiamata viene **rifiutata** con `409` se non passi
`confirm_data_loss: true`, e il rifiuto dice quanto costa:

```json
{"error":"removing Speed would delete 50 tag(s) across the instances of this type,
 and with them every value ever recorded for those tags (1200000 recorded rows).
 Re-send with confirm_data_loss=true if that is intended.",
 "impact":{"members":["Speed"],"tags":50,"history_rows":1200000}}
```

**Come agente: non reinviare automaticamente con `confirm_data_loss`.** Riporta
il numero all'utente e fatti dire di procedere. Quel rifiuto e' l'unica cosa che
separa una modifica da un incidente.

### Eliminare

- `DELETE /api/udt/instances/{id}` — rimuove l'apparecchiatura e i suoi tag
  (storico compreso). Non chiede conferma: eliminare una macchina con nome e' un
  atto esplicito.
- `DELETE /api/udt/types/{id}` — rifiutato con `409` finche' esistono istanze.

### Permessi

Scrivere tipi e istanze richiede `can_write_tags` (gli admin passano sempre).
Chi puo' comandare un'uscita puo' gia' cambiare cosa sono i tag, e un permesso
separato suggerirebbe una separazione che non esiste. Le letture sono aperte a
chiunque sia autenticato nell'organizzazione.

---

## 20. OpenEdge CLI

Il CLI `openedge` è il modo più diretto per interagire con la piattaforma da terminale o da script.

### Installazione

```bash
# Build da sorgente
make build-cli          # → bin/openedge
make install-cli        # → /usr/local/bin/openedge (richiede sudo)

# Oppure scarica il binario pre-compilato
curl -sL https://github.com/inferis995/openedge/releases/latest/download/openedge-linux-amd64 \
  -o /usr/local/bin/openedge && chmod +x /usr/local/bin/openedge
```

### Login

```bash
openedge login --url https://app.yourdomain.com
# → richiede username e password, salva il token in ~/.openedge/config.json
```

### Comandi principali

```bash
# Organizzazioni
openedge orgs list
openedge orgs get 1
openedge orgs invite --email user@example.com --org 1

# Gateway
openedge gateways list [--org 1]
openedge gateways get 2
openedge gateways test 2           # testa la connessione
openedge gateways lorawan 2        # lista dispositivi LoRaWAN

# Tag
openedge tags list [--gateway 2]
openedge tags get 42
openedge tags write 43 --value 1
openedge tags history 42 --from 2026-06-19T00:00:00Z --to 2026-06-20T00:00:00Z
openedge tags shadows [--gateway 2]

# Allarmi
openedge alarms list [--active] [--severity critical]
openedge alarms ack 45

# Fleet
openedge fleet status
openedge fleet restart --org 1
openedge fleet update --org 1 --version v2.1.0

# AI-Ops
openedge aiops summary [--hours 24]
openedge aiops anomalies --tag 42
openedge aiops digest

# Health / diagnostics
openedge health
openedge diagnostics
```

### Flag globali

| Flag | Env var | Descrizione |
|------|---------|-------------|
| `--url` | `OPENEDGE_URL` | URL della piattaforma |
| `--token` | `OPENEDGE_TOKEN` | JWT token (override config file) |
| `--org` | `OPENEDGE_ORG_ID` | Organization ID |
| `--json` | — | Output JSON grezzo invece di tabella |

**Priority**: env vars → CLI flags → `~/.openedge/config.json`

---

## 21. MCP Server per AI Agent Integration

Il comando `openedge mcp` avvia un server MCP (Model Context Protocol) su stdio.
Permette a Claude Code, Cursor, e altri AI agent di controllare OpenEdge direttamente
come se fossero tool nativi.

### Configurazione in Claude Code

```json
// ~/.claude/settings.json
{
  "mcpServers": {
    "openedge": {
      "command": "openedge",
      "args": ["mcp"],
      "env": {
        "OPENEDGE_URL": "https://app.yourdomain.com",
        "OPENEDGE_TOKEN": "eyJ...",
        "OPENEDGE_ORG_ID": "1"
      }
    }
  }
}
```

Dopo la configurazione, Claude Code vede 39 tool nativi OpenEdge.

I primi 18 **leggono** un impianto. I restanti lo **costruiscono**: gerarchia,
gateway con il loro driver, tag, tipi definiti dall'utente e pagine sinottiche.
Un agente può quindi mettere in servizio una linea da zero — sito, area,
gateway Modbus, tipo «Motore», dieci istanze, pagina sinottica — senza toccare
l'interfaccia.

| Tool MCP | Equivalente REST |
|----------|-----------------|
| `list_organizations` | GET /api/organizations |
| `list_gateways` | GET /api/gateways |
| `list_tags` | GET /api/tags |
| `get_tag_value` | GET /api/tags/{id}/current |
| `write_tag_value` | PUT /api/i3x/v1/properties/tag-{id}/value |
| `get_tag_history` | GET /api/history/stats |
| `get_tag_shadows` | GET /api/tags/shadows |
| `list_active_alarms` | GET /api/alarms/active |
| `acknowledge_alarm` | POST /api/alarms/{id}/ack |
| `get_fleet_status` | GET /api/fleet/status |
| `fleet_restart` | POST /api/organizations/{id}/edge-restart |
| `list_lorawan_devices` | GET /api/gateways/{id}/lorawan/devices |
| `import_lorawan_tags` | POST /api/gateways/{id}/lorawan/devices/import |
| `send_lorawan_downlink` | POST /api/gateways/{id}/lorawan/downlink |
| `get_aiops_summary` | GET /api/aiops/summary |
| `detect_anomalies` | GET /api/aiops/anomalies |
| `get_alarm_digest` | GET /api/aiops/alarms/digest |
| `check_health` | GET /health + /ready |

**Provisioning** — costruire l'impianto:

| Tool MCP | Equivalente REST |
|----------|-----------------|
| `list_sites` / `create_site` | GET / POST /api/sites |
| `list_areas` / `create_area` | GET / POST /api/areas |
| `create_gateway` | POST /api/gateways |
| `create_tag` | POST /api/tags |
| `delete_tag` | DELETE /api/tags/{id} |
| `set_tag_alarms` | PUT /api/tags/{id}/alarms |

**UDT** — tipi definiti dall'utente:

| Tool MCP | Equivalente REST |
|----------|-----------------|
| `list_udt_types` / `get_udt_type` | GET /api/udt/types[/{id}] |
| `create_udt_type` | POST /api/udt/types |
| `update_udt_type` | PUT /api/udt/types/{id} |
| `delete_udt_type` | DELETE /api/udt/types/{id} |
| `list_udt_instances` | GET /api/udt/instances |
| `create_udt_instance` | POST /api/udt/instances |
| `delete_udt_instance` | DELETE /api/udt/instances/{id} |

**Sinottici** — pagine SCADA:

| Tool MCP | Equivalente REST |
|----------|-----------------|
| `list_synoptics` / `get_synoptic` | GET /api/synoptics[/{id}] |
| `create_synoptic` | POST /api/synoptics |
| `update_synoptic` | PUT /api/synoptics/{id} |
| `delete_synoptic` | DELETE /api/synoptics/{id} |

### Protocollo

- JSON-RPC 2.0 su stdio con framing `Content-Length`
- Supporta anche bare newline-delimited JSON (per debug/test)
- Log su stderr, risposte su stdout
- Protocol version: `2024-11-05`

### Test manuale del server MCP

```bash
OPENEDGE_URL=http://localhost:8081 OPENEDGE_TOKEN=eyJ... openedge mcp &
echo '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}' | openedge mcp
```

---

## Prompt di esempio per l'agente

```
"Leggi il valore corrente di tutti i tag del gateway PLC-Serbatoio1 nell'org 3"
"Ci sono allarmi Critical attivi nell'organizzazione 2?"
"Rileva anomalie sul tag Pressione_Rete (id=42) nell'ultima settimana"
"Genera un digest degli allarmi delle ultime 24 ore"
"L'edge è online per l'org 5?"
"Calcola l'OEE dell'ultimo turno per il profilo Linea-A"
"I webhook dell'org 3 stanno consegnando? Qual è l'ultimo status code?"
"Mostrami gli ultimi 10 eventi di audit per l'utente mario"
"Scrivi valore 1 sul tag Pompa_On (tag-43) del gateway PLC-Serbatoio1"
"Lista i dispositivi LoRaWAN del gateway 4 e importa la temperatura come tag"
"Invia un downlink al dispositivo eui-abc fport=2 payload=0102 sul gateway 4"
"Leggi il tag shadow del tag 42 — è live o historic?"
"Qual è lo stato della fleet? Ci sono edge offline?"
"Fai restart dell'edge dell'org 1"
"Come aggiungo openedge mcp a Claude Code?"

# Costruire un impianto da zero
"Crea il sito Stabilimento-Nord, l'area Linea-1 e un gateway Modbus TCP su
 192.168.1.50 porta 502 con scansione ogni 500 ms"
"Definisci un tipo Motore con Run, Fault e Speed 0-1500 rpm scalata da 0-27648,
 con allarme critico sul Fault"
"Istanzia il tipo Motore dieci volte sul gateway 3, da Pompa01 a Pompa10"
"Aggiungi il membro Ore al tipo Motore e dimmi quanti tag hai creato"
"Crea una pagina sinottica Linea-1 con un indicatore per il Run di ogni pompa"
```
