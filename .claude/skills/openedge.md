---
name: openedge
description: OpenEdge Industrial IoT Middleware — REST API + i3X Access API skill for AI agents
version: 2.0.0
tags: [industrial, iot, alarms, historian, timeseries, scada, i3x, cesmii]
---

# OpenEdge API — Skill

Questo documento descrive come un agente AI deve interagire con un'istanza OpenEdge.
Leggi tutto prima di fare qualsiasi chiamata API.

---

## Variabili d'ambiente attese

```bash
OPENEDGE_HOST=localhost
OPENEDGE_PORT=8081
OPENEDGE_USERNAME=admin
OPENEDGE_PASSWORD=admin123
OPENEDGE_ORG_ID=1
```

---

## 1. Autenticazione

Tutti gli endpoint (eccetto `/health` e `/ready`) richiedono un JWT Bearer token
e l'header `X-Organization-ID`.

### Login

```
POST http://{OPENEDGE_HOST}:{OPENEDGE_PORT}/api/auth/login
Content-Type: application/json

{"username": "{OPENEDGE_USERNAME}", "password": "{OPENEDGE_PASSWORD}"}
```

Risposta:
```json
{"token": "eyJ...", "user": {"id": 1, "role": "admin"}}
```

Il token scade dopo **24 ore**. Rinnova prima di ogni sessione di lavoro.

### Script shell per ottenere TOKEN

```bash
TOKEN=$(curl -s -X POST http://{OPENEDGE_HOST}:{OPENEDGE_PORT}/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"{OPENEDGE_USERNAME}","password":"{OPENEDGE_PASSWORD}"}' \
  | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])")
```

### Header obbligatori per ogni chiamata

```
Authorization: Bearer {TOKEN}
X-Organization-ID: {OPENEDGE_ORG_ID}
```

---

## 2. Health Check (pubblico, senza auth)

```
GET http://{OPENEDGE_HOST}:{OPENEDGE_PORT}/health
GET http://{OPENEDGE_HOST}:{OPENEDGE_PORT}/ready
```

- `/health` → `{"status":"ok"}` — server risponde
- `/ready` → `{"status":"ready","db":"ok","redis":"ok"}` — tutto up

**Usa `/ready` per verificare che OpenEdge sia operativo prima di fare query.**

---

## 3. i3X Access API — Protocollo CESMII Standard

Il prefisso è `/api/i3x/v1/`. Tutti gli endpoint richiedono auth + org.

Questa è l'API **vendor-neutral** compatibile con la specifica CESMII i3X v1.
Usa per integrazioni con sistemi esterni, SCADA, o agenti che devono leggere/scrivere
dati in formato standard senza conoscere la struttura interna di OpenEdge.

### 3.1 ID Format

Tutti gli ID nell'i3X API sono stringhe con prefisso:

| Tipo | Formato | Esempio |
|------|---------|---------|
| Organizzazione | `org-{n}` | `org-1` |
| Sito | `site-{n}` | `site-3` |
| Area | `area-{n}` | `area-7` |
| Gateway / Equipment | `gw-{n}` | `gw-2` |
| Tag / Property | `tag-{n}` | `tag-42` |

### 3.2 Quality Codes (OPC-UA — diversi dall'API standard!)

| Valore | Significato |
|--------|-------------|
| `192` | Good — dato affidabile |
| `64` | Uncertain |
| `0` | Bad — problema comunicazione o dato assente |

> ⚠️ L'API standard REST usa `0=Good, 1=Bad`. L'i3X usa codici OPC-UA: `192=Good, 0=Bad`. Non confonderli.

### 3.3 Data Types

| i3X | Tipi interni corrispondenti |
|-----|-----------------------------|
| `Boolean` | `BOOL` |
| `Int32` | `INT`, `DINT` |
| `Float` | `REAL` |
| `String` | `STRING`, `WORD`, altri |

---

### GET /api/i3x/v1/equipment — Lista equipment

Restituisce la gerarchia completa: org → sito → area → gateway.

```bash
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 1" \
  http://localhost:8081/api/i3x/v1/equipment
```

Risposta:
```json
{
  "items": [
    {
      "id": "org-1",
      "name": "MyOrganization",
      "type": "Assembly",
      "parentId": null,
      "path": "MyOrganization"
    },
    {
      "id": "site-3",
      "name": "Sito-Crotone",
      "type": "Assembly",
      "parentId": "org-1",
      "path": "MyOrganization/Sito-Crotone"
    },
    {
      "id": "gw-2",
      "name": "PLC-Serbatoio1",
      "type": "Equipment",
      "parentId": "area-7",
      "path": "MyOrganization/Sito-Crotone/Zona-A/PLC-Serbatoio1",
      "attributes": {
        "driver_type": "MODBUS_TCP",
        "connection_status": "online",
        "enabled": true
      }
    }
  ],
  "total": 5
}
```

Tipi: `Assembly` = nodo logico (org/site/area), `Equipment` = dispositivo fisico (gateway).

---

### GET /api/i3x/v1/equipment/:id — Dettaglio equipment

```bash
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 1" \
  http://localhost:8081/api/i3x/v1/equipment/gw-2
```

---

### GET /api/i3x/v1/equipment/:id/properties — Tag di un equipment

Lista tutti i tag (properties) di un gateway, con valore corrente da Redis.

```bash
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 1" \
  http://localhost:8081/api/i3x/v1/equipment/gw-2/properties
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
        "timestamp": "2026-04-27T10:30:00Z"
      }
    },
    {
      "id": "tag-43",
      "name": "Pompa_On",
      "equipmentId": "gw-2",
      "dataType": "Boolean",
      "historize": true,
      "current": {
        "value": true,
        "quality": 192,
        "timestamp": "2026-04-27T10:30:01Z"
      }
    }
  ],
  "total": 2
}
```

Se `current` è assente → il tag non ha mai ricevuto un valore (gateway mai connesso).

---

### GET /api/i3x/v1/equipment/:id/properties/:propId — Singola property

```bash
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 1" \
  http://localhost:8081/api/i3x/v1/equipment/gw-2/properties/tag-42
```

---

### GET /api/i3x/v1/properties — Tutti i tag dell'organizzazione

```bash
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 1" \
  http://localhost:8081/api/i3x/v1/properties
```

---

### GET /api/i3x/v1/properties/:id — Singola property con valore corrente

```bash
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 1" \
  http://localhost:8081/api/i3x/v1/properties/tag-42
```

---

### PUT /api/i3x/v1/properties/:id/value — Scrivi valore su un tag

Richiede permesso `i3x_write` nel JWT oppure ruolo `admin`.

```bash
curl -X PUT \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Organization-ID: 1" \
  -H "Content-Type: application/json" \
  -d '{"value": 1}' \
  http://localhost:8081/api/i3x/v1/properties/tag-43/value
```

Risposta: `{"message": "Write command sent"}`

Il comando è inviato al driver via MQTT (`cmd/write/{gateway_id}`). La risposta
arriva in modo asincrono: il valore aggiornato sarà visibile nel campo `current`
alla prossima lettura dopo che il driver ha eseguito la scrittura sul PLC.

Errori possibili:
- `403 FORBIDDEN` — utente senza `i3x_write` o accesso a org diversa
- `503 MQTT_UNAVAILABLE` — broker MQTT non connesso

---

### GET /api/i3x/v1/alarms — Allarmi attivi in formato i3X

```bash
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 1" \
  http://localhost:8081/api/i3x/v1/alarms
```

Risposta:
```json
{
  "items": [
    {
      "id": "alarm-45",
      "propertyId": "tag-42",
      "propertyName": "Portata_Ingresso",
      "equipmentId": "gw-2",
      "equipmentName": "PLC-Serbatoio1",
      "severity": "Critical",
      "status": "Active",
      "alarmType": "high",
      "message": "Portata massima superata",
      "value": 98.7,
      "triggerTime": "2026-04-27T08:15:00Z",
      "clearTime": null
    }
  ],
  "total": 1
}
```

Status values: `Active`, `Acknowledged`, `Cleared`
Severity values: `Info`, `Warning`, `Critical`

---

### GET /api/i3x/v1/alarms/history — Storico allarmi

```bash
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 1" \
  http://localhost:8081/api/i3x/v1/alarms/history
```

Risposta analoga a `/alarms` ma include anche allarmi con status `Cleared`.

---

## 4. AI-Ops API — Endpoint ottimizzati per agenti

Questi endpoint restituiscono tutto in una sola chiamata, senza N query separate.

---

### GET /api/aiops/summary?hours=24 — Snapshot organizzazione

**Quando usarlo:** all'inizio di ogni ciclo di analisi.

```bash
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 1" \
  "http://localhost:8081/api/aiops/summary?hours=24"
```

Risposta:
```json
{
  "org_id": 1,
  "org_name": "MyOrganization",
  "period_hours": 24,
  "generated_at": "2026-04-27T07:00:00Z",
  "active_alarms_count": 3,
  "critical_alarms_count": 1,
  "total_gateways_count": 12,
  "tags": [
    {
      "tag_id": 5,
      "alias": "Portata_Ingresso",
      "data_type": "REAL",
      "gateway_name": "PLC-Serbatoio1",
      "site_name": "Sito-Crotone",
      "avg_value": 42.5,
      "min_value": 38.1,
      "max_value": 47.2,
      "sample_count": 2880,
      "has_alarm": true,
      "alarm_count": 1
    }
  ]
}
```

Note:
- `avg/min/max_value` null → gateway offline nel periodo, nessun dato
- `sample_count = 0` → problema connettività
- `has_alarm = true` → almeno un allarme nel periodo

---

### GET /api/aiops/anomalies — Rilevamento anomalie Z-score

```bash
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 1" \
  "http://localhost:8081/api/aiops/anomalies?tag_id=5&window_hours=168&baseline_days=30"
```

Parametri:
- `tag_id` (required)
- `window_hours` (default: 168 = 1 settimana, max: 720)
- `baseline_days` (default: 30, min: 7, max: 365)

Risposta:
```json
{
  "tag_id": 5,
  "tag_alias": "Portata_Ingresso",
  "baseline_mean": 42.8,
  "baseline_std_dev": 1.4,
  "anomaly_count": 3,
  "anomalies": [
    {
      "bucket": "2026-04-26T14:00:00Z",
      "value": 51.2,
      "z_score": 5.98,
      "direction": "high"
    }
  ]
}
```

Soglia: `|z_score| >= 2.5`. Direction: `high` = sopra la norma, `low` = sotto.

---

### GET /api/aiops/alarms/digest?hours=24 — Digest allarmi per report

```bash
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 1" \
  "http://localhost:8081/api/aiops/alarms/digest?hours=24"
```

Risposta:
```json
{
  "period_hours": 24,
  "total_fired": 8,
  "still_active": 2,
  "cleared": 6,
  "by_severity": {"info": 2, "warning": 4, "critical": 2},
  "alarms": [
    {
      "severity": "critical",
      "tag_id": 12,
      "tag_alias": "Pressione_Rete",
      "alarm_type": "high",
      "message": "Pressione massima superata",
      "value_at_trigger": 8.7,
      "trigger_time": "2026-04-26T22:15:00Z",
      "status": "CLEARED",
      "clear_time": "2026-04-26T23:05:00Z"
    }
  ]
}
```

---

## 5. Endpoint Standard

### Allarmi attivi

```
GET /api/alarms/active
```

### Stato gateway (connettività PLC)

```
GET /api/gateways
```

`connection_status`: `"online"` | `"offline"` | `"unknown"`

### Valore corrente tag (da Redis)

```
GET /api/tags/{tag_id}/current
```

Risposta: `{"tag_id":5,"alias":"Portata_Ingresso","value":42.5,"timestamp":1711234567000,"quality":0}`

Quality REST: **0 = Good**, **1 = Bad** (opposto i3X!)

### Statistiche storiche

```
GET /api/history/stats?tag_id=5&start=1711148167000&end=1711234567000
```

### Tag con gerarchia completa

```
GET /api/tags/with-hierarchy
```

---

## 6. Regole critiche per interpretare i dati

### Quality codes — NON confondere i due contesti

| API | Good | Bad |
|-----|------|-----|
| REST standard | `0` | `1` |
| i3X Access API | `192` | `0` |

### Timestamp

- REST standard: millisecondi Unix (`1711234567000`)
- i3X API e AI-Ops: ISO 8601 (`2026-04-27T10:30:00Z`)

### Valori BOOL

Salvati come float: `1.0` = ON, `0.0` = OFF.
In i3X la property `Boolean` restituisce già `true`/`false`.

### Gap nel trend = gateway offline, NON errore

`value = NULL` in tag_history = gateway offline in quel momento.
Il sistema inserisce marker `source='offline'` per queste finestre.

### Source field in tag_history

| source | Significato |
|--------|-------------|
| `mqtt` | Dato normale dal driver |
| `sparkplug_b` | Dato dal protocollo Sparkplug B |
| `offline` | Marker: gateway disconnesso |
| `seed` | Valore iniettato via REST per continuità trend |

---

## 7. Quando usare i3X vs API standard

| Scenario | API da usare |
|----------|-------------|
| Integrazione con sistema esterno (SCADA, MES, cloud) | **i3X** |
| Lettura valore corrente di un tag specifico | i3X `GET /properties/{id}` o REST `GET /tags/{id}/current` |
| Scrittura valore su PLC | **i3X** `PUT /properties/{id}/value` |
| Lista tag con gerarchia | **i3X** `GET /equipment/{id}/properties` |
| Analisi anomalie / report | **AI-Ops** |
| Alarm monitor real-time | AI-Ops o i3X `/alarms` |
| Dashboard interna OpenEdge | REST standard |

---

## 8. Pattern operativo per agenti

### Alarm Monitor (ogni 5 min)

```
1. GET /api/i3x/v1/alarms
2. Se severity=Critical AND status=Active → notifica urgente
3. Se solo Warning → aggiungi nota a ticket giornaliero
4. Lista vuota → nessuna azione
```

### Gateway Health Monitor (ogni 15 min)

```
1. GET /api/gateways
2. Per ogni gateway: controlla connection_status
3. Se offline E last_seen > 30 min → apri ticket
4. Se torna online → chiudi ticket + notifica recovery
```

### Daily Report (ogni giorno alle 07:00)

```
1. GET /api/aiops/alarms/digest?hours=24
2. GET /api/aiops/summary?hours=24
3. Genera sezione allarmi dal digest
4. Tabella tag critici (has_alarm=true o sample_count=0)
5. Invia report
```

### Anomaly Detector (ogni ora)

```
1. GET /api/aiops/summary?hours=1
2. Per tag con has_alarm=false E sample_count > 0:
   GET /api/aiops/anomalies?tag_id={id}&window_hours=24
3. Se anomaly_count > 0 → analisi + ticket
```

### Lettura valore e scrittura via i3X

```
# Leggi valore corrente
GET /api/i3x/v1/properties/tag-42

# Scrivi valore (richiede i3x_write o admin)
PUT /api/i3x/v1/properties/tag-43/value
{"value": 1}
```

---

## 9. Gestione errori

| HTTP | Significato | Azione |
|------|-------------|--------|
| 200 | OK | Procedi |
| 400 | Parametro non valido | Correggi la richiesta |
| 401 | Token scaduto o mancante | Rinnova con POST /api/auth/login |
| 403 | Permessi insufficienti | Verifica ruolo utente o claim i3x_write |
| 404 | Risorsa non trovata | Verifica ID e org_id |
| 503 | MQTT non connesso | Solo per PUT write — broker offline |
| 500 | Errore server | Logga e riprova dopo 30 secondi |

Tutte le risposte di errore:
```json
{"error": "descrizione"}
```

Errori i3X (formato esteso):
```json
{"code": "FORBIDDEN", "message": "i3X write permission required"}
```

---

*Aggiornare questo file quando vengono aggiunti nuovi endpoint a OpenEdge.*
