---
name: openedge
description: OpenEdge Industrial IoT — monitor allarmi, leggi dati real-time, storico, anomalie, OEE via REST + i3X. Multi-tenant SaaS: ogni org è isolata. Security Center, infrastruttura, fleet status, health endpoints, OTA update check, edge heartbeat. Usa openedge-ops per deploy/configurazione.
version: 4.1.0
tags: [industrial, iot, alarms, historian, timeseries, scada, i3x, cesmii, monitoring, multi-tenant, saas, oee, webhooks, security, nis2, infrastructure, health, ota, fleet]
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
OPENEDGE_PASSWORD=admin123         # cambia al primo login
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
```
